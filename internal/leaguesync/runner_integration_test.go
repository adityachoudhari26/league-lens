//go:build integration

package leaguesync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	migrations "github.com/adityachoudhari26/league-lens/db"
	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/espn"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/news"
	"github.com/adityachoudhari26/league-lens/internal/secrets"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/adityachoudhari26/league-lens/internal/winchance"
)

const owner = "owner@test"

type fakeSource struct {
	data     fantasy.LeagueData
	fetchErr error
	release  chan struct{}
	fetches  atomic.Int32
}

func (s *fakeSource) DescribeLeague(context.Context, string) (fantasy.League, error) {
	return s.data.League, nil
}

func (s *fakeSource) FetchLeague(ctx context.Context, _ fantasy.League, _ fantasy.FetchRequest) (fantasy.LeagueData, error) {
	s.fetches.Add(1)
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return fantasy.LeagueData{}, ctx.Err()
		}
	}
	return s.data, s.fetchErr
}

type fakeSources map[fantasy.ProviderID]fantasy.Provider

func (f fakeSources) NFLSchedule(context.Context, int) ([]fantasy.NFLGame, error) {
	return nil, errors.New("schedule unavailable in tests")
}

type scheduleSources struct {
	fakeSources
	games []fantasy.NFLGame
	err   error
	calls atomic.Int32
}

func (s *scheduleSources) NFLSchedule(context.Context, int) ([]fantasy.NFLGame, error) {
	s.calls.Add(1)
	return s.games, s.err
}

type importRefreshSources struct {
	scheduleSources
	news news.Source
}

func (s *importRefreshSources) NewsSource() news.Source { return s.news }

func (fakeSources) NFLScoreboard(context.Context) (fantasy.NFLScoreboard, error) {
	return fantasy.NFLScoreboard{}, nil
}

func (fakeSources) NFLWeekScoreboard(context.Context, int, int) (fantasy.NFLScoreboard, error) {
	return fantasy.NFLScoreboard{}, nil
}

func (fakeSources) NFLGamePlays(context.Context, string) ([]fantasy.NFLPlay, error) {
	return nil, nil
}

func (fakeSources) NewsSource() news.Source { return emptyNewsSource{} }

func (f fakeSources) SourceFor(target Target) (fantasy.Provider, error) {
	return f[target.Selection.League.Provider], nil
}

type liveScoreSources struct {
	fakeSources
	board fantasy.NFLScoreboard
	calls atomic.Int32
}

func (s *liveScoreSources) NFLScoreboard(context.Context) (fantasy.NFLScoreboard, error) {
	s.calls.Add(1)
	return s.board, nil
}

type fakeLiveProvider struct {
	*fakeSource
	live  fantasy.LiveMatchup
	calls atomic.Int32
}

func (s *fakeLiveProvider) FetchLiveMatchup(context.Context, fantasy.League, fantasy.LiveMatchupRequest) (fantasy.LiveMatchup, error) {
	s.calls.Add(1)
	return s.live, nil
}

type storedCredentialSources struct {
	ProviderSources
}

func (storedCredentialSources) NFLSchedule(context.Context, int) ([]fantasy.NFLGame, error) {
	return nil, nil
}

func (storedCredentialSources) NewsSource() news.Source { return emptyNewsSource{} }

type fakeNewsSources struct {
	fakeSources
	news news.Source
}

func (f fakeNewsSources) NewsSource() news.Source { return f.news }

type emptyNewsSource struct{}

func (emptyNewsSource) Name() string { return "empty" }

func (emptyNewsSource) PlayerNews(context.Context, []news.Player) ([]news.Item, error) {
	return nil, nil
}

type fakeNewsSource struct {
	err      error
	done     chan struct{}
	headline string
}

func (s fakeNewsSource) Name() string { return "fake" }

func (s fakeNewsSource) PlayerNews(_ context.Context, players []news.Player) ([]news.Item, error) {
	if s.done != nil {
		defer close(s.done)
	}
	if s.err != nil {
		return nil, s.err
	}
	if len(players) == 0 {
		return nil, nil
	}
	return []news.Item{{
		PlayerID:     players[0].ID,
		SourceItemID: "item-1",
		Headline:     s.headline,
		Summary:      "A saved fixture summary.",
		URL:          "https://example.test/player",
		SourceLabel:  "Fixture source",
		PublishedAt:  time.Now().UTC(),
	}}, nil
}

func openDatabase(t *testing.T) *database.DB {
	db, _ := openDatabaseWithTestPool(t, false)
	return db
}

