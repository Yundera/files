// Package fsop holds the filesystem primitives shared by internal/trash and
// internal/jobs: conflict resolution, tree copying, and the move that survives a
// filesystem boundary.
package fsop

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/yundera/files/internal/ownership"
	"github.com/yundera/files/internal/vfs"
)

// Policy decides what happens when a destination already exists.
// The three values match CasaOS's paste menu.
type Policy string

const (
	// Skip leaves the existing file and reports the item as skipped.
	Skip Policy = "skip"
	// Overwrite replaces the destination, preserving ITS ownership and mode
	// (see internal/ownership — an overwrite is not a create).
	Overwrite Policy = "overwrite"
	// KeepBoth renames the incoming item to "name (2).ext".
	KeepBoth Policy = "keepBoth"
)

// ErrSkipped reports that an item was deliberately not written.
var ErrSkipped = errors.New("skipped")

// Resolve applies the policy to a destination that may already exist, returning
// the path actually to write. It works on root-relative paths.
func Resolve(root *os.Root, destRel string, policy Policy) (string, error) {
	if _, err := root.Lstat(destRel); errors.Is(err, fs.ErrNotExist) {
		return destRel, nil
	} else if err != nil {
		return "", err
	}
	switch policy {
	case Skip:
		return "", ErrSkipped
	case Overwrite:
		return destRel, nil
	case KeepBoth:
		return uniqueName(root, destRel)
	default:
		// An unknown policy must not silently overwrite. Refusing is the only
		// safe default when the caller's intent is unreadable.
		return "", fmt.Errorf("destination exists and no conflict policy was given")
	}
}

// uniqueName finds "name (2).ext", "name (3).ext" and so on.
//
// The suffix goes before the extension, not after, so "report (2).pdf" still
// opens as a PDF. Appending after would produce "report.pdf (2)", which most
// systems treat as extensionless.
func uniqueName(root *os.Root, destRel string) (string, error) {
	dir, base := path.Split(destRel)
	stem, ext := base, ""
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		stem, ext = base[:i], base[i:]
	}
	for n := 2; n < 1000; n++ {
		cand := dir + stem + " (" + strconv.Itoa(n) + ")" + ext
		if _, err := root.Lstat(cand); errors.Is(err, fs.ErrNotExist) {
			return cand, nil
		}
	}
	return "", fmt.Errorf("could not find a free name for %q", base)
}

// Move renames srcRel to destRel, falling back to copy-then-delete when the two
// are on different filesystems.
//
// On today's PCS boxes the fallback never runs: /DATA is a plain directory on
// the root volume, not a mountpoint (verified on holyhorse and wisera). That is
// an observation about the current layout, not a guarantee — a future PCS with
// /DATA on its own volume would otherwise make every delete-to-trash fail with
// EXDEV. One helper, used by trash, move and the upload commit, so the fallback
// exists in exactly one place.
func Move(root *os.Root, srcRel, destRel string, o ownership.Owner) error {
	err := root.Rename(srcRel, destRel)
	if err == nil || !vfs.IsCrossDevice(err) {
		return err
	}
	if err := CopyTree(root, srcRel, destRel, o, nil); err != nil {
		return err
	}
	return root.RemoveAll(srcRel)
}

// Progress is called after each item with the bytes copied so far. It may be
// nil. Returning a non-nil error cancels the copy — this is how job
// cancellation reaches the inner loop.
type Progress func(done int64, currentPath string) error

// CopyTree copies a file or a whole directory.
//
// Everything it creates is stamped with the configured owner and mode: a copy is
// a new inode, so it belongs to PUID:PGID regardless of what the source was
// owned by. (A MOVE, by contrast, keeps the original ownership — it is the same
// inode.)
func CopyTree(root *os.Root, srcRel, destRel string, o ownership.Owner, prog Progress) error {
	var done int64
	return copyTree(root, srcRel, destRel, o, prog, &done)
}

func copyTree(root *os.Root, srcRel, destRel string, o ownership.Owner, prog Progress, done *int64) error {
	fi, err := root.Lstat(srcRel)
	if err != nil {
		return err
	}

	switch {
	case fi.IsDir():
		if err := root.Mkdir(destRel, o.DirMode); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		if d, err := root.Open(destRel); err == nil {
			o.ApplyDir(d)
			_ = d.Close()
		}
		dir, err := root.Open(srcRel)
		if err != nil {
			return err
		}
		names, err := dir.Readdirnames(-1)
		_ = dir.Close()
		if err != nil {
			return err
		}
		for _, name := range names {
			if err := copyTree(root, path.Join(srcRel, name), path.Join(destRel, name), o, prog, done); err != nil {
				return err
			}
		}
		return nil

	case fi.Mode()&os.ModeSymlink != 0:
		// A symlink is copied as a link, not as its contents. Following it would
		// silently turn one link into a full second copy of a large tree, and
		// could duplicate data from outside the selection.
		target, err := root.Readlink(srcRel)
		if err != nil {
			return err
		}
		return root.Symlink(target, destRel)

	default:
		return copyFile(root, srcRel, destRel, fi, o, prog, done)
	}
}

func copyFile(root *os.Root, srcRel, destRel string, fi os.FileInfo, o ownership.Owner, prog Progress, done *int64) error {
	src, err := root.Open(srcRel)
	if err != nil {
		return err
	}
	defer src.Close()

	// Capture the destination's ownership BEFORE truncating it: an overwrite
	// must not re-home an existing file to PUID.
	var saved ownership.Preserved
	if dfi, err := root.Lstat(destRel); err == nil {
		saved = ownership.Capture(dfi)
	}

	dst, err := root.OpenFile(destRel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, o.FileMode)
	if err != nil {
		return err
	}
	defer dst.Close()

	// Copy in chunks rather than io.Copy so progress and cancellation get a say
	// between chunks. 1 MiB is large enough that the syscall overhead is
	// irrelevant and small enough that a cancel is felt immediately.
	buf := make([]byte, 1<<20)
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
			*done += int64(n)
			if prog != nil {
				if err := prog(*done, srcRel); err != nil {
					return err
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}

	if !saved.Restore(dst) {
		o.ApplyFile(dst)
	}
	_ = fi
	return nil
}

// TreeSize walks a path and returns its total apparent size and file count.
//
// Used before a delete so the trash can report how much space it is holding. It
// makes deleting a huge directory O(n) where the rename itself is O(1) — a walk
// is still far cheaper than a copy, and without it the Trash view cannot tell
// the user where their disk went.
func TreeSize(root *os.Root, rel string) (size int64, count int) {
	fi, err := root.Lstat(rel)
	if err != nil {
		return 0, 0
	}
	if !fi.IsDir() {
		return fi.Size(), 1
	}
	dir, err := root.Open(rel)
	if err != nil {
		return 0, 0
	}
	names, err := dir.Readdirnames(-1)
	_ = dir.Close()
	if err != nil {
		return 0, 0
	}
	for _, name := range names {
		s, c := TreeSize(root, path.Join(rel, name))
		size += s
		count += c
	}
	return size, count + 1
}
