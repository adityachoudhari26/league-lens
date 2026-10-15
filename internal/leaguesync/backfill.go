package leaguesync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/replay"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

// RequestWinChanceBackfill starts a finished matchup replay unless one is running or recently failed.
func (r *Runner) RequestWinChanceBackfill(matchupID pgtype.UUID) bool {
	if !matchupID.Valid || r.ctx == nil {
		return false
	}
	key := matchupID.String()
	return r.requestWinChanceJob(key, func() {
		r.runWinChanceBackfill(matchupID, key)
	})
}

func (r *Runner) requestWinChanceJob(key string, run func()) bool {
	r.mu.Lock()
	if r.ctx == nil || r.ctx.Err() != nil {
		r.mu.Unlock()
		return false
	}
	if r.backfillRunning == nil {
		r.backfillRunning = map[string]bool{}
	}
	if r.backfillFailures == nil {
		r.backfillFailures = map[string]failure{}
	}
	if r.backfillRunning[key] {
		r.mu.Unlock()
		return true
	}
	if recentFailure, found := r.backfillFailures[key]; found {
		if r.now().Sub(recentFailure.at) < winChanceBackfillFailureWindow {
			r.mu.Unlock()
			return false
		}
		delete(r.backfillFailures, key)
	}
	r.backfillRunning[key] = true
	r.wg.Add(1)
	r.mu.Unlock()

	go run()
	return true
}

func (r *Runner) runWinChanceBackfill(matchupID pgtype.UUID, key string) {
	defer r.wg.Done()
	ctx, cancel := context.WithTimeout(r.ctx, winChanceBackfillTimeout)
	defer cancel()

	samples, changes, err := r.backfillMatchup(ctx, matchupID)
	r.mu.Lock()
	delete(r.backfillRunning, key)
	if err != nil {
		r.backfillFailures[key] = failure{code: failureCode(err), at: r.now()}
	} else if samples == 0 && changes == 0 {
		r.backfillFailures[key] = failure{code: "nothing_to_rebuild", at: r.now()}
	} else {
		delete(r.backfillFailures, key)
	}
	r.mu.Unlock()

	if err != nil {
		slog.Warn("matchup win-chance backfill failed", "matchup_id", key, "code", failureCode(err))
		return
	}
	slog.Info("matchup win-chance backfill completed",
		"matchup_id", key, "samples", samples, "changes", changes)
}

// backfillMatchup fetches and persists one eligible final matchup's reconstructed history.
func (r *Runner) backfillMatchup(ctx context.Context, matchupID pgtype.UUID) (int, int, error) {
	matchup, err := r.db.Queries().BackfillMatchup(ctx, matchupID)
	if err != nil {
		return 0, 0, err
	}
	if matchup.Status != string(fantasy.MatchupFinal) || matchup.IsBye {
		return 0, 0, nil
	}

	week, ok, err := backfillWeek(matchup.ScoringPeriodsJson)
	if err != nil {
		return 0, 0, err
	}
	if !ok {
		return 0, 0, nil
	}
	var settings fantasy.LeagueSettings
	if err := json.Unmarshal(matchup.SettingsJson, &settings); err != nil {
		return 0, 0, fmt.Errorf("decode matchup scoring settings: %w", err)
	}

	hasHistory, err := r.db.Queries().MatchupHasWinChanceHistory(ctx, matchupID)
	if err != nil {
		return 0, 0, err
	}
	if hasHistory.Valid && hasHistory.Bool {
		return 0, 0, nil
	}
	starterRows, err := r.db.Queries().ListBackfillMatchupStarters(ctx, matchupID)
	if err != nil {
		return 0, 0, err
	}
	if len(starterRows) == 0 {
		return 0, 0, nil
	}

	starters, starterRowsByKey, err := backfillStarters(starterRows)
	if err != nil {
		return 0, 0, err
	}
	scoreboard, err := r.sources.NFLWeekScoreboard(ctx, int(matchup.Season), int(week))
	if err != nil {
		return 0, 0, err
	}
	games, err := r.fetchBackfillGames(ctx, scoreboard.Games)
	if err != nil {
		return 0, 0, err
	}
	timeline := replay.Build(replay.Input{
		Starters: starters, Games: games, Rules: settings.ScoringRules,
	})
	if len(timeline.Samples) == 0 && len(timeline.Changes) == 0 {
		return 0, 0, nil
	}
	return r.saveBackfillTimeline(ctx, matchupID, timeline, starterRowsByKey)
}

func backfillWeek(value []byte) (fantasy.ScoringPeriod, bool, error) {
	var weeks []fantasy.ScoringPeriod
	if err := json.Unmarshal(value, &weeks); err != nil {
		return 0, false, fmt.Errorf("decode matchup scoring periods: %w", err)
	}
	if len(weeks) != 1 || weeks[0] < 1 {
		return 0, false, nil
	}
	return weeks[0], true, nil
}

