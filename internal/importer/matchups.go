package importer

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

// matchupKey identifies a matchup within a league season; external matchup IDs
// are only unique within their period.
type matchupKey struct {
	period     fantasy.MatchupPeriod
	externalID string
}

type lineupKey struct {
	matchup matchupKey
	teamID  string
}

func (w *leagueWriter) writeMatchups(ctx context.Context) error {
	keys := map[fantasy.MatchupPeriod][]string{}
	for _, matchup := range w.data.Matchups {
		keys[matchup.Period] = append(keys[matchup.Period], matchup.ExternalID)
	}
	for _, period := range w.data.CompleteMatchupPeriods {
		ids := keys[period]
		if ids == nil {
			ids = []string{}
		}
		if err := w.queries.DeleteStaleMatchups(ctx, store.DeleteStaleMatchupsParams{
			LeagueSeasonID:     w.target.league.ID,
			Period:             int32(period),
			ExternalMatchupIds: ids,
		}); err != nil {
			return err
		}
	}
	for _, matchup := range w.data.Matchups {
		if err := w.writeMatchup(ctx, matchup); err != nil {
			return err
		}
	}
	return nil
}

func (w *leagueWriter) writeMatchup(ctx context.Context, matchup fantasy.Matchup) error {
	periods, err := optionalJSON(matchup.ScoringPeriods)
	if err != nil {
		return err
	}
	params := store.UpsertMatchupParams{
		LeagueSeasonID:     w.target.league.ID,
		ExternalMatchupID:  matchup.ExternalID,
		Period:             int32(matchup.Period),
		TeamAID:            w.teams[matchup.TeamA.TeamID].ID,
		Status:             string(matchup.Status),
		FetchedAt:          timestamp(w.data.FetchedAt),
		ScoringPeriodsJson: periods,
		IsBye:              matchup.IsBye,
	}
	params.ScoreA, err = score(matchup.TeamA.Score)
	if err != nil {
		return err
	}
	if matchup.TeamB != nil {
		params.TeamBID = w.teams[matchup.TeamB.TeamID].ID
		params.ScoreB, err = score(matchup.TeamB.Score)
		if err != nil {
			return err
		}
	}
	stored, err := w.queries.UpsertMatchup(ctx, params)
	if err != nil {
		return err
	}
	w.matchups[matchupKey{period: matchup.Period, externalID: matchup.ExternalID}] = stored.ID
	return w.queries.DeleteStaleMatchupLineupTeams(ctx, stored.ID)
}

// writeLineups replaces the stored lineup of every side the provider reported.
// Sides without a lineup in the payload keep their rows, because a missing
// lineup means unavailable rather than empty.
func (w *leagueWriter) writeLineups(ctx context.Context) error {
	for _, lineup := range w.data.Lineups {
		if err := w.replaceLineup(ctx, lineup); err != nil {
			return err
		}
	}
	return nil
}

func (w *leagueWriter) replaceLineup(ctx context.Context, lineup fantasy.MatchupLineup) error {
	matchupID := w.matchups[matchupKey{period: lineup.Period, externalID: lineup.MatchupID}]
	teamID := w.teams[lineup.TeamID].ID
	if err := w.queries.DeleteMatchupLineup(ctx, store.DeleteMatchupLineupParams{MatchupID: matchupID, TeamID: teamID}); err != nil {
		return err
	}
	for _, entry := range lineup.Entries {
		if err := w.insertLineupEntry(ctx, matchupID, teamID, entry); err != nil {
			return err
		}
	}
	return nil
}

func (w *leagueWriter) insertLineupEntry(ctx context.Context, matchupID, teamID pgtype.UUID, entry fantasy.LineupEntry) error {
	points, err := score(entry.Points)
	if err != nil {
		return err
	}
	projected, err := score(entry.ProjectedPoints)
	if err != nil {
		return err
	}
	return w.queries.InsertMatchupLineupEntry(ctx, store.InsertMatchupLineupEntryParams{
		MatchupID:       matchupID,
		TeamID:          teamID,
		PlayerID:        w.players[entry.PlayerID],
		ProviderSlotID:  optionalText(entry.ProviderSlotID),
		SlotLabel:       text(string(entry.Slot)),
		Starter:         entry.Starter,
		Points:          points,
		ProjectedPoints: projected,
	})
}

func score(points *float64) (pgtype.Numeric, error) {
	var numeric pgtype.Numeric
	if points == nil {
		return numeric, nil
	}
	err := numeric.Scan(strconv.FormatFloat(*points, 'f', 4, 64))
	return numeric, err
}
