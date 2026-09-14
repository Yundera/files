// Package jobs runs copy, move and delete in the background with progress and
// cancellation.
//
// The shape is maison's (internal/apps, internal/installer): an Event the worker
// emits, a JSON-tagged State with a phase string and a STICKY error, a Start<Op>
// that returns only up-front errors and then detaches, a snapshot accessor, and
// an OnProgress hook the server throttles. One thing is new here — maison has no
// cancellable job anywhere, because its jobs are installs that must not be
// abandoned half-done. A 50 GB copy is different: the user who started it by
// mistake needs a way out.
//
// Jobs live in memory only. A restart loses the list, not the work already done;
// persisting a queue would mean a database, which this app deliberately has not
// got.
package jobs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"sync"
	"time"

	"github.com/yundera/files/internal/config"
	"github.com/yundera/files/internal/fsop"
	"github.com/yundera/files/internal/ownership"
	"github.com/yundera/files/internal/trash"
	"github.com/yundera/files/internal/vfs"
)

// Kind is what the job is doing. The UI picks an icon and a verb from it.
type Kind string

const (
	KindCopy   Kind = "copy"
	KindMove   Kind = "move"
	KindDelete Kind = "delete"
)

// Phases. Done and Error are terminal.
const (
	PhaseRunning   = "running"
	PhaseDone      = "done"
	PhaseError     = "error"
	PhaseCancelled = "cancelled"
)

// ErrCancelled unwinds a worker when the user cancels.
var ErrCancelled = errors.New("cancelled")

// State is the wire snapshot of one job.
type State struct {
	ID    string `json:"id"`
	Kind  Kind   `json:"kind"`
	Phase string `json:"phase"`
	// Message names what is being worked on right now, for the status line.
	Message string `json:"message"`
	Dest    string `json:"dest,omitempty"`

	Done  int64 `json:"done"`
	Total int64 `json:"total"`
	Items int   `json:"items"`
	// TotalItems is the top-level selection size, not a recursive file count:
	// counting every file inside a deep tree up front would mean walking it
	// twice.
	TotalItems int `json:"totalItems"`

	// Error is sticky — it stays on the job so the tray can show why it failed
	// rather than having the row vanish.
	Error string `json:"error,omitempty"`

	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitempty"`
}

// Pct is computed client-side from Done/Total where Total is known; a job whose
// total is zero shows an indeterminate bar.

// Registry owns every running and recently finished job.
type Registry struct {
	fs    *vfs.FS
	owner ownership.Owner
	trash *trash.Store

	mu      sync.Mutex
	jobs    map[string]*State
	cancels map[string]chan struct{}

	// OnChange fires on a phase transition, OnProgress on every byte update.
	// The server throttles OnProgress; OnChange is left immediate so a job
	// finishing is never delayed. Nil is tolerated.
	OnChange   func()
	OnProgress func()
	// OnDirChanged names a directory whose contents changed, so an open listing
	// can refresh itself.
	OnDirChanged func(virtual string)
}

func New(f *vfs.FS, cfg config.Config, tr *trash.Store) *Registry {
	return &Registry{
		fs:      f,
		owner:   ownership.FromConfig(cfg),
		trash:   tr,
		jobs:    map[string]*State{},
		cancels: map[string]chan struct{}{},
	}
}

// retain is how long a finished job stays visible in the tray before it is
// pruned. Long enough to read, short enough not to accumulate.
const retain = 60 * time.Second

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (r *Registry) changed() {
	if r.OnChange != nil {
		r.OnChange()
	}
}

func (r *Registry) progressed() {
	if r.OnProgress != nil {
		r.OnProgress()
	}
}

func (r *Registry) dirChanged(virtual string) {
	if r.OnDirChanged != nil {
		r.OnDirChanged(virtual)
	}
}

