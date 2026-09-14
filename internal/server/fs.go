package server

import (
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/yundera/files/internal/listing"
	"github.com/yundera/files/internal/vfs"
)

// fsError maps a filesystem or vfs error onto the status the UI branches on.
//
// The status IS the machine-readable half of the error contract (see writeErr),
// so this mapping is load-bearing rather than cosmetic. ErrHidden deliberately
// becomes 404 and not 403: the app's own state directory should look absent, and
// a 403 would confirm that something is there.
func fsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, vfs.ErrHidden), errors.Is(err, fs.ErrNotExist):
		writeErr(w, http.StatusNotFound, "no such file or directory")
	case errors.Is(err, vfs.ErrInvalidPath):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, fs.ErrPermission):
		writeErr(w, http.StatusForbidden, "permission denied")
	case errors.Is(err, fs.ErrExist):
		writeErr(w, http.StatusConflict, "already exists")
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

// requireFS guards every filesystem route. The server comes up even when the
// data root is unopenable, so that /api/health still answers and the operator
// sees a clear message instead of a container that will not start.
func (s *Server) requireFS(w http.ResponseWriter) bool {
	if s.fs == nil {
		writeErr(w, http.StatusServiceUnavailable, "data root is not available")
		return false
	}
	return true
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	if !s.requireFS(w) {
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, err := listing.Read(s.fs, q.Get("path"), listing.Options{
		Sort:   q.Get("sort"),
		Desc:   q.Get("order") == "desc",
		Hidden: q.Get("hidden") == "1",
		Cursor: q.Get("cursor"),
		Limit:  limit,
	})
	if err != nil {
		fsError(w, err)
		return
	}
	page.Entries = orEmpty(page.Entries)
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleStat(w http.ResponseWriter, r *http.Request) {
	if !s.requireFS(w) {
		return
	}
	p := r.URL.Query().Get("path")
	// Lstat, not Stat: the details panel should describe the symlink the user
	// clicked, not silently describe its target.
	fi, err := s.fs.Lstat(p)
	if err != nil {
		fsError(w, err)
		return
	}
	rel, _ := s.fs.Clean(p)
	kind := listing.KindFile
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		kind = listing.KindSymlink
	case fi.IsDir():
		kind = listing.KindDir
	}
	writeJSON(w, http.StatusOK, listing.Entry{
		Name:    fi.Name(),
		Path:    "/" + strings.TrimPrefix(rel, "."),
		Kind:    kind,
		Size:    fi.Size(),
		ModTime: fi.ModTime().UTC(),
		Mode:    fi.Mode().String(),
	})
}

// handleTree returns directories only, for the sidebar.
//
// Depth is capped at 1 on purpose: CasaOS's Files sidebar is a FLAT list, not a
// nested tree with chevrons. Serving deep trees would invite a UI that diverges
// from the thing being copied, and would walk the whole of /DATA/Media to do it.
func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	if !s.requireFS(w) {
		return
	}
	page, err := listing.Read(s.fs, r.URL.Query().Get("path"), listing.Options{Sort: "name", Limit: listing.MaxLimit})
	if err != nil {
		fsError(w, err)
		return
	}
	dirs := make([]listing.Entry, 0, len(page.Entries))
	for _, e := range page.Entries {
		if e.Kind == listing.KindDir {
			dirs = append(dirs, e)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": page.Path, "entries": orEmpty(dirs)})
}

// inlineTypes are the only content types served without a download disposition.
//
// Everything else is forced to `attachment`. That is what stops an uploaded
// .html or .svg from executing as script on this app's own origin — which, since
// the app has no auth of its own and sits behind a shared gate, would run inside
// the user's authenticated session. The allowlist is by media type rather than
// extension so a renamed file cannot slip through.
var inlineTypes = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true,
	"image/bmp": true, "image/x-icon": true,
	"video/mp4": true, "video/webm": true, "video/ogg": true, "video/quicktime": true,
	"audio/mpeg": true, "audio/ogg": true, "audio/wav": true, "audio/flac": true,
	"audio/mp4": true, "audio/aac": true,
	"application/pdf": true,
}

func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request) {
	if !s.requireFS(w) {
		return
	}
	p := r.URL.Query().Get("path")
	f, err := s.fs.Open(p)
	if err != nil {
		fsError(w, err)
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		fsError(w, err)
		return
	}
	if fi.IsDir() {
		// Not 400: asking to download a folder is a reasonable thing to try, and
		// the answer is that this app has no archiver. See README.md.
		writeErr(w, http.StatusUnsupportedMediaType, "a folder cannot be downloaded; use SMB for bulk transfers")
		return
	}

	name := path.Base(p)
	ctype := mime.TypeByExtension(path.Ext(name))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	base, _, _ := strings.Cut(ctype, ";")

	// nosniff is what makes the disposition decision stick: without it a browser
	// may ignore the declared type and execute what it guesses instead.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", ctype)
	if r.URL.Query().Get("inline") == "1" && inlineTypes[strings.TrimSpace(base)] {
		w.Header().Set("Content-Disposition", "inline; filename*=UTF-8''"+urlEncode(name))
	} else {
		w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+urlEncode(name))
	}

	// ServeContent gives Range requests and conditional GETs for free. Range is
	// not optional: it is what makes video seeking and PDF page-jumps work.
	http.ServeContent(w, r, name, fi.ModTime(), f)
}

// urlEncode percent-encodes a filename for RFC 5987 (filename*=UTF-8”...).
//
// url.PathEscape is not usable here: it leaves characters that terminate the
// header value or confuse parsers. Encoding everything outside the RFC's
// attr-char set is the conservative choice.
func urlEncode(s string) string {
	const safe = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!#$&+-.^_`|~"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(safe, s[i]) >= 0 {
			b.WriteByte(s[i])
			continue
		}
		b.WriteByte('%')
		const hex = "0123456789ABCDEF"
		b.WriteByte(hex[s[i]>>4])
		b.WriteByte(hex[s[i]&0x0f])
	}
	return b.String()
}
