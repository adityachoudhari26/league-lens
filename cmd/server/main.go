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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/adityachoudhari26/league-lens/db"
	"github.com/adityachoudhari26/league-lens/internal/config"
	"github.com/adityachoudhari26/league-lens/internal/connections"
	"github.com/adityachoudhari26/league-lens/internal/dashboard"
	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/espn"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/httpserver"
	"github.com/adityachoudhari26/league-lens/internal/leaguesync"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
	"github.com/adityachoudhari26/league-lens/internal/secrets"
	"github.com/adityachoudhari26/league-lens/web"
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
	key, err := secrets.LoadOrCreateKey(cfg.DataDir)
	if err != nil {
		return errors.New("cannot load credential encryption key")
	}
	box, err := secrets.NewBox(key)
	if err != nil {
		return errors.New("cannot initialize credential encryption")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.MigrateOnStart {
		if err := migrateOnStart(ctx, cfg.DatabaseURL); err != nil {
			return err
		}
	}
	data, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return errors.New("could not configure database pool; check DATABASE_URL")
	}
	defer data.Close()
	catalogPath, err := playerids.DefaultCachePath()
	if err != nil {
		slog.Warn("player mapping cache unavailable; imports and syncs retain provider identities")
	}
	syncRunner := newSyncRunner(data, cfg.LocalOwnerEmail, box, catalogPath)
	defer syncRunner.Close()
	if cfg.LiveScores {
		go syncRunner.RunLiveScores(ctx, time.Minute)
	}
	connectionService := &connections.Service{
		DB: data, Box: box, OwnerEmail: cfg.LocalOwnerEmail, CatalogPath: catalogPath,
		RefreshNFLSchedule: syncRunner.RefreshNFLSchedule,
		RefreshPlayerNews:  syncRunner.RefreshPlayerNews,
	}
	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpserver.RequireLocalHost(httpserver.NewHandler(
			data.Ping,
			dashboard.New(data, cfg.LocalOwnerEmail, newProfileSource(time.Now()), syncRunner),
			leaguesync.NewHandler(syncRunner),
			connectionService,
			web.Dist(),
		)),
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

// migrateOnStart applies pending migrations before the app pool opens when
// MIGRATE_ON_START=true, as in Docker. Development keeps `just migrate`
// explicit, and returned errors omit database credentials.
func migrateOnStart(ctx context.Context, databaseURL string) error {
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return errors.New("invalid DATABASE_URL")
	}
	conn := stdlib.OpenDB(*cfg)
	defer conn.Close()

	migrationCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := db.Migrate(migrationCtx, conn, "up"); err != nil {
		return errors.New("startup migration failed; verify database availability, permissions, and migration SQL")
	}
	return nil
}

// newProfileSource reads player profiles for the NFL season in progress, which
// starts in September and runs through the February playoffs.
func newProfileSource(now time.Time) espn.ProfileSource {
	return espn.ProfileSource{HTTP: &http.Client{Timeout: 5 * time.Second}, Season: fantasy.CurrentSeason(now)}
}

// newSyncRunner uses saved ESPN credentials when available and environment
// cookies as a fallback. Sleeper needs no credentials.
func newSyncRunner(
	data *database.DB,
	ownerEmail string,
	box *secrets.Box,
	catalogPath string,
) *leaguesync.Runner {
	credentials := espn.Credentials{S2: os.Getenv("ESPN_S2"), SWID: os.Getenv("ESPN_SWID")}
	sources := leaguesync.ProviderSources{ESPN: credentials, Box: box}
	return leaguesync.New(data, ownerEmail, sources, catalogPath)
}
