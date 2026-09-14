// Package listing reads a directory and turns it into a sorted, paginated page
// of entries for the API.
package listing

import (
	"errors"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yundera/files/internal/vfs"
)

// Kind is what the UI switches on to pick an icon and a click behaviour.
type Kind string

const (
	KindDir     Kind = "dir"
	KindFile    Kind = "file"
	KindSymlink Kind = "symlink"
)

// Entry is one row or tile.
type Entry struct {
	Name string `json:"name"`
	Path string `json:"path"` // virtual path, so the UI never has to join
	Kind Kind   `json:"kind"`
	Size int64  `json:"size"`
	// ModTime is RFC 3339 UTC. The browser formats it; a Go binary formatting in
	// TZ and a browser in another zone would disagree.
	ModTime time.Time `json:"modTime"`
	Mode    string    `json:"mode"`
	Ext     string    `json:"ext,omitempty"`
	// Thumb is true when this entry has a format the thumbnailer can decode, so
	// the grid knows whether to request one at all.
	Thumb bool `json:"thumb,omitempty"`
}

// Page is one slice of a directory.
type Page struct {
	Path    string  `json:"path"`
	Entries []Entry `json:"entries"`
	// Cursor is the opaque token for the next page, empty at the end.
	Cursor string `json:"cursor,omitempty"`
	Total  int    `json:"total"`
	// Truncated reports that the directory holds more than MaxEntries and the
	// listing is incomplete. Better to say so than to imply the folder is small.
	Truncated bool `json:"truncated,omitempty"`
}

// Options control ordering and paging.
type Options struct {
	Sort   string // name | size | modified | kind
	Desc   bool
	Hidden bool // include dotfiles
	Cursor string
	Limit  int
}

const (
	// DefaultLimit is one screen's worth with room to scroll.
	DefaultLimit = 500
	MaxLimit     = 5000

	// MaxEntries caps how much of one directory is read into memory.
	//
	// A real /DATA/Downloads can hold tens of thousands of files. Sorting
	// requires the whole directory, so there is no way to page without reading
	// it — but there is a limit past which the honest answer is "this is
	// truncated" rather than an unbounded allocation and a hung tab.
	MaxEntries = 100_000

	// readBatch is how many entries are pulled per ReadDir call. Batching rather
	// than os.ReadDir avoids a single enormous allocation for a huge directory.
	readBatch = 4096
)

// thumbExts are the formats internal/thumb can decode with pure Go. HEIC, AVIF
// and RAW are deliberately absent: no pure-Go decoder, so the grid shows a kind
// icon instead of a broken image.
var thumbExts = map[string]bool{
	"jpg": true, "jpeg": true, "png": true, "gif": true, "webp": true, "bmp": true,
}

// Thumbable reports whether an extension has a pure-Go decoder. Exported so
// search results advertise thumbnails on the same basis a listing does — two
// copies of this set would drift.
func Thumbable(ext string) bool { return thumbExts[ext] }

// Read lists one directory.
func Read(f *vfs.FS, virtual string, opt Options) (*Page, error) {
	rel, err := f.Clean(virtual)
	if err != nil {
		return nil, err
	}
	dir, err := f.Root().Open(rel)
	if err != nil {
		return nil, err
	}
	defer dir.Close()

	if fi, err := dir.Stat(); err == nil && !fi.IsDir() {
		return nil, errors.New("not a directory")
	}

	entries := make([]Entry, 0, 128)
	truncated := false
	for len(entries) < MaxEntries {
		batch, err := dir.ReadDir(readBatch)
		for _, de := range batch {
			name := de.Name()
			if !opt.Hidden && strings.HasPrefix(name, ".") {
				continue
			}
			childRel := path.Join(rel, name)
			if rel == "." {
				childRel = name
			}
			// Skip the app's own state directory rather than failing on it: it
			// is simply not part of the user's tree.
			if f.Hidden(childRel) {
				continue
			}
			entries = append(entries, toEntry(de, childRel))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
	}
	if len(entries) >= MaxEntries {
		truncated = true
	}

	sortEntries(entries, opt.Sort, opt.Desc)

	limit := opt.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	offset := decodeCursor(opt.Cursor)
	if offset > len(entries) {
		offset = len(entries)
	}
	end := offset + limit
	if end > len(entries) {
		end = len(entries)
	}

	page := &Page{
		Path:      "/" + strings.TrimPrefix(rel, "."),
		Entries:   entries[offset:end],
		Total:     len(entries),
		Truncated: truncated,
	}
	if page.Path == "/" || rel == "." {
		page.Path = "/"
	}
	if end < len(entries) {
		page.Cursor = strconv.Itoa(end)
	}
	return page, nil
}

func toEntry(de os.DirEntry, childRel string) Entry {
	e := Entry{
		Name: de.Name(),
		Path: "/" + childRel,
		Kind: KindFile,
	}
	switch {
	case de.Type()&os.ModeSymlink != 0:
		e.Kind = KindSymlink
	case de.IsDir():
		e.Kind = KindDir
	}
	// Info is an lstat per entry and is the expensive part of a listing. It is
	// unavoidable: size and mtime are both columns.
	if fi, err := de.Info(); err == nil {
		e.Size = fi.Size()
		e.ModTime = fi.ModTime().UTC()
		e.Mode = fi.Mode().String()
	}
	if e.Kind != KindDir {
		e.Ext = ext(de.Name())
		e.Thumb = Thumbable(e.Ext)
	}
	return e
}

// ext returns the lowercased extension without the dot. A leading dot does not
// start an extension: ".bashrc" is a dotfile, not a "bashrc" file.
func ext(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 || i == len(name)-1 {
		return ""
	}
	return strings.ToLower(name[i+1:])
}

// sortEntries orders a page. Directories always come first regardless of the
// key or direction, matching CasaOS — reversing the sort must not scatter
// folders through the file list.
func sortEntries(entries []Entry, key string, desc bool) {
	less := func(a, b Entry) bool {
		switch key {
		case "size":
			if a.Size != b.Size {
				return a.Size < b.Size
			}
		case "modified":
			if !a.ModTime.Equal(b.ModTime) {
				return a.ModTime.Before(b.ModTime)
			}
		case "kind":
			if a.Ext != b.Ext {
				return a.Ext < b.Ext
			}
		}
		// Name is both the default key and the tiebreaker for every other key,
		// so the order is total and paging cannot repeat or skip an entry.
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if (a.Kind == KindDir) != (b.Kind == KindDir) {
			return a.Kind == KindDir
		}
		if desc {
			return less(b, a)
		}
		return less(a, b)
	})
}

func decodeCursor(c string) int {
	if c == "" {
		return 0
	}
	n, err := strconv.Atoi(c)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
