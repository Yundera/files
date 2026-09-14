package server

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/yundera/files/internal/fsop"
)

// validName checks a single new path segment.
//
// Names arrive from a text box and are joined onto a path. vfs.Clean would catch
// a traversal, but it would report it as an invalid PATH, which is a confusing
// thing to show someone who just typed a folder name — so the name is checked as
// a name, with a message about the name.
func validName(name string) error {
	switch {
	case name == "":
		return errors.New("the name cannot be empty")
	case name == "." || name == "..":
		return errors.New("that name is reserved")
	case strings.ContainsAny(name, `/\`):
		return errors.New("a name cannot contain a slash")
	case strings.ContainsRune(name, 0):
		return errors.New("a name cannot contain a NUL byte")
	case len(name) > 255:
		return errors.New("that name is too long")
	}
	return nil
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return false
	}
	return true
}

func (s *Server) handleMkdir(w http.ResponseWriter, r *http.Request) {
	if !s.requireFS(w) {
		return
	}
	var body struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := validName(body.Name); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	target := path.Join(body.Path, body.Name)
	rel, err := s.fs.Clean(target)
	if err != nil {
		fsError(w, err)
		return
	}
	if err := s.fs.Root().Mkdir(rel, s.owner.DirMode); err != nil {
		if errors.Is(err, fs.ErrExist) {
			// 409 is what raises the conflict dialog in the UI.
			writeErr(w, http.StatusConflict, "a folder with that name already exists")
			return
		}
		fsError(w, err)
		return
	}
	// mkdir's mode is masked by the process umask, so the requested mode is
	// applied explicitly afterwards on the directory's own descriptor.
	if d, err := s.fs.Root().Open(rel); err == nil {
		s.owner.ApplyDir(d)
		_ = d.Close()
	}
	s.broadcastDir(body.Path)
	writeJSON(w, http.StatusOK, map[string]string{"path": "/" + rel})
}

func (s *Server) handleTouch(w http.ResponseWriter, r *http.Request) {
	if !s.requireFS(w) {
		return
	}
	var body struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := validName(body.Name); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	rel, err := s.fs.Clean(path.Join(body.Path, body.Name))
	if err != nil {
		fsError(w, err)
		return
	}
	// O_EXCL so creating a file never truncates one that is already there.
	f, err := s.fs.Root().OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, s.owner.FileMode)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			writeErr(w, http.StatusConflict, "a file with that name already exists")
			return
		}
		fsError(w, err)
		return
	}
	s.owner.ApplyFile(f)
	_ = f.Close()
	s.broadcastDir(body.Path)
	writeJSON(w, http.StatusOK, map[string]string{"path": "/" + rel})
}

// handleRename renames within one directory. Moving between directories is a
// move job, not a rename — keeping them separate is what lets rename stay
// synchronous and instant.
func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	if !s.requireFS(w) {
		return
	}
	var body struct {
		Path    string `json:"path"`
		NewName string `json:"newName"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := validName(body.NewName); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	oldRel, err := s.fs.Clean(body.Path)
	if err != nil {
		fsError(w, err)
		return
	}
	if oldRel == "." {
		writeErr(w, http.StatusBadRequest, "the data root cannot be renamed")
		return
	}
	newRel, err := s.fs.Clean(path.Join(path.Dir(oldRel), body.NewName))
	if err != nil {
		fsError(w, err)
		return
	}
	// Rename would happily clobber an existing file. Check first so the UI can
	// offer a choice rather than silently destroying something.
	if _, err := s.fs.Root().Lstat(newRel); err == nil {
		writeErr(w, http.StatusConflict, "something with that name already exists")
		return
	}
	if err := s.fs.Root().Rename(oldRel, newRel); err != nil {
		fsError(w, err)
		return
	}
	s.broadcastDir("/" + path.Dir(oldRel))
	writeJSON(w, http.StatusOK, map[string]string{"path": "/" + newRel})
}

// jobBody is shared by copy, move and delete.
type jobBody struct {
	Sources  []string    `json:"sources"`
	Paths    []string    `json:"paths"` // delete spells it this way
	Dest     string      `json:"dest"`
	Conflict fsop.Policy `json:"conflict"`
}

// startJob is the common tail of the three job routes: 202 with the id, or the
// synchronous error. Returning 202 rather than 200 is the signal to the client
// that the work is not finished when the response arrives.
func (s *Server) startJob(w http.ResponseWriter, id string, err error) {
	if err != nil {
		fsError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": id})
}

func (s *Server) handleCopy(w http.ResponseWriter, r *http.Request) {
	if !s.requireJobs(w) {
		return
	}
	var body jobBody
	if !decode(w, r, &body) {
		return
	}
	id, err := s.jobs.StartCopy(body.Sources, body.Dest, body.Conflict)
	s.startJob(w, id, err)
}

func (s *Server) handleMove(w http.ResponseWriter, r *http.Request) {
	if !s.requireJobs(w) {
		return
	}
	var body jobBody
	if !decode(w, r, &body) {
		return
	}
	id, err := s.jobs.StartMove(body.Sources, body.Dest, body.Conflict)
	s.startJob(w, id, err)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireJobs(w) {
		return
	}
	var body jobBody
	if !decode(w, r, &body) {
		return
	}
	paths := body.Paths
	if len(paths) == 0 {
		paths = body.Sources
	}
	id, err := s.jobs.StartDelete(paths)
	s.startJob(w, id, err)
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	if !s.requireJobs(w) {
		return
	}
	writeJSON(w, http.StatusOK, orEmpty(s.jobs.States()))
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	if !s.requireJobs(w) {
		return
	}
	if err := s.jobs.Cancel(chi.URLParam(r, "id")); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) requireJobs(w http.ResponseWriter) bool {
	if s.jobs == nil {
		writeErr(w, http.StatusServiceUnavailable, "data root is not available")
		return false
	}
	return true
}
