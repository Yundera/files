// Package vfs is the filesystem boundary.
//
// THIS IS THE ONLY PACKAGE IN THE REPO THAT MAY CALL os.* ON A USER-SUPPLIED
// PATH. Every handler takes a *virtual path* — a slash-separated, absolute-
// looking string like "/Documents/notes/todo.md" — and hands it here. Nothing
// else ever joins a user string onto the data root.
//
// The confinement is built on os.Root (Go 1.24+), which resolves every path
// component inside an opened directory and refuses to leave it. That is what
// makes "../../etc", an absolute path, a symlink pointing outside the root, and
// a symlink RACED INTO PLACE between the check and the open all fail at the
// syscall layer rather than at a string test we have to get right. The race is
// the reason for os.Root over filepath.Clean plus a prefix check: the latter is
// what packages/maison does, and it is correct there only because the paths it
// checks are operator-supplied, not attacker-supplied.
//
// Note what os.Root does NOT do, per its own documentation: it does not stop
// traversal across filesystem boundaries, bind mounts, /proc, or device files
// that happen to live under the root. "Confined to DATA_ROOT" here means
// confined by path, not confined by device.
package vfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/yundera/files/internal/config"
)

var (
	// ErrInvalidPath is returned for anything that is not a well-formed virtual
	// path: a traversal, a NUL byte, invalid UTF-8.
	ErrInvalidPath = errors.New("invalid path")

	// ErrHidden is returned for a path inside the app's own state directory.
	// Kept distinct from ErrInvalidPath so a handler can answer 404 rather than
	// 400 — the state dir should look absent, not rejected.
	ErrHidden = errors.New("path is not available")
)

// FS is a confined view of one directory tree.
type FS struct {
	root *os.Root

	// stateRel is the state directory expressed relative to the data root, e.g.
	// "AppData/files".
	stateRel string
}

// New opens the data root. It fails if the root is missing or is not a
// directory — unlike the rest of the server, which tolerates a broken config,
// because every operation in this package is meaningless without it.
func New(cfg config.Config) (*FS, error) {
	if !cfg.StateDirInsideRoot() {
		return nil, fmt.Errorf("state dir %q must be a subdirectory of data root %q",
			cfg.StateDir(), cfg.DataRoot)
	}
	root, err := os.OpenRoot(cfg.DataRoot)
	if err != nil {
		return nil, fmt.Errorf("open data root: %w", err)
	}
	rel, err := filepath.Rel(filepath.Clean(cfg.DataRoot), filepath.Clean(cfg.StateDir()))
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("locate state dir: %w", err)
	}
	return &FS{root: root, stateRel: filepath.ToSlash(rel)}, nil
}

// Close releases the root descriptor.
func (f *FS) Close() error { return f.root.Close() }

// Clean validates a virtual path and returns it as a root-relative path suitable
// for the os.Root methods. The root itself is ".".
//
// os.Root would reject a traversal on its own; this exists so the rejection is a
// clear 400 with a stable error rather than a raw syscall error, and so every
// caller gets the same normalisation.
func (f *FS) Clean(virtual string) (string, error) {
	if strings.ContainsRune(virtual, 0) {
		return "", fmt.Errorf("%w: contains a NUL byte", ErrInvalidPath)
	}
	if !utf8.ValidString(virtual) {
		return "", fmt.Errorf("%w: not valid UTF-8", ErrInvalidPath)
	}
	// Backslash is refused rather than guessed at. On Linux it is a legal
	// filename character, so silently treating it as a separator would make
	// "a\b" ambiguous between one file and two.
	if strings.ContainsRune(virtual, '\\') {
		return "", fmt.Errorf("%w: contains a backslash", ErrInvalidPath)
	}

	// A ".." segment is REJECTED, not resolved.
	//
	// path.Clean on a rooted path would absorb it — Clean("/../etc/passwd") is
	// "/etc/passwd" — which is safe, in that the result still cannot leave the
	// root. It is nonetheless the wrong behaviour here: it answers a traversal
	// attempt with a 200 for some other file instead of a 400, so an attacker
	// probing the boundary gets useful content and the attempt leaves no
	// distinguishable trace in the logs. No legitimate client sends "..": the UI
	// builds every path from the breadcrumb it was given.
	//
	// "." segments are harmless and are collapsed by the Clean below.
	for _, seg := range strings.Split(virtual, "/") {
		if seg == ".." {
			return "", fmt.Errorf(`%w: contains a ".." segment`, ErrInvalidPath)
		}
	}

	p := path.Clean("/" + strings.TrimPrefix(virtual, "/"))
	rel := strings.TrimPrefix(p, "/")
	if rel == "" {
		rel = "."
	}
	// Belt and braces: after the segment scan and the Clean, a leading ".."
	// cannot survive. This is the assertion that catches a future change to the
	// normalisation above, and it costs nothing.
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("%w: escapes the data root", ErrInvalidPath)
	}
	if f.hidden(rel) {
		return "", fmt.Errorf("%w: %s", ErrHidden, virtual)
	}
	return rel, nil
}

