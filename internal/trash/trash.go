// Package trash implements delete-to-trash, restore and purge.
//
// Layout, under ${STATE_DIR}/trash:
//
//	<id>/meta.json          where it came from, how big it was, when
//	<id>/payload/<name>     the file or directory itself, original name kept
//
// Per-item metadata rather than one index file: two concurrent deletes never
// contend on the same file, and a corrupted meta.json costs exactly one item's
// restore path instead of the whole trash. Keeping the original basename inside
// payload/ means the trash is still legible with `ls` when something goes wrong.
//
// Deleting is a rename, so it is instant regardless of size. That is only true
// because the state dir lives inside the data root (see config.StateDirInsideRoot)
// and a PCS has a single filesystem; fsop.Move carries the EXDEV fallback for
// the day that stops holding.
package trash

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yundera/files/internal/config"
	"github.com/yundera/files/internal/fsop"
	"github.com/yundera/files/internal/ownership"
	"github.com/yundera/files/internal/vfs"
)

// Item is one trashed thing.
type Item struct {
	ID string `json:"id"`
	// OriginalPath is the virtual path it came from, which is where Restore
	// puts it back.
	OriginalPath string    `json:"originalPath"`
	Name         string    `json:"name"`
	IsDir        bool      `json:"isDir"`
	Size         int64     `json:"size"`
	Count        int       `json:"count"`
	DeletedAt    time.Time `json:"deletedAt"`
}

// Store owns the trash directory.
type Store struct {
	fs    *vfs.FS
	cfg   config.Config
	owner ownership.Owner

	// mu serialises directory-level operations (create, purge, empty) so a purge
	// cannot delete the directory a concurrent Put is writing into.
	mu sync.Mutex

	// OnChange fires after anything in the trash changes, so the server can
	// broadcast. Nil is tolerated.
	OnChange func()
}

func New(f *vfs.FS, cfg config.Config) *Store {
	return &Store{fs: f, cfg: cfg, owner: ownership.FromConfig(cfg)}
}

// rel returns a path inside the trash, relative to the data root.
func (s *Store) rel(parts ...string) string {
	return path.Join(append([]string{s.fs.StateRel(), "trash"}, parts...)...)
}

func (s *Store) changed() {
	if s.OnChange != nil {
		s.OnChange()
	}
}

// newID returns a time-ordered, filesystem-safe id.
//
// The timestamp prefix means a lexical sort of the directory is a chronological
// sort, which is what the purge needs to find the oldest items without reading
// every meta.json. The random suffix keeps two deletes in the same second apart.
func newID(now time.Time) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b[:])
}

// Put moves a virtual path into the trash.
func (s *Store) Put(virtual string) (Item, error) {
	srcRel, err := s.fs.Clean(virtual)
	if err != nil {
		return Item{}, err
	}
	if srcRel == "." {
		return Item{}, errors.New("the data root itself cannot be deleted")
	}
	root := s.fs.Root()
	fi, err := root.Lstat(srcRel)
	if err != nil {
		return Item{}, err
	}

	// Size is measured before the move. For a directory this is a walk, which
	// makes an O(1) rename into an O(n) operation — accepted, because without it
	// the Trash view cannot tell the user how much disk it is holding, and a
	// walk is still orders of magnitude cheaper than a copy.
	size, count := fsop.TreeSize(root, srcRel)

	item := Item{
		ID:           newID(time.Now()),
		OriginalPath: "/" + srcRel,
		Name:         path.Base(srcRel),
		IsDir:        fi.IsDir(),
		Size:         size,
		Count:        count,
		DeletedAt:    time.Now().UTC(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	payloadDir := s.rel(item.ID, "payload")
	if err := root.MkdirAll(payloadDir, s.owner.DirMode); err != nil {
		return Item{}, fmt.Errorf("prepare trash entry: %w", err)
	}
	if err := fsop.Move(root, srcRel, path.Join(payloadDir, item.Name), s.owner); err != nil {
		_ = root.RemoveAll(s.rel(item.ID))
		return Item{}, err
	}
	if err := s.writeMeta(item); err != nil {
		// The payload moved but the metadata did not. Put it back rather than
		// leave an item that can never be restored because nothing records
		// where it came from.
		_ = fsop.Move(root, path.Join(payloadDir, item.Name), srcRel, s.owner)
		_ = root.RemoveAll(s.rel(item.ID))
		return Item{}, fmt.Errorf("record trash entry: %w", err)
	}
	s.changed()
	return item, nil
}

func (s *Store) writeMeta(item Item) error {
	b, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return err
	}
	root := s.fs.Root()
	// Write to a temp name in the same directory and rename, so a crash mid-write
	// cannot leave a half-parsed meta.json.
	tmp := s.rel(item.ID, "meta.json.partial")
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, s.owner.FileMode)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	s.owner.ApplyFile(f)
	if err := f.Close(); err != nil {
		return err
	}
	return root.Rename(tmp, s.rel(item.ID, "meta.json"))
}

