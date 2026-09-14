package textfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yundera/files/internal/config"
	"github.com/yundera/files/internal/ownership"
	"github.com/yundera/files/internal/vfs"
)

func newFS(t *testing.T) (*vfs.FS, config.Config, string) {
	t.Helper()
	root := t.TempDir()
	cfg := config.Config{DataRoot: root, PUID: -1, PGID: -1, DirMode: 0o755, FileMode: 0o644, EditMaxBytes: 1024}
	f, err := vfs.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f, cfg, root
}

func write(t *testing.T, root, name string, body []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Opening a binary as text and saving it rewrites it as mojibake. Both signals
// — a NUL byte and invalid UTF-8 — must refuse.
func TestBinaryFilesAreRefused(t *testing.T) {
	f, cfg, root := newFS(t)
	for name, body := range map[string][]byte{
		"image.png":  {0x89, 'P', 'N', 'G', 0x00, 0x1a},
		"latin1.txt": {'c', 'a', 'f', 0xe9}, // café in latin-1: valid bytes, invalid UTF-8
		"nul.txt":    []byte("hello\x00world"),
	} {
		write(t, root, name, body)
		if _, err := Read(f, cfg, "/"+name); !errors.Is(err, ErrBinary) {
			t.Errorf("Read(%s) error = %v; want ErrBinary", name, err)
		}
	}
	// Real text, including multi-byte UTF-8, must still open.
	write(t, root, "ok.txt", []byte("héllo — ünicode ✓"))
	if _, err := Read(f, cfg, "/ok.txt"); err != nil {
		t.Errorf("Read of valid UTF-8 text failed: %v", err)
	}
}

// The size cap is checked before the read, so a huge file never allocates.
func TestOversizeFilesAreRefused(t *testing.T) {
	f, cfg, root := newFS(t)
	write(t, root, "big.txt", []byte(strings.Repeat("x", 2048)))
	if _, err := Read(f, cfg, "/big.txt"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Read of an oversize file = %v; want ErrTooLarge", err)
	}
}

// Broken YAML must block the save, not warn. On a PCS this editor is used on
// compose files, and finding out at `docker compose up` is much worse.
func TestBrokenYamlBlocksTheSaveAndReportsTheLine(t *testing.T) {
	f, _, root := newFS(t)
	owner := ownership.Owner{UID: -1, GID: -1, FileMode: 0o644}
	write(t, root, "compose.yml", []byte("services:\n  web:\n    image: nginx\n"))

	broken := "services:\n  web:\n   bad: [unclosed\n"
	_, err := Write(f, owner, "/compose.yml", broken, time.Time{})
	if err == nil {
		t.Fatal("broken YAML was saved")
	}
	var se *SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("error = %v; want a *SyntaxError", err)
	}
	if se.Line == 0 {
		t.Errorf("SyntaxError has no line number: %+v", se)
	}
	// The original must be untouched — a refused save writes nothing.
	b, _ := os.ReadFile(filepath.Join(root, "compose.yml"))
	if !strings.Contains(string(b), "image: nginx") {
		t.Errorf("the original file was modified by a refused save: %q", b)
	}
	// And no temp file is left behind.
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("a refused save left a temp file: %s", e.Name())
		}
	}
	// Valid YAML saves fine.
	if _, err := Write(f, owner, "/compose.yml", "services:\n  web:\n    image: caddy\n", time.Time{}); err != nil {
		t.Errorf("valid YAML was refused: %v", err)
	}
}

// Only YAML is validated. A .txt full of colons is not a syntax error.
func TestOnlyYamlIsValidated(t *testing.T) {
	for _, name := range []string{"notes.txt", "script.sh", "data.json"} {
		if err := Validate(name, "this: is: not: yaml: [["); err != nil {
			t.Errorf("Validate(%s) = %v; want nil — only YAML is checked", name, err)
		}
	}
	for _, name := range []string{"a.yml", "b.yaml", "COMPOSE.YML"} {
		if err := Validate(name, "a: [unclosed"); err == nil {
			t.Errorf("Validate(%s) accepted broken YAML", name)
		}
	}
}

// Lost-update protection: a save must be refused if the file moved underneath.
func TestStaleWritesAreRefused(t *testing.T) {
	f, cfg, root := newFS(t)
	owner := ownership.Owner{UID: -1, GID: -1, FileMode: 0o644}
	write(t, root, "shared.txt", []byte("v1"))

	doc, err := Read(f, cfg, "/shared.txt")
	if err != nil {
		t.Fatal(err)
	}
	// Another writer gets there first.
	time.Sleep(1100 * time.Millisecond) // mtime granularity is a second on some filesystems
	write(t, root, "shared.txt", []byte("v2 from another app"))

	if _, err := Write(f, owner, "/shared.txt", "v3 from the editor", doc.ModTime); !errors.Is(err, ErrStale) {
		t.Fatalf("stale write error = %v; want ErrStale", err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "shared.txt"))
	if string(b) != "v2 from another app" {
		t.Errorf("the other writer's content was clobbered: %q", b)
	}
	// With no baseMtime (a new file, or a client that did not send one) the
	// write proceeds — the check is opt-in by the client sending what it saw.
	if _, err := Write(f, owner, "/shared.txt", "v3", time.Time{}); err != nil {
		t.Errorf("write without a baseMtime was refused: %v", err)
	}
}

// Writing is atomic and leaves no temp file behind on success.
func TestWriteIsAtomicAndTidy(t *testing.T) {
	f, cfg, root := newFS(t)
	owner := ownership.Owner{UID: -1, GID: -1, FileMode: 0o644}
	if _, err := Write(f, owner, "/new.txt", "created", time.Time{}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "new.txt"))
	if err != nil || string(b) != "created" {
		t.Errorf("new file = %q, %v", b, err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v; want just the saved file", names)
	}
	doc, _ := Read(f, cfg, "/new.txt")
	if doc.Content != "created" {
		t.Errorf("round trip = %q", doc.Content)
	}
}

func TestLanguageMapping(t *testing.T) {
	for name, want := range map[string]string{
		"compose.yml": "yaml", "a.yaml": "yaml", "b.json": "json",
		"README.md": "markdown", "page.html": "html", "s.css": "css",
		"app.ts": "javascript", "run.sh": "text", "Dockerfile": "text",
		"noext": "text", "x.unknown": "text",
	} {
		if got := Language(name); got != want {
			t.Errorf("Language(%q) = %q; want %q", name, got, want)
		}
	}
}

// The line number is returned as its own field so the editor can jump to it, so
// it must not also be left in the message — the UI renders "Line N: " itself and
// would otherwise show it twice.
func TestSyntaxErrorMessageDoesNotRepeatTheLine(t *testing.T) {
	err := Validate("compose.yml", "services:\n  web:\n   bad: [unclosed\n")
	var se *SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("error = %v; want *SyntaxError", err)
	}
	if se.Line == 0 {
		t.Fatal("no line number reported")
	}
	if strings.HasPrefix(se.Message, "line ") {
		t.Errorf("Message = %q; the line prefix should be stripped (Line is %d)", se.Message, se.Line)
	}
}
