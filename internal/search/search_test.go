package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yundera/files/internal/config"
	"github.com/yundera/files/internal/vfs"
)

func newFS(t *testing.T, depth int, timeout time.Duration) (*vfs.FS, config.Config, string) {
	t.Helper()
	root := t.TempDir()
	cfg := config.Config{
		DataRoot: root, PUID: -1, PGID: -1, DirMode: 0o755, FileMode: 0o644,
		SearchMaxDepth: depth, SearchTimeout: timeout,
	}
	f, err := vfs.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f, cfg, root
}

func touch(t *testing.T, root, rel string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func names(r *Result) []string {
	out := make([]string, 0, len(r.Entries))
	for _, e := range r.Entries {
		out = append(out, e.Path)
	}
	return out
}

func TestFindsMatchesCaseInsensitivelyAtAnyDepth(t *testing.T) {
	f, cfg, root := newFS(t, 8, 2*time.Second)
	touch(t, root, "Documents/Report-2024.pdf")
	touch(t, root, "Documents/sub/deeper/report_final.txt")
	touch(t, root, "Documents/unrelated.txt")
	if err := os.MkdirAll(filepath.Join(root, "Documents", "Reports"), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := Run(f, cfg, "/Documents", "REPORT")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(names(res), " ")
	for _, want := range []string{"/Documents/Report-2024.pdf", "/Documents/sub/deeper/report_final.txt", "/Documents/Reports"} {
		if !strings.Contains(got, want) {
			t.Errorf("results %v missing %s", names(res), want)
		}
	}
	if strings.Contains(got, "unrelated") {
		t.Errorf("results %v include a non-match", names(res))
	}
	// Directories sort first, as in a listing.
	if res.Entries[0].Kind != "dir" {
		t.Errorf("first result is %s (%s); want the directory first", res.Entries[0].Name, res.Entries[0].Kind)
	}
}

// The depth bound is what stops a search over a deep tree costing everything.
func TestDepthIsBounded(t *testing.T) {
	f, cfg, root := newFS(t, 2, 2*time.Second)
	touch(t, root, "a/target-shallow.txt")
	touch(t, root, "a/b/c/d/e/target-deep.txt")

	res, err := Run(f, cfg, "/", "target")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(names(res), " ")
	if !strings.Contains(got, "target-shallow") {
		t.Errorf("results %v missing the shallow match", names(res))
	}
	if strings.Contains(got, "target-deep") {
		t.Errorf("results %v include a match past SearchMaxDepth=2", names(res))
	}
}

// The state directory is not part of the user's tree and must never appear.
func TestStateDirIsNeverSearched(t *testing.T) {
	f, cfg, root := newFS(t, 8, 2*time.Second)
	touch(t, root, "AppData/files/trash/01/payload/secret-doc.txt")
	touch(t, root, "AppData/other/secret-doc.txt")

	res, err := Run(f, cfg, "/", "secret")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range names(res) {
		if strings.Contains(p, "AppData/files") {
			t.Errorf("a trashed file leaked into search results: %s", p)
		}
	}
	if len(res.Entries) != 1 {
		t.Errorf("results = %v; want only the one outside the state dir", names(res))
	}
}

// Dotfiles are skipped, and dot-directories are not descended into — most of the
// cost of searching a source tree is .git.
func TestDotfilesAreSkipped(t *testing.T) {
	f, cfg, root := newFS(t, 8, 2*time.Second)
	touch(t, root, "proj/.git/objects/target-in-git.txt")
	touch(t, root, "proj/.target-hidden.txt")
	touch(t, root, "proj/target-visible.txt")

	res, err := Run(f, cfg, "/", "target")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Entries) != 1 || !strings.HasSuffix(res.Entries[0].Path, "target-visible.txt") {
		t.Errorf("results = %v; want only the visible file", names(res))
	}
}

// Hitting the result cap must be reported, not silently implied to be the whole
// answer.
func TestResultCapIsReportedAsTruncated(t *testing.T) {
	f, cfg, root := newFS(t, 8, 5*time.Second)
	for i := 0; i < MaxResults+25; i++ {
		touch(t, root, filepath.Join("many", "hit-"+strings.Repeat("x", i%5)+string(rune('a'+i%26))+string(rune('0'+i/26))+".txt"))
	}
	res, err := Run(f, cfg, "/many", "hit-")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Entries) > MaxResults {
		t.Errorf("returned %d results; the cap is %d", len(res.Entries), MaxResults)
	}
	if !res.Truncated {
		t.Error("the cap was hit but Truncated is false — the UI would imply this was the whole answer")
	}
}

// An empty query is not an error and not a match-everything.
func TestEmptyQueryReturnsNothing(t *testing.T) {
	f, cfg, root := newFS(t, 8, time.Second)
	touch(t, root, "a.txt")
	for _, q := range []string{"", "   "} {
		res, err := Run(f, cfg, "/", q)
		if err != nil {
			t.Fatalf("Run(%q) error: %v", q, err)
		}
		if len(res.Entries) != 0 {
			t.Errorf("Run(%q) returned %v; want nothing", q, names(res))
		}
	}
}

// A bad root is rejected by the vfs, not silently searched from somewhere else.
func TestBadRootIsRefused(t *testing.T) {
	f, cfg, _ := newFS(t, 8, time.Second)
	for _, p := range []string{"/../etc", "/AppData/files"} {
		if _, err := Run(f, cfg, p, "x"); err == nil {
			t.Errorf("Run(%q) was accepted", p)
		}
	}
}
