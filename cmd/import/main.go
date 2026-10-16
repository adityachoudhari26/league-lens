// Command import is a temporary local entry point for the reusable import service.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/config"
	"github.com/adityachoudhari26/league-lens/internal/connections"
	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/espn"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out, errOut io.Writer) error {
	options, err := parseImportOptions(args, errOut)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return errors.New("cannot configure database")
	}
	defer db.Close()
	if err = db.Ping(ctx); err != nil {
		return errors.New("cannot reach database; start Postgres and run migrations first")
	}
	cachePath, err := mappingCachePath(options.mappingCache)
	if err != nil {
		return err
	}
	catalog, version, err := playerids.Load(ctx, cachePath)
	if err != nil {
		fmt.Fprintln(errOut, "Player mapping unavailable; retaining provider identities for this import.")
	}
	prepared, err := connections.PrepareSource(ctx, connections.ImportSpec{
		Provider: options.provider, LeagueID: options.leagueID,
		Season: options.season, TeamID: options.teamID, Username: options.username,
		ESPN: espn.Credentials{S2: os.Getenv("ESPN_S2"), SWID: os.Getenv("ESPN_SWID")},
	})
	if err != nil {
		return err
	}
	service := importer.Service{DB: db, Source: prepared.Provider, Catalog: catalog, CatalogVersion: version}
	result, err := service.Import(ctx, importer.Request{
		OwnerEmail: options.ownerEmail, ExternalAccountID: prepared.AccountID, Selection: prepared.Selection, League: prepared.League,
	})
	if err != nil {
		return fmt.Errorf("import failed: %w", err)
	}
	fmt.Fprintf(out, "Imported %d teams, %d encountered players, and %d available matchup periods.\n", result.Teams, result.Players, result.MatchupPeriods)
	return nil
}

func mappingCachePath(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	return playerids.DefaultCachePath()
}