func openDatabaseWithTestPool(t *testing.T, withPool bool) (*database.DB, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL required")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin := stdlib.OpenDB(*cfg)
	schema := fmt.Sprintf("leaguesync_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec("DROP SCHEMA " + schema + " CASCADE"); admin.Close() })
	cfg.RuntimeParams["search_path"] = schema
	sqlDB := stdlib.OpenDB(*cfg)
	defer sqlDB.Close()
	if err = migrations.Migrate(context.Background(), sqlDB, "up"); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := database.Open(context.Background(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)

	if !withPool {
		return db, nil
	}
	pool, err := pgxpool.New(context.Background(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return db, pool
}

func leagueData(provider fantasy.ProviderID, homeName string) fantasy.LeagueData {
	now := time.Now().UTC()
	league := fantasy.League{Ref: fantasy.LeagueRef{Provider: provider, ExternalID: "league-" + string(provider), Season: 2026}, Name: string(provider) + " league", TeamCount: 2}
	return fantasy.LeagueData{League: league, FetchedAt: now,
		Teams:   []fantasy.Team{{ExternalID: "1", Name: homeName}, {ExternalID: "2", Name: "Away"}},
		Players: []fantasy.Player{{ExternalID: "p", Name: "Fixture Player", Kind: fantasy.PlayerPerson}},
		Rosters: []fantasy.RosterSnapshot{
			{TeamID: "1", ScoringPeriod: 1, Origin: fantasy.SnapshotObserved, Coverage: fantasy.CoverageFull, ObservedAt: now, Entries: []fantasy.RosterEntry{{PlayerID: "p", Slot: fantasy.SlotBench}}},
			{TeamID: "2", ScoringPeriod: 1, Origin: fantasy.SnapshotObserved, Coverage: fantasy.CoverageFull, ObservedAt: now},
		},
	}
}

// seedLeague imports a league the way the CLI does, so the runner can rebuild its target.
func seedLeague(t *testing.T, db *database.DB, provider fantasy.ProviderID) {
	t.Helper()
	data := leagueData(provider, "Home")
	service := importer.Service{DB: db, Source: &fakeSource{data: data}}
	_, err := service.Import(context.Background(), importer.Request{OwnerEmail: owner, ExternalAccountID: "account", Selection: fantasy.ConnectionSelection{League: data.League.Ref, SelectedTeamIDs: []string{"1"}}})
	if err != nil {
		t.Fatal(err)
	}
}

func liveLeagueFixture() fantasy.LeagueData {
	data := leagueData(fantasy.ProviderSleeper, "Home")
	data.Players = append(data.Players,
		fantasy.Player{ExternalID: "q", Name: "Away Player", Kind: fantasy.PlayerPerson},
		fantasy.Player{ExternalID: "old", Name: "Dropped Player", Kind: fantasy.PlayerPerson},
		fantasy.Player{ExternalID: "bench", Name: "Bench Player", Kind: fantasy.PlayerPerson},
	)
	data.Rosters[0].ScoringPeriod = 5
	data.Rosters[0].Entries = []fantasy.RosterEntry{{PlayerID: "p", Slot: fantasy.SlotRB, ProTeam: "KC"}}
	data.Rosters[1].ScoringPeriod = 5
	data.Rosters[1].Entries = []fantasy.RosterEntry{{PlayerID: "q", Slot: fantasy.SlotRB, ProTeam: "SF"}}
	data.Matchups = []fantasy.Matchup{{
		ExternalID: "m", Period: 5, ScoringPeriods: []fantasy.ScoringPeriod{5}, Status: fantasy.MatchupFinal,
		TeamA: fantasy.MatchupSide{TeamID: "1", Score: floatPtr(10)},
		TeamB: &fantasy.MatchupSide{TeamID: "2", Score: floatPtr(20)},
	}}
	data.CompleteMatchupPeriods = []fantasy.MatchupPeriod{5}
	data.Lineups = []fantasy.MatchupLineup{
		{Period: 5, MatchupID: "m", TeamID: "1", Entries: []fantasy.LineupEntry{
			{PlayerID: "p", Slot: fantasy.SlotRB, Starter: true, Points: floatPtr(1), ProjectedPoints: floatPtr(60)},
			{PlayerID: "old", Slot: fantasy.SlotRB, Starter: true, Points: floatPtr(2), ProjectedPoints: floatPtr(60)},
			{PlayerID: "bench", Slot: fantasy.SlotBench, Points: floatPtr(4), ProjectedPoints: floatPtr(5)},
		}},
		{Period: 5, MatchupID: "m", TeamID: "2", Entries: []fantasy.LineupEntry{
			{PlayerID: "q", Slot: fantasy.SlotRB, Starter: true, Points: floatPtr(3), ProjectedPoints: floatPtr(30)},
		}},
	}
	return data
}

func floatPtr(value float64) *float64 { return &value }

func liveMatchupFixture(externalID string) fantasy.LiveMatchup {
	return fantasy.LiveMatchup{
		Matchup: fantasy.Matchup{
			ExternalID: externalID, Period: 5,
			TeamA: fantasy.MatchupSide{TeamID: "2", Score: floatPtr(200)},
			TeamB: &fantasy.MatchupSide{TeamID: "1", Score: floatPtr(120)},
		},
		Lineups: []fantasy.MatchupLineup{
			{TeamID: "2", Entries: []fantasy.LineupEntry{
				{PlayerID: "q", Points: floatPtr(22), Scoring: &fantasy.ScoringCounts{Touchdowns: 1, FieldGoals: 2}},
			}},
			{TeamID: "1", Entries: []fantasy.LineupEntry{
				{PlayerID: "p", Points: floatPtr(17), Scoring: &fantasy.ScoringCounts{Touchdowns: 2, FieldGoals: 1}},
				{PlayerID: "ghost", Points: floatPtr(8), Scoring: &fantasy.ScoringCounts{Touchdowns: 1}},
				{PlayerID: "bench", Points: floatPtr(4)},
			}},
		},
	}
}

func pointChangeLiveFixture(points, benchPoints float64, touchdowns, fieldGoals int) fantasy.LiveMatchup {
	live := liveMatchupFixture("m")
	live.Lineups[1].Entries[0].Points = floatPtr(points)
	live.Lineups[1].Entries[0].Scoring = &fantasy.ScoringCounts{Touchdowns: touchdowns, FieldGoals: fieldGoals}
	live.Lineups[1].Entries[2].Points = floatPtr(benchPoints)
	return live
}

func runLiveLoopUntilScoreboardTick(
	t *testing.T,
	db *database.DB,
	pool *pgxpool.Pool,
	sources *liveScoreSources,
	source *fakeLiveProvider,
	wantScoreA string,
	wantScoreB string,
	tickTimes ...time.Time,
) time.Time {
	t.Helper()
	var previousFetchedAt time.Time
	if err := pool.QueryRow(context.Background(), `
		SELECT m.fetched_at FROM matchups m
		JOIN league_seasons ls ON ls.id=m.league_season_id
		WHERE ls.external_league_id='league-sleeper' AND m.external_matchup_id='m'
	`).Scan(&previousFetchedAt); err != nil {
		t.Fatal(err)
	}
	runner := New(db, owner, sources, "")
	if len(tickTimes) > 0 {
		tickAt := tickTimes[0]
		runner.now = func() time.Time { return tickAt }
	}
	defer runner.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	initialCalls := source.calls.Load()
	go func() {
		runner.RunLiveScores(ctx, 10*time.Millisecond)
		close(done)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if source.calls.Load() > initialCalls {
			var scoreA, scoreB string
			var fetchedAt time.Time
			err := pool.QueryRow(context.Background(), `
				SELECT m.score_a::text, COALESCE(m.score_b::text, ''), m.fetched_at FROM matchups m
				JOIN league_seasons ls ON ls.id=m.league_season_id
				WHERE ls.external_league_id='league-sleeper' AND m.external_matchup_id='m'
			`).Scan(&scoreA, &scoreB, &fetchedAt)
			if err != nil {
				cancel()
				<-done
				t.Fatal(err)
			}
			if fetchedAt.After(previousFetchedAt) && scoreA == wantScoreA && scoreB == wantScoreB {
				cancel()
				<-done
				return fetchedAt
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatal("live score tick did not update matchup")
	return time.Time{}
}

type savedWinChanceSample struct {
	observedAt time.Time
	chance     string
	scoreA     string
	scoreB     string
	projectedA string
	projectedB string
}

func readWinChanceSamples(t *testing.T, pool *pgxpool.Pool) []savedWinChanceSample {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT s.observed_at, s.team_a_win_chance::text, s.score_a::text, s.score_b::text, s.projected_a::text, s.projected_b::text
		FROM win_chance_samples s
		JOIN matchups m ON m.id=s.matchup_id
		JOIN league_seasons ls ON ls.id=m.league_season_id
		WHERE ls.external_league_id='league-sleeper' AND m.external_matchup_id='m'
		ORDER BY observed_at
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var samples []savedWinChanceSample
	for rows.Next() {
		var sample savedWinChanceSample
		if err := rows.Scan(
			&sample.observedAt, &sample.chance, &sample.scoreA, &sample.scoreB, &sample.projectedA, &sample.projectedB,
		); err != nil {
			t.Fatal(err)
		}
		samples = append(samples, sample)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return samples
}

func expectedFixtureWinChance(scoreA, scoreB float64) string {
	estimate := winchance.Estimate(
		winchance.Side{
			Score: scoreA,
			Starters: []winchance.Starter{
				{Projected: floatPtr(60), Remaining: 1},
				{Projected: floatPtr(60), Remaining: 1},
			},
		},
		winchance.Side{
			Score:    scoreB,
			Starters: []winchance.Starter{{Projected: floatPtr(30), Remaining: 1}},
		},
	)
	return fmt.Sprintf("%.4f", estimate.WinChanceA)
}

func sameSampleValues(got, want savedWinChanceSample) bool {
	return got.chance == want.chance && got.scoreA == want.scoreA && got.scoreB == want.scoreB &&
		got.projectedA == want.projectedA && got.projectedB == want.projectedB
}

func readSavedLiveLineup(t *testing.T, pool *pgxpool.Pool, playerID string) (string, int32, int32) {
	t.Helper()
	var points string
	var touchdowns, fieldGoals int32
	err := pool.QueryRow(context.Background(), `
		SELECT ml.points::text, ml.touchdowns, ml.field_goals
		FROM matchup_lineups ml
		JOIN matchups m ON m.id=ml.matchup_id
		JOIN league_seasons ls ON ls.id=m.league_season_id
		JOIN player_external_ids x ON x.player_id=ml.player_id AND x.provider='sleeper'
		WHERE ls.external_league_id='league-sleeper' AND m.external_matchup_id='m' AND x.external_id=$1
	`, playerID).Scan(&points, &touchdowns, &fieldGoals)
	if err != nil {
		t.Fatal(err)
	}
	return points, touchdowns, fieldGoals
}

type savedLineupPointChange struct {
	playerID                          string
	pointsBefore, pointsAfter         string
	touchdownsBefore, touchdownsAfter pgtype.Int4
	fieldGoalsBefore, fieldGoalsAfter pgtype.Int4
	observedAt                        time.Time
}

func int4Is(value pgtype.Int4, want int32) bool {
	return value.Valid && value.Int32 == want
}

func readLineupPointChanges(t *testing.T, pool *pgxpool.Pool) []savedLineupPointChange {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT x.external_id, c.points_before::text, c.points_after::text,
			c.touchdowns_before, c.touchdowns_after, c.field_goals_before, c.field_goals_after, c.observed_at
		FROM lineup_point_changes c
		JOIN matchups m ON m.id=c.matchup_id
		JOIN league_seasons ls ON ls.id=m.league_season_id
		JOIN player_external_ids x ON x.player_id=c.player_id AND x.provider='sleeper'
		WHERE ls.external_league_id='league-sleeper' AND m.external_matchup_id='m'
		ORDER BY c.observed_at, x.external_id
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var changes []savedLineupPointChange
	for rows.Next() {
		var change savedLineupPointChange
		if err := rows.Scan(
			&change.playerID, &change.pointsBefore, &change.pointsAfter,
			&change.touchdownsBefore, &change.touchdownsAfter,
			&change.fieldGoalsBefore, &change.fieldGoalsAfter, &change.observedAt,
		); err != nil {
			t.Fatal(err)
		}
		changes = append(changes, change)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return changes
}

type savedLiveLineupObservation struct {
	points                 string
	touchdowns, fieldGoals pgtype.Int4
	observedAt             time.Time
}

func readLiveLineupObservation(t *testing.T, pool *pgxpool.Pool, playerID string) savedLiveLineupObservation {
	t.Helper()
	var observation savedLiveLineupObservation
	err := pool.QueryRow(context.Background(), `
		SELECT COALESCE(o.points::text, ''), o.touchdowns, o.field_goals, o.observed_at
		FROM live_lineup_observations o
		JOIN matchups m ON m.id=o.matchup_id
		JOIN league_seasons ls ON ls.id=m.league_season_id
		JOIN player_external_ids x ON x.player_id=o.player_id AND x.provider='sleeper'
		WHERE ls.external_league_id='league-sleeper' AND m.external_matchup_id='m' AND x.external_id=$1
	`, playerID).Scan(
		&observation.points, &observation.touchdowns, &observation.fieldGoals, &observation.observedAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	return observation
}

func TestLiveScoreLoopUpdatesMatchupAndReimportPreservesCounts(t *testing.T) {
	db, pool := openDatabaseWithTestPool(t, true)
	data := liveLeagueFixture()
	initial := &fakeSource{data: data}
	importerService := importer.Service{DB: db, Source: initial}
	request := importer.Request{
		OwnerEmail: owner, ExternalAccountID: "account",
		Selection: fantasy.ConnectionSelection{League: data.League.Ref, SelectedTeamIDs: []string{"1"}},
	}
	if _, err := importerService.Import(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	firstTick := liveMatchupFixture("m")
	firstTick.Lineups[1].Entries[0].Scoring = &fantasy.ScoringCounts{Touchdowns: 1}
	liveSource := &fakeLiveProvider{fakeSource: initial, live: firstTick}
	sources := &liveScoreSources{
		fakeSources: fakeSources{fantasy.ProviderSleeper: liveSource},
		board: fantasy.NFLScoreboard{
			Season: 2026, SeasonType: 2, Week: 5,
			Games: []fantasy.NFLGameStatus{{
				HomeTeam: "KC", AwayTeam: "SF", State: fantasy.NFLGameLive, Period: 1, Clock: 15 * time.Minute,
			}},
		},
	}
	firstFetchedAt := runLiveLoopUntilScoreboardTick(t, db, pool, sources, liveSource, "120.0000", "200.0000")

	ctx := context.Background()
	var scoreA, scoreB, status string
	if err := pool.QueryRow(ctx, `
		SELECT m.score_a::text, m.score_b::text, m.status FROM matchups m
		JOIN league_seasons ls ON ls.id=m.league_season_id
		WHERE ls.external_league_id='league-sleeper' AND m.external_matchup_id='m'
	`).Scan(&scoreA, &scoreB, &status); err != nil {
		t.Fatal(err)
	}
	if scoreA != "120.0000" || scoreB != "200.0000" || status != string(fantasy.MatchupFinal) {
		t.Fatalf("stored matchup scores/status = %s/%s/%s", scoreA, scoreB, status)
	}
	samples := readWinChanceSamples(t, pool)
	expectedFirstSample := savedWinChanceSample{
		chance: expectedFixtureWinChance(120, 200),
		scoreA: "120.0000", scoreB: "200.0000", projectedA: "240.0000", projectedB: "230.0000",
	}
	if len(samples) != 1 || !sameSampleValues(samples[0], expectedFirstSample) || samples[0].observedAt.IsZero() {
		t.Fatalf("first win-chance samples = %+v, want [%+v]", samples, expectedFirstSample)
	}

	secondFetchedAt := runLiveLoopUntilScoreboardTick(t, db, pool, sources, liveSource, "120.0000", "200.0000")
	if !secondFetchedAt.After(firstFetchedAt) {
		t.Fatalf("identical second tick at %v was not later than first tick at %v", secondFetchedAt, firstFetchedAt)
	}
	if samples := readWinChanceSamples(t, pool); len(samples) != 1 {
		t.Fatalf("identical later tick saved %d win-chance samples, want 1", len(samples))
	}

	rows, err := pool.Query(ctx, `
		SELECT x.external_id, ml.points::text, COALESCE(ml.touchdowns, -1), COALESCE(ml.field_goals, -1)
		FROM matchup_lineups ml
		JOIN matchups m ON m.id=ml.matchup_id
		JOIN league_seasons ls ON ls.id=m.league_season_id
		JOIN player_external_ids x ON x.player_id=ml.player_id AND x.provider='sleeper'
		WHERE ls.external_league_id='league-sleeper' AND m.external_matchup_id='m'
	`)
	if err != nil {
		t.Fatal(err)
	}
	type savedLineup struct {
		points                 string
		touchdowns, fieldGoals int32
	}
	saved := map[string]savedLineup{}
	defer rows.Close()
	for rows.Next() {
		var playerID string
		var entry savedLineup
		if err := rows.Scan(&playerID, &entry.points, &entry.touchdowns, &entry.fieldGoals); err != nil {
			t.Fatal(err)
		}
		saved[playerID] = entry
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if saved["p"] != (savedLineup{points: "17.0000", touchdowns: 1, fieldGoals: 0}) ||
		saved["q"] != (savedLineup{points: "22.0000", touchdowns: 1, fieldGoals: 2}) {
		t.Fatalf("stored live lineups = %+v", saved)
	}
	if _, exists := saved["ghost"]; exists {
		t.Fatal("unknown live player was inserted")
	}

	nilCountsTick := liveMatchupFixture("m")
	nilCountsTick.Matchup.TeamA.Score = floatPtr(201)
	nilCountsTick.Matchup.TeamB.Score = floatPtr(121)
	nilCountsTick.Lineups[1].Entries[0].Points = floatPtr(18)
	nilCountsTick.Lineups[1].Entries[0].Scoring = nil
	liveSource.live = nilCountsTick
	thirdFetchedAt := runLiveLoopUntilScoreboardTick(t, db, pool, sources, liveSource, "121.0000", "201.0000")
	if !thirdFetchedAt.After(secondFetchedAt) {
		t.Fatalf("changed third tick at %v was not later than second tick at %v", thirdFetchedAt, secondFetchedAt)
	}
	if points, touchdowns, fieldGoals := readSavedLiveLineup(t, pool, "p"); points != "18.0000" || touchdowns != 1 || fieldGoals != 0 {
		t.Fatalf("nil-count tick saved %s points and %d/%d counts, want 18.0000 and 1/0", points, touchdowns, fieldGoals)
	}
	samples = readWinChanceSamples(t, pool)
	expectedChangedSample := savedWinChanceSample{
		chance: expectedFixtureWinChance(121, 201),
		scoreA: "121.0000", scoreB: "201.0000", projectedA: "241.0000", projectedB: "231.0000",
	}
	if len(samples) != 2 || !sameSampleValues(samples[1], expectedChangedSample) ||
		!samples[1].observedAt.After(samples[0].observedAt) {
		t.Fatalf("changed-score win-chance samples = %+v, want second sample %+v", samples, expectedChangedSample)
	}

	zeroCountsTick := liveMatchupFixture("m")
	zeroCountsTick.Matchup.TeamA.Score = floatPtr(202)
	zeroCountsTick.Matchup.TeamB.Score = floatPtr(122)
	zeroCountsTick.Lineups[1].Entries[0].Points = floatPtr(19)
	zeroCountsTick.Lineups[1].Entries[0].Scoring = &fantasy.ScoringCounts{}
	liveSource.live = zeroCountsTick
	runLiveLoopUntilScoreboardTick(t, db, pool, sources, liveSource, "122.0000", "202.0000")
	if points, touchdowns, fieldGoals := readSavedLiveLineup(t, pool, "p"); points != "19.0000" || touchdowns != 0 || fieldGoals != 0 {
		t.Fatalf("zero-count tick saved %s points and %d/%d counts, want 19.0000 and 0/0", points, touchdowns, fieldGoals)
	}

	data.Lineups[0].Entries = []fantasy.LineupEntry{{PlayerID: "p", Slot: fantasy.SlotRB, Starter: true, Points: floatPtr(1)}}
	data.Lineups[1].Entries = []fantasy.LineupEntry{{PlayerID: "q", Slot: fantasy.SlotRB, Starter: true, Points: floatPtr(3)}}
	importerService.Source = &fakeSource{data: data}
	if _, err := importerService.Import(ctx, request); err != nil {
		t.Fatal(err)
	}
	var touchdowns, fieldGoals int32
	if err := pool.QueryRow(ctx, `
		SELECT touchdowns, field_goals FROM matchup_lineups ml
		JOIN matchups m ON m.id=ml.matchup_id
		JOIN league_seasons ls ON ls.id=m.league_season_id
		JOIN player_external_ids x ON x.player_id=ml.player_id AND x.provider='sleeper'
		WHERE ls.external_league_id='league-sleeper' AND m.external_matchup_id='m' AND x.external_id='p'
	`).Scan(&touchdowns, &fieldGoals); err != nil {
		t.Fatal(err)
	}
	if touchdowns != 0 || fieldGoals != 0 {
		t.Fatalf("retained counts = %d/%d, want 0/0", touchdowns, fieldGoals)
	}
	if err := pool.QueryRow(ctx, `
		SELECT touchdowns, field_goals FROM matchup_lineups ml
		JOIN matchups m ON m.id=ml.matchup_id
		JOIN league_seasons ls ON ls.id=m.league_season_id
		JOIN player_external_ids x ON x.player_id=ml.player_id AND x.provider='sleeper'
		WHERE ls.external_league_id='league-sleeper' AND m.external_matchup_id='m' AND x.external_id='q'
	`).Scan(&touchdowns, &fieldGoals); err != nil {
		t.Fatal(err)
	}
	if touchdowns != 1 || fieldGoals != 2 {
		t.Fatalf("second retained counts = %d/%d, want 1/2", touchdowns, fieldGoals)
	}
	var dropped int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM matchup_lineups ml
		JOIN matchups m ON m.id=ml.matchup_id
		JOIN league_seasons ls ON ls.id=m.league_season_id
		JOIN player_external_ids x ON x.player_id=ml.player_id AND x.provider='sleeper'
		WHERE ls.external_league_id='league-sleeper' AND m.external_matchup_id='m' AND x.external_id='old'
	`).Scan(&dropped); err != nil {
		t.Fatal(err)
	}
	if dropped != 0 {
		t.Fatalf("dropped player lineup rows = %d, want 0", dropped)
	}
}

func TestLiveScoreLoopPersistsPointChangesFromLiveObservations(t *testing.T) {
	db, pool := openDatabaseWithTestPool(t, true)
	data := liveLeagueFixture()
	initial := &fakeSource{data: data}
	importerService := importer.Service{DB: db, Source: initial}
	if _, err := importerService.Import(context.Background(), importer.Request{
		OwnerEmail: owner, ExternalAccountID: "account",
		Selection: fantasy.ConnectionSelection{League: data.League.Ref, SelectedTeamIDs: []string{"1"}},
	}); err != nil {
		t.Fatal(err)
	}

	liveSource := &fakeLiveProvider{fakeSource: initial}
	sources := &liveScoreSources{
		fakeSources: fakeSources{fantasy.ProviderSleeper: liveSource},
		board: fantasy.NFLScoreboard{
			Season: 2026, SeasonType: 2, Week: 5,
			Games: []fantasy.NFLGameStatus{{
				HomeTeam: "KC", AwayTeam: "SF", State: fantasy.NFLGameLive, Period: 1, Clock: 15 * time.Minute,
			}},
		},
	}
	firstAt := time.Now().UTC().Add(time.Minute)
	liveSource.live = pointChangeLiveFixture(17, 4, 0, 0)
	runLiveLoopUntilScoreboardTick(t, db, pool, sources, liveSource, "120.0000", "200.0000", firstAt)
	if changes := readLineupPointChanges(t, pool); len(changes) != 0 {
		t.Fatalf("first tick saved %d point changes, want none", len(changes))
	}
	var observationCount int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM live_lineup_observations o
		JOIN matchups m ON m.id=o.matchup_id
		JOIN league_seasons ls ON ls.id=m.league_season_id
		WHERE ls.external_league_id='league-sleeper' AND m.external_matchup_id='m'
	`).Scan(&observationCount); err != nil {
		t.Fatal(err)
	}
	if observationCount != 3 {
		t.Fatalf("first tick saved %d observations, want 3", observationCount)
	}
	firstObservation := readLiveLineupObservation(t, pool, "p")
	if firstObservation.points != "17.0000" || !int4Is(firstObservation.touchdowns, 0) ||
		!int4Is(firstObservation.fieldGoals, 0) {
		t.Fatalf("first live observation = %+v, want 17 points and known 0/0 counts", firstObservation)
	}

	secondAt := firstAt.Add(time.Minute)
	liveSource.live = pointChangeLiveFixture(23.8, 4, 1, 0)
	secondFetchedAt := runLiveLoopUntilScoreboardTick(t, db, pool, sources, liveSource, "120.0000", "200.0000", secondAt)
	changes := readLineupPointChanges(t, pool)
	if len(changes) != 1 {
		t.Fatalf("second tick saved %d point changes, want 1", len(changes))
	}
	firstChange := changes[0]
	if firstChange.playerID != "p" || firstChange.pointsBefore != "17.0000" || firstChange.pointsAfter != "23.8000" ||
		!int4Is(firstChange.touchdownsBefore, 0) ||
		!int4Is(firstChange.touchdownsAfter, 1) ||
		!int4Is(firstChange.fieldGoalsBefore, 0) ||
		!int4Is(firstChange.fieldGoalsAfter, 0) ||
		!firstChange.observedAt.Equal(secondFetchedAt) {
		t.Fatalf("second-tick point change = %+v, want p 17.0000→23.8000 and TD 0→1", firstChange)
	}

	thirdAt := secondAt.Add(time.Minute)
	liveSource.live = pointChangeLiveFixture(23.8, 4, 1, 0)
	runLiveLoopUntilScoreboardTick(t, db, pool, sources, liveSource, "120.0000", "200.0000", thirdAt)
	if changes := readLineupPointChanges(t, pool); len(changes) != 1 {
		t.Fatalf("identical tick saved %d point changes, want 1 total", len(changes))
	}

	benchChangeAt := thirdAt.Add(time.Minute)
	liveSource.live = pointChangeLiveFixture(23.8, 7, 1, 0)
	runLiveLoopUntilScoreboardTick(t, db, pool, sources, liveSource, "120.0000", "200.0000", benchChangeAt)
	if changes := readLineupPointChanges(t, pool); len(changes) != 1 {
		t.Fatalf("bench points change saved %d point changes, want 1 total", len(changes))
	}

	gapTickAt := benchChangeAt.Add(liveBaselineMaxAge + time.Minute)
	liveSource.live = pointChangeLiveFixture(30, 7, 1, 0)
	gapFetchedAt := runLiveLoopUntilScoreboardTick(t, db, pool, sources, liveSource, "120.0000", "200.0000", gapTickAt)
	if changes := readLineupPointChanges(t, pool); len(changes) != 1 {
		t.Fatalf("tick after observation gap saved %d point changes, want 1 total", len(changes))
	}
	gapObservation := readLiveLineupObservation(t, pool, "p")
	if gapObservation.points != "30.0000" || !gapObservation.observedAt.Equal(gapFetchedAt) {
		t.Fatalf("post-gap observation = %+v, want 30.0000 at %s", gapObservation, gapTickAt)
	}

	if _, err := pool.Exec(context.Background(), `
		UPDATE matchup_lineups ml
		SET points=99.0000
		FROM matchups m, league_seasons ls, player_external_ids x
		WHERE ml.matchup_id=m.id AND m.league_season_id=ls.id
			AND ls.external_league_id='league-sleeper' AND m.external_matchup_id='m'
			AND x.player_id=ml.player_id AND x.provider='sleeper' AND x.external_id='p'
	`); err != nil {
		t.Fatal(err)
	}
	refreshTickAt := gapTickAt.Add(time.Minute)
	liveSource.live = pointChangeLiveFixture(30.5, 7, 1, 0)
	runLiveLoopUntilScoreboardTick(t, db, pool, sources, liveSource, "120.0000", "200.0000", refreshTickAt)
	changes = readLineupPointChanges(t, pool)
	if len(changes) != 2 {
		t.Fatalf("post-refresh tick saved %d point changes, want 2 total", len(changes))
	}
	lastChange := changes[1]
	if lastChange.playerID != "p" || lastChange.pointsBefore != "30.0000" || lastChange.pointsAfter != "30.5000" {
		t.Fatalf("post-refresh point change = %+v, want 30.0000→30.5000 from the live baseline", lastChange)
	}
}

func TestLiveScoreLoopDoesNotSampleMatchupWithoutOpponent(t *testing.T) {
	db, pool := openDatabaseWithTestPool(t, true)
	data := liveLeagueFixture()
	data.Matchups[0].TeamB = nil
	data.Lineups = data.Lineups[:1]
	initial := &fakeSource{data: data}
	importerService := importer.Service{DB: db, Source: initial}
	if _, err := importerService.Import(context.Background(), importer.Request{
		OwnerEmail: owner, ExternalAccountID: "account",
		Selection: fantasy.ConnectionSelection{League: data.League.Ref, SelectedTeamIDs: []string{"1"}},
	}); err != nil {
		t.Fatal(err)
	}
	liveSource := &fakeLiveProvider{fakeSource: initial, live: fantasy.LiveMatchup{
		Matchup: fantasy.Matchup{
			ExternalID: "m", Period: 5, TeamA: fantasy.MatchupSide{TeamID: "1", Score: floatPtr(12)},
		},
		Lineups: []fantasy.MatchupLineup{{TeamID: "1", Entries: []fantasy.LineupEntry{
			{PlayerID: "p", Points: floatPtr(12)},
		}}},
	}}
	sources := &liveScoreSources{
		fakeSources: fakeSources{fantasy.ProviderSleeper: liveSource},
		board: fantasy.NFLScoreboard{
			Season: 2026, SeasonType: 2, Week: 5,
			Games: []fantasy.NFLGameStatus{{
				HomeTeam: "KC", AwayTeam: "SF", State: fantasy.NFLGameLive, Period: 1, Clock: 15 * time.Minute,
			}},
		},
	}
	runLiveLoopUntilScoreboardTick(t, db, pool, sources, liveSource, "12.0000", "")
	if samples := readWinChanceSamples(t, pool); len(samples) != 0 {
		t.Fatalf("matchup without an opponent saved %d win-chance samples", len(samples))
	}
}

func TestLiveScoreLoopRejectsMismatchedMatchupID(t *testing.T) {
	db, pool := openDatabaseWithTestPool(t, true)
	data := liveLeagueFixture()
	initial := &fakeSource{data: data}
	importerService := importer.Service{DB: db, Source: initial}
	if _, err := importerService.Import(context.Background(), importer.Request{
		OwnerEmail: owner, ExternalAccountID: "account",
		Selection: fantasy.ConnectionSelection{League: data.League.Ref, SelectedTeamIDs: []string{"1"}},
	}); err != nil {
		t.Fatal(err)
	}
	liveSource := &fakeLiveProvider{fakeSource: initial, live: liveMatchupFixture("wrong")}
	sources := &liveScoreSources{fakeSources: fakeSources{fantasy.ProviderSleeper: liveSource},
		board: fantasy.NFLScoreboard{Season: 2026, SeasonType: 2, Week: 5,
			Games: []fantasy.NFLGameStatus{{HomeTeam: "KC", AwayTeam: "SF", State: fantasy.NFLGameLive}}}}
	runner := New(db, owner, sources, "")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runner.RunLiveScores(ctx, 10*time.Millisecond)
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for liveSource.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	called := liveSource.calls.Load() > 0
	cancel()
	<-done
	runner.Close()
	if !called {
		t.Fatal("live provider was not called")
	}
	var scoreA, scoreB string
	var touchdowns, fieldGoals *int32
	if err := pool.QueryRow(context.Background(), `
		SELECT score_a::text,score_b::text,ml.touchdowns,ml.field_goals FROM matchups m
		JOIN league_seasons ls ON ls.id=m.league_season_id
		JOIN matchup_lineups ml ON ml.matchup_id=m.id
		JOIN player_external_ids x ON x.player_id=ml.player_id AND x.provider='sleeper' AND x.external_id='p'
		WHERE ls.external_league_id='league-sleeper' AND m.external_matchup_id='m'
	`).Scan(&scoreA, &scoreB, &touchdowns, &fieldGoals); err != nil {
		t.Fatal(err)
	}
	if scoreA != "10.0000" || scoreB != "20.0000" {
		t.Fatalf("mismatched matchup changed scores to %s/%s", scoreA, scoreB)
	}
	if touchdowns != nil || fieldGoals != nil {
		t.Fatalf("mismatched matchup wrote counts %v/%v", touchdowns, fieldGoals)
	}
}

func TestLiveScoreLoopWithNoLiveGamesDoesNotCallLeagueProvider(t *testing.T) {
	db := openDatabase(t)
	data := liveLeagueFixture()
	initial := &fakeSource{data: data}
	importerService := importer.Service{DB: db, Source: initial}
	if _, err := importerService.Import(context.Background(), importer.Request{
		OwnerEmail: owner, ExternalAccountID: "account",
		Selection: fantasy.ConnectionSelection{League: data.League.Ref, SelectedTeamIDs: []string{"1"}},
	}); err != nil {
		t.Fatal(err)
	}
	liveSource := &fakeLiveProvider{fakeSource: initial, live: liveMatchupFixture("m")}
	sources := &liveScoreSources{fakeSources: fakeSources{fantasy.ProviderSleeper: liveSource},
		board: fantasy.NFLScoreboard{Season: 2026, SeasonType: 2, Week: 5}}
	runner := New(db, owner, sources, "")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runner.RunLiveScores(ctx, 10*time.Millisecond)
		close(done)
	}()
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-done
	runner.Close()
	if sources.calls.Load() == 0 {
		t.Fatal("scoreboard was not read")
	}
	if liveSource.calls.Load() != 0 {
		t.Fatalf("league provider calls = %d, want 0", liveSource.calls.Load())
	}
}

func waitUntilIdle(t *testing.T, runner *Runner) map[fantasy.ProviderID]Status {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		statuses, err := runner.Status(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		byProvider := map[fantasy.ProviderID]Status{}
		syncing := false
		for _, status := range statuses {
			byProvider[status.Target.Selection.League.Provider] = status
			syncing = syncing || status.State == StateSyncing
		}
		if !syncing {
			return byProvider
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("sync did not finish")
	return nil
}

func homeTeamName(t *testing.T, db *database.DB, provider fantasy.ProviderID) string {
	t.Helper()
	ctx := context.Background()
	queries := db.Queries()
	user, err := queries.GetUserByEmail(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	leagues, err := queries.ListLeaguesForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, league := range leagues {
		if league.Provider != string(provider) {
			continue
		}
		teams, err := queries.ListTeamsForUser(ctx, store.ListTeamsForUserParams{UserID: user.ID, LeagueSeasonID: league.ID})
		if err != nil {
			t.Fatal(err)
		}
		for _, team := range teams {
			if team.ExternalTeamID == "1" {
				return team.Name
			}
		}
	}
	t.Fatalf("no home team for %s", provider)
	return ""
}

func TestRunnerRefreshesLeaguesAndIsolatesFailures(t *testing.T) {
	db := openDatabase(t)
	seedLeague(t, db, fantasy.ProviderSleeper)
	seedLeague(t, db, fantasy.ProviderESPN)
	sources := fakeSources{
		fantasy.ProviderSleeper: &fakeSource{data: leagueData(fantasy.ProviderSleeper, "Home Renamed")},
		fantasy.ProviderESPN:    &fakeSource{data: leagueData(fantasy.ProviderESPN, "Home Renamed"), fetchErr: errors.New("espn unavailable")},
	}
	runner := New(db, owner, sources, "")
	t.Cleanup(runner.Close)

	started, err := runner.Start(context.Background(), false)
	if err != nil || len(started) != 2 {
		t.Fatalf("start = %d statuses, %v", len(started), err)
	}
	for _, status := range started {
		if status.State != StateSyncing {
			t.Fatalf("started league state = %s, want syncing", status.State)
		}
	}

	finished := waitUntilIdle(t, runner)
	if got := finished[fantasy.ProviderSleeper].State; got != StateSucceeded {
		t.Errorf("sleeper state = %s, want succeeded", got)
	}
	if got := homeTeamName(t, db, fantasy.ProviderSleeper); got != "Home Renamed" {
		t.Errorf("sleeper team = %q, want refreshed name", got)
	}
	espnStatus := finished[fantasy.ProviderESPN]
	if espnStatus.State != StateFailed || espnStatus.ErrorCode != "provider_fetch_failed" || espnStatus.FailedAt.IsZero() {
		t.Errorf("espn status = %+v, want saved provider_fetch_failed", espnStatus)
	}
	if got := homeTeamName(t, db, fantasy.ProviderESPN); got != "Home" {
		t.Errorf("espn team = %q, want last successful data", got)
	}
}

func TestStartOnlyStaleSkipsFreshLeagues(t *testing.T) {
	db := openDatabase(t)
	seedLeague(t, db, fantasy.ProviderSleeper)
	source := &fakeSource{data: leagueData(fantasy.ProviderSleeper, "Home")}
	runner := New(db, owner, fakeSources{fantasy.ProviderSleeper: source}, "")
	t.Cleanup(runner.Close)

	statuses, err := runner.Start(context.Background(), true)
	if err != nil || len(statuses) != 1 || statuses[0].State != StateSucceeded {
		t.Fatalf("start = %+v, %v; want fresh league left alone", statuses, err)
	}
	if source.fetches.Load() != 0 {
		t.Fatal("fresh league was fetched")
	}
}

func TestStartDoesNotDuplicateRunningSync(t *testing.T) {
	db := openDatabase(t)
	seedLeague(t, db, fantasy.ProviderSleeper)
	source := &fakeSource{data: leagueData(fantasy.ProviderSleeper, "Home"), release: make(chan struct{})}
	runner := New(db, owner, fakeSources{fantasy.ProviderSleeper: source}, "")
	t.Cleanup(runner.Close)

	for range 3 {
		if _, err := runner.Start(context.Background(), false); err != nil {
			t.Fatal(err)
		}
	}
	close(source.release)
	waitUntilIdle(t, runner)

	if got := source.fetches.Load(); got != 1 {
		t.Fatalf("fetches = %d, want 1", got)
	}
}

func TestMissingESPNCredentialsFailWithoutLosingData(t *testing.T) {
	db := openDatabase(t)
	seedLeague(t, db, fantasy.ProviderESPN)
	runner := New(db, owner, ProviderSources{}, "")
	t.Cleanup(runner.Close)

	if _, err := runner.Start(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	status := waitUntilIdle(t, runner)[fantasy.ProviderESPN]
	if status.State != StateFailed || status.ErrorCode != "espn_credentials_missing" {
		t.Fatalf("status = %+v, want espn_credentials_missing", status)
	}
	if got := homeTeamName(t, db, fantasy.ProviderESPN); got != "Home" {
		t.Errorf("espn team = %q, want last successful data", got)
	}
}

func TestUnavailableStoredESPNCredentialsMarkReconnect(t *testing.T) {
	db := openDatabase(t)
	seedLeague(t, db, fantasy.ProviderESPN)
	ctx := context.Background()
	user, err := db.Queries().GetUserByEmail(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := db.Queries().ListSyncTargets(ctx, user.ID)
	if err != nil || len(targets) != 1 {
		t.Fatalf("sync targets = %d, %v", len(targets), err)
	}
	oldBox, err := secrets.NewBox(bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := oldBox.Seal([]byte(`{"s2":"synthetic-s2","swid":"{synthetic-swid}"}`), targets[0].ConnectionID.Bytes[:])
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Queries().UpdateConnectionForUser(ctx, store.UpdateConnectionForUserParams{
		ID: targets[0].ConnectionID, UserID: user.ID, EncryptedCredentials: encrypted, Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	box, err := secrets.NewBox(bytes.Repeat([]byte{6}, 32))
	if err != nil {
		t.Fatal(err)
	}
	runner := New(db, owner, storedCredentialSources{
		ProviderSources: ProviderSources{Box: box},
	}, "")
	t.Cleanup(runner.Close)
	if _, err := runner.Start(ctx, false); err != nil {
		t.Fatal(err)
	}
	statuses := waitUntilIdle(t, runner)
	status := statuses[fantasy.ProviderESPN]
	if status.ErrorCode != "espn_credentials_unavailable" {
		t.Fatalf("failure code = %q, want espn_credentials_unavailable", status.ErrorCode)
	}
	connection, err := db.Queries().GetConnectionForUser(ctx, store.GetConnectionForUserParams{
		ID: targets[0].ConnectionID, UserID: user.ID,
	})
	if err != nil || connection.Status != "needs_reconnect" {
		t.Fatalf("connection status = %q, %v", connection.Status, err)
	}
}

func TestRunnerStoresPlayerNews(t *testing.T) {
	db := openDatabase(t)
	seedLeague(t, db, fantasy.ProviderSleeper)
	source := fakeNewsSource{headline: "Saved news", done: make(chan struct{})}
	runner := New(db, owner, fakeNewsSources{
		fakeSources: fakeSources{fantasy.ProviderSleeper: &fakeSource{data: leagueData(fantasy.ProviderSleeper, "Home")}},
		news:        source,
	}, "")
	t.Cleanup(runner.Close)

	if _, err := runner.Start(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	waitUntilIdle(t, runner)
	<-source.done
	playerID := waitForPlayerNews(t, db, "Saved news")
	rows, err := db.Queries().ListRecentPlayerNews(context.Background(), store.ListRecentPlayerNewsParams{
		PlayerIds: []pgtype.UUID{playerID},
		Since:     pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
	})
	if err != nil || len(rows) != 1 || rows[0].Summary != "A saved fixture summary." || rows[0].FetchedAt.Time.IsZero() {
		t.Fatalf("saved news = %+v, %v", rows, err)
	}
}

func TestFailedPlayerNewsRefreshKeepsSavedNewsAndLeagueSucceeds(t *testing.T) {
	db := openDatabase(t)
	seedLeague(t, db, fantasy.ProviderSleeper)
	playerRows, err := db.Queries().ListNewsPlayers(context.Background(), owner)
	if err != nil || len(playerRows) != 1 {
		t.Fatalf("news players = %v, %v", playerRows, err)
	}
	playerID := playerRows[0].PlayerID
	if err := db.Queries().AddPlayerNews(context.Background(), store.AddPlayerNewsParams{
		Source:       "fake",
		SourceItemID: "saved-item",
		PlayerID:     playerID,
		Headline:     "Previously saved",
		Summary:      "Keep me on a failed refresh.",
		Url:          "https://example.test/saved",
		SourceLabel:  "Fixture source",
		PublishedAt:  pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		FetchedAt:    pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	source := fakeNewsSource{err: errors.New("news unavailable"), done: make(chan struct{})}
	runner := New(db, owner, fakeNewsSources{
		fakeSources: fakeSources{fantasy.ProviderSleeper: &fakeSource{data: leagueData(fantasy.ProviderSleeper, "Home Updated")}},
		news:        source,
	}, "")
	t.Cleanup(runner.Close)

	if _, err := runner.Start(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	statuses := waitUntilIdle(t, runner)
	<-source.done
	runner.Close()
	if got := statuses[fantasy.ProviderSleeper].State; got != StateSucceeded {
		t.Fatalf("league state = %s, want succeeded", got)
	}
	rows, err := db.Queries().ListRecentPlayerNews(context.Background(), store.ListRecentPlayerNewsParams{
		PlayerIds: []pgtype.UUID{playerID},
		Since:     pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
	})
	if err != nil || len(rows) != 1 || rows[0].SourceItemID != "saved-item" || rows[0].Headline != "Previously saved" {
		t.Fatalf("saved news after failed refresh = %+v, %v", rows, err)
	}
}

func TestRefreshNFLScheduleSavesGames(t *testing.T) {
	db := openDatabase(t)
	source := &scheduleSources{fakeSources: fakeSources{}, games: kcSFScheduleFixture()}
	runner := New(db, owner, source, "")
	t.Cleanup(runner.Close)

	runner.RefreshNFLSchedule(context.Background(), 2026)

	assertScheduleGames(t, weekGames(t, db, 2026, 6), kcSFScheduleFixture())
	if calls := source.calls.Load(); calls != 1 {
		t.Fatalf("schedule source calls = %d, want 1", calls)
	}
}

func TestRefreshNFLScheduleSkipsFreshAndReplacesStale(t *testing.T) {
	db := openDatabase(t)
	source := &scheduleSources{fakeSources: fakeSources{}, games: kcSFScheduleFixture()}
	runner := New(db, owner, source, "")
	t.Cleanup(runner.Close)
	base := time.Now()
	runner.now = func() time.Time { return base }

	runner.RefreshNFLSchedule(context.Background(), 2026)
	freshGames := weekGames(t, db, 2026, 6)
	runner.RefreshNFLSchedule(context.Background(), 2026)
	afterFreshRefresh := weekGames(t, db, 2026, 6)
	if len(afterFreshRefresh) != len(freshGames) {
		t.Fatalf("schedule rows after fresh refresh = %d, want unchanged count %d", len(afterFreshRefresh), len(freshGames))
	}
	for team, before := range freshGames {
		after, ok := afterFreshRefresh[team]
		if !ok || before.Opponent != after.Opponent || !before.KickoffAt.Time.Equal(after.KickoffAt.Time) {
			t.Fatalf("schedule row for %s changed after fresh refresh: before %+v, after %+v", team, before, after)
		}
	}
	if calls := source.calls.Load(); calls != 1 {
		t.Fatalf("source calls after fresh refresh = %d, want 1", calls)
	}

	replacement := []fantasy.NFLGame{
		{Season: 2026, Week: 6, Team: "BUF", Opponent: "MIA", KickoffAt: time.Date(2026, time.October, 18, 17, 0, 0, 0, time.UTC)},
		{Season: 2026, Week: 6, Team: "MIA", Opponent: "BUF", KickoffAt: time.Date(2026, time.October, 18, 17, 0, 0, 0, time.UTC)},
	}
	source.games = replacement
	runner.now = func() time.Time { return base.Add(25 * time.Hour) }
	runner.RefreshNFLSchedule(context.Background(), 2026)

	assertScheduleGames(t, weekGames(t, db, 2026, 6), replacement)
	if calls := source.calls.Load(); calls != 2 {
		t.Fatalf("source calls after stale refresh = %d, want 2", calls)
	}
}

func TestRefreshNFLScheduleFailureKeepsSavedGames(t *testing.T) {
	db := openDatabase(t)
	source := &scheduleSources{fakeSources: fakeSources{}, games: kcSFScheduleFixture()}
	runner := New(db, owner, source, "")
	t.Cleanup(runner.Close)
	base := time.Now()
	runner.now = func() time.Time { return base }

	runner.RefreshNFLSchedule(context.Background(), 2026)
	runner.now = func() time.Time { return base.Add(25 * time.Hour) }
	source.err = errors.New("schedule unavailable")
	runner.RefreshNFLSchedule(context.Background(), 2026)

	assertScheduleGames(t, weekGames(t, db, 2026, 6), kcSFScheduleFixture())
	if calls := source.calls.Load(); calls != 2 {
		t.Fatalf("schedule source calls = %d, want 2", calls)
	}
}

func kcSFScheduleFixture() []fantasy.NFLGame {
	kickoff := time.Date(2026, time.October, 18, 20, 25, 0, 0, time.UTC)
	return []fantasy.NFLGame{
		{Season: 2026, Week: 6, Team: "KC", Opponent: "SF", KickoffAt: kickoff},
		{Season: 2026, Week: 6, Team: "SF", Opponent: "KC", KickoffAt: kickoff},
	}
}

func weekGames(t *testing.T, db *database.DB, season, week int) map[string]store.ListNFLGamesForWeekRow {
	t.Helper()
	rows, err := db.Queries().ListNFLGamesForWeek(context.Background(), store.ListNFLGamesForWeekParams{
		Season: int32(season), Week: int32(week),
	})
	if err != nil {
		t.Fatal(err)
	}
	games := make(map[string]store.ListNFLGamesForWeekRow, len(rows))
	for _, row := range rows {
		games[row.Team] = row
	}
	return games
}

func assertScheduleGames(t *testing.T, got map[string]store.ListNFLGamesForWeekRow, want []fantasy.NFLGame) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("schedule rows = %d, want %d: %+v", len(got), len(want), got)
	}
	for _, expected := range want {
		actual, ok := got[expected.Team]
		if !ok || actual.Opponent != expected.Opponent || !actual.KickoffAt.Time.Equal(expected.KickoffAt) {
			t.Errorf("schedule row for %s = %+v, want opponent %s and kickoff %s",
				expected.Team, actual, expected.Opponent, expected.KickoffAt)
		}
	}
}

func waitForPlayerNews(t *testing.T, db *database.DB, headline string) pgtype.UUID {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		players, err := db.Queries().ListNewsPlayers(context.Background(), owner)
		if err != nil {
			t.Fatal(err)
		}
		for _, player := range players {
			rows, err := db.Queries().ListRecentPlayerNews(context.Background(), store.ListRecentPlayerNewsParams{
				PlayerIds: []pgtype.UUID{player.PlayerID},
				Since:     pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) == 1 && rows[0].Headline == headline {
				return player.PlayerID
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("player news with headline %q was not saved", headline)
	return pgtype.UUID{}
}

// beginRefresh opens a second import of the seeded league so its sync run stays
// open until a job finishes it.
func beginRefresh(t *testing.T, db *database.DB, source fantasy.Provider, ref fantasy.LeagueRef) importer.Started {
	t.Helper()
	service := importer.Service{DB: db, Source: source}
	started, err := service.Begin(context.Background(), importer.Request{
		OwnerEmail: owner, ExternalAccountID: "account",
		Selection: fantasy.ConnectionSelection{League: ref, SelectedTeamIDs: []string{"1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return started
}

// importJobFor builds the job for a begun import from the runner's sync targets,
// so Target fields match what a real sync would use.
func importJobFor(t *testing.T, runner *Runner, source fantasy.Provider, started importer.Started) ImportJob {
	t.Helper()
	targets, err := runner.listTargets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if target.ID == started.LeagueSeasonID().String() {
			return ImportJob{Target: target, Source: source, Started: started}
		}
	}
	t.Fatalf("no sync target for league %s", started.LeagueSeasonID())
	return ImportJob{}
}

func TestStartImportsReportsSyncingThenSucceeded(t *testing.T) {
	db := openDatabase(t)
	seedLeague(t, db, fantasy.ProviderSleeper)
	release := make(chan struct{})
	source := &fakeSource{data: leagueData(fantasy.ProviderSleeper, "Home"), release: release}
	started := beginRefresh(t, db, source, source.data.League.Ref)
	newsDone := make(chan struct{})
	sources := &importRefreshSources{
		scheduleSources: scheduleSources{fakeSources: fakeSources{}, games: kcSFScheduleFixture()},
		news:            fakeNewsSource{headline: "Import news", done: newsDone},
	}
	runner := New(db, owner, sources, "")

	runner.StartImports([]ImportJob{importJobFor(t, runner, source, started)})
	statuses, err := runner.Status(context.Background())
	if err != nil || len(statuses) != 1 || statuses[0].State != StateSyncing {
		t.Fatalf("status during blocked fetch = %+v, %v", statuses, err)
	}

	close(release)
	status := waitUntilIdle(t, runner)[fantasy.ProviderSleeper]
	<-newsDone
	waitForPlayerNews(t, db, "Import news")
	runner.Close()
	if status.State != StateSucceeded {
		t.Fatalf("state = %s, want %s", status.State, StateSucceeded)
	}
	if calls := sources.calls.Load(); calls < 1 {
		t.Fatalf("schedule source calls = %d, want at least 1", calls)
	}
}

func TestStartImportsUnauthorizedMarksReconnect(t *testing.T) {
	db := openDatabase(t)
	seedLeague(t, db, fantasy.ProviderSleeper)
	source := &fakeSource{data: leagueData(fantasy.ProviderSleeper, "Home"), fetchErr: espn.ErrUnauthorized}
	started := beginRefresh(t, db, source, source.data.League.Ref)
	newsDone := make(chan struct{})
	sources := &importRefreshSources{
		scheduleSources: scheduleSources{fakeSources: fakeSources{}, games: kcSFScheduleFixture()},
		news:            fakeNewsSource{headline: "Import news", done: newsDone},
	}
	runner := New(db, owner, sources, "")
	job := importJobFor(t, runner, source, started)

	runner.StartImports([]ImportJob{job})
	status := waitUntilIdle(t, runner)[fantasy.ProviderSleeper]
	runner.Close()

	if status.State != StateFailed || status.ErrorCode != "provider_fetch_failed" {
		t.Fatalf("status = %s/%s, want failed/provider_fetch_failed", status.State, status.ErrorCode)
	}
	user, err := db.Queries().GetUserByEmail(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.Queries().GetConnectionForUser(context.Background(), store.GetConnectionForUserParams{
		ID: job.Target.ConnectionID, UserID: user.ID,
	})
	if err != nil || connection.Status != "needs_reconnect" {
		t.Fatalf("connection status = %q, %v", connection.Status, err)
	}
	if calls := sources.calls.Load(); calls != 0 {
		t.Fatalf("schedule source calls after failed import = %d, want 0", calls)
	}
	select {
	case <-newsDone:
		t.Fatal("news refresh ran after a failed import")
	default:
	}
}

func TestStartImportsAbandonsDuplicateLeague(t *testing.T) {
	db, pool := openDatabaseWithTestPool(t, true)
	seedLeague(t, db, fantasy.ProviderSleeper)
	release := make(chan struct{})
	source := &fakeSource{data: leagueData(fantasy.ProviderSleeper, "Home"), release: release}
	started := beginRefresh(t, db, source, source.data.League.Ref)
	duplicate := &fakeSource{data: leagueData(fantasy.ProviderSleeper, "Home")}
	duplicateStarted := beginRefresh(t, db, duplicate, duplicate.data.League.Ref)
	newsDone := make(chan struct{})
	sources := &importRefreshSources{
		scheduleSources: scheduleSources{fakeSources: fakeSources{}, games: kcSFScheduleFixture()},
		news:            fakeNewsSource{headline: "Import news", done: newsDone},
	}
	runner := New(db, owner, sources, "")

	runner.StartImports([]ImportJob{importJobFor(t, runner, source, started)})
	runner.StartImports([]ImportJob{importJobFor(t, runner, duplicate, duplicateStarted)})

	var status, code string
	err := pool.QueryRow(context.Background(),
		`SELECT status,error_code FROM sync_runs WHERE status='failed'`).Scan(&status, &code)
	if err != nil || code != "sync_already_running" {
		t.Fatalf("duplicate's sync run = %s/%s, %v", status, code, err)
	}

	close(release)
	final := waitUntilIdle(t, runner)[fantasy.ProviderSleeper]
	<-newsDone
	waitForPlayerNews(t, db, "Import news")
	runner.Close()
	if final.State != StateSucceeded {
		t.Fatalf("state = %s, want %s", final.State, StateSucceeded)
	}
	var succeeded int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM sync_runs WHERE status='succeeded'`).Scan(&succeeded); err != nil || succeeded != 2 {
		t.Fatalf("succeeded runs = %d, %v, want 2 (seed + first job)", succeeded, err)
	}
}

func TestStartImportsMixedOutcomes(t *testing.T) {
	db := openDatabase(t)
	seedLeague(t, db, fantasy.ProviderSleeper)
	seedLeague(t, db, fantasy.ProviderESPN)
	releaseSleeper := make(chan struct{})
	releaseESPN := make(chan struct{})
	sleeperSource := &fakeSource{data: leagueData(fantasy.ProviderSleeper, "Home"), release: releaseSleeper}
	espnSource := &fakeSource{data: leagueData(fantasy.ProviderESPN, "Home"), release: releaseESPN, fetchErr: errors.New("upstream down")}
	sleeperStarted := beginRefresh(t, db, sleeperSource, sleeperSource.data.League.Ref)
	espnStarted := beginRefresh(t, db, espnSource, espnSource.data.League.Ref)
	newsDone := make(chan struct{})
	sources := &importRefreshSources{
		scheduleSources: scheduleSources{fakeSources: fakeSources{}, games: kcSFScheduleFixture()},
		news:            fakeNewsSource{headline: "Import news", done: newsDone},
	}
	runner := New(db, owner, sources, "")

	runner.StartImports([]ImportJob{
		importJobFor(t, runner, sleeperSource, sleeperStarted),
		importJobFor(t, runner, espnSource, espnStarted),
	})
	statuses, err := runner.Status(context.Background())
	if err != nil || len(statuses) != 2 {
		t.Fatalf("statuses during blocked fetches = %+v, %v", statuses, err)
	}
	for _, status := range statuses {
		if status.State != StateSyncing {
			t.Fatalf("state during blocked fetches = %s, want syncing", status.State)
		}
	}

	close(releaseSleeper)
	close(releaseESPN)
	finished := waitUntilIdle(t, runner)
	<-newsDone
	waitForPlayerNews(t, db, "Import news")
	runner.Close()

	if got := finished[fantasy.ProviderSleeper].State; got != StateSucceeded {
		t.Fatalf("sleeper state = %s, want %s", got, StateSucceeded)
	}
	espnStatus := finished[fantasy.ProviderESPN]
	if espnStatus.State != StateFailed || espnStatus.ErrorCode != "provider_fetch_failed" {
		t.Fatalf("espn status = %+v, want failed/provider_fetch_failed", espnStatus)
	}
	if calls := sources.calls.Load(); calls < 1 {
		t.Fatalf("schedule source calls = %d, want the shared refresh to run once", calls)
	}
}

// Close cancels the in-flight fetch, but its outcome is still recorded: the
// begun import's sync run ends failed rather than left running, and the shared
// refresh does not run because nothing succeeded.
func TestCloseDuringImportRecordsFailedRun(t *testing.T) {
	db, pool := openDatabaseWithTestPool(t, true)
	seedLeague(t, db, fantasy.ProviderSleeper)
	source := &fakeSource{data: leagueData(fantasy.ProviderSleeper, "Home"), release: make(chan struct{})}
	started := beginRefresh(t, db, source, source.data.League.Ref)
	sources := &importRefreshSources{
		scheduleSources: scheduleSources{fakeSources: fakeSources{}, games: kcSFScheduleFixture()},
		news:            fakeNewsSource{headline: "Import news"},
	}
	runner := New(db, owner, sources, "")

	runner.StartImports([]ImportJob{importJobFor(t, runner, source, started)})
	deadline := time.Now().Add(5 * time.Second)
	for source.fetches.Load() == 0 {
		if time.Now().After(deadline) {
			runner.Close()
			t.Fatal("import fetch did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	runner.Close()

	var status, code string
	var completedAt pgtype.Timestamptz
	err := pool.QueryRow(context.Background(),
		`SELECT status,error_code,completed_at FROM sync_runs ORDER BY started_at DESC LIMIT 1`).Scan(&status, &code, &completedAt)
	if err != nil || status != "failed" || code == "" || !completedAt.Valid {
		t.Fatalf("interrupted sync run = %s/%s completed %v, %v; want a recorded failure", status, code, completedAt, err)
	}
	if calls := sources.calls.Load(); calls != 0 {
		t.Fatalf("schedule source calls after interrupted import = %d, want 0", calls)
	}
}

// With saved teams present, the newest-season fallback stays unused: a league
// attached by Begin alone in a newer season does not become a sync target.
func TestListSyncTargetsKeepsSavedSeasonWhenTeamsExist(t *testing.T) {
	db := openDatabase(t)
	seedLeague(t, db, fantasy.ProviderSleeper)
	source := &fakeSource{data: leagueData(fantasy.ProviderSleeper, "Next Season")}
	source.data.League.Ref = fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "league-sleeper-next", Season: 2027}
	beginRefresh(t, db, source, source.data.League.Ref)
	runner := New(db, owner, fakeSources{}, "")
	defer runner.Close()

	statuses, err := runner.Status(context.Background())
	if err != nil || len(statuses) != 1 {
		t.Fatalf("statuses = %+v, %v; want only the saved season's league", statuses, err)
	}
	if statuses[0].Target.Selection.League.ExternalID != "league-sleeper" {
		t.Fatalf("target league = %q, want the league with saved teams", statuses[0].Target.Selection.League.ExternalID)
	}
}

// A league attached by Begin appears as a sync target before its first import
// saves any teams: the target listing falls back to the newest attached season.
func TestListSyncTargetsIncludesFirstImportBeforeSave(t *testing.T) {
	db := openDatabase(t)
	source := &fakeSource{data: leagueData(fantasy.ProviderSleeper, "Home")}
	started, err := (&importer.Service{DB: db, Source: source}).Begin(context.Background(), importer.Request{
		OwnerEmail: owner, ExternalAccountID: "account",
		Selection: fantasy.ConnectionSelection{League: source.data.League.Ref, SelectedTeamIDs: []string{"1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := New(db, owner, fakeSources{}, "")
	defer runner.Close()

	statuses, err := runner.Status(context.Background())
	if err != nil || len(statuses) != 1 {
		t.Fatalf("statuses = %+v, %v", statuses, err)
	}
	if statuses[0].Target.ID != started.LeagueSeasonID().String() || statuses[0].State != StateNeverSynced {
		t.Fatalf("status = %+v, want the begun league as never_synced", statuses[0])
	}
}
