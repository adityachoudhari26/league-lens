package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/config"
	"github.com/adityachoudhari26/league-lens/internal/dashboard"
	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/espn"
	"github.com/adityachoudhari26/league-lens/internal/httpserver"
	"github.com/adityachoudhari26/league-lens/internal/leaguesync"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
)

func main() {
	if err := run(); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	data, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return errors.New("could not configure database pool; check DATABASE_URL")
	}
	defer data.Close()
	syncRunner := newSyncRunner(data, cfg.LocalOwnerEmail)
	defer syncRunner.Close()
	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpserver.NewHandler(
			data.Ping,
			dashboard.New(data, cfg.LocalOwnerEmail),
			leaguesync.NewHandler(syncRunner),
		),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
	}
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	slog.Info("starting League Lens", "address", cfg.HTTPAddr)
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return errors.New("HTTP server failed; check listen address and port availability")
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			_ = srv.Close()
			return errors.New("HTTP server shutdown timed out")
		}
		return nil
	}
}

// newSyncRunner reads ESPN cookies from the environment, as the import CLI does,
// until encrypted connection credentials exist. Sleeper needs no credentials.
func newSyncRunner(data *database.DB, ownerEmail string) *leaguesync.Runner {
	credentials := espn.Credentials{S2: os.Getenv("ESPN_S2"), SWID: os.Getenv("ESPN_SWID")}
	catalogPath, err := playerids.DefaultCachePath()
	if err != nil {
		slog.Warn("player mapping cache unavailable; syncs retain provider identities")
	}
	sources := leaguesync.ProviderSources{ESPN: credentials}
	return leaguesync.New(data, ownerEmail, sources, catalogPath)
}
