package leaguesync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/replay"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

const liveGapIncompletePlayTail = 2 * time.Minute

type gapStarterCounts struct {
	touchdowns bool
	fieldGoals bool
}

// requestWinChanceGapFill schedules one stale live window for background replay.
func (r *Runner) requestWinChanceGapFill(matchupID pgtype.UUID, startedAt time.Time) bool {
	if !matchupID.Valid || startedAt.IsZero() {
		return false
	}
	key := winChanceGapKey(matchupID, startedAt)
	return r.requestWinChanceJob(key, func() {
		r.runWinChanceGapFill(matchupID, startedAt, key)
	})
}

func winChanceGapKey(matchupID pgtype.UUID, startedAt time.Time) string {
	return matchupID.String() + ":" + startedAt.UTC().Format(time.RFC3339Nano)
}

func (r *Runner) requestUnfilledWinChanceGaps(ctx context.Context) {
	if r.db == nil || ctx.Err() != nil {
		return
	}
	readCtx, cancel := context.WithTimeout(ctx, targetReadTimeout)
	gaps, err := r.db.Queries().ListUnfilledWinChanceGaps(readCtx)
	cancel()
	if err != nil {
		slog.Warn("unfilled win-chance gaps read failed", "code", failureCode(err))
		return
	}
	for _, gap := range gaps {
		r.requestWinChanceGapFill(gap.MatchupID, gap.StartedAt.Time)
	}
}

func (r *Runner) runWinChanceGapFill(matchupID pgtype.UUID, startedAt time.Time, key string) {
	defer r.wg.Done()
	ctx, cancel := context.WithTimeout(r.ctx, winChanceBackfillTimeout)
	defer cancel()

	samples, changes, err := r.fillWinChanceGap(ctx, matchupID, startedAt)
	r.mu.Lock()
	delete(r.backfillRunning, key)
	if err != nil {
		r.backfillFailures[key] = failure{code: failureCode(err), at: r.now()}
	} else {
		delete(r.backfillFailures, key)
	}
	r.mu.Unlock()

	if err != nil {
		slog.Warn("live win-chance gap fill failed", "matchup_id", matchupID.String(),
			"started_at", startedAt, "code", failureCode(err))
		return
	}
	slog.Info("live win-chance gap fill completed", "matchup_id", matchupID.String(),
		"started_at", startedAt, "samples", samples, "changes", changes)
}

func (r *Runner) fillWinChanceGap(
	ctx context.Context,
	matchupID pgtype.UUID,
	startedAt time.Time,
) (int, int, error) {
	gap, err := r.db.Queries().LoadWinChanceGap(ctx, store.LoadWinChanceGapParams{
		MatchupID: matchupID,
		StartedAt: pgtype.Timestamptz{Time: startedAt, Valid: true},
	})
	if err != nil {
		return 0, 0, err
	}
	if gap.FilledAt.Valid {
		return 0, 0, nil
	}
	week, ok, err := backfillWeek(gap.ScoringPeriodsJson)
	if err != nil {
		return 0, 0, err
	}
	if !ok {
		return 0, 0, fmt.Errorf("live matchup gap does not cover one scoring week")
	}
	var settings fantasy.LeagueSettings
	if err := json.Unmarshal(gap.SettingsJson, &settings); err != nil {
		return 0, 0, fmt.Errorf("decode live gap scoring settings: %w", err)
	}

	scoreboard, err := r.sources.NFLScoreboard(ctx)
	if err != nil {
		return 0, 0, err
	}
	if scoreboard.SeasonType != 2 || scoreboard.Season != int(gap.Season) ||
		scoreboard.Week != week {
		return 0, 0, fmt.Errorf("live gap does not match the current NFL scoreboard week")
	}
	starterRows, err := r.db.Queries().ListWinChanceGapStarters(ctx, store.ListWinChanceGapStartersParams{
		MatchupID: matchupID,
		StartedAt: pgtype.Timestamptz{Time: startedAt, Valid: true},
	})
	if err != nil {
		return 0, 0, err
	}
	starters, rowsByKey, countsByKey, proTeams, err := gapReplayStarters(starterRows)
	if err != nil {
		return 0, 0, err
	}
	if len(starters) == 0 {
		return 0, 0, fmt.Errorf("live gap has no starter snapshots")
	}
	games, err := r.fetchLiveGapGames(ctx, scoreboard.Games, proTeams, gap.EndedAt.Time)
	if err != nil {
		return 0, 0, err
	}
	timeline := replay.Build(replay.Input{
		Starters: starters, Games: games, Rules: settings.ScoringRules,
		From: gap.StartedAt.Time, To: gap.EndedAt.Time,
	})
	return r.saveLiveGapTimeline(ctx, matchupID, gap.StartedAt.Time, gap.EndedAt.Time,
		timeline, rowsByKey, countsByKey)
}

