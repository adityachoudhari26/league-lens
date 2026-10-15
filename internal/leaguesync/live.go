package leaguesync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/adityachoudhari26/league-lens/internal/winchance"
)

const (
	liveScoreboardTimeout = 15 * time.Second
	liveLeagueTimeout     = 30 * time.Second
	liveTeamWindow        = 15 * time.Minute
)

// RunLiveScores refreshes recently active matchups on each ticker event.
// Ticks run synchronously so one slow tick cannot overlap the next.
func (r *Runner) RunLiveScores(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		slog.Warn("live score loop requires a positive interval")
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastLive := make(map[string]time.Time)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.runLiveScoreTick(ctx, lastLive)
		}
	}
}

// runLiveScoreTick fetches the board and updates only matchups affected by recent live games.
func (r *Runner) runLiveScoreTick(ctx context.Context, lastLive map[string]time.Time) {
	scoreboardCtx, cancelScoreboard := context.WithTimeout(ctx, liveScoreboardTimeout)
	scoreboard, err := r.sources.NFLScoreboard(scoreboardCtx)
	cancelScoreboard()
	if err != nil {
		slog.Warn("live NFL scoreboard read failed", "code", failureCode(err))
		return
	}
	if scoreboard.SeasonType != 2 {
		return
	}

	now := r.now()
	recordLiveTeams(scoreboard.Games, now, lastLive)
	activeTeams := activeNFLTeams(now, lastLive)
	if len(activeTeams) == 0 {
		slog.Debug("skipping live score tick without recently live NFL teams",
			"season", scoreboard.Season, "week", scoreboard.Week)
		return
	}

	ownerID, targets, err := r.listTargetsWithOwner(ctx)
	if err != nil {
		slog.Warn("live matchup targets read failed", "code", failureCode(err))
		return
	}
	if !ownerID.Valid {
		return
	}

	candidateCtx, cancelCandidates := context.WithTimeout(ctx, targetReadTimeout)
	candidates, err := r.db.Queries().ListLiveMatchupCandidates(candidateCtx, store.ListLiveMatchupCandidatesParams{
		UserID: ownerID, Season: int32(scoreboard.Season), Week: int32(scoreboard.Week),
	})
	cancelCandidates()
	if err != nil {
		slog.Warn("live matchup candidates read failed", "code", failureCode(err))
		return
	}

	r.updateLiveCandidates(ctx, scoreboard, targets, candidates, activeTeams)
}

// recordLiveTeams remembers teams currently playing live games.
func recordLiveTeams(games []fantasy.NFLGameStatus, now time.Time, lastLive map[string]time.Time) {
	for _, game := range games {
		if game.State != fantasy.NFLGameLive {
			continue
		}
		if game.HomeTeam != "" {
			lastLive[game.HomeTeam] = now
		}
		if game.AwayTeam != "" {
			lastLive[game.AwayTeam] = now
		}
	}
}

// activeNFLTeams returns teams that played live within the correction window.
func activeNFLTeams(now time.Time, lastLive map[string]time.Time) map[string]struct{} {
	active := make(map[string]struct{}, len(lastLive))
	for team, liveAt := range lastLive {
		if now.Sub(liveAt) < liveTeamWindow {
			active[team] = struct{}{}
		}
	}
	return active
}

// liveCandidateRelevant keeps candidates with a live starter or unknown team data.
func liveCandidateRelevant(starterTeams []string, activeTeams map[string]struct{}) bool {
	if len(starterTeams) == 0 {
		return true
	}
	for _, team := range starterTeams {
		if team == "" {
			return true
		}
		if _, active := activeTeams[team]; active {
			return true
		}
	}
	return false
}

// updateLiveCandidates refreshes each relevant attached league concurrently.
func (r *Runner) updateLiveCandidates(
	ctx context.Context,
	scoreboard fantasy.NFLScoreboard,
	targets []Target,
	candidates []store.ListLiveMatchupCandidatesRow,
	activeTeams map[string]struct{},
) {
	targetByLeague := make(map[string]Target, len(targets))
	for _, target := range targets {
		targetByLeague[target.ID] = target
	}

	var batch sync.WaitGroup
	for _, candidate := range candidates {
		target, found := targetByLeague[uuidString(candidate.LeagueSeasonID)]
		if !found || !liveCandidateRelevant(candidate.StarterProTeams, activeTeams) {
			continue
		}
		batch.Add(1)
		go func(target Target, candidate store.ListLiveMatchupCandidatesRow) {
			defer batch.Done()
			r.updateLiveCandidate(ctx, scoreboard, target, candidate)
		}(target, candidate)
	}
	batch.Wait()
}

