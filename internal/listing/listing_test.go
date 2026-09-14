package listing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yundera/files/internal/config"
	"github.com/yundera/files/internal/vfs"
)

func newFS(t *testing.T) (*vfs.FS, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "AppData", "files"), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := vfs.New(config.Config{DataRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f, root
}

func write(t *testing.T, root, name string, size int, age time.Duration) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(p, when, when); err != nil {
		t.Fatal(err)
	}
}

// Directories come first whatever the key and whatever the direction. Reversing
// a sort must not scatter folders through the file list — that is CasaOS's
// behaviour and the thing users notice immediately if it breaks.
func TestDirectoriesSortFirstInBothDirections(t *testing.T) {
	f, root := newFS(t)
	write(t, root, "zzz.txt", 10, 0)
	write(t, root, "aaa.txt", 5000, time.Hour)
	if err := os.Mkdir(filepath.Join(root, "mmm-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"name", "size", "modified", "kind"} {
		for _, desc := range []bool{false, true} {
			page, err := Read(f, "/", Options{Sort: key, Desc: desc})
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if len(page.Entries) == 0 {
				t.Fatalf("%s/desc=%v: no entries", key, desc)
			}
			if page.Entries[0].Kind != KindDir {
				t.Errorf("sort=%s desc=%v: first entry is %q (%s); want the directory first",
					key, desc, page.Entries[0].Name, page.Entries[0].Kind)
			}
		}
	}
}

// The app's own state directory must never appear in a listing of its parent.
// vfs.Clean refuses a path INTO it; this is the other half — the entry itself
// has to be skipped rather than shown or errored on.
func TestStateDirIsNotListed(t *testing.T) {
	f, root := newFS(t)
	write(t, root, "AppData/other/keep.txt", 1, 0)
	page, err := Read(f, "/AppData", Options{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	for _, e := range page.Entries {
		if e.Name == "files" {
			t.Fatal("the state directory appeared in the listing of /AppData")
		}
	}
	if len(page.Entries) != 1 || page.Entries[0].Name != "other" {
		t.Errorf("entries = %+v; want only \"other\"", page.Entries)
	}
}

// Dotfiles are hidden unless asked for.
func TestHiddenFilesAreOptIn(t *testing.T) {
	f, root := newFS(t)
	write(t, root, ".secret", 1, 0)
	write(t, root, "visible.txt", 1, 0)

	page, _ := Read(f, "/", Options{})
	for _, e := range page.Entries {
		if e.Name == ".secret" {
			t.Error(".secret listed without Hidden:true")
		}
	}
	page, _ = Read(f, "/", Options{Hidden: true})
	var found bool
	for _, e := range page.Entries {
		if e.Name == ".secret" {
			found = true
		}
	}
	if !found {
		t.Error(".secret missing with Hidden:true")
	}
}

// Paging must be total and stable: every entry appears exactly once across the
// pages. Name is the tiebreaker on every sort key precisely so that an offset
// cursor cannot repeat or skip an entry.
func TestPagingCoversEveryEntryExactlyOnce(t *testing.T) {
	f, root := newFS(t)
	const n = 25
	for i := 0; i < n; i++ {
		// Identical sizes and mtimes, so only the tiebreaker orders them.
		write(t, root, "f"+string(rune('a'+i%26))+string(rune('0'+i/26))+".txt", 100, 0)
	}
	seen := map[string]int{}
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > 20 {
			t.Fatal("paging did not terminate")
		}
		page, err := Read(f, "/", Options{Sort: "size", Limit: 7, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page.Entries {
			seen[e.Name]++
		}
		if page.Cursor == "" {
			break
		}
		cursor = page.Cursor
	}
	// The root also holds AppData, so count only the files this test created.
	files := 0
	for name, count := range seen {
		if count != 1 {
			t.Errorf("%s appeared %d times across pages; want exactly 1", name, count)
		}
		if strings.HasSuffix(name, ".txt") {
			files++
		}
	}
	if files != n {
		t.Errorf("saw %d of the created files across all pages; want %d", files, n)
	}
}

// A thumbnail is only advertised for a format the thumbnailer can actually
// decode. Claiming one for a .heic would render a broken image in the grid.
func TestThumbFlagOnlyForDecodableFormats(t *testing.T) {
	f, root := newFS(t)
	for _, n := range []string{"a.jpg", "b.PNG", "c.heic", "d.txt", "e.webp", "noext"} {
		write(t, root, n, 1, 0)
	}
	page, _ := Read(f, "/", Options{})
	want := map[string]bool{"a.jpg": true, "b.PNG": true, "c.heic": false, "d.txt": false, "e.webp": true, "noext": false}
	for _, e := range page.Entries {
		if w, ok := want[e.Name]; ok && e.Thumb != w {
			t.Errorf("%s: Thumb = %v; want %v (ext %q)", e.Name, e.Thumb, w, e.Ext)
		}
	}
}

// A dotfile has no extension — ".bashrc" is a hidden file, not a "bashrc" file.
func TestLeadingDotIsNotAnExtension(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{".bashrc", ""},
		{"note.txt", "txt"},
		{"archive.tar.gz", "gz"},
		{"noext", ""},
		{"trailing.", ""},
		{"UPPER.JPG", "jpg"},
	} {
		if got := ext(c.in); got != c.want {
			t.Errorf("ext(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}