func backfillStarters(
	rows []store.ListBackfillMatchupStartersRow,
) ([]replay.Starter, map[string]store.ListBackfillMatchupStartersRow, error) {
	starters := make([]replay.Starter, 0, len(rows))
	rowsByKey := make(map[string]store.ListBackfillMatchupStartersRow, len(rows))
	for _, row := range rows {
		finalPoints, err := lineupPointsOrZero(row.Points)
		if err != nil {
			return nil, nil, err
		}
		projected, err := liveProjection(row.ProjectedPoints)
		if err != nil {
			return nil, nil, err
		}
		athleteID := numericAthleteID(row.AthleteID)
		key := backfillStarterKey(row.TeamID, row.PlayerID)
		starters = append(starters, replay.Starter{
			Key: key, TeamB: row.TeamB, AthleteID: athleteID, ProTeam: row.ProTeam,
			FinalPoints: finalPoints, Projected: projected,
		})
		rowsByKey[key] = row
	}
	return starters, rowsByKey, nil
}

func numericAthleteID(value string) string {
	if value == "" {
		return ""
	}
	if _, err := strconv.ParseUint(value, 10, 64); err != nil {
		return ""
	}
	return value
}

func backfillStarterKey(teamID, playerID pgtype.UUID) string {
	return teamID.String() + ":" + playerID.String()
}

func (r *Runner) fetchBackfillGames(
	ctx context.Context,
	scoreboardGames []fantasy.NFLGameStatus,
) ([]replay.Game, error) {
	games := make([]replay.Game, 0)
	for _, game := range scoreboardGames {
		plays, err := r.sources.NFLGamePlays(ctx, game.ID)
		if err != nil {
			return nil, err
		}
		games = append(games, replay.Game{
			HomeTeam: game.HomeTeam, AwayTeam: game.AwayTeam, Plays: plays,
		})
	}
	return games, nil
}

func (r *Runner) saveBackfillTimeline(
	ctx context.Context,
	matchupID pgtype.UUID,
	timeline replay.Timeline,
	starterRows map[string]store.ListBackfillMatchupStartersRow,
) (int, int, error) {
	sampleCount, changeCount := 0, 0
	err := r.db.InTx(ctx, func(queries *store.Queries) error {
		if err := queries.LockImportWrites(ctx); err != nil {
			return err
		}
		hasHistory, err := queries.MatchupHasWinChanceHistory(ctx, matchupID)
		if err != nil {
			return err
		}
		if hasHistory.Valid && hasHistory.Bool {
			return nil
		}

		observedAt := make(map[time.Time]bool, len(timeline.Samples))
		for _, sample := range timeline.Samples {
			at := uniqueBackfillSampleTime(sample.At, observedAt)
			params, err := newWinChanceSample(matchupID, at, sample.ScoreA, sample.ScoreB, sample.Estimate)
			if err != nil {
				return err
			}
			if err := queries.InsertWinChanceSample(ctx, params); err != nil {
				return err
			}
			sampleCount++
		}
		for _, change := range timeline.Changes {
			row, found := starterRows[change.Key]
			if !found {
				return fmt.Errorf("replay returned unknown matchup starter")
			}
			pointsBefore, err := fourDecimalNumeric(change.PointsBefore)
			if err != nil {
				return err
			}
			pointsAfter, err := fourDecimalNumeric(change.PointsAfter)
			if err != nil {
				return err
			}
			if err := queries.InsertLineupPointChange(ctx, store.InsertLineupPointChangeParams{
				MatchupID: matchupID, TeamID: row.TeamID, PlayerID: row.PlayerID,
				ObservedAt:       pgtype.Timestamptz{Time: change.At, Valid: true},
				PointsBefore:     pointsBefore,
				PointsAfter:      pointsAfter,
				TouchdownsBefore: pgtype.Int4{Int32: int32(change.TouchdownsBefore), Valid: true},
				TouchdownsAfter:  pgtype.Int4{Int32: int32(change.TouchdownsAfter), Valid: true},
				FieldGoalsBefore: pgtype.Int4{Int32: int32(change.FieldGoalsBefore), Valid: true},
				FieldGoalsAfter:  pgtype.Int4{Int32: int32(change.FieldGoalsAfter), Valid: true},
			}); err != nil {
				return err
			}
			changeCount++
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return sampleCount, changeCount, nil
}

func uniqueBackfillSampleTime(at time.Time, seen map[time.Time]bool) time.Time {
	at = at.UTC()
	for seen[at] {
		at = at.Add(time.Microsecond)
	}
	seen[at] = true
	return at
}
