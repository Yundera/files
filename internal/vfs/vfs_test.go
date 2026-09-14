package vfs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yundera/files/internal/config"
)

// newFS builds an FS over a temp tree laid out like a real data root.
func newFS(t *testing.T) (*FS, string) {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"Documents", "Downloads", "AppData/files/trash", "AppData/other"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("setup %s: %v", d, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "Documents", "note.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := New(config.Config{DataRoot: root})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f, root
}

// The escape table. Every one of these is a way out of the data root that a
// naive filepath.Join would allow, and each must be refused. A regression here
// is the difference between a file manager and an arbitrary-file-read.
func TestCleanRefusesEveryEscape(t *testing.T) {
	f, _ := newFS(t)
	for _, c := range []struct {
		name, in string
	}{
		{"parent", "/.."},
		{"parent bare", ".."},
		{"leading traversal", "/../etc/passwd"},
		{"embedded traversal", "/Documents/../../etc/passwd"},
		{"trailing traversal", "/Documents/.."},
		{"deep traversal", "/a/b/c/../../../../etc"},
		{"traversal with dot segments", "/./../."},
		{"NUL byte", "/Documents/no\x00pe"},
		{"NUL only", "\x00"},
		{"invalid utf8", "/Documents/\xff\xfe"},
		{"backslash", `/Documents\..\etc`},
	} {
		if got, err := f.Clean(c.in); err == nil {
			t.Errorf("%s: Clean(%q) = %q, nil; want an error", c.name, c.in, got)
		} else if !errors.Is(err, ErrInvalidPath) {
			t.Errorf("%s: Clean(%q) error = %v; want ErrInvalidPath", c.name, c.in, err)
		}
	}
}

// Normalisation: several spellings of the same location must produce one
// canonical form, and the root must be "." (what the os.Root methods expect).
func TestCleanNormalises(t *testing.T) {
	f, _ := newFS(t)
	for _, c := range []struct{ in, want string }{
		{"", "."},
		{"/", "."},
		{".", "."},
		{"//", "."},
		{"/Documents", "Documents"},
		{"Documents", "Documents"},
		{"/Documents/", "Documents"},
		{"//Documents//note.txt", "Documents/note.txt"},
		{"/Documents/./note.txt", "Documents/note.txt"},
	} {
		got, err := f.Clean(c.in)
		if err != nil {
			t.Errorf("Clean(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("Clean(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

// The app's own state directory must be invisible and untouchable through the
// vfs. If this breaks, a user can browse into their own trash and into the tus
// staging area. ErrHidden is distinct from ErrInvalidPath so the handler can
// answer 404 (absent) rather than 400 (malformed).
func TestStateDirIsHidden(t *testing.T) {
	f, _ := newFS(t)
	for _, p := range []string{
		"/AppData/files",
		"/AppData/files/",
		"/AppData/files/trash",
		"/AppData/files/trash/01JC/meta.json",
		"/AppData/./files",
	} {
		if _, err := f.Clean(p); !errors.Is(err, ErrHidden) {
			t.Errorf("Clean(%q) error = %v; want ErrHidden", p, err)
		}
	}
	// A sibling whose name merely starts with the same bytes must NOT be hidden.
	// "AppData/files-backup" shares the prefix "AppData/files" and a naive
	// strings.HasPrefix without the separator would swallow it.
	for _, p := range []string{"/AppData/other", "/AppData/files-backup", "/AppData/filesx"} {
		if _, err := f.Clean(p); err != nil {
			t.Errorf("Clean(%q) = %v; want it to be allowed", p, err)
		}
	}
}

// Every mutating method must refuse a hidden path too, not just Clean. This is
// the test that catches a new method added without going through Clean.
func TestEveryMethodRefusesHiddenAndEscapingPaths(t *testing.T) {
	f, _ := newFS(t)
	for _, bad := range []string{"/AppData/files/trash", "/../etc/passwd"} {
		ops := map[string]error{
			"Open":       first(f.Open(bad)),
			"OpenFile":   first(f.OpenFile(bad, os.O_RDONLY, 0)),
			"Stat":       firstInfo(f.Stat(bad)),
			"Lstat":      firstInfo(f.Lstat(bad)),
			"Mkdir":      f.Mkdir(bad, 0o755),
			"MkdirAll":   f.MkdirAll(bad, 0o755),
			"Remove":     f.Remove(bad),
			"RemoveAll":  f.RemoveAll(bad),
			"Rename_src": f.Rename(bad, "/Documents/x"),
			"Rename_dst": f.Rename("/Documents/note.txt", bad),
		}
		for name, err := range ops {
			if err == nil {
				t.Errorf("%s(%q) = nil; want a refusal", name, bad)
			}
		}
	}
}

// A symlink pointing out of the root must not be followable, even though the
// link itself lives inside. os.Root enforces this at the syscall layer, which is
// the whole reason for using it; this test pins that we actually get that
// behaviour rather than having disabled it somewhere.
func TestSymlinkOutOfRootIsNotFollowed(t *testing.T) {
	f, root := newFS(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("classified"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := f.Open("/escape"); err == nil {
		t.Fatal("Open(/escape) succeeded; a symlink out of the root must not be followable")
	}
	// An absolute symlink to a directory, traversed through, must fail too.
	if err := os.Symlink("/etc", filepath.Join(root, "etclink")); err == nil {
		if _, err := f.Open("/etclink/passwd"); err == nil {
			t.Fatal("Open(/etclink/passwd) succeeded; traversal through an escaping symlink must fail")
		}
	}
}

// A symlink whose target is inside the root is legitimate and must keep working
// — the confinement is about leaving the tree, not about symlinks as such.
func TestSymlinkInsideRootStillWorks(t *testing.T) {
	f, root := newFS(t)
	if err := os.Symlink("Documents/note.txt", filepath.Join(root, "shortcut")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	b, err := readAll(f, "/shortcut")
	if err != nil {
		t.Fatalf("Open(/shortcut): %v", err)
	}
	if string(b) != "hi" {
		t.Errorf("read through in-root symlink = %q; want %q", b, "hi")
	}
	// Lstat must still report it as a link rather than as its target.
	fi, err := f.Lstat("/shortcut")
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Error("Lstat(/shortcut) did not report a symlink")
	}
}

// A state dir outside the data root cannot be expressed with a single os.Root,
// so New must refuse rather than half-work.
func TestNewRefusesAStateDirOutsideTheRoot(t *testing.T) {
	root := t.TempDir()
	_, err := New(config.Config{DataRoot: root, StateDirPath: t.TempDir()})
	if err == nil {
		t.Fatal("New accepted a state dir outside the data root; want an error")
	}
	if !strings.Contains(err.Error(), "subdirectory") {
		t.Errorf("New error = %v; want it to name the subdirectory requirement", err)
	}
}

func first(f *os.File, err error) error {
	if f != nil {
		_ = f.Close()
	}
	return err
}

func firstInfo(_ os.FileInfo, err error) error { return err }

func readAll(f *FS, p string) ([]byte, error) {
	fh, err := f.Open(p)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	buf := make([]byte, 64)
	n, _ := fh.Read(buf)
	return buf[:n], nil
}
