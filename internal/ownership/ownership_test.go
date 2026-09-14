package ownership

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func statOf(t *testing.T, p string) (uid, gid int, mode os.FileMode) {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	st := fi.Sys().(*syscall.Stat_t)
	return int(st.Uid), int(st.Gid), fi.Mode().Perm()
}

// A newly created file takes PUID:PGID and the configured mode. Requires root to
// chown to an arbitrary uid, which the golang container provides; skip rather
// than fail elsewhere so `go test` still works for a non-root developer.
func TestCreatedFileGetsTheConfiguredOwnerAndMode(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root to chown to an arbitrary uid")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "new.txt")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	Owner{UID: 1234, GID: 5678, FileMode: 0o664}.ApplyFile(f)
	f.Close()

	uid, gid, mode := statOf(t, p)
	if uid != 1234 || gid != 5678 {
		t.Errorf("owner = %d:%d; want 1234:5678", uid, gid)
	}
	if mode != 0o664 {
		t.Errorf("mode = %#o; want 0664", mode)
	}
}

// The mode must be applied even when the process umask would have cleared the
// bits. This is the whole reason modes are set explicitly instead of via a
// umask: a umask can only clear bits, so under the usual 022 it can never
// produce the group-writable result that lets another uid in the same group
// write to an uploaded file.
func TestModeSurvivesAHostileUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)

	dir := t.TempDir()
	p := filepath.Join(dir, "grouped.txt")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	Owner{UID: -1, GID: -1, FileMode: 0o664}.ApplyFile(f)
	f.Close()

	if _, _, mode := statOf(t, p); mode != 0o664 {
		t.Errorf("mode = %#o under umask 077; want 0664 — group-writable is the point", mode)
	}
}

// THE ASYMMETRY. Overwriting an existing file must leave its uid, gid and mode
// alone. Editing an app's config.yaml must not re-home it to PUID and break the
// app that owns it. If this test is deleted or "simplified", that breaks
// silently and only shows up as a broken app on a user's box.
func TestOverwritePreservesTheOriginalOwnerAndMode(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root to chown to an arbitrary uid")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "app-config.yaml")
	if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(p, 70, 70); err != nil { // an app's own uid, as postgres would be
		t.Fatal(err)
	}

	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	saved := Capture(fi)

	// Rewrite it the way the editor does, then put the ownership back.
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("new"); err != nil {
		t.Fatal(err)
	}
	if !saved.Restore(f) {
		t.Fatal("Restore reported nothing to restore for an existing file")
	}
	f.Close()

	uid, gid, mode := statOf(t, p)
	if uid != 70 || gid != 70 {
		t.Errorf("owner after overwrite = %d:%d; want it left at 70:70", uid, gid)
	}
	if mode != 0o600 {
		t.Errorf("mode after overwrite = %#o; want it left at 0600", mode)
	}
}

// Capture on a file that does not exist must yield a no-op, so callers can
// capture unconditionally without first testing for existence.
func TestCaptureOfNothingRestoresNothing(t *testing.T) {
	var zero Preserved
	f, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if zero.Restore(f) {
		t.Error("a zero Preserved reported that it restored something")
	}
}