// States returns a snapshot, pruning jobs that finished a while ago.
func (r *Registry) States() []State {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]State, 0, len(r.jobs))
	for id, st := range r.jobs {
		if !st.FinishedAt.IsZero() && time.Since(st.FinishedAt) > retain {
			delete(r.jobs, id)
			delete(r.cancels, id)
			continue
		}
		out = append(out, *st)
	}
	return out
}

// Cancel asks a job to stop at the next item or chunk boundary.
//
// Cooperative rather than immediate: a half-written destination file is worse
// than a few more megabytes copied, so the worker finishes the chunk it is on,
// closes the file and stops. Already-copied items are left in place — a
// cancelled copy is a partial copy, and pretending otherwise would mean an
// undo that can itself fail.
func (r *Registry) Cancel(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch, ok := r.cancels[id]
	if !ok {
		return fmt.Errorf("no such job")
	}
	select {
	case <-ch: // already cancelled
	default:
		close(ch)
	}
	return nil
}

// start registers a job and launches its worker.
func (r *Registry) start(kind Kind, sources []string, dest string, work func(st *State, cancel <-chan struct{}) error) (string, error) {
	if len(sources) == 0 {
		return "", errors.New("nothing selected")
	}
	id := newID()
	st := &State{
		ID:         id,
		Kind:       kind,
		Phase:      PhaseRunning,
		Dest:       dest,
		TotalItems: len(sources),
		Message:    "Starting",
		StartedAt:  time.Now().UTC(),
	}
	cancel := make(chan struct{})

	r.mu.Lock()
	r.jobs[id] = st
	r.cancels[id] = cancel
	r.mu.Unlock()
	r.changed()

	// context.Background, not the request context: the job must outlive the
	// request that asked for it. This is maison's rule and the reason its jobs
	// survive a page reload.
	go func() {
		err := work(st, cancel)

		r.mu.Lock()
		st.FinishedAt = time.Now().UTC()
		switch {
		case errors.Is(err, ErrCancelled):
			st.Phase = PhaseCancelled
			st.Message = "Cancelled"
		case err != nil:
			st.Phase = PhaseError
			st.Error = err.Error()
			st.Message = err.Error()
		default:
			st.Phase = PhaseDone
			st.Message = "Done"
		}
		r.mu.Unlock()
		r.changed()
	}()
	return id, nil
}

// cancelled turns a closed channel into ErrCancelled, for use inside a worker.
func cancelled(ch <-chan struct{}) error {
	select {
	case <-ch:
		return ErrCancelled
	default:
		return nil
	}
}

// measure totals the selection so the progress bar has a denominator.
func (r *Registry) measure(rels []string) (bytes int64, files int) {
	for _, rel := range rels {
		b, c := fsop.TreeSize(r.fs.Root(), rel)
		bytes += b
		files += c
	}
	return
}

// resolveAll validates every source path up front, so a typo in the third of ten
// paths fails the request rather than half-completing the job.
func (r *Registry) resolveAll(paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		rel, err := r.fs.Clean(p)
		if err != nil {
			return nil, err
		}
		if rel == "." {
			return nil, errors.New("the data root itself cannot be moved, copied or deleted")
		}
		out = append(out, rel)
	}
	return out, nil
}

// StartCopy copies a selection into dest.
func (r *Registry) StartCopy(sources []string, dest string, policy fsop.Policy) (string, error) {
	srcRels, err := r.resolveAll(sources)
	if err != nil {
		return "", err
	}
	destRel, err := r.fs.Clean(dest)
	if err != nil {
		return "", err
	}
	if err := r.refuseIntoItself(srcRels, destRel); err != nil {
		return "", err
	}

	return r.start(KindCopy, sources, dest, func(st *State, cancel <-chan struct{}) error {
		total, files := r.measure(srcRels)
		r.mu.Lock()
		st.Total, st.TotalItems = total, files
		r.mu.Unlock()
		r.progressed()

		root := r.fs.Root()
		var base int64
		for _, srcRel := range srcRels {
			if err := cancelled(cancel); err != nil {
				return err
			}
			name := path.Base(srcRel)
			target, err := fsop.Resolve(root, path.Join(destRel, name), policy)
			if errors.Is(err, fsop.ErrSkipped) {
				continue
			}
			if err != nil {
				return err
			}
			r.mu.Lock()
			st.Message = name
			r.mu.Unlock()

			err = fsop.CopyTree(root, srcRel, target, r.owner, func(done int64, cur string) error {
				if err := cancelled(cancel); err != nil {
					return err
				}
				r.mu.Lock()
				st.Done = base + done
				r.mu.Unlock()
				r.progressed()
				return nil
			})
			if err != nil {
				return err
			}
			b, _ := fsop.TreeSize(root, target)
			base += b
			r.mu.Lock()
			st.Done, st.Items = base, st.Items+1
			r.mu.Unlock()
			r.progressed()
		}
		r.dirChanged(dest)
		return nil
	})
}

