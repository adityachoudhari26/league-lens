package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/adityachoudhari26/league-lens/internal/sleeper"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// leagueWriter exists only within the import write transaction. Its maps share
// resolved identities across current rosters and historical weeks.
type leagueWriter struct {
	service *Service
	queries *store.Queries
	target  importTarget
	data    sleeper.Data
	teams   map[int]store.Team
	players map[string]pgtype.UUID
}

func newLeagueWriter(service *Service, queries *store.Queries, target importTarget, data sleeper.Data) *leagueWriter {
	return &leagueWriter{service: service, queries: queries, target: target, data: data, teams: map[int]store.Team{}, players: map[string]pgtype.UUID{}}
}

func (w *leagueWriter) write(ctx context.Context) (int, int, int, error) {
	if err := w.writeCurrentRosters(ctx); err != nil {
		return 0, 0, 0, err
	}
	importedWeeks, err := w.writeHistory(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	return len(w.teams), len(w.players), importedWeeks, nil
}

func (w *leagueWriter) resolvePlayer(ctx context.Context, externalID string) (pgtype.UUID, error) {
	if playerID, found := w.players[externalID]; found {
		return playerID, nil
	}
	playerID, err := w.service.resolvePlayer(ctx, w.queries, externalID)
	if err == nil {
		w.players[externalID] = playerID
	}
	return playerID, err
}

func teamNamesByOwner(users []sleeper.User) map[string]string {
	names := map[string]string{}
	for _, user := range users {
		name := user.Metadata["team_name"]
		if name == "" {
			name = user.DisplayName
		}
		names[user.ID] = name
	}
	return names
}

func (w *leagueWriter) writeCurrentRosters(ctx context.Context) error {
	if err := w.queries.ClearSelectedTeams(ctx, store.ClearSelectedTeamsParams{ConnectionID: w.target.connection.ID, LeagueSeasonID: w.target.league.ID}); err != nil {
		return err
	}
	teamNames := teamNamesByOwner(w.data.Users)
	for _, roster := range w.data.Rosters {
		team, err := w.writeTeam(ctx, roster, teamNames[roster.OwnerID])
		if err != nil {
			return err
		}
		w.teams[roster.ID] = team
		if err = w.writeCurrentSnapshot(ctx, team, roster); err != nil {
			return err
		}
	}
	return nil
}

func (w *leagueWriter) writeTeam(ctx context.Context, roster sleeper.Roster, name string) (store.Team, error) {
	if name == "" {
		name = fmt.Sprintf("Team %d", roster.ID)
	}
	team, err := w.queries.UpsertTeam(ctx, store.UpsertTeamParams{LeagueSeasonID: w.target.league.ID, ExternalTeamID: strconv.Itoa(roster.ID), Name: name})
	if err != nil {
		return team, err
	}
	if rosterOwnedBy(roster, w.data.User.ID) {
		err = w.queries.SelectUserTeam(ctx, store.SelectUserTeamParams{UserID: w.target.user.ID, ConnectionID: w.target.connection.ID, LeagueSeasonID: w.target.league.ID, TeamID: team.ID})
	}
	return team, err
}

func rosterOwnedBy(roster sleeper.Roster, userID string) bool {
	if roster.OwnerID == userID {
		return true
	}
	for _, coOwner := range roster.CoOwners {
		if coOwner == userID {
			return true
		}
	}
	return false
}

func (w *leagueWriter) writeCurrentSnapshot(ctx context.Context, team store.Team, roster sleeper.Roster) error {
	if roster.Players == nil && roster.Starters == nil {
		return nil
	}
	entries, coverage, err := rosterEntries(roster.Players, roster.Starters, roster.Reserve, roster.Taxi, w.data.League.Positions)
	if err != nil {
		return err
	}
	// Resolve even unchanged rosters so the summary includes every encountered identity.
	for _, entry := range entries {
		if _, err = w.resolvePlayer(ctx, entry.ID); err != nil {
			return err
		}
	}
	var record []byte
	if roster.Settings != nil {
		record, err = json.Marshal(roster.Settings)
		if err != nil {
			return err
		}
	}
	return w.writeSnapshot(ctx, team, w.data.Period, "observed", entries, coverage, record)
}
