//go:build integration

package livescore

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/leaguesync"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

type backfillSources struct {
	fakeSources
	board      fantasy.NFLScoreboard
	plays      []fantasy.NFLPlay
	scoreCalls int
	playCalls  int
	playIDs    []string
}

func (s *backfillSources) NFLWeekScoreboard(context.Context, int, int) (fantasy.NFLScoreboard, error) {
	s.scoreCalls++
	return s.board, nil
}

func (s *backfillSources) NFLGamePlays(_ context.Context, gameID string) ([]fantasy.NFLPlay, error) {
	s.playCalls++
	s.playIDs = append(s.playIDs, gameID)
	return s.plays, nil
}

func TestWinChanceBackfillPersistsOnceAndSkipsExistingHistory(t *testing.T) {
	db, pool := openDatabaseWithTestPool(t, true)
	now := time.Date(2026, time.October, 9, 0, 0, 0, 0, time.UTC)
	data := fantasy.LeagueData{
		League: fantasy.League{
			Ref:  fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "backfill", Season: 2026},
			Name: "Backfill fixture", TeamCount: 2,
			Settings: fantasy.LeagueSettings{
				ScoringRules: []fantasy.ScoringRule{
					{Stat: fantasy.StatRushingYards, Points: 1, PerUnits: 10},
					{Stat: fantasy.StatRushingTouchdowns, Points: 6, PerUnits: 1},
				},
			},
		},
		FetchedAt: now,
		Teams:     []fantasy.Team{{ExternalID: "1", Name: "Home"}, {ExternalID: "2", Name: "Away"}},
		Players: []fantasy.Player{
			{ExternalID: "101", Name: "Replay Runner", Kind: fantasy.PlayerPerson},
			{ExternalID: "202", Name: "Other Starter", Kind: fantasy.PlayerPerson},
		},
		Rosters: []fantasy.RosterSnapshot{
			{TeamID: "1", ScoringPeriod: 5, Origin: fantasy.SnapshotObserved, Coverage: fantasy.CoverageFull, ObservedAt: now,
				Entries: []fantasy.RosterEntry{{PlayerID: "101", Slot: fantasy.SlotRB, ProTeam: "KC"}}},
			{TeamID: "2", ScoringPeriod: 5, Origin: fantasy.SnapshotObserved, Coverage: fantasy.CoverageFull, ObservedAt: now,
				Entries: []fantasy.RosterEntry{{PlayerID: "202", Slot: fantasy.SlotRB, ProTeam: "SF"}}},
		},
		Matchups: []fantasy.Matchup{{
			ExternalID: "week-5", Period: 5, ScoringPeriods: []fantasy.ScoringPeriod{5},
			Status: fantasy.MatchupFinal,
			TeamA:  fantasy.MatchupSide{TeamID: "1", Score: floatPtr(7)},
			TeamB:  &fantasy.MatchupSide{TeamID: "2", Score: floatPtr(0)},
		}, {
			ExternalID: "multi-week", Period: 5, ScoringPeriods: []fantasy.ScoringPeriod{5, 6},
			Status: fantasy.MatchupFinal,
			TeamA:  fantasy.MatchupSide{TeamID: "1", Score: floatPtr(7)},
			TeamB:  &fantasy.MatchupSide{TeamID: "2", Score: floatPtr(0)},
		}},
		CompleteMatchupPeriods: []fantasy.MatchupPeriod{5, 6},
		Lineups: []fantasy.MatchupLineup{
			{Period: 5, MatchupID: "week-5", TeamID: "1", Entries: []fantasy.LineupEntry{
				{PlayerID: "101", Slot: fantasy.SlotRB, Starter: true, Points: floatPtr(7), ProjectedPoints: floatPtr(14)},
			}},
			{Period: 5, MatchupID: "week-5", TeamID: "2", Entries: []fantasy.LineupEntry{
				{PlayerID: "202", Slot: fantasy.SlotRB, Starter: true, Points: floatPtr(0), ProjectedPoints: floatPtr(14)},
			}},
		},
	}
	provider := &fakeSource{data: data}
	importService := &importer.Service{DB: db, Source: provider}
	if _, err := importService.Import(context.Background(), importer.Request{
		OwnerEmail: owner, ExternalAccountID: "account",
		Selection: fantasy.ConnectionSelection{
			League: data.League.Ref, SelectedTeamIDs: []string{"1"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	plays := []fantasy.NFLPlay{
		{
			At: now.Add(12*time.Minute + 15*time.Second), Period: 1, Clock: 12 * time.Minute,
			Kind: fantasy.NFLPlayRushingTouchdown, Yards: 10,
			Participants: []fantasy.NFLPlayParticipant{
				{AthleteID: "101", Role: "rusher"},
				{AthleteID: "101", Role: "scorer"},
			},
		},
		{At: now.Add(time.Hour), Period: 4, Kind: fantasy.NFLPlayEndOfGame},
	}
	sources := &backfillSources{
		fakeSources: fakeSources{fantasy.ProviderESPN: provider},
		board: fantasy.NFLScoreboard{
			Season: 2026, SeasonType: 2, Week: 5,
			Games: []fantasy.NFLGameStatus{{ID: "event-5", HomeTeam: "KC", AwayTeam: "SF"}},
		},
		plays: plays,
	}
	syncRunner := leaguesync.New(db, owner, sources, "")
	loop := New(db, sources, syncRunner)
	defer syncRunner.Close()
	defer loop.Close()
	var matchupID pgtype.UUID
	if err := pool.QueryRow(context.Background(), `
		SELECT m.id FROM matchups m
		JOIN league_seasons l ON l.id=m.league_season_id
		WHERE l.external_league_id='backfill' AND m.external_matchup_id='week-5'
	`).Scan(&matchupID); err != nil {
		t.Fatal(err)
	}

	sampleCount, changeCount, err := loop.backfillMatchup(context.Background(), matchupID)
	if err != nil {
		t.Fatal(err)
	}
	if sampleCount < 2 || changeCount != 1 {
		t.Fatalf("backfill wrote %d samples and %d changes, want at least 2 and exactly 1", sampleCount, changeCount)
	}
	wantSamples, wantChanges := sampleCount, changeCount
	var savedSamples, savedChanges int
	var finalScoreA, finalScoreB string
	if err := pool.QueryRow(context.Background(), `
		SELECT
			(SELECT count(*) FROM win_chance_samples WHERE matchup_id=$1),
			(SELECT count(*) FROM lineup_point_changes WHERE matchup_id=$1),
			(SELECT score_a::text FROM win_chance_samples WHERE matchup_id=$1 ORDER BY observed_at DESC LIMIT 1),
			(SELECT score_b::text FROM win_chance_samples WHERE matchup_id=$1 ORDER BY observed_at DESC LIMIT 1)
	`, matchupID).Scan(&savedSamples, &savedChanges, &finalScoreA, &finalScoreB); err != nil {
		t.Fatal(err)
	}
	if savedSamples != sampleCount || savedChanges != changeCount ||
		finalScoreA != "7.0000" || finalScoreB != "0.0000" {
		t.Fatalf("saved history = %d samples/%d changes, final %s-%s", savedSamples, savedChanges, finalScoreA, finalScoreB)
	}

	sampleCount, changeCount, err = loop.backfillMatchup(context.Background(), matchupID)
	if err != nil {
		t.Fatal(err)
	}
	if sampleCount != 0 || changeCount != 0 || sources.scoreCalls != 1 || sources.playCalls != 1 {
		t.Fatalf("second backfill = %d/%d with %d scoreboard and %d play calls",
			sampleCount, changeCount, sources.scoreCalls, sources.playCalls)
	}
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM win_chance_samples WHERE matchup_id=$1
	`, matchupID).Scan(&savedSamples); err != nil {
		t.Fatal(err)
	}
	if savedSamples != wantSamples || savedChanges != wantChanges {
		t.Fatalf("second backfill changed saved history to %d samples/%d changes", savedSamples, savedChanges)
	}

	if _, err := pool.Exec(context.Background(),
		`DELETE FROM lineup_point_changes WHERE matchup_id=$1`, matchupID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM win_chance_samples WHERE matchup_id=$1`, matchupID); err != nil {
		t.Fatal(err)
	}
	tailAt := now.Add(10 * time.Minute)
	tailStarters, err := db.Queries().ListBackfillMatchupStarters(context.Background(), matchupID)
	if err != nil {
		t.Fatal(err)
	}
	for _, starter := range tailStarters {
		startPoints := 0.0
		if starter.AthleteID == "101" {
			startPoints = 4
		}
		points, err := fourDecimalNumeric(startPoints)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Queries().UpsertLiveLineupObservation(context.Background(),
			store.UpsertLiveLineupObservationParams{
				MatchupID: matchupID, TeamID: starter.TeamID, PlayerID: starter.PlayerID,
				Points: points, Touchdowns: pgtype.Int4{Int32: 0, Valid: true},
				FieldGoals: pgtype.Int4{Int32: 0, Valid: true},
				ObservedAt: pgtype.Timestamptz{Time: tailAt, Valid: true},
			}); err != nil {
			t.Fatal(err)
		}
	}
	half, err := fourDecimalNumeric(0.5)
	if err != nil {
		t.Fatal(err)
	}
	four, err := fourDecimalNumeric(4)
	if err != nil {
		t.Fatal(err)
	}
	zero, err := fourDecimalNumeric(0)
	if err != nil {
		t.Fatal(err)
	}
	fourteen, err := fourDecimalNumeric(14)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Queries().InsertWinChanceSample(context.Background(), store.InsertWinChanceSampleParams{
		MatchupID: matchupID, ObservedAt: pgtype.Timestamptz{Time: tailAt, Valid: true},
		TeamAWinChance: half, ScoreA: four, ScoreB: zero, ProjectedA: fourteen, ProjectedB: fourteen,
	}); err != nil {
		t.Fatal(err)
	}

	tailSamples, tailChanges, err := loop.backfillMatchup(context.Background(), matchupID)
	if err != nil {
		t.Fatal(err)
	}
	if tailSamples == 0 || tailChanges != 1 || sources.scoreCalls != 2 || sources.playCalls != 2 {
		t.Fatalf("tail backfill wrote %d samples/%d changes with %d scoreboard and %d play calls",
			tailSamples, tailChanges, sources.scoreCalls, sources.playCalls)
	}
	var tailSampleCount, tailChangeCount int
	var lastTailSampleAt time.Time
	var lastTailScoreA, lastTailScoreB string
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*),
			(SELECT observed_at FROM win_chance_samples WHERE matchup_id=$1 ORDER BY observed_at DESC LIMIT 1),
			(SELECT score_a::text FROM win_chance_samples WHERE matchup_id=$1 ORDER BY observed_at DESC LIMIT 1),
			(SELECT score_b::text FROM win_chance_samples WHERE matchup_id=$1 ORDER BY observed_at DESC LIMIT 1)
		FROM win_chance_samples WHERE matchup_id=$1 AND observed_at > $2
	`, matchupID, tailAt).Scan(&tailSampleCount, &lastTailSampleAt, &lastTailScoreA, &lastTailScoreB); err != nil {
		t.Fatal(err)
	}
	if tailSampleCount != tailSamples || !lastTailSampleAt.After(tailAt) ||
		lastTailScoreA != "7.0000" || lastTailScoreB != "0.0000" {
		t.Fatalf("tail history has %d new samples; latest %s is %s-%s",
			tailSampleCount, lastTailSampleAt, lastTailScoreA, lastTailScoreB)
	}
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM lineup_point_changes WHERE matchup_id=$1`, matchupID,
	).Scan(&tailChangeCount); err != nil {
		t.Fatal(err)
	}
	var changeAt time.Time
	var changeBefore, changeAfter string
	if err := pool.QueryRow(context.Background(), `
		SELECT observed_at,points_before::text,points_after::text
		FROM lineup_point_changes WHERE matchup_id=$1 ORDER BY observed_at
	`, matchupID).Scan(&changeAt, &changeBefore, &changeAfter); err != nil {
		t.Fatal(err)
	}
	if tailChangeCount != 1 || !changeAt.After(tailAt) ||
		changeBefore != "4.0000" || changeAfter != "11.0000" {
		t.Fatalf("tail changes = %d; credited change %s %s→%s, want one post-T play 4→11",
			tailChangeCount, changeAt, changeBefore, changeAfter)
	}
	samplesBeforeRepeat, changesBeforeRepeat := tailSampleCount, tailChangeCount
	sampleCount, changeCount, err = loop.backfillMatchup(context.Background(), matchupID)
	if err != nil {
		t.Fatal(err)
	}
	if sampleCount != 0 || changeCount != 0 || sources.scoreCalls != 2 || sources.playCalls != 2 {
		t.Fatalf("completed tail replay wrote %d/%d with %d scoreboard and %d play calls",
			sampleCount, changeCount, sources.scoreCalls, sources.playCalls)
	}
	if err := pool.QueryRow(context.Background(), `
		SELECT (SELECT count(*) FROM win_chance_samples WHERE matchup_id=$1 AND observed_at > $2),
			(SELECT count(*) FROM lineup_point_changes WHERE matchup_id=$1)
	`, matchupID, tailAt).Scan(&tailSampleCount, &tailChangeCount); err != nil {
		t.Fatal(err)
	}
	if tailSampleCount != samplesBeforeRepeat || tailChangeCount != changesBeforeRepeat {
		t.Fatalf("repeat tail replay changed history to %d samples/%d changes",
			tailSampleCount, tailChangeCount)
	}

	var ineligibleMatchupID pgtype.UUID
	if err := pool.QueryRow(context.Background(), `
		SELECT id FROM matchups WHERE external_matchup_id='multi-week'
	`).Scan(&ineligibleMatchupID); err != nil {
		t.Fatal(err)
	}
	if !loop.RequestWinChanceBackfill(ineligibleMatchupID) {
		t.Fatal("first ineligible backfill request was not scheduled")
	}
	loop.wg.Wait()
	loop.mu.Lock()
	failure := loop.backfillFailures[ineligibleMatchupID.String()]
	loop.mu.Unlock()
	if failure.code != "nothing_to_rebuild" {
		t.Fatalf("ineligible backfill failure code = %q, want nothing_to_rebuild", failure.code)
	}
	if loop.RequestWinChanceBackfill(ineligibleMatchupID) {
		t.Fatal("second ineligible backfill request should be throttled")
	}
}

