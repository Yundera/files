package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func post(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// awaitJobs polls until no job is running, so a test can assert on the result of
// an asynchronous operation without sleeping a fixed amount.
func awaitJobs(t *testing.T, h http.Handler) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var states []struct {
			Phase string `json:"phase"`
		}
		_ = json.Unmarshal(get(t, h, "/api/jobs").Body.Bytes(), &states)
		running := false
		for _, s := range states {
			if s.Phase == "running" {
				running = true
			}
		}
		if !running {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("jobs did not settle")
}

// A name is checked as a NAME, with a message about the name — not passed
// through to the path validator, which would report "invalid path" to someone
// who just typed a folder name.
func TestNewNamesAreValidatedAsNames(t *testing.T) {
	h, _ := newFSServer(t)
	for _, name := range []string{"", ".", "..", "a/b", `a\b`, strings.Repeat("x", 300)} {
		body, _ := json.Marshal(map[string]string{"path": "/", "name": name})
		rec := post(t, h, "/api/fs/mkdir", string(body))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("mkdir name %q = %d %s; want 400", name, rec.Code, rec.Body.String())
		}
	}
}

// Creating something that already exists is a 409, which is what raises the
// conflict dialog. Anything else and the UI cannot tell a collision from a
// failure.
func TestCollisionsAnswer409(t *testing.T) {
	h, root := newFSServer(t)
	if err := os.Mkdir(filepath.Join(root, "Taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Both at the root (for mkdir/touch) and in Documents, which is where the
	// rename below lands.
	for _, p := range []string{"taken.txt", filepath.Join("Documents", "taken.txt")} {
		if err := os.WriteFile(filepath.Join(root, p), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct{ route, body string }{
		{"/api/fs/mkdir", `{"path":"/","name":"Taken"}`},
		{"/api/fs/touch", `{"path":"/","name":"taken.txt"}`},
		{"/api/fs/rename", `{"path":"/Documents/note.txt","newName":"taken.txt"}`},
	} {
		if rec := post(t, h, c.route, c.body); rec.Code != http.StatusConflict {
			t.Errorf("%s %s = %d %s; want 409", c.route, c.body, rec.Code, rec.Body.String())
		}
	}
}

// Creating a file must never truncate one that is already there. This is the
// O_EXCL guarantee and it is the difference between "that name is taken" and
// silent data loss.
func TestTouchNeverTruncatesAnExistingFile(t *testing.T) {
	h, root := newFSServer(t)
	post(t, h, "/api/fs/touch", `{"path":"/Documents","name":"note.txt"}`)
	b, err := os.ReadFile(filepath.Join(root, "Documents", "note.txt"))
	if err != nil || string(b) != "hello" {
		t.Errorf("existing file = %q, %v; want it untouched", b, err)
	}
}

func TestMkdirAndRenameRoundTrip(t *testing.T) {
	h, root := newFSServer(t)
	if rec := post(t, h, "/api/fs/mkdir", `{"path":"/Documents","name":"New Folder"}`); rec.Code != http.StatusOK {
		t.Fatalf("mkdir = %d %s", rec.Code, rec.Body.String())
	}
	if fi, err := os.Stat(filepath.Join(root, "Documents", "New Folder")); err != nil || !fi.IsDir() {
		t.Fatalf("directory not created: %v", err)
	}
	if rec := post(t, h, "/api/fs/rename", `{"path":"/Documents/New Folder","newName":"Renamed"}`); rec.Code != http.StatusOK {
		t.Fatalf("rename = %d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "Documents", "Renamed")); err != nil {
		t.Errorf("renamed directory missing: %v", err)
	}
}

// Delete answers 202 with a job id — the signal that the work is not done when
// the response arrives — and routes the file to the trash rather than destroying
// it.
func TestDeleteReturnsAJobAndFillsTheTrash(t *testing.T) {
	h, root := newFSServer(t)
	rec := post(t, h, "/api/fs/delete", `{"paths":["/Documents/note.txt"]}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("delete = %d %s; want 202", rec.Code, rec.Body.String())
	}
	var started struct {
		JobID string `json:"jobId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil || started.JobID == "" {
		t.Fatalf("delete body = %s; want a jobId", rec.Body.String())
	}
	awaitJobs(t, h)

	if _, err := os.Stat(filepath.Join(root, "Documents", "note.txt")); !os.IsNotExist(err) {
		t.Error("the file is still at its original path")
	}
	var trash struct {
		Items []struct {
			ID           string `json:"id"`
			OriginalPath string `json:"originalPath"`
		} `json:"items"`
		Size int64 `json:"size"`
	}
	if err := json.Unmarshal(get(t, h, "/api/trash").Body.Bytes(), &trash); err != nil {
		t.Fatal(err)
	}
	if len(trash.Items) != 1 || trash.Items[0].OriginalPath != "/Documents/note.txt" {
		t.Fatalf("trash = %+v; want the deleted file", trash)
	}
	if trash.Size != 5 {
		t.Errorf("trash size = %d; want 5 — the Trash view has to be able to say where the disk went", trash.Size)
	}

	// And it comes back.
	body, _ := json.Marshal(map[string]any{"ids": []string{trash.Items[0].ID}, "conflict": "skip"})
	if rec := post(t, h, "/api/trash/restore", string(body)); rec.Code != http.StatusOK {
		t.Fatalf("restore = %d %s", rec.Code, rec.Body.String())
	}
	if b, err := os.ReadFile(filepath.Join(root, "Documents", "note.txt")); err != nil || string(b) != "hello" {
		t.Errorf("restored file = %q, %v", b, err)
	}
}

// Job routes must not be swallowed by a wildcard, and cancelling something that
// does not exist is a 404 rather than a silent success.
func TestCancelOfAnUnknownJobIs404(t *testing.T) {
	h, _ := newFSServer(t)
	if rec := post(t, h, "/api/jobs/nope/cancel", ""); rec.Code != http.StatusNotFound {
		t.Errorf("cancel unknown = %d %s; want 404", rec.Code, rec.Body.String())
	}
}

// Every mutating route must refuse a path into the state directory, not just the
// read routes.
func TestMutationsRefuseTheStateDir(t *testing.T) {
	h, _ := newFSServer(t)
	for _, c := range []struct{ route, body string }{
		{"/api/fs/mkdir", `{"path":"/AppData/files","name":"x"}`},
		{"/api/fs/touch", `{"path":"/AppData/files/trash","name":"x"}`},
		{"/api/fs/rename", `{"path":"/AppData/files","newName":"x"}`},
		{"/api/fs/delete", `{"paths":["/AppData/files"]}`},
		{"/api/fs/copy", `{"sources":["/AppData/files"],"dest":"/Documents"}`},
		{"/api/fs/move", `{"sources":["/Documents/note.txt"],"dest":"/AppData/files"}`},
	} {
		rec := post(t, h, c.route, c.body)
		if rec.Code < 400 {
			t.Errorf("%s %s = %d %s; want a refusal", c.route, c.body, rec.Code, rec.Body.String())
		}
	}
}

// A full tus round trip through the real router.
//
// This exists because of a bug that unit tests could not have caught: tusd's
// UnroutedHandler expects its router to have stripped the base path already
// (extractIDFromPath is strings.Trim(path, "/")). Without http.StripPrefix the
// POST succeeds and looks healthy while every PATCH answers 404, so only an
// end-to-end request through the mounted routes shows it.
func TestTusUploadRoundTripThroughTheRouter(t *testing.T) {
	h, root := newFSServer(t)

	body := strings.Repeat("payload-", 512) // 4096 bytes, sent in two chunks
	meta := "filename " + base64.StdEncoding.EncodeToString([]byte("up.bin")) +
		",dest " + base64.StdEncoding.EncodeToString([]byte("/Documents"))

	req := httptest.NewRequest(http.MethodPost, "/api/tus/", nil)
	req.Header.Set("Tus-Resumable", "1.0.0")
	req.Header.Set("Upload-Length", strconv.Itoa(len(body)))
	req.Header.Set("Upload-Metadata", meta)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s; want 201", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatal("create returned no Location header")
	}
	id := loc[strings.LastIndexByte(loc, '/')+1:]

	half := len(body) / 2
	for i, chunk := range []string{body[:half], body[half:]} {
		offset := i * half
		req = httptest.NewRequest(http.MethodPatch, "/api/tus/"+id, strings.NewReader(chunk))
		req.Header.Set("Tus-Resumable", "1.0.0")
		req.Header.Set("Upload-Offset", strconv.Itoa(offset))
		req.Header.Set("Content-Type", "application/offset+octet-stream")
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("chunk %d at offset %d = %d %s; want 204", i, offset, rec.Code, rec.Body.String())
		}
		want := strconv.Itoa(offset + len(chunk))
		if got := rec.Header().Get("Upload-Offset"); got != want {
			t.Fatalf("chunk %d: Upload-Offset = %q; want %q", i, got, want)
		}
	}

	// The commit is asynchronous (it runs off the CompleteUploads channel).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(filepath.Join(root, "Documents", "up.bin")); err == nil {
			if string(b) != body {
				t.Fatalf("uploaded file is %d bytes; want %d", len(b), len(body))
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the upload never landed at its destination")
}
