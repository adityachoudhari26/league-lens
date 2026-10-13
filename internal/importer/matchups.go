package importer

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/adityachoudhari26/league-lens/internal/sleeper"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

func (w *leagueWriter) writeHistory(ctx context.Context) (int, error) {
	importedWeeks := 0
	for week := 1; week <= w.data.Period; week++ {
		matchups := w.data.Weeks[week]
		if len(matchups) == 0 {
			continue
		}
		if err := w.writeWeek(ctx, week, matchups); err != nil {
			return 0, err
		}
		importedWeeks++
	}
	return importedWeeks, nil
}

func (w *leagueWriter) isHistoricalWeek(week int) bool {
	return week < w.data.Period || w.data.State.SeasonType == "post"
}

func matchupKey(matchup sleeper.Matchup) string {
	if matchup.MatchupID != nil && *matchup.MatchupID > 0 {
		return strconv.Itoa(*matchup.MatchupID)
	}
	return "bye-" + strconv.Itoa(matchup.RosterID)
}

func (w *leagueWriter) writeHistoricalSnapshot(ctx context.Context, week int, matchup sleeper.Matchup) error {
	team, found := w.teams[matchup.RosterID]
	if !found {
		return fmt.Errorf("unknown matchup team")
	}
	// Missing benches retain lineup-only coverage; absent lineups create no snapshot.
	if !w.isHistoricalWeek(week) || (matchup.Players == nil && matchup.Starters == nil) {
		return nil
	}
	entries, coverage, err := rosterEntries(matchup.Players, matchup.Starters, nil, nil, w.data.League.Positions)
	if err != nil {
		return err
	}
	return w.writeSnapshot(ctx, team, week, "provider_history", entries, coverage, nil)
}

func (w *leagueWriter) writeWeek(ctx context.Context, week int, matchups []sleeper.Matchup) error {
	groups := map[string][]sleeper.Matchup{}
	for _, matchup := range matchups {
		if err := w.writeHistoricalSnapshot(ctx, week, matchup); err != nil {
			return err
		}
		key := matchupKey(matchup)
		groups[key] = append(groups[key], matchup)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	if err := w.queries.DeleteStaleMatchups(ctx, store.DeleteStaleMatchupsParams{LeagueSeasonID: w.target.league.ID, Period: int32(week), Column3: keys}); err != nil {
		return err
	}
	for key, opponents := range groups {
		if err := w.writeMatchup(ctx, week, key, opponents); err != nil {
			return err
		}
	}
	return nil
}

func (w *leagueWriter) writeMatchup(ctx context.Context, week int, key string, opponents []sleeper.Matchup) error {
	if len(opponents) > 2 {
		return fmt.Errorf("more than two teams in matchup")
	}
	sort.Slice(opponents, func(i, j int) bool { return opponents[i].RosterID < opponents[j].RosterID })
	status := "unknown"
	if w.isHistoricalWeek(week) {
		status = "final"
	}
	matchup := store.UpsertMatchupParams{LeagueSeasonID: w.target.league.ID, ExternalMatchupID: key, Period: int32(week), TeamAID: w.teams[opponents[0].RosterID].ID, Status: status, FetchedAt: timestamp(w.data.ObservedAt)}
	var err error
	matchup.ScoreA, err = score(opponents[0])
	if err != nil {
		return err
	}
	if len(opponents) == 2 {
		matchup.TeamBID = w.teams[opponents[1].RosterID].ID
		matchup.ScoreB, err = score(opponents[1])
		if err != nil {
			return err
		}
	}
	_, err = w.queries.UpsertMatchup(ctx, matchup)
	return err
}

func score(matchup sleeper.Matchup) (pgtype.Numeric, error) {
	points := matchup.Points
	if matchup.CustomPoints != nil {
		points = matchup.CustomPoints
	}
	var numeric pgtype.Numeric
	if points == nil {
		return numeric, nil
	}
	err := numeric.Scan(strconv.FormatFloat(*points, 'f', 4, 64))
	return numeric, err
}
