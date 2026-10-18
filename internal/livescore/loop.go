package livescore

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/leaguesync"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

const (
	liveScoreboardTimeout = 15 * time.Second
	liveLeagueTimeout     = 30 * time.Second
	liveTeamWindow        = 15 * time.Minute
	lineupResyncCooldown  = 10 * time.Minute
	targetReadTimeout     = 5 * time.Second
)

// Loop refreshes live matchups each tick and runs win-chance gap-fill and backfill jobs.
type Loop struct {
	db      *database.DB
	sources leaguesync.SourceFactory
	sync    *leaguesync.Runner
	now     func() time.Time

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu               sync.Mutex
	lineupResyncAt   map[string]time.Time
	backfillRunning  map[string]bool
	backfillFailures map[string]failure
}

type failure struct {
	code string
	at   time.Time
}

// New creates a live score loop linked to the sync runner.
func New(db *database.DB, sources leaguesync.SourceFactory, syncRunner *leaguesync.Runner) *Loop {
	ctx, cancel := context.WithCancel(context.Background())
	return &Loop{
		db:               db,
		sources:          sources,
		sync:             syncRunner,
		now:              time.Now,
		ctx:              ctx,
		cancel:           cancel,
		lineupResyncAt:   map[string]time.Time{},
		backfillRunning:  map[string]bool{},
		backfillFailures: map[string]failure{},
	}
}

// Close cancels background live jobs and waits for them to record their outcome.
func (l *Loop) Close() {
	l.mu.Lock()
	l.cancel()
	l.mu.Unlock()
	l.wg.Wait()
}

// Run refreshes recently active matchups on each ticker event.
// Ticks run synchronously so one slow tick cannot overlap the next.
func (l *Loop) Run(ctx context.Context, interval time.Duration) {
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
			l.runLiveScoreTick(ctx, lastLive)
		}
	}
}