// updateLiveCandidate reads and persists one league's current matchup.
func (r *Runner) updateLiveCandidate(
	parent context.Context,
	scoreboard fantasy.NFLScoreboard,
	target Target,
	candidate store.ListLiveMatchupCandidatesRow,
) {
	ctx, cancel := context.WithTimeout(parent, liveLeagueTimeout)
	defer cancel()

	source, err := r.sources.SourceFor(target)
	if err != nil {
		r.logLiveScoreFailure(target, err)
		return
	}
	scorer, ok := source.(fantasy.LiveScorer)
	if !ok {
		return
	}

	var settings fantasy.LeagueSettings
	if err := json.Unmarshal(candidate.SettingsJson, &settings); err != nil {
		r.logLiveScoreFailure(target, fmt.Errorf("decode league settings: %w", err))
		return
	}
	league := fantasy.League{Ref: target.Selection.League, Name: target.Name, Settings: settings}
	live, err := scorer.FetchLiveMatchup(ctx, league, fantasy.LiveMatchupRequest{
		Week: scoreboard.Week, Period: fantasy.MatchupPeriod(candidate.Period), TeamID: candidate.OwnerTeamID,
	})
	if err != nil {
		r.logLiveScoreFailure(target, err)
		return
	}

	updatedEntries, skippedEntries, err := r.saveLiveMatchup(ctx, scoreboard, target, candidate, live)
	if err != nil {
		r.logLiveScoreFailure(target, err)
		return
	}
	slog.Info("live matchup updated", "league", target.ID, "provider", target.Selection.League.Provider,
		"updated_entries", updatedEntries, "skipped_entries", skippedEntries)
}

// logLiveScoreFailure records a provider failure and marks reconnectable credentials.
func (r *Runner) logLiveScoreFailure(target Target, err error) {
	if shouldMarkNeedsReconnect(err) {
		r.markNeedsReconnect(target)
	}
	slog.Warn("live matchup update failed", "league", target.ID,
		"provider", target.Selection.League.Provider, "code", failureCode(err))
}

// saveLiveMatchup validates provider identities and persists all live scores atomically.
func (r *Runner) saveLiveMatchup(
	ctx context.Context,
	scoreboard fantasy.NFLScoreboard,
	target Target,
	candidate store.ListLiveMatchupCandidatesRow,
	live fantasy.LiveMatchup,
) (int, int, error) {
	updatedEntries, skippedEntries := 0, 0
	err := r.db.InTx(ctx, func(queries *store.Queries) error {
		if err := queries.LockImportWrites(ctx); err != nil {
			return err
		}

		teamIDs, scoreA, scoreB, err := liveMatchupTeams(candidate, live.Matchup)
		if err != nil {
			return err
		}
		for _, lineup := range live.Lineups {
			if _, found := teamIDs[lineup.TeamID]; !found {
				return fmt.Errorf("live lineup team does not match stored matchup")
			}
		}

		numericScoreA, err := liveScore(scoreA)
		if err != nil {
			return err
		}
		numericScoreB, err := liveScore(scoreB)
		if err != nil {
			return err
		}
		if err := queries.UpdateLiveMatchupScores(ctx, store.UpdateLiveMatchupScoresParams{
			ScoreA: numericScoreA, ScoreB: numericScoreB,
			FetchedAt: pgtype.Timestamptz{Time: r.now(), Valid: true}, MatchupID: candidate.MatchupID,
		}); err != nil {
			return err
		}

		for _, lineup := range live.Lineups {
			for _, entry := range lineup.Entries {
				points, err := liveScore(entry.Points)
				if err != nil {
					return err
				}
				touchdowns, fieldGoals := liveCounts(entry.Scoring)
				rows, err := queries.UpdateLiveLineupEntry(ctx, store.UpdateLiveLineupEntryParams{
					Points: points, Touchdowns: touchdowns, FieldGoals: fieldGoals,
					MatchupID: candidate.MatchupID, TeamID: teamIDs[lineup.TeamID],
					Provider: string(target.Selection.League.Provider), ExternalPlayerID: entry.PlayerID,
				})
				if err != nil {
					return err
				}
				if rows == 0 {
					skippedEntries++
				} else {
					updatedEntries++
				}
			}
		}
		return r.saveWinChanceSample(ctx, queries, candidate, scoreboard, scoreA, scoreB)
	})
	return updatedEntries, skippedEntries, err
}

