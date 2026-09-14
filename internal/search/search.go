// Package search does a bounded, index-free name search of a subtree.
//
// No index, no background crawler, no state. That is the whole design: the
// moment this needs an index it becomes a service with a lifecycle — build,
// invalidate, repair, store — and /DATA/Media is exactly the tree that makes an
// index expensive. A bounded walk answers "where did I put that file" well
// enough, and says so honestly when it ran out of budget.
package search

import (
	"context"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/yundera/files/internal/config"
	"github.com/yundera/files/internal/listing"
	"github.com/yundera/files/internal/vfs"
)

// MaxResults caps one response. Past a couple of hundred hits the query is the
// problem, not the limit.
const MaxResults = 200

// Result is one page of matches.
type Result struct {
	Query   string          `json:"query"`
	Root    string          `json:"root"`
	Entries []listing.Entry `json:"entries"`
	// Truncated is set when a bound was hit, so the UI can say "showing the
	// first N" instead of implying the search was exhaustive.
	Truncated bool          `json:"truncated"`
	Elapsed   time.Duration `json:"-"`
	ElapsedMs int64         `json:"elapsedMs"`
}

// Run walks from virtual looking for entries whose name contains q, case
// insensitively.
func Run(f *vfs.FS, cfg config.Config, virtual, q string) (*Result, error) {
	rel, err := f.Clean(virtual)
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(strings.TrimSpace(q))
	res := &Result{Query: q, Root: "/" + strings.TrimPrefix(rel, "."), Entries: []listing.Entry{}}
	if res.Root == "/." || rel == "." {
		res.Root = "/"
	}
	if needle == "" {
		return res, nil
	}

	// Whichever bound is hit first ends the walk. A context deadline is what
	// makes a search over /DATA/Media return something rather than hanging the
	// request until the client gives up.
	ctx, cancel := context.WithTimeout(context.Background(), cfg.SearchTimeout)
	defer cancel()

	start := time.Now()
	baseDepth := strings.Count(rel, "/")
	if rel == "." {
		baseDepth = -1
	}

	// WalkDir over Root().FS() rather than filepath.Walk: it carries DirEntry
	// and only stats on demand, so a directory the search merely passes through
	// costs one readdir instead of one lstat per file. On a large media tree
	// that difference is the whole budget.
	walkErr := fs.WalkDir(f.Root().FS(), rel, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable subdirectory must not fail the whole search — skip
			// it and keep going.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		select {
		case <-ctx.Done():
			res.Truncated = true
			return fs.SkipAll
		default:
		}

		// The app's own state directory is not part of the user's tree.
		if f.Hidden(p) {
			return fs.SkipDir
		}
		if p == rel {
			return nil
		}

		name := d.Name()
		// Dotfiles and dot-directories are skipped entirely: a search should not
		// surface .git internals, and descending into them is most of the cost
		// on a source tree.
		if strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		if d.IsDir() && strings.Count(p, "/")-baseDepth >= cfg.SearchMaxDepth {
			return fs.SkipDir
		}

		if strings.Contains(strings.ToLower(name), needle) {
			e := listing.Entry{
				Name: name,
				Path: "/" + p,
				Kind: listing.KindFile,
			}
			switch {
			case d.Type()&fs.ModeSymlink != 0:
				e.Kind = listing.KindSymlink
			case d.IsDir():
				e.Kind = listing.KindDir
			}
			if fi, err := d.Info(); err == nil {
				e.Size = fi.Size()
				e.ModTime = fi.ModTime().UTC()
				e.Mode = fi.Mode().String()
			}
			if e.Kind != listing.KindDir {
				e.Ext = ext(name)
				e.Thumb = listing.Thumbable(e.Ext)
			}
			res.Entries = append(res.Entries, e)
			if len(res.Entries) >= MaxResults {
				res.Truncated = true
				return fs.SkipAll
			}
		}
		return nil
	})
	if walkErr != nil && !res.Truncated {
		return nil, walkErr
	}

	// Directories first, then by name — the same ordering as a listing, so
	// results do not feel like a different kind of view.
	sort.SliceStable(res.Entries, func(i, j int) bool {
		a, b := res.Entries[i], res.Entries[j]
		if (a.Kind == listing.KindDir) != (b.Kind == listing.KindDir) {
			return a.Kind == listing.KindDir
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})

	res.Elapsed = time.Since(start)
	res.ElapsedMs = res.Elapsed.Milliseconds()
	return res, nil
}

func ext(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 || i == len(name)-1 {
		return ""
	}
	return strings.ToLower(name[i+1:])
}

var _ = path.Join
