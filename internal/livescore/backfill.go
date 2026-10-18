package livescore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/leaguesync"
	"github.com/adityachoudhari26/league-lens/internal/replay"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

const (
	winChanceBackfillTimeout       = 2 * time.Minute
	winChanceBackfillFailureWindow = 10 * time.Minute
	lateHistoryGrace               = 5 * time.Minute
)

// RequestWinChanceBackfill starts a matchup replay unless one is running or recently failed.
func (l *Loop) RequestWinChanceBackfill(matchupID pgtype.UUID) bool {
	if !matchupID.Valid || l.ctx == nil {
		return false
	}
	key := matchupID.String()
	return l.requestWinChanceJob(key, func() {
		l.runWinChanceBackfill(matchupID, key)
	})
}

func (l *Loop) requestWinChanceJob(key string, run func()) bool {
	l.mu.Lock()
	if l.ctx == nil || l.ctx.Err() != nil {
		l.mu.Unlock()
		return false
	}
	if l.backfillRunning == nil {
		l.backfillRunning = map[string]bool{}
	}
	if l.backfillFailures == nil {
		l.backfillFailures = map[string]failure{}
	}
	if l.backfillRunning[key] {
		l.mu.Unlock()
		return true
	}
	if recentFailure, found := l.backfillFailures[key]; found {
		if l.now().Sub(recentFailure.at) < winChanceBackfillFailureWindow {
			l.mu.Unlock()
			return false
		}
		delete(l.backfillFailures, key)
	}
	l.backfillRunning[key] = true
	l.wg.Add(1)
	l.mu.Unlock()

	go run()
	return true
}

func (l *Loop) runWinChanceBackfill(matchupID pgtype.UUID, key string) {
	defer l.wg.Done()
	ctx, cancel := context.WithTimeout(l.ctx, winChanceBackfillTimeout)
	defer cancel()

	samples, changes, err := l.backfillMatchup(ctx, matchupID)
	l.mu.Lock()
	delete(l.backfillRunning, key)
	if err != nil {
		l.backfillFailures[key] = failure{code: leaguesync.FailureCode(err), at: l.now()}
	} else if samples == 0 && changes == 0 {
		l.backfillFailures[key] = failure{code: "nothing_to_rebuild", at: l.now()}
	} else {
		delete(l.backfillFailures, key)
	}
	l.mu.Unlock()

	if err != nil {
		slog.Warn("matchup win-chance backfill failed", "matchup_id", key, "code", leaguesync.FailureCode(err))
		return
	}
	slog.Info("matchup win-chance backfill completed",
		"matchup_id", key, "samples", samples, "changes", changes)
}

