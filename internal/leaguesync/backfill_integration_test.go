//go:build integration

package leaguesync

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

type backfillSources struct {
	fakeSources
	board      fantasy.NFLScoreboard
	plays      []fantasy.NFLPlay
	scoreCalls int
	playCalls  int
}

func (s *backfillSources) NFLWeekScoreboard(context.Context, int, int) (fantasy.NFLScoreboard, error) {
	s.scoreCalls++
	return s.board, nil
}

func (s *backfillSources) NFLGamePlays(context.Context, string) ([]fantasy.NFLPlay, error) {
	s.playCalls++
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
	runner := New(db, owner, sources, "")
	defer runner.Close()
	var matchupID pgtype.UUID
	if err := pool.QueryRow(context.Background(), `
		SELECT m.id FROM matchups m
		JOIN league_seasons l ON l.id=m.league_season_id
		WHERE l.external_league_id='backfill' AND m.external_matchup_id='week-5'
	`).Scan(&matchupID); err != nil {
		t.Fatal(err)
	}

	sampleCount, changeCount, err := runner.backfillMatchup(context.Background(), matchupID)
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

	sampleCount, changeCount, err = runner.backfillMatchup(context.Background(), matchupID)
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

	tailSamples, tailChanges, err := runner.backfillMatchup(context.Background(), matchupID)
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
	sampleCount, changeCount, err = runner.backfillMatchup(context.Background(), matchupID)
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
	if !runner.RequestWinChanceBackfill(ineligibleMatchupID) {
		t.Fatal("first ineligible backfill request was not scheduled")
	}
	runner.wg.Wait()
	runner.mu.Lock()
	failure := runner.backfillFailures[ineligibleMatchupID.String()]
	runner.mu.Unlock()
	if failure.code != "nothing_to_rebuild" {
		t.Fatalf("ineligible backfill failure code = %q, want nothing_to_rebuild", failure.code)
	}
	if runner.RequestWinChanceBackfill(ineligibleMatchupID) {
		t.Fatal("second ineligible backfill request should be throttled")
	}
}
