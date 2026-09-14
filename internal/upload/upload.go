// Package upload handles chunked, resumable uploads over the tus protocol.
//
// tus rather than a hand-rolled Content-Range scheme because it is a real spec
// with a maintained browser client (tus-js-client), so the half of this that
// runs in the browser — retries, offset negotiation, resuming after a dropped
// connection or a page reload — is not ours to invent or debug. It is also what
// FileBrowser used, so uploads behave the way users of the old app expect.
//
// The shape of the flow:
//
//	POST  /api/tus/       create an upload, carrying dest+filename metadata
//	PATCH /api/tus/{id}   send a chunk, repeatedly, resumable
//	                      -> chunks land in ${STATE_DIR}/uploads/{id}
//	on complete           chown/chmod, then RENAME into the destination
//
// Staging inside STATE_DIR and committing with a rename is what keeps a partial
// file from ever appearing in the user's folder, where they could click it or
// another app could pick it up half-written.
package upload

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/tus/tusd/v2/pkg/filestore"
	tus "github.com/tus/tusd/v2/pkg/handler"
	// tusd 2.10 still types Config.Logger as golang.org/x/exp/slog rather than
	// the stdlib log/slog, so the logger has to be built from the same package.
	// The two are API-compatible; this import goes away when tusd migrates.
	xslog "golang.org/x/exp/slog"

	"github.com/yundera/files/internal/config"
	"github.com/yundera/files/internal/fsop"
	"github.com/yundera/files/internal/ownership"
	"github.com/yundera/files/internal/vfs"
)

// abandonedAfter is how long an untouched partial upload survives before the
// collector removes it. Long enough to resume after a laptop is closed
// overnight, short enough that a failed 4 GB upload does not sit on the disk
// forever.
const abandonedAfter = 24 * time.Hour

// Manager owns the tus handler and the staging area.
type Manager struct {
	fs    *vfs.FS
	cfg   config.Config
	owner ownership.Owner
	tus   *tus.UnroutedHandler

	// OnDirChanged names a directory whose contents changed, so open listings
	// refresh when an upload lands.
	OnDirChanged func(virtual string)
}

func New(f *vfs.FS, cfg config.Config) (*Manager, error) {
	m := &Manager{fs: f, cfg: cfg, owner: ownership.FromConfig(cfg)}

	dir := cfg.UploadDir()
	if err := os.MkdirAll(dir, cfg.DirMode); err != nil {
		return nil, fmt.Errorf("create upload staging dir: %w", err)
	}

	store := filestore.New(dir)
	// Match the app's configured modes so a staged file already has the right
	// permissions before it is renamed into place.
	store.DirModePerm = cfg.DirMode
	store.FileModePerm = cfg.FileMode

	composer := tus.NewStoreComposer()
	store.UseIn(composer)

	h, err := tus.NewUnroutedHandler(tus.Config{
		BasePath: "/api/tus/",
		// WARN, not the default INFO: tusd logs a line per request, and an 8 MiB
		// chunk size means a single 4 GB upload would write ~1500 lines. Errors
		// and protocol problems still surface.
		Logger:                xslog.New(xslog.NewTextHandler(os.Stderr, &xslog.HandlerOptions{Level: xslog.LevelWarn})),
		StoreComposer:         composer,
		MaxSize:               cfg.UploadMaxBytes,
		NotifyCompleteUploads: true,
		// No CORS headers. The app is same-origin behind a gate, and tusd's
		// defaults would otherwise contradict the rest of the API, which sets
		// none at all.
		Cors:                    &tus.CorsConfig{Disable: true},
		DisableDownload:         true, // reads go through /api/fs/raw, which enforces the disposition rules
		DisableConcatenation:    true, // no client needs it, and it widens the surface
		PreUploadCreateCallback: m.validateCreate,
	})
	if err != nil {
		return nil, err
	}
	m.tus = h

	go m.collectCompleted()
	go m.collectAbandoned()
	return m, nil
}

// Handler exposes the tus endpoints for the router to mount.
func (m *Manager) Handler() *tus.UnroutedHandler { return m.tus }

// destOf reads the destination directory and filename from upload metadata.
func destOf(info tus.FileInfo) (dir, name string) {
	dir = info.MetaData["dest"]
	if dir == "" {
		dir = "/"
	}
	name = info.MetaData["filename"]
	// A browser folder-drop sends a relative path in "relativePath"
	// ("photos/2024/a.jpg"), which is how uploading a whole directory keeps its
	// structure instead of flattening into one folder.
	if rp := info.MetaData["relativePath"]; rp != "" && rp != "null" {
		name = rp
	}
	return dir, name
}

// validateCreate rejects an upload before any bytes are accepted.
//
// Failing here rather than at commit time means a user who picks an impossible
// destination is told immediately, instead of after uploading four gigabytes.
func (m *Manager) validateCreate(hook tus.HookEvent) (tus.HTTPResponse, tus.FileInfoChanges, error) {
	dir, name := destOf(hook.Upload)
	if name == "" {
		return tus.HTTPResponse{}, tus.FileInfoChanges{}, tus.NewError(
			"ERR_NO_FILENAME", "the upload metadata must carry a filename", http.StatusBadRequest)
	}
	if _, err := m.targetRel(dir, name); err != nil {
		return tus.HTTPResponse{}, tus.FileInfoChanges{}, tus.NewError(
			"ERR_BAD_DEST", err.Error(), http.StatusBadRequest)
	}
	return tus.HTTPResponse{}, tus.FileInfoChanges{}, nil
}

