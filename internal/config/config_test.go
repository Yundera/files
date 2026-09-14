package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A malformed mode must not abort the boot. This pins the policy documented at
// the top of config.go: fall back and warn, never fatal. If someone "improves"
// envMode into a hard error, a single typo in a compose file stops the file
// manager from starting at all.
func TestMalformedValuesFallBackInsteadOfFailing(t *testing.T) {
	for _, c := range []struct {
		key, val string
		check    func(Config) bool
		want     string
	}{
		{"DIR_MODE", "not-a-mode", func(c Config) bool { return c.DirMode == 0o775 }, "0775"},
		{"DIR_MODE", "99999999", func(c Config) bool { return c.DirMode == 0o775 }, "0775"},
		{"FILE_MODE", "", func(c Config) bool { return c.FileMode == 0o664 }, "0664"},
		{"PUID", "nope", func(c Config) bool { return c.PUID == 1000 }, "1000"},
		{"PUID", "-5", func(c Config) bool { return c.PUID == 1000 }, "1000"},
		{"THUMB_CACHE_MB", "banana", func(c Config) bool { return c.ThumbCacheBytes == 512<<20 }, "512MB"},
	} {
		t.Setenv(c.key, c.val)
		if got := FromEnv(); !c.check(got) {
			t.Errorf("%s=%q did not fall back to %s", c.key, c.val, c.want)
		}
		os.Unsetenv(c.key)
	}
}

// All three octal spellings a compose file can produce must mean the same thing.
// YAML strips the leading zero from an unquoted 0644, so "644" reaches us for a
// value the author wrote as 0644.
func TestModeAcceptsEveryOctalSpelling(t *testing.T) {
	for _, spelling := range []string{"755", "0755", "0o755"} {
		t.Setenv("DIR_MODE", spelling)
		if got := FromEnv().DirMode; got != 0o755 {
			t.Errorf("DIR_MODE=%q -> %#o; want 0755", spelling, got)
		}
	}
}

// The default state dir must sit inside the data root. That is what makes
// delete-to-trash a single os.Root.Rename; a state dir outside it would need a
// cross-root rename, which os.Root does not offer.
func TestStateDirDefaultsInsideTheDataRoot(t *testing.T) {
	c := Config{DataRoot: "/DATA"}
	if want := filepath.Join("/DATA", "AppData", "files"); c.StateDir() != want {
		t.Errorf("StateDir() = %q; want %q", c.StateDir(), want)
	}
	if !c.StateDirInsideRoot() {
		t.Error("the default StateDir must report as inside the data root")
	}
	for _, sub := range []string{c.TrashDir(), c.ThumbDir(), c.UploadDir()} {
		if filepath.Dir(sub) != c.StateDir() {
			t.Errorf("%q is not directly under StateDir %q", sub, c.StateDir())
		}
	}
}

// An operator can move STATE_DIR, and moving it out of the tree has to be
// detectable so the server can refuse rather than fail every rename at runtime.
func TestStateDirOutsideTheRootIsDetected(t *testing.T) {
	for _, c := range []struct {
		name          string
		cfg           Config
		wantContained bool
	}{
		{"default", Config{DataRoot: "/DATA"}, true},
		{"explicit subdir", Config{DataRoot: "/DATA", StateDirPath: "/DATA/AppData/x"}, true},
		{"sibling", Config{DataRoot: "/DATA", StateDirPath: "/var/lib/files"}, false},
		{"parent", Config{DataRoot: "/DATA/sub", StateDirPath: "/DATA"}, false},
		{"equal to root", Config{DataRoot: "/DATA", StateDirPath: "/DATA"}, false},
		{"traversal", Config{DataRoot: "/DATA", StateDirPath: "/DATA/../etc"}, false},
	} {
		if got := c.cfg.StateDirInsideRoot(); got != c.wantContained {
			t.Errorf("%s: StateDirInsideRoot() = %v; want %v (state dir %q, root %q)",
				c.name, got, c.wantContained, c.cfg.StateDir(), c.cfg.DataRoot)
		}
	}
}
