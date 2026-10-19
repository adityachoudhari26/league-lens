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
	"github.com/adityachoudhari26/league-lens/internal/assistant"
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
	"github.com/adityachoudhari26/league-lens/internal/sleeper"
	"github.com/adityachoudhari26/league-lens/web"
)

// liveScoreInterval is how often live matchups are re-read from providers during games.
// The web app's MATCHUP_LIVE_POLL_MS uses the same interval.
const liveScoreInterval = 15 * time.Second

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
	sources := newProviderSources(box, catalogPath)
	syncRunner := leaguesync.New(data, cfg.LocalOwnerEmail, sources, catalogPath)
	defer syncRunner.Close()
	if cfg.LiveScores {
		go syncRunner.RunLiveScores(ctx, liveScoreInterval)
	}
	connectionService := &connections.Service{
		DB: data, Box: box, OwnerEmail: cfg.LocalOwnerEmail, CatalogPath: catalogPath,
		Imports: syncRunner,
	}
	dashboardService := dashboard.New(data, cfg.LocalOwnerEmail, newProfileSource(time.Now()), syncRunner, leaguesync.ProviderSources{})
	syncHandler := leaguesync.NewHandler(syncRunner)
	assistantService := &assistant.Service{
		DB: data, Box: box, OwnerEmail: cfg.LocalOwnerEmail,
		Dashboard: dashboardService, Sync: syncHandler, ProviderSources: sources, GameContext: sources,
		Trending: sleeper.NewClient(&http.Client{Timeout: 10 * time.Second}), CatalogPath: catalogPath,
		Injuries: espn.NewInjuryClient(nil),
		Usage:    sleeper.NewClient(&http.Client{Timeout: 10 * time.Second}),
	}
	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpserver.RequireLocalHost(httpserver.NewHandler(
			data.Ping,
			dashboardService,
			syncHandler,
			connectionService,
			assistantService,
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

// newProviderSources configures saved ESPN credentials and the mapping cache.
// Saved cookies take precedence over environment cookies; Sleeper needs none.
func newProviderSources(box *secrets.Box, catalogPath string) leaguesync.ProviderSources {
	credentials := espn.Credentials{S2: os.Getenv("ESPN_S2"), SWID: os.Getenv("ESPN_SWID")}
	return leaguesync.ProviderSources{ESPN: credentials, Box: box, PlayerCatalogPath: catalogPath}
}
