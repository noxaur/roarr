// Command roarr is a single-binary front end for Sonarr, Radarr, Prowlarr,
// Seerr and Jellyfin: search anything, see what indexers have in seconds,
// grab an episode, a season, a show, a movie or a whole collection.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/noxaur/roarr/internal/app"
	"github.com/noxaur/roarr/internal/config"
	"github.com/noxaur/roarr/internal/server"
	"github.com/noxaur/roarr/internal/webui"
)

var version = "dev"

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe the local server and exit (for Docker HEALTHCHECK)")
	flag.Parse()

	port := env("PORT", "3847")
	if *healthcheck {
		c := &http.Client{Timeout: 3 * time.Second}
		res, err := c.Get("http://127.0.0.1:" + port + "/api/health")
		if err != nil || res.StatusCode != 200 {
			os.Exit(1)
		}
		os.Exit(0)
	}

	level := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
	server.Version = version

	dataDir := env("ROARR_DATA", "/config")
	store, err := config.Open(dataDir)
	if err != nil {
		slog.Error("cannot load settings", "dir", dataDir, "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a := app.New(ctx, store, dataDir)
	srv, err := server.New(a, webui.FS())
	if err != nil {
		slog.Error("server init failed", "err", err)
		os.Exit(1)
	}
	st := store.Get()
	if st.AuthMode == "none" {
		slog.Warn("authentication is off (AUTH_MODE=none); do not expose roarr to the internet like this")
	}
	httpSrv := &http.Server{Addr: ":" + port, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		slog.Info(fmt.Sprintf("roarr %s listening on :%s", version, port), "data", dataDir, "auth", st.AuthMode)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server failed", "err", err)
			os.Exit(1)
		}
	}()
	<-ctx.Done()
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(sctx)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