// runLiveScoreTick fetches the board and updates only matchups affected by recent live games.
func (l *Loop) runLiveScoreTick(ctx context.Context, lastLive map[string]time.Time) {
	scoreboardCtx, cancelScoreboard := context.WithTimeout(ctx, liveScoreboardTimeout)
	scoreboard, err := l.sources.NFLScoreboard(scoreboardCtx)
	cancelScoreboard()
	if err != nil {
		slog.Warn("live NFL scoreboard read failed", "code", leaguesync.FailureCode(err))
		return
	}
	if scoreboard.SeasonType != 2 {
		return
	}
	defer l.requestUnfilledWinChanceGaps(ctx)

	now := l.now()
	recordLiveTeams(scoreboard.Games, now, lastLive)
	activeTeams := activeNFLTeams(now, lastLive)
	if len(activeTeams) == 0 {
		slog.Debug("skipping live score tick without recently live NFL teams",
			"season", scoreboard.Season, "week", scoreboard.Week)
		return
	}

	ownerID, targets, err := l.sync.TargetsWithOwner(ctx)
	if err != nil {
		slog.Warn("live matchup targets read failed", "code", leaguesync.FailureCode(err))
		return
	}
	if !ownerID.Valid {
		return
	}

	candidateCtx, cancelCandidates := context.WithTimeout(ctx, targetReadTimeout)
	candidates, err := l.db.Queries().ListLiveMatchupCandidates(candidateCtx, store.ListLiveMatchupCandidatesParams{
		UserID: ownerID, Season: int32(scoreboard.Season), Week: int32(scoreboard.Week),
	})
	cancelCandidates()
	if err != nil {
		slog.Warn("live matchup candidates read failed", "code", leaguesync.FailureCode(err))
		return
	}

	l.updateLiveCandidates(ctx, scoreboard, targets, candidates, activeTeams)
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
func (l *Loop) updateLiveCandidates(
	ctx context.Context,
	scoreboard fantasy.NFLScoreboard,
	targets []leaguesync.Target,
	candidates []store.ListLiveMatchupCandidatesRow,
	activeTeams map[string]struct{},
) {
	targetByLeague := make(map[string]leaguesync.Target, len(targets))
	for _, target := range targets {
		targetByLeague[target.ID] = target
	}

	var batch sync.WaitGroup
	for _, candidate := range candidates {
		target, found := targetByLeague[candidate.LeagueSeasonID.String()]
		if !found || !liveCandidateRelevant(candidate.StarterProTeams, activeTeams) {
			continue
		}
		batch.Add(1)
		go func(target leaguesync.Target, candidate store.ListLiveMatchupCandidatesRow) {
			defer batch.Done()
			l.updateLiveCandidate(ctx, scoreboard, target, candidate)
		}(target, candidate)
	}
	batch.Wait()
}

// updateLiveCandidate reads and persists one league's current matchup.
func (l *Loop) updateLiveCandidate(
	parent context.Context,
	scoreboard fantasy.NFLScoreboard,
	target leaguesync.Target,
	candidate store.ListLiveMatchupCandidatesRow,
) {
	ctx, cancel := context.WithTimeout(parent, liveLeagueTimeout)
	defer cancel()

	source, err := l.sources.SourceFor(target)
	if err != nil {
		l.logLiveScoreFailure(target, err)
		return
	}
	scorer, ok := source.(fantasy.LiveScorer)
	if !ok {
		return
	}

	var settings fantasy.LeagueSettings
	if err := json.Unmarshal(candidate.SettingsJson, &settings); err != nil {
		l.logLiveScoreFailure(target, fmt.Errorf("decode league settings: %w", err))
		return
	}
	league := fantasy.League{Ref: target.Selection.League, Name: target.Name, Settings: settings}
	live, err := scorer.FetchLiveMatchup(ctx, league, fantasy.LiveMatchupRequest{
		Week: scoreboard.Week, Period: fantasy.MatchupPeriod(candidate.Period), TeamID: candidate.OwnerTeamID,
	})
	if err != nil {
		l.logLiveScoreFailure(target, err)
		return
	}

	updatedEntries, skippedEntries, lineupChanged, err := l.saveLiveMatchup(ctx, scoreboard, target, candidate, live)
	if err != nil {
		l.logLiveScoreFailure(target, err)
		return
	}
	if lineupChanged {
		l.startLineupResync(target)
	}
	slog.Info("live matchup updated", "league", target.ID, "provider", target.Selection.League.Provider,
		"updated_entries", updatedEntries, "skipped_entries", skippedEntries)
}

// startLineupResync launches one full sync after a changed live lineup, subject to a per-league cooldown.
func (l *Loop) startLineupResync(target leaguesync.Target) {
	now := l.now()
	l.mu.Lock()
	lastResync, hasResynced := l.lineupResyncAt[target.ID]
	if hasResynced && now.Sub(lastResync) < lineupResyncCooldown {
		l.mu.Unlock()
		return
	}
	if !l.sync.StartTarget(target) {
		l.mu.Unlock()
		return
	}
	if l.lineupResyncAt == nil {
		l.lineupResyncAt = make(map[string]time.Time)
	}
	l.lineupResyncAt[target.ID] = now
	l.mu.Unlock()

	slog.Info("live lineup changed; resyncing league", "league", target.ID)
}

// logLiveScoreFailure records a provider failure and marks reconnectable credentials.
func (l *Loop) logLiveScoreFailure(target leaguesync.Target, err error) {
	if leaguesync.ShouldMarkNeedsReconnect(err) {
		l.sync.MarkNeedsReconnect(target)
	}
	slog.Warn("live matchup update failed", "league", target.ID,
		"provider", target.Selection.League.Provider, "code", leaguesync.FailureCode(err))
}

// saveLiveMatchup validates provider identities and atomically persists scores, lineup observations, point changes, gaps, and win-chance samples.
func (l *Loop) saveLiveMatchup(
	ctx context.Context,
	scoreboard fantasy.NFLScoreboard,
	target leaguesync.Target,
	candidate store.ListLiveMatchupCandidatesRow,
	live fantasy.LiveMatchup,
) (int, int, bool, error) {
	updatedEntries, skippedEntries := 0, 0
	lineupChanged := false
	tickAt := l.now()
	err := l.db.InTx(ctx, func(queries *store.Queries) error {
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
		storedLineup, err := queries.ListMatchupLineupPlayers(ctx, candidate.MatchupID)
		if err != nil {
			return err
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
			FetchedAt: pgtype.Timestamptz{Time: l.now(), Valid: true}, MatchupID: candidate.MatchupID,
		}); err != nil {
			return err
		}
		reportedTeams := make(map[pgtype.UUID]struct{}, len(live.Lineups))
		livePlayers := make(map[liveMatchupLineupKey]struct{})

		observations, err := loadLiveLineupObservations(ctx, queries, candidate.MatchupID)
		if err != nil {
			return err
		}
		gapStartedAt, gapExists := staleLiveGapStart(observations, tickAt)
		if gapExists {
			if err := queries.InsertWinChanceGap(ctx, store.InsertWinChanceGapParams{
				MatchupID: candidate.MatchupID,
				StartedAt: pgtype.Timestamptz{Time: gapStartedAt, Valid: true},
				EndedAt:   pgtype.Timestamptz{Time: tickAt, Valid: true},
			}); err != nil {
				return err
			}
		}
		for _, lineup := range live.Lineups {
			teamID := teamIDs[lineup.TeamID]
			reportedTeams[teamID] = struct{}{}
			for _, entry := range lineup.Entries {
				row, matched, err := l.saveLiveLineupEntry(
					ctx, queries, candidate.MatchupID, teamID,
					string(target.Selection.League.Provider), entry, observations, tickAt, gapStartedAt,
				)
				if err != nil {
					return err
				}
				if !matched {
					lineupChanged = true
					skippedEntries++
				} else {
					updatedEntries++
					livePlayers[liveMatchupLineupKey{teamID: teamID, playerID: row.PlayerID}] = struct{}{}
					if row.Starter != entry.Starter {
						lineupChanged = true
					}
				}
			}
		}
		for _, row := range storedLineup {
			if _, reported := reportedTeams[row.TeamID]; !reported {
				continue
			}
			if _, found := livePlayers[liveMatchupLineupKey{teamID: row.TeamID, playerID: row.PlayerID}]; !found {
				lineupChanged = true
				break
			}
		}
		return l.saveWinChanceSample(ctx, queries, candidate, scoreboard, scoreA, scoreB)
	})
	return updatedEntries, skippedEntries, lineupChanged, err
}

type liveMatchupLineupKey struct {
	teamID   pgtype.UUID
	playerID pgtype.UUID
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
	if points == nil {
		return pgtype.Numeric{}, nil
	}
	return fourDecimalNumeric(*points)
}

// fourDecimalNumeric converts a value to the importer's four-decimal numeric scale.
func fourDecimalNumeric(value float64) (pgtype.Numeric, error) {
	var numeric pgtype.Numeric
	err := numeric.Scan(strconv.FormatFloat(value, 'f', 4, 64))
	return numeric, err
}

func liveCounts(counts *fantasy.ScoringCounts) (pgtype.Int4, pgtype.Int4) {
	if counts == nil {
		return pgtype.Int4{}, pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(counts.Touchdowns), Valid: true},
		pgtype.Int4{Int32: int32(counts.FieldGoals), Valid: true}
}