// backfillMatchup fetches and persists reconstructed history for an eligible matchup.
func (l *Loop) backfillMatchup(ctx context.Context, matchupID pgtype.UUID) (int, int, error) {
	matchup, err := l.db.Queries().BackfillMatchup(ctx, matchupID)
	if err != nil {
		return 0, 0, err
	}
	if matchup.IsBye {
		return 0, 0, nil
	}
	isFinal := matchup.Status == string(fantasy.MatchupFinal)

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

	hasHistory, err := l.db.Queries().MatchupHasWinChanceHistory(ctx, matchupID)
	if err != nil {
		return 0, 0, err
	}
	inProgressHistory := !isFinal && hasHistory.Valid && hasHistory.Bool
	tailReplay := isFinal && hasHistory.Valid && hasHistory.Bool
	var from time.Time
	if tailReplay {
		latestSample, err := l.db.Queries().LatestWinChanceSample(ctx, matchupID)
		if err == nil {
			matchesFinal, err := latestBackfillSampleMatchesFinal(latestSample, matchup.ScoreA, matchup.ScoreB)
			if err != nil {
				return 0, 0, err
			}
			if matchesFinal {
				return 0, 0, nil
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return 0, 0, err
		}

		latestObservation, err := l.db.Queries().LatestLiveLineupObservationAt(ctx, matchupID)
		if err != nil {
			return 0, 0, err
		}
		if !latestObservation.Valid {
			return 0, 0, nil
		}
		from = latestObservation.Time
	}
	starterRows, err := l.db.Queries().ListBackfillMatchupStarters(ctx, matchupID)
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
	var starterCounts map[string]gapStarterCounts
	if tailReplay {
		observations, err := loadLiveLineupObservations(ctx, l.db.Queries(), matchupID)
		if err != nil {
			return 0, 0, err
		}
		starters, starterCounts, err = tailBackfillStarters(starters, starterRowsByKey, observations)
		if err != nil {
			return 0, 0, err
		}
	}
	scoreboard, err := l.sources.NFLWeekScoreboard(ctx, int(matchup.Season), int(week))
	if err != nil {
		return 0, 0, err
	}
	if !isFinal && !hasStartedBackfillGame(scoreboard.Games) {
		return 0, 0, nil
	}
	games, err := l.fetchBackfillGames(ctx, scoreboard.Games, !isFinal)
	if err != nil {
		return 0, 0, err
	}
	replaceHistory := false
	if inProgressHistory {
		earliestPlay := earliestBackfillPlayAt(games)
		earliestSample, err := l.db.Queries().EarliestWinChanceSampleAt(ctx, matchupID)
		if err != nil {
			return 0, 0, err
		}
		if !earliestSample.Valid || earliestPlay.IsZero() ||
			!earliestSample.Time.After(earliestPlay.Add(lateHistoryGrace)) {
			return 0, 0, nil
		}
		replaceHistory = true
	}
	input := replay.Input{Starters: starters, Games: games, Rules: settings.ScoringRules}
	if tailReplay {
		to := latestBackfillPlayAt(games)
		if !to.After(from) {
			to = from.Add(time.Minute)
		}
		input.From, input.To = from, to
	} else if !isFinal {
		input.To = l.now()
	}
	timeline := replay.Build(input)
	if len(timeline.Samples) == 0 && len(timeline.Changes) == 0 {
		return 0, 0, nil
	}
	if tailReplay {
		return l.saveBackfillTailTimeline(ctx, matchupID, from, input.To, timeline,
			starterRowsByKey, starterCounts)
	}
	return l.saveBackfillTimeline(ctx, matchupID, timeline, starterRowsByKey, replaceHistory)
}

// latestBackfillSampleMatchesFinal compares the latest saved scores to the final matchup score.
func latestBackfillSampleMatchesFinal(
	sample store.LatestWinChanceSampleRow,
	scoreA, scoreB pgtype.Numeric,
) (bool, error) {
	if !scoreA.Valid || !scoreB.Valid || !sample.ScoreA.Valid || !sample.ScoreB.Valid {
		return false, nil
	}
	sampleA, err := requiredBackfillNumeric(sample.ScoreA)
	if err != nil {
		return false, err
	}
	sampleB, err := requiredBackfillNumeric(sample.ScoreB)
	if err != nil {
		return false, err
	}
	finalA, err := requiredBackfillNumeric(scoreA)
	if err != nil {
		return false, err
	}
	finalB, err := requiredBackfillNumeric(scoreB)
	if err != nil {
		return false, err
	}
	return math.Round(sampleA*10000) == math.Round(finalA*10000) &&
		math.Round(sampleB*10000) == math.Round(finalB*10000), nil
}

func requiredBackfillNumeric(value pgtype.Numeric) (float64, error) {
	result, err := value.Float64Value()
	if err != nil {
		return 0, err
	}
	if !result.Valid {
		return 0, fmt.Errorf("required backfill score is null")
	}
	return result.Float64, nil
}

// tailBackfillStarters anchors a final replay to observed values when available.
func tailBackfillStarters(
	starters []replay.Starter,
	rows map[string]store.ListBackfillMatchupStartersRow,
	observations map[liveLineupObservationKey]store.ListLiveLineupObservationsRow,
) ([]replay.Starter, map[string]gapStarterCounts, error) {
	counts := make(map[string]gapStarterCounts, len(starters))
	for i, starter := range starters {
		row := rows[starter.Key]
		observation, found := observations[liveLineupObservationKey{
			teamID: row.TeamID, playerID: row.PlayerID,
		}]
		if !found {
			starters[i].StartPoints = starter.FinalPoints
			starters[i].AthleteID = ""
			starters[i].ProTeam = ""
			starters[i].Projected = nil
			continue
		}
		startPoints, err := lineupPointsOrZero(observation.Points)
		if err != nil {
			return nil, nil, err
		}
		starters[i].StartPoints = startPoints
		starters[i].StartTouchdowns = int4OrZero(observation.Touchdowns)
		starters[i].StartFieldGoals = int4OrZero(observation.FieldGoals)
		counts[starter.Key] = gapStarterCounts{
			touchdowns: observation.Touchdowns.Valid,
			fieldGoals: observation.FieldGoals.Valid,
		}
	}
	return starters, counts, nil
}

// latestBackfillPlayAt returns the end of the final replay window.
func latestBackfillPlayAt(games []replay.Game) time.Time {
	var latest time.Time
	for _, game := range games {
		for _, play := range game.Plays {
			if play.At.After(latest) {
				latest = play.At
			}
		}
	}
	return latest
}

// earliestBackfillPlayAt returns the start of the first replay window.
func earliestBackfillPlayAt(games []replay.Game) time.Time {
	var earliest time.Time
	for _, game := range games {
		for _, play := range game.Plays {
			if earliest.IsZero() || play.At.Before(earliest) {
				earliest = play.At
			}
		}
	}
	return earliest
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

func (l *Loop) fetchBackfillGames(
	ctx context.Context,
	scoreboardGames []fantasy.NFLGameStatus,
	skipScheduled bool,
) ([]replay.Game, error) {
	games := make([]replay.Game, 0)
	for _, game := range scoreboardGames {
		if skipScheduled && game.State == fantasy.NFLGameScheduled {
			games = append(games, replay.Game{HomeTeam: game.HomeTeam, AwayTeam: game.AwayTeam})
			continue
		}
		plays, err := l.sources.NFLGamePlays(ctx, game.ID)
		if err != nil {
			return nil, err
		}
		games = append(games, replay.Game{
			HomeTeam: game.HomeTeam, AwayTeam: game.AwayTeam, Plays: plays,
		})
	}
	return games, nil
}

func hasStartedBackfillGame(games []fantasy.NFLGameStatus) bool {
	for _, game := range games {
		if game.State != fantasy.NFLGameScheduled {
			return true
		}
	}
	return false
}

func (l *Loop) saveBackfillTimeline(
	ctx context.Context,
	matchupID pgtype.UUID,
	timeline replay.Timeline,
	starterRows map[string]store.ListBackfillMatchupStartersRow,
	replace bool,
) (int, int, error) {
	sampleCount, changeCount := 0, 0
	err := l.db.InTx(ctx, func(queries *store.Queries) error {
		if err := queries.LockImportWrites(ctx); err != nil {
			return err
		}
		if replace {
			if err := queries.DeleteMatchupWinChanceHistory(ctx, matchupID); err != nil {
				return err
			}
		} else {
			hasHistory, err := queries.MatchupHasWinChanceHistory(ctx, matchupID)
			if err != nil {
				return err
			}
			if hasHistory.Valid && hasHistory.Bool {
				return nil
			}
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

// saveBackfillTailTimeline persists only the reconstructed tail if no newer history appeared.
func (l *Loop) saveBackfillTailTimeline(
	ctx context.Context,
	matchupID pgtype.UUID,
	from, to time.Time,
	timeline replay.Timeline,
	starterRows map[string]store.ListBackfillMatchupStartersRow,
	counts map[string]gapStarterCounts,
) (int, int, error) {
	targets := make(map[string]windowedStarterTarget, len(starterRows))
	for key, row := range starterRows {
		targets[key] = windowedStarterTarget{teamID: row.TeamID, playerID: row.PlayerID}
	}
	return l.saveWindowedTimeline(ctx, matchupID, from, to, timeline, targets, counts,
		windowedTimelineOptions{rejectNewerThan: &from, includeEndSample: true})
}

func uniqueBackfillSampleTime(at time.Time, seen map[time.Time]bool) time.Time {
	at = at.UTC()
	for seen[at] {
		at = at.Add(time.Microsecond)
	}
	seen[at] = true
	return at
}
