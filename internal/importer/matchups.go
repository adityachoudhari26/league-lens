package importer

import (
	"context"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"strconv"
)

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
		if err := w.queries.DeleteStaleMatchups(ctx, store.DeleteStaleMatchupsParams{LeagueSeasonID: w.target.league.ID, Period: int32(period), Column3: ids}); err != nil {
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
	params := store.UpsertMatchupParams{LeagueSeasonID: w.target.league.ID, ExternalMatchupID: matchup.ExternalID, Period: int32(matchup.Period), TeamAID: w.teams[matchup.TeamA.TeamID].ID, Status: string(matchup.Status), FetchedAt: timestamp(w.data.FetchedAt), ScoringPeriodsJson: periods, IsBye: matchup.IsBye}
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
	_, err = w.queries.UpsertMatchup(ctx, params)
	return err
}
func score(points *float64) (pgtype.Numeric, error) {
	var numeric pgtype.Numeric
	if points == nil {
		return numeric, nil
	}
	err := numeric.Scan(strconv.FormatFloat(*points, 'f', 4, 64))
	return numeric, err
}
