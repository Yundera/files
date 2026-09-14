package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/yundera/files/internal/config"
)

// newFSServer builds the real server over a populated temp tree.
func newFSServer(t *testing.T) (http.Handler, string) {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"Documents", "AppData/files/trash"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "Documents", "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	return New(config.Config{DataRoot: root}, fstest.MapFS{}), root
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// The status code is the machine-readable half of the error contract, so each
// class of bad path must map to its own status. A traversal is a client bug
// (400); the hidden state dir must look absent (404), never 403, which would
// confirm something is there.
func TestBadPathsMapToTheRightStatus(t *testing.T) {
	h, _ := newFSServer(t)
	for _, c := range []struct {
		name, path string
		want       int
	}{
		{"traversal", "/api/fs/list?path=/../etc", http.StatusBadRequest},
		{"deep traversal", "/api/fs/list?path=/Documents/../../etc", http.StatusBadRequest},
		{"backslash", `/api/fs/list?path=/Documents\..\etc`, http.StatusBadRequest},
		{"state dir", "/api/fs/list?path=/AppData/files", http.StatusNotFound},
		{"state dir child", "/api/fs/list?path=/AppData/files/trash", http.StatusNotFound},
		{"missing", "/api/fs/list?path=/nope", http.StatusNotFound},
		{"raw traversal", "/api/fs/raw?path=/../etc/passwd", http.StatusBadRequest},
		{"raw state dir", "/api/fs/raw?path=/AppData/files/trash", http.StatusNotFound},
		{"stat traversal", "/api/fs/stat?path=/..", http.StatusBadRequest},
	} {
		if rec := get(t, h, c.path); rec.Code != c.want {
			t.Errorf("%s: GET %s = %d %s; want %d", c.name, c.path, rec.Code, rec.Body.String(), c.want)
		}
	}
}

// Downloads must never be served in a way that lets uploaded markup execute on
// this app's origin. The app has no auth of its own and sits behind a shared
// gate, so script running here runs inside the user's authenticated session.
func TestRawForcesAttachmentForExecutableTypes(t *testing.T) {
	h, root := newFSServer(t)
	for _, name := range []string{"evil.html", "evil.svg", "script.js", "doc.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("<script>x</script>"), 0o644); err != nil {
			t.Fatal(err)
		}
		// Even with inline explicitly requested, these must download.
		rec := get(t, h, "/api/fs/raw?inline=1&path=/"+name)
		if got := rec.Header().Get("Content-Disposition"); got == "" || got[:10] != "attachment" {
			t.Errorf("%s: Content-Disposition = %q; want attachment even with inline=1", name, got)
		}
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q; want nosniff", name, got)
		}
	}
}

// Media must still be displayable inline, or no viewer works.
func TestRawAllowsInlineForMedia(t *testing.T) {
	h, root := newFSServer(t)
	if err := os.WriteFile(filepath.Join(root, "pic.png"), []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := get(t, h, "/api/fs/raw?inline=1&path=/pic.png")
	if got := rec.Header().Get("Content-Disposition"); got == "" || got[:6] != "inline" {
		t.Errorf("Content-Disposition = %q; want inline for a png", got)
	}
	// Without inline=1 the same file must download.
	rec = get(t, h, "/api/fs/raw?path=/pic.png")
	if got := rec.Header().Get("Content-Disposition"); got[:10] != "attachment" {
		t.Errorf("Content-Disposition = %q; want attachment by default", got)
	}
}

// Range support is what makes video seeking and PDF page-jumps work, and it
// comes from http.ServeContent. This pins that raw actually goes through it.
func TestRawSupportsRangeRequests(t *testing.T) {
	h, root := newFSServer(t)
	if err := os.WriteFile(filepath.Join(root, "big.bin"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/fs/raw?path=/big.bin", nil)
	req.Header.Set("Range", "bytes=2-5")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("ranged GET = %d; want 206", rec.Code)
	}
	if got := rec.Body.String(); got != "2345" {
		t.Errorf("ranged body = %q; want %q", got, "2345")
	}
}

// Asking to download a folder is a reasonable thing for a user to try, and the
// answer is that this app has no archiver — not a 400 or a 500.
func TestRawRefusesAFolderWithAnExplanation(t *testing.T) {
	h, _ := newFSServer(t)
	rec := get(t, h, "/api/fs/raw?path=/Documents")
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("GET raw on a folder = %d %s; want 415", rec.Code, rec.Body.String())
	}
	var body struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Error == "" {
		t.Error("a refusal must carry an explanation the UI can show")
	}
}

// An empty directory must serialise as [] and not null, or every client needs a
// null check on a value it was told is a list.
func TestEmptyListingIsAnArrayNotNull(t *testing.T) {
	h, root := newFSServer(t)
	if err := os.Mkdir(filepath.Join(root, "Empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := get(t, h, "/api/fs/list?path=/Empty").Body.String()
	if !contains(body, `"entries":[]`) {
		t.Errorf("empty listing body = %s; want entries to be []", body)
	}
}

func contains(hay, needle string) bool {
	return len(hay) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(hay); i++ {
			if hay[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