// saveWinChanceSample records a changed live win estimate with the lineup and score update.
func (r *Runner) saveWinChanceSample(
	ctx context.Context,
	queries *store.Queries,
	candidate store.ListLiveMatchupCandidatesRow,
	scoreboard fantasy.NFLScoreboard,
	scoreA, scoreB *float64,
) error {
	if !candidate.TeamBID.Valid {
		return nil
	}

	rows, err := queries.ListLiveMatchupStarters(ctx, candidate.MatchupID)
	if err != nil {
		return err
	}
	gamesByTeam, allFinal := scoreboardGamesByTeam(scoreboard.Games)
	sideA := winchance.Side{Score: scoreOrZero(scoreA)}
	sideB := winchance.Side{Score: scoreOrZero(scoreB)}
	for _, row := range rows {
		projection, err := liveProjection(row.ProjectedPoints)
		if err != nil {
			return err
		}
		starter := winchance.Starter{
			Projected: projection,
			Remaining: starterRemaining(row.ProTeam, gamesByTeam, allFinal),
		}
		switch row.TeamID {
		case candidate.TeamAID:
			sideA.Starters = append(sideA.Starters, starter)
		case candidate.TeamBID:
			sideB.Starters = append(sideB.Starters, starter)
		default:
			return fmt.Errorf("live matchup starter team does not match stored matchup")
		}
	}

	estimate := winchance.Estimate(sideA, sideB)
	sample, err := newWinChanceSample(candidate.MatchupID, r.now(), sideA.Score, sideB.Score, estimate)
	if err != nil {
		return err
	}
	previous, err := queries.LatestWinChanceSample(ctx, candidate.MatchupID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		matches, err := sameWinChanceSample(previous, sample)
		if err != nil {
			return err
		}
		if matches {
			return nil
		}
	}
	return queries.InsertWinChanceSample(ctx, sample)
}

func newWinChanceSample(
	matchupID pgtype.UUID,
	observedAt time.Time,
	scoreA, scoreB float64,
	estimate winchance.Result,
) (store.InsertWinChanceSampleParams, error) {
	values := []*float64{&estimate.WinChanceA, &scoreA, &scoreB, &estimate.ProjectedA, &estimate.ProjectedB}
	numbers := make([]pgtype.Numeric, len(values))
	for i, value := range values {
		number, err := liveScore(value)
		if err != nil {
			return store.InsertWinChanceSampleParams{}, err
		}
		numbers[i] = number
	}
	return store.InsertWinChanceSampleParams{
		MatchupID:      matchupID,
		ObservedAt:     pgtype.Timestamptz{Time: observedAt, Valid: true},
		TeamAWinChance: numbers[0],
		ScoreA:         numbers[1],
		ScoreB:         numbers[2],
		ProjectedA:     numbers[3],
		ProjectedB:     numbers[4],
	}, nil
}

func sameWinChanceSample(previous store.LatestWinChanceSampleRow, current store.InsertWinChanceSampleParams) (bool, error) {
	previousValues := []pgtype.Numeric{
		previous.TeamAWinChance, previous.ScoreA, previous.ScoreB, previous.ProjectedA, previous.ProjectedB,
	}
	currentValues := []pgtype.Numeric{
		current.TeamAWinChance, current.ScoreA, current.ScoreB, current.ProjectedA, current.ProjectedB,
	}
	for i, previousValue := range previousValues {
		previousFloat, err := previousValue.Float64Value()
		if err != nil {
			return false, err
		}
		currentFloat, err := currentValues[i].Float64Value()
		if err != nil {
			return false, err
		}
		if !previousFloat.Valid || !currentFloat.Valid || previousFloat.Float64 != currentFloat.Float64 {
			return false, nil
		}
	}
	return true, nil
}