func newInProgressBackfillFixture(
	t *testing.T,
) (*database.DB, *pgxpool.Pool, *backfillSources, *fakeSource, pgtype.UUID, time.Time) {
	t.Helper()
	db, pool := openDatabaseWithTestPool(t, true)
	now := time.Now().UTC().Truncate(time.Minute)
	data := fantasy.LeagueData{
		League: fantasy.League{
			Ref:  fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "in-progress-backfill", Season: 2026},
			Name: "In-progress backfill fixture", TeamCount: 2,
			Settings: fantasy.LeagueSettings{ScoringRules: []fantasy.ScoringRule{
				{Stat: fantasy.StatRushingYards, Points: 1, PerUnits: 10},
				{Stat: fantasy.StatRushingTouchdowns, Points: 6, PerUnits: 1},
			}},
		},
		FetchedAt: now,
		Teams:     []fantasy.Team{{ExternalID: "1", Name: "Home"}, {ExternalID: "2", Name: "Away"}},
		Players: []fantasy.Player{
			{ExternalID: "101", Name: "Finished Game Starter", Kind: fantasy.PlayerPerson},
			{ExternalID: "202", Name: "Scheduled Game Starter", Kind: fantasy.PlayerPerson},
		},
		Rosters: []fantasy.RosterSnapshot{
			{TeamID: "1", ScoringPeriod: 6, Origin: fantasy.SnapshotObserved, Coverage: fantasy.CoverageFull, ObservedAt: now,
				Entries: []fantasy.RosterEntry{{PlayerID: "101", Slot: fantasy.SlotRB, ProTeam: "KC"}}},
			{TeamID: "2", ScoringPeriod: 6, Origin: fantasy.SnapshotObserved, Coverage: fantasy.CoverageFull, ObservedAt: now,
				Entries: []fantasy.RosterEntry{{PlayerID: "202", Slot: fantasy.SlotWR, ProTeam: "BUF"}}},
		},
		Matchups: []fantasy.Matchup{{
			ExternalID: "week-6", Period: 6, ScoringPeriods: []fantasy.ScoringPeriod{6},
			Status: fantasy.MatchupInProgress,
			TeamA:  fantasy.MatchupSide{TeamID: "1", Score: floatPtr(7)},
			TeamB:  &fantasy.MatchupSide{TeamID: "2", Score: floatPtr(0)},
		}},
		CompleteMatchupPeriods: []fantasy.MatchupPeriod{6},
		Lineups: []fantasy.MatchupLineup{
			{Period: 6, MatchupID: "week-6", TeamID: "1", Entries: []fantasy.LineupEntry{
				{PlayerID: "101", Slot: fantasy.SlotRB, Starter: true, Points: floatPtr(7)},
			}},
			{Period: 6, MatchupID: "week-6", TeamID: "2", Entries: []fantasy.LineupEntry{
				{PlayerID: "202", Slot: fantasy.SlotWR, Starter: true, Points: floatPtr(0), ProjectedPoints: floatPtr(14)},
			}},
		},
	}
	provider := &fakeSource{data: data}
	if _, err := (&importer.Service{DB: db, Source: provider}).Import(context.Background(), importer.Request{
		OwnerEmail: owner, ExternalAccountID: "account",
		Selection: fantasy.ConnectionSelection{League: data.League.Ref, SelectedTeamIDs: []string{"1"}},
	}); err != nil {
		t.Fatal(err)
	}
	sources := &backfillSources{
		fakeSources: fakeSources{fantasy.ProviderESPN: provider},
		board: fantasy.NFLScoreboard{
			Season: 2026, SeasonType: 2, Week: 6,
			Games: []fantasy.NFLGameStatus{
				{ID: "finished", HomeTeam: "KC", AwayTeam: "CIN", State: fantasy.NFLGameFinal},
				{ID: "scheduled", HomeTeam: "BUF", AwayTeam: "MIA", State: fantasy.NFLGameScheduled},
			},
		},
		plays: []fantasy.NFLPlay{
			{At: now.Add(12 * time.Minute), Period: 1, Kind: fantasy.NFLPlayRushingTouchdown, Yards: 10,
				Participants: []fantasy.NFLPlayParticipant{{AthleteID: "101", Role: "rusher"}, {AthleteID: "101", Role: "scorer"}}},
			{At: now.Add(time.Hour), Period: 4, Kind: fantasy.NFLPlayEndOfGame},
		},
	}
	var matchupID pgtype.UUID
	if err := pool.QueryRow(context.Background(), `
		SELECT m.id FROM matchups m
		JOIN league_seasons l ON l.id=m.league_season_id
		WHERE l.external_league_id='in-progress-backfill' AND m.external_matchup_id='week-6'
	`).Scan(&matchupID); err != nil {
		t.Fatal(err)
	}
	return db, pool, sources, provider, matchupID, now
}

