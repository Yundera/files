package server

import (
	"bytes"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/yundera/files/internal/search"
	"github.com/yundera/files/internal/thumb"
)

func (s *Server) handleThumb(w http.ResponseWriter, r *http.Request) {
	if !s.requireFS(w) || s.thumbs == nil {
		if s.fs != nil {
			writeErr(w, http.StatusServiceUnavailable, "thumbnails are not available")
		}
		return
	}
	q := r.URL.Query()
	width, _ := strconv.Atoi(q.Get("w"))
	if width <= 0 {
		width = 256
	}

	b, err := s.thumbs.Get(q.Get("path"), width)
	if err != nil {
		if errors.Is(err, thumb.ErrUnsupported) {
			// 415, not 500 or 404: the file is there, we just cannot decode it.
			// The grid falls back to a kind icon on any non-200, so this is
			// purely about not logging a real error for an ordinary HEIC.
			writeErr(w, http.StatusUnsupportedMediaType, err.Error())
			return
		}
		fsError(w, err)
		return
	}

	// A thumbnail's content is fixed by its cache key, which folds in the
	// source's mtime and size — so it can be cached hard and never revalidated.
	// A changed file produces a different URL, not a stale hit.
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "thumb.jpg", time.Time{}, bytes.NewReader(b))
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if !s.requireFS(w) {
		return
	}
	q := r.URL.Query()
	res, err := search.Run(s.fs, s.cfg, q.Get("path"), q.Get("q"))
	if err != nil {
		fsError(w, err)
		return
	}
	res.Entries = orEmpty(res.Entries)
	writeJSON(w, http.StatusOK, res)
}
