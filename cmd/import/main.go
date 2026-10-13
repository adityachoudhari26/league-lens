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
	"path/filepath"
	"syscall"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/config"
	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
	"github.com/adityachoudhari26/league-lens/internal/sleeper"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string, out, errOut io.Writer) error {
	if len(args) == 0 || args[0] != "sleeper" {
		return errors.New("usage: import sleeper --username NAME --league-id ID [--owner-email EMAIL]")
	}
	flags := flag.NewFlagSet("import sleeper", flag.ContinueOnError)
	flags.SetOutput(errOut)
	username := flags.String("username", "", "Sleeper username or user ID")
	league := flags.String("league-id", "", "selected Sleeper league ID")
	owner := flags.String("owner-email", "owner@localhost", "local owner identity; not a login or verified email")
	cache := flags.String("mapping-cache", "", "player ID CSV cache path (defaults to OS user cache)")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || *username == "" || *league == "" {
		return errors.New("--username and --league-id are required; no positional arguments")
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
	if *cache == "" {
		dir, e := os.UserCacheDir()
		if e != nil {
			return e
		}
		*cache = filepath.Join(dir, "league-lens", "playerids.csv")
	}
	catalog, version, err := playerids.Load(ctx, *cache)
	if err != nil {
		fmt.Fprintln(errOut, "Player mapping unavailable; retaining Sleeper identities for this import.")
	}
	service := importer.Service{DB: db, Source: sleeper.NewClient(nil), Catalog: catalog, CatalogVersion: version}
	result, err := service.Import(ctx, importer.Request{OwnerEmail: *owner, Username: *username, LeagueID: *league})
	if err != nil {
		return fmt.Errorf("import failed: %w", err)
	}
	fmt.Fprintf(out, "Imported %d teams, %d encountered players, and %d available matchup weeks.\n", result.Teams, result.Players, result.Weeks)
	return nil
}
