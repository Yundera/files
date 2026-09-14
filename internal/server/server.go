// Package server wires the HTTP surface: a chi router, the JSON API under /api,
// and the embedded SPA on everything else.
package server

import (
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/yundera/files/internal/config"
	"github.com/yundera/files/internal/jobs"
	"github.com/yundera/files/internal/live"
	"github.com/yundera/files/internal/ownership"
	"github.com/yundera/files/internal/thumb"
	"github.com/yundera/files/internal/trash"
	"github.com/yundera/files/internal/upload"
	"github.com/yundera/files/internal/vfs"
)

// Server holds every dependency. All unexported; construction happens in New so
// that main stays a wiring-free entry point.
type Server struct {
	cfg  config.Config
	uiFS fs.FS

	// fs is nil when the data root could not be opened. Every filesystem route
	// guards on requireFS; the server still comes up so /api/health answers and
	// the operator sees the reason in the logs rather than a restart loop.
	// jobs and trash are nil in exactly the same case.
	fs     *vfs.FS
	jobs   *jobs.Registry
	trash  *trash.Store
	upload *upload.Manager
	thumbs *thumb.Cache
	owner  ownership.Owner
	hub    *live.Hub
}

// New builds the whole handler. It tolerates a bare Config — an unreadable or
// missing data root does not prevent the server coming up, it just makes every
// listing fail with a clear error. That is what lets a test construct the real
// server with New(config.Config{DataRoot: t.TempDir()}, fstest.MapFS{}) and
// drive it through ServeHTTP.
func New(cfg config.Config, uiFS fs.FS) http.Handler {
	// A state dir outside the data root would make trash, upload-commit and
	// atomic save cross-root renames, which os.Root cannot express. Refusing
	// here keeps exactly one code path for all three. See config.StateDirInsideRoot.
	if !cfg.StateDirInsideRoot() {
		log.Printf("WARNING: STATE_DIR (%s) is not a subdirectory of DATA_ROOT (%s); "+
			"trash, uploads and saves will not work", cfg.StateDir(), cfg.DataRoot)
	}

	s := &Server{cfg: cfg, uiFS: uiFS}

	s.owner = ownership.FromConfig(cfg)
	s.hub = live.NewHub()

	if vf, err := vfs.New(cfg); err != nil {
		log.Printf("WARNING: data root unavailable, file routes will fail: %v", err)
	} else {
		s.fs = vf
		s.trash = trash.New(vf, cfg)
		s.jobs = jobs.New(vf, cfg, s.trash)

		s.hub.JobsSnapshot = func() any { return s.jobs.States() }
		s.hub.TrashSnapshot = func() any { items, _ := s.trash.List(); return items }

		// A phase change is broadcast immediately — a job finishing must never
		// be delayed. Byte-level progress is throttled to maison's 300ms, which
		// turns a copy of ten thousand files into a few broadcasts a second
		// instead of ten thousand.
		s.jobs.OnChange = s.broadcastJobs
		s.jobs.OnProgress = live.Throttle(300*time.Millisecond, s.broadcastJobs)
		s.jobs.OnDirChanged = s.broadcastDir
		s.trash.OnChange = s.broadcastTrash

		s.thumbs = thumb.New(vf, cfg)

		s.trash.RunPurgeLoop()

		// Uploads are optional in the sense that a failure here must not stop
		// the rest of the app serving: browsing and editing still work without
		// them, and the operator gets a clear reason in the logs.
		if um, err := upload.New(vf, cfg); err != nil {
			log.Printf("WARNING: uploads unavailable: %v", err)
		} else {
			um.OnDirChanged = s.broadcastDir
			s.upload = um
		}
	}

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", s.handleHealth)
		r.Get("/config", s.handleConfig)

		r.Route("/fs", func(r chi.Router) {
			r.Get("/list", s.handleList)
			r.Get("/stat", s.handleStat)
			r.Get("/tree", s.handleTree)
			r.Get("/raw", s.handleRaw)
			r.Get("/thumb", s.handleThumb)
			r.Get("/search", s.handleSearch)

			r.Post("/mkdir", s.handleMkdir)
			r.Post("/touch", s.handleTouch)
			r.Post("/rename", s.handleRename)
			// Copy, move and delete answer 202 with a job id: a 50 GB copy is
			// not a request.
			r.Post("/copy", s.handleCopy)
			r.Post("/move", s.handleMove)
			r.Post("/delete", s.handleDelete)
		})

		r.Route("/file", func(r chi.Router) {
			r.Get("/text", s.handleReadText)
			r.Put("/text", s.handleWriteText)
		})

		r.Get("/jobs", s.handleJobs)
		r.Post("/jobs/{id}/cancel", s.handleCancelJob)

		// tus lives under /api so the AppShield gate covers it with everything
		// else — there is no path exemption for uploads.
		if s.upload != nil {
			h := s.upload.Handler()

			// THE BASE PATH MUST BE STRIPPED BEFORE tusd SEES THE REQUEST.
			//
			// UnroutedHandler assumes its router already did that: its
			// extractIDFromPath is literally strings.Trim(path, "/"), so a
			// request still carrying /api/tus/ yields an upload id of
			// "api/tus/<id>" and every PATCH answers 404 ERR_UPLOAD_NOT_FOUND
			// while the POST that created it looked fine.
			strip := func(fn http.HandlerFunc) http.Handler {
				return http.StripPrefix("/api/tus", fn)
			}
			r.Route("/tus", func(r chi.Router) {
				// tusd's Middleware answers OPTIONS and validates protocol
				// headers before any of the verbs below run.
				r.Use(h.Middleware)
				r.Post("/", h.PostFile)
				r.Method(http.MethodHead, "/{id}", strip(h.HeadFile))
				r.Method(http.MethodPatch, "/{id}", strip(h.PatchFile))
				r.Method(http.MethodDelete, "/{id}", strip(h.DelFile))
			})
		}

		r.Route("/trash", func(r chi.Router) {
			r.Get("/", s.handleTrashList)
			r.Post("/restore", s.handleTrashRestore)
			r.Post("/delete", s.handleTrashDelete)
			r.Post("/empty", s.handleTrashEmpty)
		})
	})

	// The live channel. One socket multiplexes jobs, trash and directory hints.
	r.Get("/ws", s.hub.ServeWS)

	r.Handle("/*", spaHandler(uiFS))

	return r
}

