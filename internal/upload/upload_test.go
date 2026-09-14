package upload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tus "github.com/tus/tusd/v2/pkg/handler"

	"github.com/yundera/files/internal/config"
	"github.com/yundera/files/internal/vfs"
)

func newManager(t *testing.T) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DataRoot: root, PUID: -1, PGID: -1, DirMode: 0o755, FileMode: 0o644}
	f, err := vfs.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	m, err := New(f, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return m, root
}

// Upload metadata is attacker-controlled: the filename and destination both
// arrive in a header. Every escape has to be refused before a single byte is
// accepted.
func TestUploadTargetsAreValidated(t *testing.T) {
	m, _ := newManager(t)
	for _, c := range []struct{ dir, name string }{
		{"/Documents", "../../etc/passwd"},
		{"/Documents", "../escape.txt"},
		{"/../etc", "passwd"},
		{"/Documents", ""},
		{"/Documents", "."},
		{"/Documents", ".."},
		{"/Documents", "a\x00b"},
		{"/Documents", `sub\..\..\x`},
		{"/AppData/files", "sneak.txt"},
		{"/AppData/files/trash", "sneak.txt"},
		{"/", ""},
	} {
		if got, err := m.targetRel(c.dir, c.name); err == nil {
			t.Errorf("targetRel(%q, %q) = %q, nil; want a refusal", c.dir, c.name, got)
		}
	}
}

// A folder drop sends a relative path, which is how uploading a directory keeps
// its structure instead of flattening. Those must still resolve inside the root.
func TestFolderDropPathsAreAccepted(t *testing.T) {
	m, _ := newManager(t)
	for _, c := range []struct{ dir, name, want string }{
		{"/Documents", "a.txt", "Documents/a.txt"},
		{"/Documents", "photos/2024/a.jpg", "Documents/photos/2024/a.jpg"},
		{"/", "top.txt", "top.txt"},
	} {
		got, err := m.targetRel(c.dir, c.name)
		if err != nil || got != c.want {
			t.Errorf("targetRel(%q, %q) = %q, %v; want %q", c.dir, c.name, got, err, c.want)
		}
	}
}

// relativePath wins over filename, so a folder drop nests rather than dumping
// every file into one directory.
func TestRelativePathOverridesFilename(t *testing.T) {
	for _, c := range []struct {
		meta     map[string]string
		wantName string
	}{
		{map[string]string{"filename": "a.jpg"}, "a.jpg"},
		{map[string]string{"filename": "a.jpg", "relativePath": "photos/a.jpg"}, "photos/a.jpg"},
		// tus-js-client sends the string "null" when there is no relative path.
		{map[string]string{"filename": "a.jpg", "relativePath": "null"}, "a.jpg"},
		{map[string]string{"filename": "a.jpg", "relativePath": ""}, "a.jpg"},
	} {
		_, name := destOf(tus.FileInfo{MetaData: c.meta})
		if name != c.wantName {
			t.Errorf("destOf(%v) name = %q; want %q", c.meta, name, c.wantName)
		}
	}
}

// A missing dest means the root, not a failure — a plain drop onto the file
// pane has no explicit destination.
func TestMissingDestDefaultsToTheRoot(t *testing.T) {
	dir, _ := destOf(tus.FileInfo{MetaData: map[string]string{"filename": "a.txt"}})
	if dir != "/" {
		t.Errorf("dest = %q; want /", dir)
	}
}

// The create callback must reject a bad destination up front, so a user who
// picks an impossible target learns before uploading four gigabytes.
func TestCreateIsRejectedEarlyForABadDestination(t *testing.T) {
	m, _ := newManager(t)
	_, _, err := m.validateCreate(tus.HookEvent{
		Upload: tus.FileInfo{MetaData: map[string]string{"dest": "/Documents", "filename": "../../x"}},
	})
	if err == nil {
		t.Fatal("validateCreate accepted a traversing filename")
	}
	// And a good one is accepted.
	if _, _, err := m.validateCreate(tus.HookEvent{
		Upload: tus.FileInfo{MetaData: map[string]string{"dest": "/Documents", "filename": "ok.txt"}},
	}); err != nil {
		t.Errorf("validateCreate rejected a valid upload: %v", err)
	}
}

// A completed upload lands at its destination, and an upload whose destination
// vanished while it was in flight must NOT be written anywhere.
func TestCommitMovesStagedDataAndRefusesAVanishedDestination(t *testing.T) {
	m, root := newManager(t)

	stage := func(id, body string) {
		p := filepath.Join(m.cfg.UploadDir(), id)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p+".info", []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	stage("upl1", "payload")
	err := m.commit(tus.FileInfo{ID: "upl1", MetaData: map[string]string{"dest": "/Documents", "filename": "landed.txt"}})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, "Documents", "landed.txt"))
	if err != nil || string(b) != "payload" {
		t.Errorf("landed file = %q, %v; want %q", b, err, "payload")
	}
	// Staging is cleaned up, including the sidecar.
	if _, err := os.Stat(filepath.Join(m.cfg.UploadDir(), "upl1")); !os.IsNotExist(err) {
		t.Error("staged payload survived the commit")
	}
	if _, err := os.Stat(filepath.Join(m.cfg.UploadDir(), "upl1.info")); !os.IsNotExist(err) {
		t.Error(".info sidecar survived the commit")
	}

	// A destination that became invalid while the upload was in flight.
	stage("upl2", "nope")
	err = m.commit(tus.FileInfo{ID: "upl2", MetaData: map[string]string{"dest": "/AppData/files", "filename": "sneak.txt"}})
	if err == nil {
		t.Fatal("commit wrote into the state directory")
	}
	if !strings.Contains(err.Error(), "no longer valid") {
		t.Errorf("commit error = %v; want it to name the re-validation", err)
	}
}

// An upload must never silently replace a file the user already had: the
// browser offers no undo and the original would be gone.
func TestCommitNeverOverwritesAnExistingFile(t *testing.T) {
	m, root := newManager(t)
	if err := os.WriteFile(filepath.Join(root, "Documents", "dup.txt"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(m.cfg.UploadDir(), "upl3")
	if err := os.WriteFile(p, []byte("incoming"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.commit(tus.FileInfo{ID: "upl3", MetaData: map[string]string{"dest": "/Documents", "filename": "dup.txt"}}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	orig, _ := os.ReadFile(filepath.Join(root, "Documents", "dup.txt"))
	if string(orig) != "original" {
		t.Errorf("existing file = %q; an upload must not replace it", orig)
	}
	kept, err := os.ReadFile(filepath.Join(root, "Documents", "dup (2).txt"))
	if err != nil || string(kept) != "incoming" {
		t.Errorf("keep-both copy = %q, %v; want %q at dup (2).txt", kept, err, "incoming")
	}
}

// A folder drop creates the directories it needs.
func TestCommitCreatesMissingSubdirectories(t *testing.T) {
	m, root := newManager(t)
	p := filepath.Join(m.cfg.UploadDir(), "upl4")
	if err := os.WriteFile(p, []byte("deep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.commit(tus.FileInfo{ID: "upl4", MetaData: map[string]string{
		"dest": "/Documents", "filename": "photos/2024/a.jpg"}}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(root, "Documents", "photos", "2024", "a.jpg")); err != nil || string(b) != "deep" {
		t.Errorf("nested upload = %q, %v", b, err)
	}
}
