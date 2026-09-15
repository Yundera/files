// Command files serves the Yundera file manager: a Go binary with the Svelte UI
// embedded in it, serving one bind-mounted directory tree.
//
// It authenticates nobody. See ARCHITECTURE.md "Security boundary" — on a PCS the
// boundary is the AppShield gate in front of it, and the compose keeps this
// process off the shared network so the gate cannot be bypassed.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yundera/files/internal/bootstrap"
	"github.com/yundera/files/internal/config"
	"github.com/yundera/files/internal/server"
	"github.com/yundera/files/internal/ui"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("files: ")

	cfg := config.FromEnv()

	// The sidebar links to Documents, Downloads and Media unconditionally, so
	// they have to be there. Never fatal — see package bootstrap.
	bootstrap.EnsureRoots(cfg)

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: server.New(cfg, ui.Dist()),
		// The rest of the timeouts are deliberately unset: this app streams
		// arbitrarily large files in both directions, so a write deadline would
		// cut off a legitimate slow download. Only the header read is bounded.
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("listening on %s (data root %s, state dir %s)", cfg.Addr, cfg.DataRoot, cfg.StateDir())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Print("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