func insertInProgressBackfillSample(t *testing.T, db *database.DB, matchupID pgtype.UUID, observedAt time.Time) {
	t.Helper()
	half, err := fourDecimalNumeric(0.5)
	if err != nil {
		t.Fatal(err)
	}
	zero, err := fourDecimalNumeric(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Queries().InsertWinChanceSample(context.Background(), store.InsertWinChanceSampleParams{
		MatchupID: matchupID, ObservedAt: pgtype.Timestamptz{Time: observedAt, Valid: true},
		TeamAWinChance: half, ScoreA: zero, ScoreB: zero, ProjectedA: zero, ProjectedB: zero,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestInProgressWinChanceBackfillUsesFinishedAndScheduledGames(t *testing.T) {
	db, pool, sources, _, matchupID, now := newInProgressBackfillFixture(t)
	syncRunner := leaguesync.New(db, owner, sources, "")
	loop := New(db, sources, syncRunner)
	defer syncRunner.Close()
	loop.now = func() time.Time { return now.Add(2 * time.Hour) }
	defer loop.Close()

	samples, changes, err := loop.backfillMatchup(context.Background(), matchupID)
	if err != nil {
		t.Fatal(err)
	}
	if samples < 2 || changes != 1 {
		t.Fatalf("in-progress backfill wrote %d samples and %d changes, want at least 2 and one", samples, changes)
	}
	if sources.playCalls != 1 || len(sources.playIDs) != 1 || sources.playIDs[0] != "finished" {
		t.Fatalf("fetched game plays %v, want only finished game", sources.playIDs)
	}
	var savedSamples int
	var firstScoreA, firstScoreB string
	var lastScoreA, lastScoreB, lastProjectedB string
	var lastWinChance float64
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*),
			(SELECT score_a::text FROM win_chance_samples WHERE matchup_id=$1 ORDER BY observed_at LIMIT 1),
			(SELECT score_b::text FROM win_chance_samples WHERE matchup_id=$1 ORDER BY observed_at LIMIT 1),
			(SELECT score_a::text FROM win_chance_samples WHERE matchup_id=$1 ORDER BY observed_at DESC LIMIT 1),
			(SELECT score_b::text FROM win_chance_samples WHERE matchup_id=$1 ORDER BY observed_at DESC LIMIT 1),
			(SELECT projected_b::text FROM win_chance_samples WHERE matchup_id=$1 ORDER BY observed_at DESC LIMIT 1),
			(SELECT team_a_win_chance FROM win_chance_samples WHERE matchup_id=$1 ORDER BY observed_at DESC LIMIT 1)
		FROM win_chance_samples WHERE matchup_id=$1
	`, matchupID).Scan(&savedSamples, &firstScoreA, &firstScoreB, &lastScoreA, &lastScoreB, &lastProjectedB, &lastWinChance); err != nil {
		t.Fatal(err)
	}
	if savedSamples != samples || firstScoreA != "0.0000" || firstScoreB != "0.0000" {
		t.Fatalf("saved %d samples; first scores %s-%s, want pregame 0-0", savedSamples, firstScoreA, firstScoreB)
	}
	if lastScoreA != "7.0000" || lastScoreB != "0.0000" ||
		lastProjectedB <= lastScoreB || lastWinChance <= 0 || lastWinChance >= 1 {
		t.Fatalf("last sample = %s-%s, projected B %s, win chance %.4f; want 7-0, remaining B projection, and undecided chance",
			lastScoreA, lastScoreB, lastProjectedB, lastWinChance)
	}
	var pointChanges int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM lineup_point_changes c
		JOIN player_external_ids x ON x.player_id=c.player_id AND x.provider='espn'
		WHERE c.matchup_id=$1 AND x.external_id='101'
	`, matchupID).Scan(&pointChanges); err != nil {
		t.Fatal(err)
	}
	if pointChanges != 1 {
		t.Fatalf("finished starter point-change rows = %d, want 1", pointChanges)
	}
}

func TestInProgressWinChanceBackfillSkipsAllScheduledGames(t *testing.T) {
	db, _, sources, _, matchupID, _ := newInProgressBackfillFixture(t)
	sources.board.Games = []fantasy.NFLGameStatus{
		{ID: "scheduled-1", HomeTeam: "KC", AwayTeam: "CIN", State: fantasy.NFLGameScheduled},
		{ID: "scheduled-2", HomeTeam: "BUF", AwayTeam: "MIA", State: fantasy.NFLGameScheduled},
	}
	syncRunner := leaguesync.New(db, owner, sources, "")
	loop := New(db, sources, syncRunner)
	defer syncRunner.Close()
	defer loop.Close()

	samples, changes, err := loop.backfillMatchup(context.Background(), matchupID)
	if err != nil {
		t.Fatal(err)
	}
	if samples != 0 || changes != 0 || sources.scoreCalls != 1 || sources.playCalls != 0 {
		t.Fatalf("all-scheduled backfill = %d samples/%d changes, %d scoreboard/%d play calls; want 0/0, 1/0",
			samples, changes, sources.scoreCalls, sources.playCalls)
	}
}

func TestInProgressWinChanceBackfillSkipsExistingHistory(t *testing.T) {
	db, pool, sources, _, matchupID, now := newInProgressBackfillFixture(t)
	sampleAt := now.Add(15 * time.Minute)
	insertInProgressBackfillSample(t, db, matchupID, sampleAt)
	syncRunner := leaguesync.New(db, owner, sources, "")
	loop := New(db, sources, syncRunner)
	defer syncRunner.Close()
	loop.now = func() time.Time { return now.Add(2 * time.Hour) }
	defer loop.Close()

	samples, changes, err := loop.backfillMatchup(context.Background(), matchupID)
	if err != nil {
		t.Fatal(err)
	}
	if samples != 0 || changes != 0 || sources.scoreCalls != 1 ||
		sources.playCalls != 1 || len(sources.playIDs) != 1 || sources.playIDs[0] != "finished" {
		t.Fatalf("on-time backfill = %d/%d with %d scoreboard/%d play calls (%v); want untouched history after fetching started-game plays",
			samples, changes, sources.scoreCalls, sources.playCalls, sources.playIDs)
	}
	var savedAt time.Time
	if err := pool.QueryRow(context.Background(), `
		SELECT observed_at FROM win_chance_samples WHERE matchup_id=$1
	`, matchupID).Scan(&savedAt); err != nil {
		t.Fatal(err)
	}
	if !savedAt.Equal(sampleAt) {
		t.Fatalf("on-time sample moved from %s to %s", sampleAt, savedAt)
	}
}

func TestInProgressWinChanceBackfillRebuildsLateHistory(t *testing.T) {
	db, pool, sources, _, matchupID, now := newInProgressBackfillFixture(t)
	syncRunner := leaguesync.New(db, owner, sources, "")
	loop := New(db, sources, syncRunner)
	defer syncRunner.Close()
	loop.now = func() time.Time { return now.Add(2 * time.Hour) }
	defer loop.Close()

	insertInProgressBackfillSample(t, db, matchupID, now.Add(90*time.Minute))
	var teamID, playerID pgtype.UUID
	if err := pool.QueryRow(context.Background(), `
		SELECT ml.team_id,ml.player_id
		FROM matchup_lineups ml
		JOIN player_external_ids x ON x.player_id=ml.player_id
		WHERE ml.matchup_id=$1 AND x.provider='espn' AND x.external_id='101'
	`, matchupID).Scan(&teamID, &playerID); err != nil {
		t.Fatal(err)
	}
	zero, err := fourDecimalNumeric(0)
	if err != nil {
		t.Fatal(err)
	}
	one, err := fourDecimalNumeric(1)
	if err != nil {
		t.Fatal(err)
	}
	seven, err := fourDecimalNumeric(7)
	if err != nil {
		t.Fatal(err)
	}
	strayChangeAt := now.Add(95 * time.Minute)
	if err := db.Queries().InsertLineupPointChange(context.Background(), store.InsertLineupPointChangeParams{
		MatchupID: matchupID, TeamID: teamID, PlayerID: playerID,
		ObservedAt:   pgtype.Timestamptz{Time: strayChangeAt, Valid: true},
		PointsBefore: zero, PointsAfter: one,
	}); err != nil {
		t.Fatal(err)
	}
	gapAt := now.Add(100 * time.Minute)
	gapStartedAt := pgtype.Timestamptz{Time: gapAt, Valid: true}
	if err := db.Queries().InsertWinChanceGap(context.Background(), store.InsertWinChanceGapParams{
		MatchupID: matchupID, StartedAt: gapStartedAt,
		EndedAt: pgtype.Timestamptz{Time: gapAt.Add(time.Minute), Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Queries().InsertWinChanceGapStarter(context.Background(), store.InsertWinChanceGapStarterParams{
		MatchupID: matchupID, StartedAt: gapStartedAt, TeamID: teamID, PlayerID: playerID,
		PointsBefore: zero, PointsAfter: one,
	}); err != nil {
		t.Fatal(err)
	}
	observationAt := now.Add(80 * time.Minute)
	if err := db.Queries().UpsertLiveLineupObservation(context.Background(), store.UpsertLiveLineupObservationParams{
		MatchupID: matchupID, TeamID: teamID, PlayerID: playerID, Points: seven,
		Touchdowns: pgtype.Int4{Int32: 1, Valid: true}, FieldGoals: pgtype.Int4{Int32: 0, Valid: true},
		ObservedAt: pgtype.Timestamptz{Time: observationAt, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}

	samples, changes, err := loop.backfillMatchup(context.Background(), matchupID)
	if err != nil {
		t.Fatal(err)
	}
	if samples <= 1 || changes != 1 || sources.scoreCalls != 1 ||
		sources.playCalls != 1 || len(sources.playIDs) != 1 || sources.playIDs[0] != "finished" {
		t.Fatalf("late-history backfill wrote %d samples/%d changes with %d scoreboard/%d play calls (%v); want replay of only the finished game",
			samples, changes, sources.scoreCalls, sources.playCalls, sources.playIDs)
	}

	firstPlayAt := sources.plays[0].At.Truncate(time.Minute)
	var firstSampleAt time.Time
	var savedSamples, savedChanges, savedGaps, savedGapStarters, observations int
	var savedChangePlayer, changeBefore, changeAfter string
	var savedChangeAt time.Time
	var observationPoints string
	var observationTouchdowns, observationFieldGoals int32
	var savedObservationAt time.Time
	if err := pool.QueryRow(context.Background(), `
		SELECT
			(SELECT min(observed_at) FROM win_chance_samples WHERE matchup_id=$1),
			(SELECT count(*) FROM win_chance_samples WHERE matchup_id=$1),
			(SELECT count(*) FROM lineup_point_changes WHERE matchup_id=$1),
			(SELECT count(*) FROM win_chance_gaps WHERE matchup_id=$1),
			(SELECT count(*) FROM win_chance_gap_starters WHERE matchup_id=$1),
			(SELECT count(*) FROM live_lineup_observations WHERE matchup_id=$1),
			(SELECT x.external_id FROM lineup_point_changes c
			 JOIN player_external_ids x ON x.player_id=c.player_id AND x.provider='espn'
			 WHERE c.matchup_id=$1 LIMIT 1),
			(SELECT points_before::text FROM lineup_point_changes WHERE matchup_id=$1 LIMIT 1),
			(SELECT points_after::text FROM lineup_point_changes WHERE matchup_id=$1 LIMIT 1),
			(SELECT observed_at FROM lineup_point_changes WHERE matchup_id=$1 LIMIT 1),
			(SELECT points::text FROM live_lineup_observations WHERE matchup_id=$1 LIMIT 1),
			(SELECT touchdowns FROM live_lineup_observations WHERE matchup_id=$1 LIMIT 1),
			(SELECT field_goals FROM live_lineup_observations WHERE matchup_id=$1 LIMIT 1),
			(SELECT observed_at FROM live_lineup_observations WHERE matchup_id=$1 LIMIT 1)
	`, matchupID).Scan(
		&firstSampleAt, &savedSamples, &savedChanges, &savedGaps, &savedGapStarters, &observations,
		&savedChangePlayer, &changeBefore, &changeAfter, &savedChangeAt,
		&observationPoints, &observationTouchdowns, &observationFieldGoals, &savedObservationAt,
	); err != nil {
		t.Fatal(err)
	}
	if savedSamples != samples || firstSampleAt.After(firstPlayAt) {
		t.Fatalf("saved %d samples; first at %s, want %d samples and no later than first-play minute %s",
			savedSamples, firstSampleAt, samples, firstPlayAt)
	}
	if savedChanges != changes || savedChangePlayer != "101" ||
		changeBefore != "0.0000" || changeAfter != "7.0000" ||
		!savedChangeAt.Equal(sources.plays[0].At) {
		t.Fatalf("saved point changes = %d rows for player %q, %s→%s at %s; want the one replayed change for player 101",
			savedChanges, savedChangePlayer, changeBefore, changeAfter, savedChangeAt)
	}
	if savedGaps != 0 || savedGapStarters != 0 {
		t.Fatalf("replacement left %d gaps and %d gap starters", savedGaps, savedGapStarters)
	}
	if observations != 1 || observationPoints != "7.0000" || observationTouchdowns != 1 ||
		observationFieldGoals != 0 || !savedObservationAt.Equal(observationAt) {
		t.Fatalf("live observations changed: rows=%d points=%s TD=%d FG=%d observed_at=%s",
			observations, observationPoints, observationTouchdowns, observationFieldGoals, savedObservationAt)
	}

	historyBefore := inProgressBackfillHistorySnapshot(t, pool, matchupID)
	samples, changes, err = loop.backfillMatchup(context.Background(), matchupID)
	if err != nil {
		t.Fatal(err)
	}
	historyAfter := inProgressBackfillHistorySnapshot(t, pool, matchupID)
	if samples != 0 || changes != 0 || historyAfter != historyBefore {
		t.Fatalf("second backfill = %d/%d; history changed=%t, want no rebuild or row changes", samples, changes, historyAfter != historyBefore)
	}
}

func inProgressBackfillHistorySnapshot(t *testing.T, pool *pgxpool.Pool, matchupID pgtype.UUID) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(context.Background(), `
		SELECT
			coalesce((SELECT string_agg(concat_ws(':',observed_at::text,team_a_win_chance::text,score_a::text,
				score_b::text,projected_a::text,projected_b::text),'|' ORDER BY observed_at)
				FROM win_chance_samples WHERE matchup_id=$1),'')
			|| '#' || coalesce((SELECT string_agg(concat_ws(':',team_id::text,player_id::text,observed_at::text,
				points_before::text,points_after::text),'|' ORDER BY observed_at,team_id,player_id)
				FROM lineup_point_changes WHERE matchup_id=$1),'')
			|| '#' || coalesce((SELECT string_agg(concat_ws(':',started_at::text,ended_at::text,filled_at::text),'|'
				ORDER BY started_at) FROM win_chance_gaps WHERE matchup_id=$1),'')
			|| '#' || coalesce((SELECT string_agg(concat_ws(':',started_at::text,team_id::text,player_id::text,
				points_before::text,points_after::text),'|' ORDER BY started_at,team_id,player_id)
				FROM win_chance_gap_starters WHERE matchup_id=$1),'')
			|| '#' || coalesce((SELECT string_agg(concat_ws(':',team_id::text,player_id::text,points::text,
				touchdowns::text,field_goals::text,observed_at::text),'|' ORDER BY team_id,player_id)
				FROM live_lineup_observations WHERE matchup_id=$1),'')
	`, matchupID).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

type inProgressLiveBackfillSources struct {
	*backfillSources
	liveBoard fantasy.NFLScoreboard
}

func (s *inProgressLiveBackfillSources) NFLScoreboard(context.Context) (fantasy.NFLScoreboard, error) {
	return s.liveBoard, nil
}

func TestInProgressBackfillThenLiveTickDoesNotDuplicatePointChange(t *testing.T) {
	testInProgressBackfillThenLiveTickDoesNotDuplicatePointChange(t, false)
}

func TestInProgressLateBackfillThenLiveTickDoesNotDuplicatePointChange(t *testing.T) {
	testInProgressBackfillThenLiveTickDoesNotDuplicatePointChange(t, true)
}

func testInProgressBackfillThenLiveTickDoesNotDuplicatePointChange(t *testing.T, lateSample bool) {
	t.Helper()
	db, pool, backfill, provider, matchupID, now := newInProgressBackfillFixture(t)
	if lateSample {
		insertInProgressBackfillSample(t, db, matchupID, now.Add(90*time.Minute))
	}
	liveProvider := &fakeLiveProvider{fakeSource: provider}
	backfill.fakeSources[fantasy.ProviderESPN] = liveProvider
	liveSources := &inProgressLiveBackfillSources{
		backfillSources: backfill,
		liveBoard: fantasy.NFLScoreboard{
			Season: 2026, SeasonType: 2, Week: 6,
			Games: []fantasy.NFLGameStatus{{
				ID: "finished", HomeTeam: "KC", AwayTeam: "CIN",
				State: fantasy.NFLGameLive, Period: 4, Clock: time.Minute,
			}},
		},
	}
	liveProvider.live = fantasy.LiveMatchup{
		Matchup: fantasy.Matchup{
			ExternalID: "week-6", Period: 6,
			TeamA: fantasy.MatchupSide{TeamID: "1", Score: floatPtr(7)},
			TeamB: &fantasy.MatchupSide{TeamID: "2", Score: floatPtr(0)},
		},
		Lineups: []fantasy.MatchupLineup{
			{TeamID: "1", Entries: []fantasy.LineupEntry{{PlayerID: "101", Starter: true, Points: floatPtr(7)}}},
			{TeamID: "2", Entries: []fantasy.LineupEntry{{PlayerID: "202", Starter: true, Points: floatPtr(0)}}},
		},
	}
	syncRunner := leaguesync.New(db, owner, liveSources, "")
	loop := New(db, liveSources, syncRunner)
	defer syncRunner.Close()
	loop.now = func() time.Time { return now.Add(2 * time.Hour) }
	defer loop.Close()

	backfilled, _, err := loop.backfillMatchup(context.Background(), matchupID)
	if err != nil {
		t.Fatal(err)
	}
	var changesBefore, samplesBefore int
	if err := pool.QueryRow(context.Background(), `
		SELECT (SELECT count(*) FROM lineup_point_changes WHERE matchup_id=$1),
			(SELECT count(*) FROM win_chance_samples WHERE matchup_id=$1)
	`, matchupID).Scan(&changesBefore, &samplesBefore); err != nil {
		t.Fatal(err)
	}
	if changesBefore != 1 || samplesBefore != backfilled {
		t.Fatalf("backfill saved %d changes/%d samples, want one change and %d samples",
			changesBefore, samplesBefore, backfilled)
	}

	loop.runLiveScoreTick(context.Background(), map[string]time.Time{})
	var changesAfter, samplesAfter int
	if err := pool.QueryRow(context.Background(), `
		SELECT (SELECT count(*) FROM lineup_point_changes WHERE matchup_id=$1),
			(SELECT count(*) FROM win_chance_samples WHERE matchup_id=$1)
	`, matchupID).Scan(&changesAfter, &samplesAfter); err != nil {
		t.Fatal(err)
	}
	if changesAfter != changesBefore {
		var rows []string
		result, err := pool.Query(context.Background(), `
			SELECT x.external_id || ':' || points_before::text || '→' || points_after::text
			FROM lineup_point_changes c
			JOIN player_external_ids x ON x.player_id=c.player_id AND x.provider='espn'
			WHERE c.matchup_id=$1 ORDER BY c.observed_at
		`, matchupID)
		if err != nil {
			t.Fatal(err)
		}
		for result.Next() {
			var row string
			if err := result.Scan(&row); err != nil {
				result.Close()
				t.Fatal(err)
			}
			rows = append(rows, row)
		}
		result.Close()
		t.Fatalf("live tick created duplicate point-change rows: %v", rows)
	}
	if samplesAfter <= samplesBefore {
		t.Fatalf("live tick added no win-chance sample: before=%d after=%d", samplesBefore, samplesAfter)
	}
}