func gapReplayStarters(
	rows []store.ListWinChanceGapStartersRow,
) ([]replay.Starter, map[string]store.ListWinChanceGapStartersRow, map[string]gapStarterCounts, map[string]bool, error) {
	starters := make([]replay.Starter, 0, len(rows))
	rowsByKey := make(map[string]store.ListWinChanceGapStartersRow, len(rows))
	countsByKey := make(map[string]gapStarterCounts, len(rows))
	proTeams := make(map[string]bool)
	for _, row := range rows {
		startPoints, err := lineupPointsOrZero(row.PointsBefore)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		finalPoints, err := lineupPointsOrZero(row.PointsAfter)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		projected, err := liveProjection(row.ProjectedPoints)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		key := backfillStarterKey(row.TeamID, row.PlayerID)
		starters = append(starters, replay.Starter{
			Key: key, TeamB: row.TeamB, AthleteID: numericAthleteID(row.AthleteID), ProTeam: row.ProTeam,
			StartPoints: startPoints, StartTouchdowns: int4OrZero(row.TouchdownsBefore),
			StartFieldGoals: int4OrZero(row.FieldGoalsBefore), FinalPoints: finalPoints, Projected: projected,
		})
		rowsByKey[key] = row
		countsByKey[key] = gapStarterCounts{
			touchdowns: row.TouchdownsBefore.Valid,
			fieldGoals: row.FieldGoalsBefore.Valid,
		}
		if row.ProTeam != "" {
			proTeams[row.ProTeam] = true
		}
	}
	return starters, rowsByKey, countsByKey, proTeams, nil
}

func int4OrZero(value pgtype.Int4) int {
	if !value.Valid {
		return 0
	}
	return int(value.Int32)
}

func (r *Runner) fetchLiveGapGames(
	ctx context.Context,
	scoreboardGames []fantasy.NFLGameStatus,
	proTeams map[string]bool,
	endedAt time.Time,
) ([]replay.Game, error) {
	games := make([]replay.Game, 0)
	for _, game := range scoreboardGames {
		if !proTeams[game.HomeTeam] && !proTeams[game.AwayTeam] {
			continue
		}
		plays, err := r.sources.NFLGamePlays(ctx, game.ID)
		if err != nil {
			return nil, err
		}
		if game.State == fantasy.NFLGameLive && playsEndTooEarly(plays, endedAt) {
			slog.Warn("live win-chance gap play feed appears incomplete; leaving gap for retry",
				"event_id", game.ID, "ended_at", endedAt)
			return nil, errors.New("incomplete live play feed")
		}
		games = append(games, replay.Game{
			HomeTeam: game.HomeTeam, AwayTeam: game.AwayTeam, Plays: plays,
		})
	}
	return games, nil
}

func playsEndTooEarly(plays []fantasy.NFLPlay, endedAt time.Time) bool {
	latest := time.Time{}
	for _, play := range plays {
		if play.At.After(latest) {
			latest = play.At
		}
	}
	return latest.IsZero() || latest.Before(endedAt.Add(-liveGapIncompletePlayTail))
}

func (r *Runner) saveLiveGapTimeline(
	ctx context.Context,
	matchupID pgtype.UUID,
	startedAt, endedAt time.Time,
	timeline replay.Timeline,
	starterRows map[string]store.ListWinChanceGapStartersRow,
	counts map[string]gapStarterCounts,
) (int, int, error) {
	sampleCount, changeCount := 0, 0
	err := r.db.InTx(ctx, func(queries *store.Queries) error {
		if err := queries.LockImportWrites(ctx); err != nil {
			return err
		}
		gap, err := queries.LoadWinChanceGap(ctx, store.LoadWinChanceGapParams{
			MatchupID: matchupID,
			StartedAt: pgtype.Timestamptz{Time: startedAt, Valid: true},
		})
		if err != nil {
			return err
		}
		if gap.FilledAt.Valid {
			return nil
		}
		seen := make(map[time.Time]bool, len(timeline.Samples))
		for _, sample := range timeline.Samples {
			if !sample.At.After(startedAt) || !sample.At.Before(endedAt) {
				continue
			}
			at := uniqueBackfillSampleTime(sample.At, seen)
			if !at.Before(endedAt) {
				continue
			}
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
				return fmt.Errorf("replay returned unknown live gap starter")
			}
			pointsBefore, err := fourDecimalNumeric(change.PointsBefore)
			if err != nil {
				return err
			}
			pointsAfter, err := fourDecimalNumeric(change.PointsAfter)
			if err != nil {
				return err
			}
			knownCounts := counts[change.Key]
			if err := queries.InsertLineupPointChange(ctx, store.InsertLineupPointChangeParams{
				MatchupID: matchupID, TeamID: row.TeamID, PlayerID: row.PlayerID,
				ObservedAt:       pgtype.Timestamptz{Time: change.At, Valid: true},
				PointsBefore:     pointsBefore,
				PointsAfter:      pointsAfter,
				TouchdownsBefore: pgtype.Int4{Int32: int32(change.TouchdownsBefore), Valid: knownCounts.touchdowns},
				TouchdownsAfter:  pgtype.Int4{Int32: int32(change.TouchdownsAfter), Valid: knownCounts.touchdowns},
				FieldGoalsBefore: pgtype.Int4{Int32: int32(change.FieldGoalsBefore), Valid: knownCounts.fieldGoals},
				FieldGoalsAfter:  pgtype.Int4{Int32: int32(change.FieldGoalsAfter), Valid: knownCounts.fieldGoals},
			}); err != nil {
				return err
			}
			changeCount++
		}
		return queries.MarkWinChanceGapFilled(ctx, store.MarkWinChanceGapFilledParams{
			MatchupID: matchupID,
			StartedAt: pgtype.Timestamptz{Time: startedAt, Valid: true},
		})
	})
	if err != nil {
		return 0, 0, err
	}
	return sampleCount, changeCount, nil
}