// handleHealth is the one route the AppShield gate lets through
// unauthenticated, via ALLOWED_PATHS=api/health. It must stay cheap and must not
// touch the filesystem: it is polled, and a hung stat on a busy disk would make
// a healthy app look down.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleConfig tells the UI what the server was configured with, so the client
// does not hardcode limits that an operator can change.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"editMaxBytes":   s.cfg.EditMaxBytes,
		"uploadMaxBytes": s.cfg.UploadMaxBytes,
		"searchMaxDepth": s.cfg.SearchMaxDepth,
		"trashEnabled":   true,
		"uploadEnabled":  s.upload != nil,
	})
}

// broadcastJobs pushes the current job list to anyone watching the tray.
func (s *Server) broadcastJobs() {
	s.hub.BroadcastLazy(live.ChannelJobs, func() any { return s.jobs.States() })
}

func (s *Server) broadcastTrash() {
	s.hub.BroadcastLazy(live.ChannelTrash, func() any { items, _ := s.trash.List(); return items })
}

// broadcastDir tells open listings that a directory changed.
//
// It carries the path, not the new listing: the client re-reads only if it
// happens to be looking at that directory, so a background job does not push a
// full page of entries to every connected browser.
func (s *Server) broadcastDir(virtual string) {
	if virtual == "" {
		virtual = "/"
	}
	s.hub.Broadcast(live.ChannelDir, map[string]string{"path": virtual})
}
