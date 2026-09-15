// Package bootstrap prepares the data root for first use.
//
// The sidebar offers Documents, Downloads, Media and AppData as fixed
// shortcuts, so the app is asserting that those directories exist. On a
// provisioned PCS they do — template-root creates them — but the app is also
// installed on a bare bind mount, on a demo instance that has been recycled, and
// on a developer's ./DATA, and there the shortcuts led to a dead end: clicking
// Documents reported "no such file or directory" and left the breadcrumb at
// Root, with no way forward except guessing that New Folder was the answer.
//
// So the app creates what it advertises, rather than the deployment being
// expected to have done it. Doing this at startup (from main, not from
// server.New) keeps it a deployment-time side effect: server.New stays
// constructible against a bare temp dir, which is what the handler tests rely
// on.
package bootstrap

import (
	"errors"
	"log"
	"os"

	"github.com/yundera/files/internal/config"
	"github.com/yundera/files/internal/ownership"
	"github.com/yundera/files/internal/vfs"
)

// Roots are the directories the sidebar links to, in the order it lists them.
//
// The UI's copy of this list is `roots` in web/src/lib/browse.svelte.ts — it
// carries the virtual paths and the display names, this one only the names to
// create. Change one and change the other: a shortcut with no directory behind
// it is the bug this package exists to prevent.
//
// AppData is included for completeness; the state directory underneath it
// (AppData/files) means it gets created either way.
var Roots = []string{"Documents", "Downloads", "Media", "AppData"}

// EnsureRoots creates any missing root directory, owned by PUID:PGID with the
// configured DirMode.
//
// It never fails the boot. A read-only data root, or one the process cannot
// write, is a real deployment to serve — read-only browsing still works, and a
// file manager that refuses to start is a worse outcome than one with a
// shortcut that errors. Every failure is logged instead.
func EnsureRoots(cfg config.Config) {
	fsys, err := vfs.New(cfg)
	if err != nil {
		log.Printf("WARNING: cannot prepare data root: %v", err)
		return
	}
	defer func() { _ = fsys.Close() }()

	owner := ownership.FromConfig(cfg)

	for _, name := range Roots {
		switch err := fsys.Mkdir(name, cfg.DirMode); {
		case err == nil:
			// Only a directory this call created is stamped. An existing
			// Documents owned by someone else is left exactly as it is — see
			// the CREATE/OVERWRITE asymmetry in package ownership.
			stamp(fsys, name, owner)
			log.Printf("created %s", name)
		case errors.Is(err, os.ErrExist):
			// The overwhelmingly common case, every boot after the first.
		default:
			log.Printf("WARNING: cannot create %s: %v", name, err)
		}
	}
}

// stamp applies PUID:PGID and DirMode to a directory the caller just created.
// Ownership works on the open descriptor, never the path, because os.Root's
// path-based Chown is documented as racy against a symlink swap.
func stamp(fsys *vfs.FS, name string, owner ownership.Owner) {
	d, err := fsys.Open(name)
	if err != nil {
		log.Printf("WARNING: cannot set ownership on %s: %v", name, err)
		return
	}
	defer func() { _ = d.Close() }()
	owner.ApplyDir(d)
}
