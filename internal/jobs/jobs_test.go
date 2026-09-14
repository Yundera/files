package jobs

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yundera/files/internal/config"
	"github.com/yundera/files/internal/fsop"
	"github.com/yundera/files/internal/trash"
	"github.com/yundera/files/internal/vfs"
)

func newReg(t *testing.T) (*Registry, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DataRoot: root, PUID: -1, PGID: -1, DirMode: 0o755, FileMode: 0o644,
		TrashRetention: 30 * 24 * time.Hour}
	f, err := vfs.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return New(f, cfg, trash.New(f, cfg)), root
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// await waits for a job to leave the running phase.
func await(t *testing.T, r *Registry, id string) State {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, st := range r.States() {
			if st.ID == id && st.Phase != PhaseRunning {
				return st
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish in time", id)
	return State{}
}

func TestCopyDuplicatesATreeAndReportsProgress(t *testing.T) {
	r, root := newReg(t)
	write(t, root, "Documents/proj/a.txt", "aaa")
	write(t, root, "Documents/proj/sub/b.txt", "bb")
	if err := os.MkdirAll(filepath.Join(root, "Target"), 0o755); err != nil {
		t.Fatal(err)
	}

	id, err := r.StartCopy([]string{"/Documents/proj"}, "/Target", fsop.Skip)
	if err != nil {
		t.Fatalf("StartCopy: %v", err)
	}
	st := await(t, r, id)
	if st.Phase != PhaseDone {
		t.Fatalf("phase = %s (%s); want done", st.Phase, st.Error)
	}
	for rel, want := range map[string]string{
		"Target/proj/a.txt":     "aaa",
		"Target/proj/sub/b.txt": "bb",
	} {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || string(b) != want {
			t.Errorf("%s = %q, %v; want %q", rel, b, err, want)
		}
	}
	// The source must still be there — this is a copy, not a move.
	if _, err := os.Stat(filepath.Join(root, "Documents", "proj", "a.txt")); err != nil {
		t.Error("copy removed the source")
	}
	if st.Done != 5 || st.Total != 5 {
		t.Errorf("done/total = %d/%d; want 5/5", st.Done, st.Total)
	}
}

// Copying a directory into its own subtree recurses until the disk fills,
// because the copy keeps finding the files it is itself creating. It must be
// refused up front, synchronously, not discovered by the worker.
func TestCopyIntoItselfIsRefusedBeforeTheJobStarts(t *testing.T) {
	r, root := newReg(t)
	write(t, root, "Documents/proj/a.txt", "a")
	if err := os.MkdirAll(filepath.Join(root, "Documents", "proj", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dest := range []string{"/Documents/proj", "/Documents/proj/sub"} {
		if _, err := r.StartCopy([]string{"/Documents/proj"}, dest, fsop.Skip); err == nil {
			t.Errorf("StartCopy into %q was accepted; it must be refused", dest)
		}
	}
	// A sibling whose name merely shares a prefix is NOT inside it.
	if err := os.MkdirAll(filepath.Join(root, "Documents", "proj-backup"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := r.StartCopy([]string{"/Documents/proj"}, "/Documents/proj-backup", fsop.Skip); err != nil {
		t.Errorf("StartCopy into a prefix-sharing sibling was refused: %v", err)
	}
}

func TestMoveRelocatesAndLeavesNothingBehind(t *testing.T) {
	r, root := newReg(t)
	write(t, root, "Documents/note.txt", "hi")
	if err := os.MkdirAll(filepath.Join(root, "Target"), 0o755); err != nil {
		t.Fatal(err)
	}
	id, err := r.StartMove([]string{"/Documents/note.txt"}, "/Target", fsop.Skip)
	if err != nil {
		t.Fatal(err)
	}
	if st := await(t, r, id); st.Phase != PhaseDone {
		t.Fatalf("phase = %s (%s); want done", st.Phase, st.Error)
	}
	if _, err := os.Stat(filepath.Join(root, "Documents", "note.txt")); !os.IsNotExist(err) {
		t.Error("the source survived a move")
	}
	b, err := os.ReadFile(filepath.Join(root, "Target", "note.txt"))
	if err != nil || string(b) != "hi" {
		t.Errorf("moved file = %q, %v", b, err)
	}
}

// Delete routes through the trash rather than destroying anything.
func TestDeleteGoesToTheTrashNotToOblivion(t *testing.T) {
	r, root := newReg(t)
	write(t, root, "Documents/bye.txt", "gone?")
	id, err := r.StartDelete([]string{"/Documents/bye.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if st := await(t, r, id); st.Phase != PhaseDone {
		t.Fatalf("phase = %s (%s); want done", st.Phase, st.Error)
	}
	if _, err := os.Stat(filepath.Join(root, "Documents", "bye.txt")); !os.IsNotExist(err) {
		t.Error("the file is still at its original path")
	}
	items, err := r.trash.List()
	if err != nil || len(items) != 1 || items[0].OriginalPath != "/Documents/bye.txt" {
		t.Errorf("trash = %+v, %v; want one item from /Documents/bye.txt", items, err)
	}
}

// Cancel stops a copy partway. Maison has no cancellable job, so this is the
// piece with no precedent to copy — worth pinning that it actually takes effect
// and reports the right terminal phase.
func TestCancelStopsACopyAndMarksItCancelled(t *testing.T) {
	r, root := newReg(t)
	// Enough data that the copy cannot finish before the cancel lands.
	big := make([]byte, 6<<20)
	for i := 0; i < 8; i++ {
		p := filepath.Join(root, "Documents", "big", "f"+string(rune('a'+i)))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, big, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "Target"), 0o755); err != nil {
		t.Fatal(err)
	}

	id, err := r.StartCopy([]string{"/Documents/big"}, "/Target", fsop.Skip)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Cancel(id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	st := await(t, r, id)
	if st.Phase != PhaseCancelled {
		t.Errorf("phase = %s (%s); want cancelled", st.Phase, st.Error)
	}
	// Cancelling an unknown job is an error, not a silent success.
	if err := r.Cancel("nope"); err == nil {
		t.Error("Cancel of an unknown job returned nil")
	}
}

// A bad path anywhere in the selection fails the request synchronously, so the
// caller learns about it instead of getting a job that half-completes.
func TestBadSourcesFailBeforeAnyWorkHappens(t *testing.T) {
	r, root := newReg(t)
	write(t, root, "Documents/ok.txt", "x")
	for _, bad := range []string{"/../etc/passwd", "/AppData/files/trash", "/"} {
		if _, err := r.StartCopy([]string{"/Documents/ok.txt", bad}, "/Documents", fsop.Skip); err == nil {
			t.Errorf("StartCopy with source %q was accepted", bad)
		}
	}
	if len(r.States()) != 0 {
		t.Errorf("a rejected request still registered a job: %+v", r.States())
	}
}

// A finished job stays in the tray long enough to read, then is pruned.
func TestFinishedJobsArePrunedEventually(t *testing.T) {
	r, root := newReg(t)
	write(t, root, "Documents/a.txt", "a")
	id, _ := r.StartDelete([]string{"/Documents/a.txt"})
	await(t, r, id)

	if len(r.States()) != 1 {
		t.Fatalf("a just-finished job is not visible: %+v", r.States())
	}
	// Backdate it past the retention window.
	r.mu.Lock()
	r.jobs[id].FinishedAt = time.Now().Add(-2 * retain)
	r.mu.Unlock()
	if got := r.States(); len(got) != 0 {
		t.Errorf("States() = %+v; want the stale job pruned", got)
	}
}