func (s *Store) readMeta(id string) (Item, error) {
	b, err := s.fs.Root().ReadFile(s.rel(id, "meta.json"))
	if err != nil {
		return Item{}, err
	}
	var item Item
	if err := json.Unmarshal(b, &item); err != nil {
		return Item{}, err
	}
	if item.ID == "" {
		item.ID = id
	}
	return item, nil
}

// List returns the trash, newest first.
func (s *Store) List() ([]Item, error) {
	root := s.fs.Root()
	dir, err := root.Open(s.rel())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil // nothing has been deleted yet
	}
	if err != nil {
		return nil, err
	}
	names, err := dir.Readdirnames(-1)
	_ = dir.Close()
	if err != nil {
		return nil, err
	}

	items := make([]Item, 0, len(names))
	for _, id := range names {
		item, err := s.readMeta(id)
		if err != nil {
			// A corrupt or half-written entry must not hide the rest of the
			// trash. Skip it; Purge will eventually clear it.
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].DeletedAt.After(items[j].DeletedAt) })
	return items, nil
}

// Size reports the total the trash is holding.
func (s *Store) Size() int64 {
	items, _ := s.List()
	var total int64
	for _, it := range items {
		total += it.Size
	}
	return total
}

// Restore puts an item back where it came from.
func (s *Store) Restore(id string, policy fsop.Policy) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, err := s.readMeta(id)
	if err != nil {
		return "", fmt.Errorf("no such trash item")
	}
	// The recorded path came from a Clean at delete time, but it has been on
	// disk since — re-validate rather than trust it.
	destRel, err := s.fs.Clean(item.OriginalPath)
	if err != nil {
		return "", err
	}
	root := s.fs.Root()

	// The original parent may have been deleted in the meantime. Recreate it
	// with the configured ownership rather than failing the restore.
	if parent := path.Dir(destRel); parent != "." && parent != "/" {
		if err := root.MkdirAll(parent, s.owner.DirMode); err != nil {
			return "", err
		}
	}
	finalRel, err := fsop.Resolve(root, destRel, policy)
	if err != nil {
		return "", err
	}
	if err := fsop.Move(root, s.rel(id, "payload", item.Name), finalRel, s.owner); err != nil {
		return "", err
	}
	if err := root.RemoveAll(s.rel(id)); err != nil {
		return "", err
	}
	s.changed()
	return "/" + finalRel, nil
}

// Delete removes items permanently.
func (s *Store) Delete(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		if !validID(id) {
			return fmt.Errorf("invalid trash id")
		}
		if err := s.fs.Root().RemoveAll(s.rel(id)); err != nil {
			return err
		}
	}
	s.changed()
	return nil
}

// Empty removes everything.
func (s *Store) Empty() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	root := s.fs.Root()
	if err := root.RemoveAll(s.rel()); err != nil {
		return err
	}
	if err := root.MkdirAll(s.rel(), s.owner.DirMode); err != nil {
		return err
	}
	s.changed()
	return nil
}

// validID rejects anything that is not a name this package generated.
//
// Ids arrive from the client, and every one of them is joined onto a path. The
// vfs would catch a traversal anyway, but the trash builds its paths from
// StateRel and so goes around Clean — this is the check that stands in for it.
func validID(id string) bool {
	if id == "" || len(id) > 64 || strings.ContainsAny(id, "/\\.") {
		return false
	}
	for _, r := range id {
		ok := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '-'
		if !ok {
			return false
		}
	}
	return true
}

// Purge enforces the retention period and the size cap. Oldest first.
func (s *Store) Purge() (removed int, err error) {
	items, err := s.List()
	if err != nil {
		return 0, err
	}
	// Oldest first, which is both the retention order and the eviction order.
	sort.Slice(items, func(i, j int) bool { return items[i].DeletedAt.Before(items[j].DeletedAt) })

	var total int64
	for _, it := range items {
		total += it.Size
	}

	var doomed []string
	cutoff := time.Now().Add(-s.cfg.TrashRetention)
	keep := items[:0:0]
	for _, it := range items {
		if s.cfg.TrashRetention > 0 && it.DeletedAt.Before(cutoff) {
			doomed = append(doomed, it.ID)
			total -= it.Size
			continue
		}
		keep = append(keep, it)
	}
	if s.cfg.TrashMaxBytes > 0 {
		for _, it := range keep {
			if total <= s.cfg.TrashMaxBytes {
				break
			}
			doomed = append(doomed, it.ID)
			total -= it.Size
		}
	}
	if len(doomed) == 0 {
		return 0, nil
	}
	if err := s.Delete(doomed); err != nil {
		return 0, err
	}
	return len(doomed), nil
}

// RunPurgeLoop purges at startup and daily thereafter.
func (s *Store) RunPurgeLoop() {
	purge := func() {
		if n, err := s.Purge(); err != nil {
			// Not fatal: a trash that cannot be purged is a disk-space problem,
			// not a reason to stop serving files.
			log.Printf("trash: purge failed: %v", err)
		} else if n > 0 {
			log.Printf("trash: purged %d expired item(s)", n)
		}
	}
	purge()
	go func() {
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for range t.C {
			purge()
		}
	}()
}