// targetRel resolves dest+name to a root-relative path, rejecting anything the
// vfs refuses.
//
// name may carry a relative path from a folder drop, so each of its segments is
// checked rather than the whole thing being treated as one filename.
func (m *Manager) targetRel(dir, name string) (string, error) {
	// The DESTINATION is validated first, on its own.
	//
	// Order matters and is not obvious: path.Join cleans as it joins, so
	// Join("/../etc", "passwd") is "/etc/passwd" — the ".." is absorbed and
	// vfs.Clean never sees it. Validating dir before the join is what makes the
	// traversal visible. (The same absorption is why vfs.Clean rejects ".."
	// rather than resolving it.)
	dirRel, err := m.fs.Clean(dir)
	if err != nil {
		return "", err
	}

	// Then the filename, segment by segment: a folder drop legitimately carries
	// a relative path here, so it cannot simply be checked as one name.
	for _, seg := range strings.Split(name, "/") {
		switch {
		case seg == "" || seg == "." || seg == "..":
			return "", errors.New("that filename is not allowed")
		case strings.ContainsAny(seg, `\`):
			return "", errors.New("a filename cannot contain a backslash")
		case strings.ContainsRune(seg, 0):
			return "", errors.New("a filename cannot contain a NUL byte")
		}
	}

	// Both halves are clean now, so the join cannot hide anything.
	rel, err := m.fs.Clean(path.Join(dirRel, name))
	if err != nil {
		return "", err
	}
	if rel == "." {
		return "", errors.New("an upload needs a destination")
	}
	return rel, nil
}

// collectCompleted commits each finished upload into its destination.
func (m *Manager) collectCompleted() {
	for event := range m.tus.CompleteUploads {
		if err := m.commit(event.Upload); err != nil {
			log.Printf("upload %s: commit failed: %v", event.Upload.ID, err)
		}
	}
}

// commit moves a finished upload out of staging and into the user's tree.
func (m *Manager) commit(info tus.FileInfo) error {
	dir, name := destOf(info)

	// Re-validate. The destination was checked when the upload was created, but
	// a resumable upload can complete hours later, by which time the directory
	// may have been renamed or deleted out from under it. Trusting the
	// create-time check here would mean writing to wherever that path now
	// points.
	destRel, err := m.targetRel(dir, name)
	if err != nil {
		return fmt.Errorf("destination no longer valid: %w", err)
	}

	root := m.fs.Root()
	stagedRel := path.Join(m.fs.StateRel(), "uploads", info.ID)
	if _, err := root.Lstat(stagedRel); err != nil {
		return fmt.Errorf("staged data missing: %w", err)
	}

	// A folder drop carries sub-directories that may not exist yet.
	if parent := path.Dir(destRel); parent != "." {
		if err := root.MkdirAll(parent, m.owner.DirMode); err != nil {
			return err
		}
	}

	// keepBoth, not overwrite: an upload must never silently replace a file the
	// user already had. The browser offers no undo and the original is gone.
	finalRel, err := fsop.Resolve(root, destRel, fsop.KeepBoth)
	if err != nil {
		return err
	}

	// Stamp ownership BEFORE the rename. A rename keeps the inode, so a file
	// chowned afterwards would be owned by whoever tusd created it as — root —
	// for the window in between, and a failure after the rename would leave it
	// that way permanently.
	if f, err := root.OpenFile(stagedRel, os.O_WRONLY, 0); err == nil {
		m.owner.ApplyFile(f)
		_ = f.Close()
	}

	if err := fsop.Move(root, stagedRel, finalRel, m.owner); err != nil {
		return err
	}
	// The .info sidecar is no longer needed once the payload has landed.
	_ = root.Remove(stagedRel + ".info")

	if m.OnDirChanged != nil {
		m.OnDirChanged("/" + path.Dir(finalRel))
	}
	return nil
}

// collectAbandoned removes partial uploads nobody came back for.
//
// Runs at startup and every six hours. Without it, every cancelled or failed
// upload leaves its bytes in the staging directory permanently — invisible to
// the user, because the staging area is inside the hidden state dir.
func (m *Manager) collectAbandoned() {
	sweep := func() {
		root := m.fs.Root()
		relDir := path.Join(m.fs.StateRel(), "uploads")
		d, err := root.Open(relDir)
		if err != nil {
			return
		}
		names, err := d.Readdirnames(-1)
		_ = d.Close()
		if err != nil {
			return
		}
		removed := 0
		for _, n := range names {
			rel := path.Join(relDir, n)
			fi, err := root.Lstat(rel)
			if err != nil || time.Since(fi.ModTime()) < abandonedAfter {
				continue
			}
			if err := root.RemoveAll(rel); err == nil {
				removed++
			}
		}
		if removed > 0 {
			log.Printf("upload: removed %d abandoned partial upload(s)", removed)
		}
	}
	sweep()
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for range t.C {
		sweep()
	}
}

var _ = fs.ErrNotExist
