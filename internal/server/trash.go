package server

import (
	"net/http"

	"github.com/yundera/files/internal/fsop"
	"github.com/yundera/files/internal/trash"
)

func (s *Server) requireTrash(w http.ResponseWriter) bool {
	if s.trash == nil {
		writeErr(w, http.StatusServiceUnavailable, "data root is not available")
		return false
	}
	return true
}

// handleTrashList returns the trash plus its total size.
//
// The size is reported rather than left implicit because the trash is real disk:
// without it a user who deletes 40 GB sees their free space drop and has no way
// to connect the two.
func (s *Server) handleTrashList(w http.ResponseWriter, r *http.Request) {
	if !s.requireTrash(w) {
		return
	}
	items, err := s.trash.List()
	if err != nil {
		fsError(w, err)
		return
	}
	var total int64
	for _, it := range items {
		total += it.Size
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": orEmpty(items),
		"size":  total,
	})
}

func (s *Server) handleTrashRestore(w http.ResponseWriter, r *http.Request) {
	if !s.requireTrash(w) {
		return
	}
	var body struct {
		IDs      []string    `json:"ids"`
		Conflict fsop.Policy `json:"conflict"`
	}
	if !decode(w, r, &body) {
		return
	}
	restored := make([]string, 0, len(body.IDs))
	for _, id := range body.IDs {
		p, err := s.trash.Restore(id, body.Conflict)
		if err != nil {
			// Report the partial result alongside the failure: the user needs to
			// know which of five items came back before the sixth collided.
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":    err.Error(),
				"restored": orEmpty(restored),
			})
			return
		}
		restored = append(restored, p)
		s.broadcastDir(parentOf(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"restored": orEmpty(restored)})
}

func (s *Server) handleTrashDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireTrash(w) {
		return
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := s.trash.Delete(body.IDs); err != nil {
		fsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleTrashEmpty(w http.ResponseWriter, r *http.Request) {
	if !s.requireTrash(w) {
		return
	}
	if err := s.trash.Empty(); err != nil {
		fsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

var _ = trash.Item{}

func parentOf(p string) string {
	for i := len(p) - 1; i > 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return "/"
}
