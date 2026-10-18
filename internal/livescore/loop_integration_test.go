//go:build integration

package livescore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/leaguesync"
	"github.com/adityachoudhari26/league-lens/internal/winchance"
)

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
	syncRunner := leaguesync.New(db, owner, sources, "")
	loop := New(db, sources, syncRunner)
	defer syncRunner.Close()
	if len(tickTimes) > 0 {
		tickAt := tickTimes[0]
		loop.now = func() time.Time { return tickAt }
	}
	defer loop.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	initialCalls := source.calls.Load()
	go func() {
		loop.Run(ctx, 10*time.Millisecond)
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
	if observationCount != 4 {
		t.Fatalf("first tick saved %d observations, want 4", observationCount)
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
	data.Lineups[0].Entries = data.Lineups[0].Entries[:1]
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
			{PlayerID: "p", Starter: true, Points: floatPtr(12)},
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
	syncRunner := leaguesync.New(db, owner, sources, "")
	loop := New(db, sources, syncRunner)
	defer syncRunner.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		loop.Run(ctx, 10*time.Millisecond)
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for liveSource.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	called := liveSource.calls.Load() > 0
	cancel()
	<-done
	loop.Close()
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
	syncRunner := leaguesync.New(db, owner, sources, "")
	loop := New(db, sources, syncRunner)
	defer syncRunner.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		loop.Run(ctx, 10*time.Millisecond)
		close(done)
	}()
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-done
	loop.Close()
	if sources.calls.Load() == 0 {
		t.Fatal("scoreboard was not read")
	}

	if liveSource.calls.Load() != 0 {
		t.Fatalf("league provider calls = %d, want 0", liveSource.calls.Load())
	}
}
