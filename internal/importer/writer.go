package importer

import (
	"context"
	"encoding/json"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// leagueWriter is scoped to the import transaction; adapters have already
// resolved provider response semantics into explicit league data.
type leagueWriter struct {
	service  *Service
	queries  *store.Queries
	target   importTarget
	data     fantasy.LeagueData
	teams    map[string]store.Team
	players  map[string]pgtype.UUID
	matchups map[matchupKey]pgtype.UUID
}

func newLeagueWriter(service *Service, queries *store.Queries, target importTarget, data fantasy.LeagueData) *leagueWriter {
	return &leagueWriter{service: service, queries: queries, target: target, data: data, teams: map[string]store.Team{}, players: map[string]pgtype.UUID{}, matchups: map[matchupKey]pgtype.UUID{}}
}
func (w *leagueWriter) write(ctx context.Context) (int, int, int, error) {
	if err := w.writeTeams(ctx); err != nil {
		return 0, 0, 0, err
	}
	for _, player := range w.data.Players {
		playerID, err := w.service.resolvePlayer(ctx, w.queries, w.data.League.Ref.Provider, player)
		if err != nil {
			return 0, 0, 0, err
		}
		w.players[player.ExternalID] = playerID
	}
	for _, snapshot := range w.data.Rosters {
		if err := w.writeSnapshot(ctx, snapshot); err != nil {
			return 0, 0, 0, err
		}
	}
	if err := w.writeMatchups(ctx); err != nil {
		return 0, 0, 0, err
	}
	if err := w.writeLineups(ctx); err != nil {
		return 0, 0, 0, err
	}
	periods := map[fantasy.MatchupPeriod]bool{}
	for _, matchup := range w.data.Matchups {
		periods[matchup.Period] = true
	}
	return len(w.teams), len(w.players), len(periods), nil
}
func (w *leagueWriter) writeTeams(ctx context.Context) error {
	if err := w.queries.ClearSelectedTeams(ctx, store.ClearSelectedTeamsParams{ConnectionID: w.target.connection.ID, LeagueSeasonID: w.target.league.ID}); err != nil {
		return err
	}
	selected := map[string]bool{}
	for _, id := range w.target.selection.SelectedTeamIDs {
		selected[id] = true
	}
	for _, team := range w.data.Teams {
		record, err := optionalJSON(team.Record)
		if err != nil {
			return err
		}
		rank := pgtype.Int4{}
		if team.Rank != nil {
			rank = pgtype.Int4{Int32: int32(*team.Rank), Valid: true}
		}
		stored, err := w.queries.UpsertTeam(ctx, store.UpsertTeamParams{LeagueSeasonID: w.target.league.ID, ExternalTeamID: team.ExternalID, Name: team.Name, RecordJson: record, Rank: rank})
		if err != nil {
			return err
		}
		w.teams[team.ExternalID] = stored
		if selected[team.ExternalID] {
			if err = w.queries.SelectUserTeam(ctx, store.SelectUserTeamParams{UserID: w.target.user.ID, ConnectionID: w.target.connection.ID, LeagueSeasonID: w.target.league.ID, TeamID: stored.ID}); err != nil {
				return err
			}
		}
	}
	return nil
}
func optionalJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if string(encoded) == "null" {
		return nil, err
	}
	return encoded, err
}
