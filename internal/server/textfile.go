package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/yundera/files/internal/textfile"
)

func (s *Server) handleReadText(w http.ResponseWriter, r *http.Request) {
	if !s.requireFS(w) {
		return
	}
	doc, err := textfile.Read(s.fs, s.cfg, r.URL.Query().Get("path"))
	if err != nil {
		textError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) handleWriteText(w http.ResponseWriter, r *http.Request) {
	if !s.requireFS(w) {
		return
	}
	var body struct {
		Path      string    `json:"path"`
		Content   string    `json:"content"`
		BaseMTime time.Time `json:"baseMtime"`
	}
	if !decode(w, r, &body) {
		return
	}
	if int64(len(body.Content)) > s.cfg.EditMaxBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "that content is larger than the edit limit")
		return
	}
	mtime, err := textfile.Write(s.fs, s.owner, body.Path, body.Content, body.BaseMTime)
	if err != nil {
		textError(w, err)
		return
	}
	s.broadcastDir(parentOf(body.Path))
	writeJSON(w, http.StatusOK, map[string]any{"modTime": mtime})
}

// textError maps editor failures onto statuses the UI branches on.
//
// The three that matter: 409 means "someone else changed it, reload before you
// overwrite them", 422 means "your YAML does not parse, here is the line", and
// 413 means "too big to open". Collapsing any of them into a 500 would leave the
// UI unable to say anything useful.
func textError(w http.ResponseWriter, err error) {
	var se *textfile.SyntaxError
	switch {
	case errors.As(err, &se):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": se.Message,
			"line":  se.Line,
		})
	case errors.Is(err, textfile.ErrStale):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, textfile.ErrTooLarge):
		writeErr(w, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, textfile.ErrBinary):
		writeErr(w, http.StatusUnsupportedMediaType, err.Error())
	default:
		fsError(w, err)
	}
}
