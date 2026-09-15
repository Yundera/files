package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yundera/files/internal/config"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		DataRoot: t.TempDir(),
		DirMode:  0o775,
		FileMode: 0o664,
		// Negative ids mean "leave ownership alone", which is what lets this run
		// as an unprivileged test user: a real chown to 1000:1000 would fail.
		PUID: -1,
		PGID: -1,
	}
}

func TestEnsureRootsCreatesMissing(t *testing.T) {
	cfg := testConfig(t)

	EnsureRoots(cfg)

	for _, name := range Roots {
		fi, err := os.Stat(filepath.Join(cfg.DataRoot, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !fi.IsDir() {
			t.Fatalf("%s is not a directory", name)
		}
		if got := fi.Mode().Perm(); got != cfg.DirMode {
			t.Errorf("%s mode = %o, want %o", name, got, cfg.DirMode)
		}
	}
}

// The second boot must be a no-op, not a re-stamp: an operator who chmodded
// Documents, or an existing directory owned by another uid, has to survive a
// restart untouched.
func TestEnsureRootsLeavesExistingAlone(t *testing.T) {
	cfg := testConfig(t)
	existing := filepath.Join(cfg.DataRoot, "Documents")
	if err := os.Mkdir(existing, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(existing, "keep.txt")
	if err := os.WriteFile(marker, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}

	EnsureRoots(cfg)
	EnsureRoots(cfg)

	fi, err := os.Stat(existing)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o700 {
		t.Errorf("mode = %o, want 0700 (existing directory was re-stamped)", got)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("contents lost: %v", err)
	}
}

// A data root that does not exist is a misconfiguration, not a crash: the server
// still comes up and every listing reports the reason.
func TestEnsureRootsSurvivesMissingDataRoot(t *testing.T) {
	cfg := testConfig(t)
	cfg.DataRoot = filepath.Join(cfg.DataRoot, "nope")

	EnsureRoots(cfg) // must not panic

	if _, err := os.Stat(cfg.DataRoot); !os.IsNotExist(err) {
		t.Errorf("data root was created; it must not be: %v", err)
	}
}

// A root that exists as a FILE cannot be turned into a directory. The app has to
// log and carry on rather than die on someone else's stray file.
func TestEnsureRootsSurvivesFileInTheWay(t *testing.T) {
	cfg := testConfig(t)
	blocker := filepath.Join(cfg.DataRoot, "Media")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}

	EnsureRoots(cfg)

	// The blocker is untouched and the other roots were still created.
	if fi, err := os.Stat(blocker); err != nil || fi.IsDir() {
		t.Errorf("Media = %v (err %v), want the original file", fi, err)
	}
	if fi, err := os.Stat(filepath.Join(cfg.DataRoot, "Documents")); err != nil || !fi.IsDir() {
		t.Errorf("Documents missing after a failure on Media: %v", err)
	}
}
