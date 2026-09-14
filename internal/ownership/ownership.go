// Package ownership applies PUID:PGID and the configured modes to what the app
// creates.
//
// Two rules, and the asymmetry between them is deliberate:
//
//	CREATE    -> chown to PUID:PGID, chmod to DirMode/FileMode.
//	OVERWRITE -> leave uid, gid and mode exactly as they were.
//
// The second rule is the one a later refactor will want to "simplify" away.
// Don't: editing an app's config.yaml through this file manager must not re-home
// it to PUID and break the app that owns it. A move (rename) touches neither,
// because it is the same inode.
package ownership

import (
	"log"
	"os"
	"sync"

	"github.com/yundera/files/internal/config"
)

// Owner carries the identity and modes to stamp onto new inodes.
type Owner struct {
	UID, GID int
	DirMode  os.FileMode
	FileMode os.FileMode
}

func FromConfig(cfg config.Config) Owner {
	return Owner{UID: cfg.PUID, GID: cfg.PGID, DirMode: cfg.DirMode, FileMode: cfg.FileMode}
}

// warnOnce keeps a failing chown from writing one line per file during a copy of
// ten thousand of them. The first failure is the informative one; the rest say
// the same thing.
var warnOnce sync.Once

// ApplyFile stamps a newly created file, given its OPEN descriptor.
//
// Operating on the fd rather than the path is a correctness requirement, not a
// style preference. Go's own documentation marks os.Root.Chmod and
// os.Root.Chown as racy on Unix: if the target is swapped from a regular file to
// a symlink while the call is in flight, the operation can land on the link
// instead of its target. A descriptor cannot be redirected that way.
func (o Owner) ApplyFile(f *os.File) {
	o.apply(f, o.FileMode)
}

// ApplyDir stamps a newly created directory, given its open descriptor.
// fchown and fchmod both work on a directory fd.
func (o Owner) ApplyDir(f *os.File) {
	o.apply(f, o.DirMode)
}

func (o Owner) apply(f *os.File, mode os.FileMode) {
	// A negative id means "leave ownership alone" — the escape hatch for running
	// somewhere chown is not possible or not wanted.
	if o.UID >= 0 && o.GID >= 0 {
		if err := f.Chown(o.UID, o.GID); err != nil {
			warnOnce.Do(func() {
				log.Printf("WARNING: cannot chown to %d:%d (%v). Files will be owned by "+
					"the user this process runs as. Further chown failures are not logged.",
					o.UID, o.GID, err)
			})
		}
	}
	if mode != 0 {
		// chmod after chown: on Linux chown clears the setuid/setgid bits, so
		// the other order would silently drop them from the requested mode.
		if err := f.Chmod(mode); err != nil {
			log.Printf("chmod %s to %#o: %v", f.Name(), mode, err)
		}
	}
}

// Preserved is the uid, gid and mode of an existing file, captured before an
// overwrite so they can be put back afterwards.
type Preserved struct {
	UID, GID int
	Mode     os.FileMode
	ok       bool
}

// Capture reads the ownership of an existing path. A missing file yields a zero
// Preserved whose Restore is a no-op, so a caller can Capture unconditionally
// without first testing whether the destination exists.
func Capture(fi os.FileInfo) Preserved {
	uid, gid, ok := idsOf(fi)
	if !ok {
		return Preserved{}
	}
	return Preserved{UID: uid, GID: gid, Mode: fi.Mode().Perm(), ok: true}
}

// Restore puts a captured ownership back onto the fd of the replacement file.
// A zero Preserved does nothing, which is what makes the create path and the
// overwrite path share one code path in the callers.
func (p Preserved) Restore(f *os.File) bool {
	if !p.ok {
		return false
	}
	if err := f.Chown(p.UID, p.GID); err != nil {
		log.Printf("restore owner on %s: %v", f.Name(), err)
	}
	if err := f.Chmod(p.Mode); err != nil {
		log.Printf("restore mode on %s: %v", f.Name(), err)
	}
	return true
}
