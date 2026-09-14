package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/yundera/files/internal/config"
)

// newServer builds the real handler the way every test here does: a throwaway
// data root and no UI. fstest.MapFS{} stands in for "vite has not run".
func newServer(t *testing.T) http.Handler {
	t.Helper()
	return New(config.Config{DataRoot: t.TempDir()}, fstest.MapFS{})
}

func call(t *testing.T, h http.Handler, method, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// The gate lets api/health through unauthenticated and the orchestrator polls
// it, so it must answer without touching the filesystem. A data root that does
// not exist must still produce a 200.
func TestHealthAnswersWithoutTouchingTheFilesystem(t *testing.T) {
	h := New(config.Config{DataRoot: "/definitely/not/here"}, fstest.MapFS{})
	code, body := call(t, h, http.MethodGet, "/api/health")
	if code != http.StatusOK {
		t.Fatalf("GET /api/health = %d %s; want 200", code, body)
	}
	var got struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil || got.Status != "ok" {
		t.Errorf("GET /api/health body = %s; want {\"status\":\"ok\"}", body)
	}
}

// The UI must not hardcode limits an operator can change, so /api/config
// reports them. This pins that the handler reads the Config it was given rather
// than the package defaults.
func TestConfigReportsTheConfiguredLimits(t *testing.T) {
	h := New(config.Config{DataRoot: t.TempDir(), EditMaxBytes: 4242, SearchMaxDepth: 3}, fstest.MapFS{})
	_, body := call(t, h, http.MethodGet, "/api/config")
	var got struct {
		EditMaxBytes   int64 `json:"editMaxBytes"`
		SearchMaxDepth int   `json:"searchMaxDepth"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal %s: %v", body, err)
	}
	if got.EditMaxBytes != 4242 || got.SearchMaxDepth != 3 {
		t.Errorf("GET /api/config = %s; want editMaxBytes 4242 and searchMaxDepth 3", body)
	}
}

// Unknown paths fall through to the SPA so client-side routing works. With no
// UI built, that surfaces as the "ui not built" 500 rather than a 404 — which is
// itself the assertion: a 404 would mean the API router swallowed the path.
func TestUnknownPathsReachTheSPAAndNotThe404(t *testing.T) {
	h := newServer(t)
	for _, p := range []string{"/", "/browse/Documents", "/trash"} {
		code, _ := call(t, h, http.MethodGet, p)
		if code == http.StatusNotFound {
			t.Errorf("GET %s = 404; want the SPA fallback to handle it", p)
		}
	}
}

// A real index.html must be served for a client-side route, not just for "/".
func TestSPAServesIndexForAClientRoute(t *testing.T) {
	ui := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html>hello")}}
	h := New(config.Config{DataRoot: t.TempDir()}, ui)
	code, body := call(t, h, http.MethodGet, "/browse/Documents/deep")
	if code != http.StatusOK || body != "<!doctype html>hello" {
		t.Errorf("GET /browse/... = %d %q; want 200 and index.html", code, body)
	}
}