func liveProjection(points pgtype.Numeric) (*float64, error) {
	if !points.Valid {
		return nil, nil
	}
	value, err := points.Float64Value()
	if err != nil {
		return nil, err
	}
	return &value.Float64, nil
}

func scoreboardGamesByTeam(games []fantasy.NFLGameStatus) (map[string]fantasy.NFLGameStatus, bool) {
	gamesByTeam := make(map[string]fantasy.NFLGameStatus, len(games)*2)
	allFinal := len(games) > 0
	for _, game := range games {
		if game.State != fantasy.NFLGameFinal {
			allFinal = false
		}
		if game.HomeTeam != "" {
			gamesByTeam[game.HomeTeam] = game
		}
		if game.AwayTeam != "" {
			gamesByTeam[game.AwayTeam] = game
		}
	}
	return gamesByTeam, allFinal
}

// starterRemaining maps known teams to game time, treating byes as complete and unknown teams conservatively.
func starterRemaining(proTeam string, games map[string]fantasy.NFLGameStatus, allFinal bool) float64 {
	if proTeam == "" {
		if allFinal {
			return 0
		}
		return 1
	}
	game, found := games[proTeam]
	if !found {
		return 0
	}
	return winchance.GameRemaining(game)
}

func scoreOrZero(score *float64) float64 {
	if score == nil {
		return 0
	}
	return *score
}

// liveMatchupTeams maps provider team IDs to stored IDs and aligns scores to stored sides.
func liveMatchupTeams(
	candidate store.ListLiveMatchupCandidatesRow,
	matchup fantasy.Matchup,
) (map[string]pgtype.UUID, *float64, *float64, error) {
	if matchup.ExternalID != candidate.ExternalMatchupID || int32(matchup.Period) != candidate.Period || !candidate.TeamAID.Valid {
		return nil, nil, nil, fmt.Errorf("live matchup identity does not match stored matchup")
	}

	teamIDs := map[string]pgtype.UUID{candidate.TeamAExternalID: candidate.TeamAID}
	if !candidate.TeamBID.Valid {
		if matchup.TeamB != nil || matchup.TeamA.TeamID != candidate.TeamAExternalID {
			return nil, nil, nil, fmt.Errorf("live matchup teams do not match stored matchup")
		}
		return teamIDs, matchup.TeamA.Score, nil, nil
	}
	if !candidate.TeamBExternalID.Valid || matchup.TeamB == nil {
		return nil, nil, nil, fmt.Errorf("live matchup teams do not match stored matchup")
	}

	teamIDs[candidate.TeamBExternalID.String] = candidate.TeamBID
	switch {
	case matchup.TeamA.TeamID == candidate.TeamAExternalID && matchup.TeamB.TeamID == candidate.TeamBExternalID.String:
		return teamIDs, matchup.TeamA.Score, matchup.TeamB.Score, nil
	case matchup.TeamA.TeamID == candidate.TeamBExternalID.String && matchup.TeamB.TeamID == candidate.TeamAExternalID:
		return teamIDs, matchup.TeamB.Score, matchup.TeamA.Score, nil
	default:
		return nil, nil, nil, fmt.Errorf("live matchup teams do not match stored matchup")
	}
}

// liveScore preserves importer's four-decimal numeric score scale.
func liveScore(points *float64) (pgtype.Numeric, error) {
	var numeric pgtype.Numeric
	if points == nil {
		return numeric, nil
	}
	err := numeric.Scan(strconv.FormatFloat(*points, 'f', 4, 64))
	return numeric, err
}

func liveCounts(counts *fantasy.ScoringCounts) (pgtype.Int4, pgtype.Int4) {
	if counts == nil {
		return pgtype.Int4{}, pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(counts.Touchdowns), Valid: true},
		pgtype.Int4{Int32: int32(counts.FieldGoals), Valid: true}
}
