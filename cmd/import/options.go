package main

import (
	"errors"
	"flag"
	"io"
)

type importOptions struct {
	provider, username, leagueID, ownerEmail, mappingCache string
	season, teamID                                         int
}

func parseImportOptions(args []string, errOut io.Writer) (importOptions, error) {
	var options importOptions
	if len(args) == 0 || (args[0] != "sleeper" && args[0] != "espn") {
		return options, errors.New("usage: import sleeper|espn --league-id ID [provider options]")
	}
	options.provider = args[0]
	flags := flag.NewFlagSet("import "+options.provider, flag.ContinueOnError)
	flags.SetOutput(errOut)
	flags.StringVar(&options.leagueID, "league-id", "", "selected league ID")
	flags.StringVar(&options.ownerEmail, "owner-email", "owner@localhost", "local owner identity; not a login or verified email")
	flags.StringVar(&options.mappingCache, "mapping-cache", "", "player ID CSV cache path (defaults to OS user cache)")
	if options.provider == "sleeper" {
		flags.StringVar(&options.username, "username", "", "Sleeper username or user ID")
	} else {
		flags.IntVar(&options.season, "season", 0, "ESPN season year (required)")
		flags.IntVar(&options.teamID, "team-id", 0, "selected ESPN team ID (required)")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return options, err
	}
	if flags.NArg() != 0 || options.leagueID == "" {
		return options, errors.New("--league-id is required; no positional arguments")
	}
	if options.provider == "sleeper" && options.username == "" {
		return options, errors.New("--username is required")
	}
	if options.provider == "espn" && (options.season < 1900 || options.season > 2200 || options.teamID <= 0) {
		return options, errors.New("valid --season and --team-id are required")
	}
	return options, nil
}
