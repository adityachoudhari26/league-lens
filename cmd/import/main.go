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
		fmt.Fprintln(errOut, "Player mapping unavailable; retaining Sleeper identities for this import.")
	}
	client := sleeper.NewClient(nil)
	league, err := client.DescribeLeague(ctx, options.leagueID)
	if err != nil {
		return err
	}
	accountID, selection, err := client.ResolveSelection(ctx, options.username, league)
	if err != nil {
		return err
	}
	service := importer.Service{DB: db, Source: client, Catalog: catalog, CatalogVersion: version}
	result, err := service.Import(ctx, importer.Request{OwnerEmail: options.ownerEmail, ExternalAccountID: accountID, Selection: selection})
	if err != nil {
		return fmt.Errorf("import failed: %w", err)
	}
	fmt.Fprintf(out, "Imported %d teams, %d encountered players, and %d available matchup periods.\n", result.Teams, result.Players, result.MatchupPeriods)
	return nil
}

type importOptions struct {
	username, leagueID, ownerEmail string
	mappingCache                   string
}

func parseImportOptions(args []string, errOut io.Writer) (importOptions, error) {
	var options importOptions
	if len(args) == 0 || args[0] != "sleeper" {
		return options, errors.New("usage: import sleeper --username NAME --league-id ID [--owner-email EMAIL]")
	}
	flags := flag.NewFlagSet("import sleeper", flag.ContinueOnError)
	flags.SetOutput(errOut)
	flags.StringVar(&options.username, "username", "", "Sleeper username or user ID")
	flags.StringVar(&options.leagueID, "league-id", "", "selected Sleeper league ID")
	flags.StringVar(&options.ownerEmail, "owner-email", "owner@localhost", "local owner identity; not a login or verified email")
	flags.StringVar(&options.mappingCache, "mapping-cache", "", "player ID CSV cache path (defaults to OS user cache)")
	if err := flags.Parse(args[1:]); err != nil {
		return options, err
	}
	if flags.NArg() != 0 || options.username == "" || options.leagueID == "" {
		return options, errors.New("--username and --league-id are required; no positional arguments")
	}
	return options, nil
}

func mappingCachePath(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	directory, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "league-lens", "playerids.csv"), nil
}
