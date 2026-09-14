package trash

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yundera/files/internal/config"
	"github.com/yundera/files/internal/fsop"
	"github.com/yundera/files/internal/vfs"
)

func newStore(t *testing.T) (*Store, *vfs.FS, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DataRoot: root, PUID: -1, PGID: -1, DirMode: 0o755, FileMode: 0o644, TrashRetention: 30 * 24 * time.Hour}
	f, err := vfs.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return New(f, cfg), f, root
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The round trip that matters: delete then restore puts the bytes back at the
// original path.
func TestPutThenRestoreReturnsTheFile(t *testing.T) {
	s, _, root := newStore(t)
	writeFile(t, root, "Documents/note.txt", "hello")

	item, err := s.Put("/Documents/note.txt")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Documents", "note.txt")); !os.IsNotExist(err) {
		t.Error("the original still exists after a delete")
	}
	if item.OriginalPath != "/Documents/note.txt" || item.Size != 5 {
		t.Errorf("item = %+v; want originalPath /Documents/note.txt and size 5", item)
	}

	got, err := s.Restore(item.ID, fsop.Skip)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got != "/Documents/note.txt" {
		t.Errorf("Restore returned %q; want the original path", got)
	}
	b, err := os.ReadFile(filepath.Join(root, "Documents", "note.txt"))
	if err != nil || string(b) != "hello" {
		t.Errorf("restored content = %q, %v; want %q", b, err, "hello")
	}
	if items, _ := s.List(); len(items) != 0 {
		t.Errorf("trash still holds %d items after a restore; want 0", len(items))
	}
}

// A whole directory goes to the trash in one rename and comes back intact.
func TestPutAndRestoreADirectoryTree(t *testing.T) {
	s, _, root := newStore(t)
	writeFile(t, root, "Documents/proj/a.txt", "a")
	writeFile(t, root, "Documents/proj/sub/b.txt", "bb")

	item, err := s.Put("/Documents/proj")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !item.IsDir || item.Size != 3 {
		t.Errorf("item = %+v; want isDir and size 3 (the walked total)", item)
	}
	if _, err := s.Restore(item.ID, fsop.Skip); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	for rel, want := range map[string]string{
		"Documents/proj/a.txt":     "a",
		"Documents/proj/sub/b.txt": "bb",
	} {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || string(b) != want {
			t.Errorf("%s = %q, %v; want %q", rel, b, err, want)
		}
	}
}

// Restoring into a spot that is occupied again must honour the conflict policy
// rather than silently clobbering whatever is there now.
func TestRestoreHonoursTheConflictPolicy(t *testing.T) {
	s, _, root := newStore(t)
	writeFile(t, root, "Documents/note.txt", "original")
	item, _ := s.Put("/Documents/note.txt")
	writeFile(t, root, "Documents/note.txt", "replacement")

	if _, err := s.Restore(item.ID, fsop.Skip); err == nil {
		t.Error("Restore with Skip overwrote an occupied destination")
	}
	got, err := s.Restore(item.ID, fsop.KeepBoth)
	if err != nil {
		t.Fatalf("Restore KeepBoth: %v", err)
	}
	if got != "/Documents/note (2).txt" {
		t.Errorf("KeepBoth restored to %q; want /Documents/note (2).txt", got)
	}
	// The file that was in the way must be untouched.
	b, _ := os.ReadFile(filepath.Join(root, "Documents", "note.txt"))
	if string(b) != "replacement" {
		t.Errorf("the occupying file changed to %q; it must be left alone", b)
	}
}

// The parent directory may have been deleted since. Restore recreates it rather
// than failing — otherwise an item can become permanently unrestorable.
func TestRestoreRecreatesAMissingParent(t *testing.T) {
	s, _, root := newStore(t)
	writeFile(t, root, "Documents/deep/note.txt", "x")
	item, _ := s.Put("/Documents/deep/note.txt")
	if err := os.RemoveAll(filepath.Join(root, "Documents", "deep")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restore(item.ID, fsop.Skip); err != nil {
		t.Fatalf("Restore with a missing parent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Documents", "deep", "note.txt")); err != nil {
		t.Errorf("file not restored: %v", err)
	}
}

// Retention purges old items and leaves recent ones.
func TestPurgeRespectsRetention(t *testing.T) {
	s, _, root := newStore(t)
	writeFile(t, root, "Documents/old.txt", "o")
	writeFile(t, root, "Documents/new.txt", "n")
	oldItem, _ := s.Put("/Documents/old.txt")
	newItem, _ := s.Put("/Documents/new.txt")

	// Backdate one entry past the retention window.
	oldItem.DeletedAt = time.Now().Add(-40 * 24 * time.Hour)
	if err := s.writeMeta(oldItem); err != nil {
		t.Fatal(err)
	}

	n, err := s.Purge()
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if n != 1 {
		t.Errorf("purged %d items; want 1", n)
	}
	items, _ := s.List()
	if len(items) != 1 || items[0].ID != newItem.ID {
		t.Errorf("after purge the trash holds %+v; want only the recent item", items)
	}
}

// Ids come from the client and are joined onto a path. The trash builds its
// paths from StateRel and so goes around vfs.Clean — validID is what stands in
// for it, and it must refuse anything this package did not generate.
func TestTrashIdsAreValidated(t *testing.T) {
	for _, bad := range []string{
		"", "..", "../../etc", "a/b", `a\b`, "id.with.dots", "id with spaces",
		"../AppData", "verylong" + string(make([]byte, 100)),
	} {
		if validID(bad) {
			t.Errorf("validID(%q) = true; want false", bad)
		}
	}
	if !validID(newID(time.Now())) {
		t.Error("a freshly generated id was rejected")
	}
}

// Deleting the data root itself is meaningless and must be refused rather than
// attempted.
func TestPutRefusesTheRoot(t *testing.T) {
	s, _, _ := newStore(t)
	for _, p := range []string{"/", "", "."} {
		if _, err := s.Put(p); err == nil {
			t.Errorf("Put(%q) succeeded; deleting the data root must be refused", p)
		}
	}
}

// The trash is inside the browsable tree, so a delete must not be able to reach
// into it — that would let a user trash their own trash.
func TestPutRefusesTheStateDir(t *testing.T) {
	s, _, _ := newStore(t)
	if _, err := s.Put("/AppData/files"); err == nil {
		t.Error("Put succeeded on the state directory; it must be refused")
	}
	if _, err := s.Put("/AppData/files/trash"); err == nil {
		t.Error("Put succeeded on the trash directory itself; it must be refused")
	}
}