// StartMove moves a selection into dest.
//
// A move is a rename, so it is instant and its progress is per item rather than
// per byte — except when fsop.Move has to fall back to copy-then-delete across a
// filesystem boundary, where the bytes really do move.
func (r *Registry) StartMove(sources []string, dest string, policy fsop.Policy) (string, error) {
	srcRels, err := r.resolveAll(sources)
	if err != nil {
		return "", err
	}
	destRel, err := r.fs.Clean(dest)
	if err != nil {
		return "", err
	}
	if err := r.refuseIntoItself(srcRels, destRel); err != nil {
		return "", err
	}

	return r.start(KindMove, sources, dest, func(st *State, cancel <-chan struct{}) error {
		root := r.fs.Root()
		parents := map[string]bool{}
		for _, srcRel := range srcRels {
			if err := cancelled(cancel); err != nil {
				return err
			}
			name := path.Base(srcRel)
			target, err := fsop.Resolve(root, path.Join(destRel, name), policy)
			if errors.Is(err, fsop.ErrSkipped) {
				continue
			}
			if err != nil {
				return err
			}
			r.mu.Lock()
			st.Message = name
			r.mu.Unlock()

			if err := fsop.Move(root, srcRel, target, r.owner); err != nil {
				return err
			}
			parents["/"+path.Dir(srcRel)] = true
			r.mu.Lock()
			st.Items++
			r.mu.Unlock()
			r.progressed()
		}
		for p := range parents {
			r.dirChanged(p)
		}
		r.dirChanged(dest)
		return nil
	})
}

// StartDelete moves a selection to the trash.
func (r *Registry) StartDelete(paths []string) (string, error) {
	srcRels, err := r.resolveAll(paths)
	if err != nil {
		return "", err
	}
	return r.start(KindDelete, paths, "", func(st *State, cancel <-chan struct{}) error {
		parents := map[string]bool{}
		for _, rel := range srcRels {
			if err := cancelled(cancel); err != nil {
				return err
			}
			r.mu.Lock()
			st.Message = path.Base(rel)
			r.mu.Unlock()
			if _, err := r.trash.Put("/" + rel); err != nil {
				return err
			}
			parents["/"+path.Dir(rel)] = true
			r.mu.Lock()
			st.Items++
			r.mu.Unlock()
			r.progressed()
		}
		for p := range parents {
			r.dirChanged(p)
		}
		return nil
	})
}

// refuseIntoItself rejects copying or moving a directory into its own subtree.
//
// Without this, copying /a into /a/b recurses until the disk fills: the copy
// keeps discovering the files it is itself creating. The check is cheap and the
// failure mode is not recoverable by the user.
func (r *Registry) refuseIntoItself(srcRels []string, destRel string) error {
	for _, src := range srcRels {
		if destRel == src {
			return fmt.Errorf("%q cannot be copied into itself", path.Base(src))
		}
		if len(destRel) > len(src) && destRel[:len(src)] == src && destRel[len(src)] == '/' {
			return fmt.Errorf("%q cannot be copied into its own subfolder", path.Base(src))
		}
	}
	return nil
}