// hidden reports whether rel is the state directory or lives inside it.
//
// Filtering at this chokepoint rather than in each handler is deliberate: there
// are too many handlers to remember, and the consequences of a leak are
// concrete — a user can browse into their own trash, delete a trashed item into
// the trash a second time, or drop a file into the tus staging area that the GC
// then eats.
func (f *FS) hidden(rel string) bool {
	if f.stateRel == "" || f.stateRel == "." {
		return false
	}
	return rel == f.stateRel || strings.HasPrefix(rel, f.stateRel+"/")
}

// Hidden reports whether a root-relative path addresses the app's own state
// directory. Exported for the listing code, which must skip such an entry
// rather than fail on it.
func (f *FS) Hidden(rel string) bool { return f.hidden(rel) }

// Root exposes the underlying os.Root.
//
// It BYPASSES the state-dir exclusion, so a caller holding it can reach the
// trash and the upload staging area. Two kinds of caller may use it:
//
//   - Read-side packages (internal/listing, internal/thumb) that have already
//     resolved their path through Clean and just need the handle.
//   - internal/trash and internal/upload, which operate INSIDE the state dir by
//     definition — Clean would refuse every one of their paths. They build
//     their paths from StateRel and must never accept one from a request.
//
// Anything else wanting raw access is a bug: route it through Clean.
func (f *FS) Root() *os.Root { return f.root }

// StateRel is the state directory as a root-relative path, e.g.
// "AppData/files". The trash and upload packages build their paths from it.
func (f *FS) StateRel() string { return f.stateRel }

// Open opens a file for reading.
func (f *FS) Open(virtual string) (*os.File, error) {
	rel, err := f.Clean(virtual)
	if err != nil {
		return nil, err
	}
	return f.root.Open(rel)
}

// OpenFile opens a file with explicit flags and permissions.
func (f *FS) OpenFile(virtual string, flag int, perm os.FileMode) (*os.File, error) {
	rel, err := f.Clean(virtual)
	if err != nil {
		return nil, err
	}
	return f.root.OpenFile(rel, flag, perm)
}

// Stat follows symlinks.
func (f *FS) Stat(virtual string) (fs.FileInfo, error) {
	rel, err := f.Clean(virtual)
	if err != nil {
		return nil, err
	}
	return f.root.Stat(rel)
}

// Lstat reports on the link itself, which is what a listing wants: a symlink
// should be shown as a symlink rather than silently rendered as its target.
func (f *FS) Lstat(virtual string) (fs.FileInfo, error) {
	rel, err := f.Clean(virtual)
	if err != nil {
		return nil, err
	}
	return f.root.Lstat(rel)
}

// Mkdir creates one directory.
func (f *FS) Mkdir(virtual string, perm os.FileMode) error {
	rel, err := f.Clean(virtual)
	if err != nil {
		return err
	}
	return f.root.Mkdir(rel, perm)
}

// MkdirAll creates a directory and any missing parents.
func (f *FS) MkdirAll(virtual string, perm os.FileMode) error {
	rel, err := f.Clean(virtual)
	if err != nil {
		return err
	}
	return f.root.MkdirAll(rel, perm)
}

// Remove removes one file or empty directory.
func (f *FS) Remove(virtual string) error {
	rel, err := f.Clean(virtual)
	if err != nil {
		return err
	}
	return f.root.Remove(rel)
}

// RemoveAll removes a tree. Used by the trash purge; user-facing deletes go to
// the trash instead.
func (f *FS) RemoveAll(virtual string) error {
	rel, err := f.Clean(virtual)
	if err != nil {
		return err
	}
	return f.root.RemoveAll(rel)
}

// Rename moves within the root. Both ends are validated.
//
// This is the primitive behind delete-to-trash, upload commit and atomic save,
// all three of which assume source and destination share a filesystem. See
// IsCrossDevice for what happens when that stops being true.
func (f *FS) Rename(oldVirtual, newVirtual string) error {
	oldRel, err := f.Clean(oldVirtual)
	if err != nil {
		return err
	}
	newRel, err := f.Clean(newVirtual)
	if err != nil {
		return err
	}
	return f.root.Rename(oldRel, newRel)
}

// IsCrossDevice reports whether err is the kernel refusing a rename across
// filesystems.
//
// On today's PCS boxes /DATA is a plain directory on the root volume, not a
// mountpoint, so this never fires — verified on holyhorse and wisera. That is an
// observation about the current layout, not a guarantee: a future PCS that puts
// /DATA on its own volume would make every delete-to-trash fail. Callers that
// must survive that fall back to copy-then-delete.
func IsCrossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }
