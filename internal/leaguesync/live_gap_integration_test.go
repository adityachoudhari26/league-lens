//go:build integration

package leaguesync

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

type liveGapSources struct {
	*liveScoreSources
	plays []fantasy.NFLPlay
}

func (s *liveGapSources) NFLGamePlays(context.Context, string) ([]fantasy.NFLPlay, error) {
	return s.plays, nil
}

func TestLiveGapFillReplaysStaleBaselineWindowOnce(t *testing.T) {
	db, pool := openDatabaseWithTestPool(t, true)
	ctx := context.Background()
	data := liveLeagueFixture()
	data.League.Settings.ScoringRules = []fantasy.ScoringRule{
		{Stat: fantasy.StatRushingYards, Points: 1, PerUnits: 10},
	}
	provider := &fakeSource{data: data}
	importService := importer.Service{DB: db, Source: provider}
	if _, err := importService.Import(ctx, importer.Request{
		OwnerEmail: owner, ExternalAccountID: "account",
		Selection: fantasy.ConnectionSelection{League: data.League.Ref, SelectedTeamIDs: []string{"1"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO player_external_ids(provider, external_id, player_id, mapping_source)
		SELECT 'espn', '123456', id, 'provider' FROM players WHERE display_name='Fixture Player'
		ON CONFLICT DO NOTHING
	`); err != nil {
		t.Fatal(err)
	}

	startedAt := time.Date(2026, time.October, 9, 18, 0, 0, 0, time.UTC)
	endedAt := startedAt.Add(8 * time.Minute)
	scoreboard := fantasy.NFLScoreboard{
		Season: 2026, SeasonType: 2, Week: 5,
		Games: []fantasy.NFLGameStatus{{
			ID: "event-gap", HomeTeam: "KC", AwayTeam: "SF", State: fantasy.NFLGameLive,
			Period: 2, Clock: 8 * time.Minute,
		}},
	}
	sources := &liveGapSources{liveScoreSources: &liveScoreSources{
		fakeSources: fakeSources{fantasy.ProviderSleeper: &fakeLiveProvider{fakeSource: provider}},
		board:       scoreboard,
	}, plays: []fantasy.NFLPlay{
		{
			At: startedAt.Add(2 * time.Minute), Period: 2, Clock: 14 * time.Minute,
			Kind: fantasy.NFLPlayRush, Yards: 10,
			Participants: []fantasy.NFLPlayParticipant{{AthleteID: "123456", Role: "rusher"}},
		},
		{
			At: startedAt.Add(7 * time.Minute), Period: 2, Clock: 9 * time.Minute,
			Kind: fantasy.NFLPlayRush, Yards: 20,
			Participants: []fantasy.NFLPlayParticipant{{AthleteID: "123456", Role: "rusher"}},
		},
	}}
	liveProvider := sources.fakeSources[fantasy.ProviderSleeper].(*fakeLiveProvider)
	runner := New(db, owner, sources, "")
	defer runner.Close()
	tickAt := startedAt
	runner.now = func() time.Time { return tickAt }
	user, err := db.Queries().GetUserByEmail(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := db.Queries().ListLiveMatchupCandidates(ctx, store.ListLiveMatchupCandidatesParams{
		UserID: user.ID, Season: 2026, Week: 5,
	})
	if err != nil || len(candidates) != 1 {
		t.Fatalf("live candidates = %d, error = %v", len(candidates), err)
	}
	targets, err := runner.listTargets(ctx)
	if err != nil || len(targets) != 1 {
		t.Fatalf("sync targets = %d, error = %v", len(targets), err)
	}

	firstTick := pointChangeLiveFixture(17, 4, 0, 0)
	firstTick.Lineups[1].Entries[0].Scoring = nil
	liveProvider.live = firstTick
	if _, _, err := runner.saveLiveMatchup(ctx, scoreboard, targets[0], candidates[0], firstTick); err != nil {
		t.Fatal(err)
	}
	tickAt = endedAt
	secondTick := pointChangeLiveFixture(23.8, 4, 1, 0)
	liveProvider.live = secondTick
	if _, _, err := runner.saveLiveMatchup(ctx, scoreboard, targets[0], candidates[0], secondTick); err != nil {
		t.Fatal(err)
	}

	var matchupID string
	var savedStart, savedEnd time.Time
	if err := pool.QueryRow(ctx, `
		SELECT g.matchup_id, g.started_at, g.ended_at
		FROM win_chance_gaps g
		JOIN matchups m ON m.id=g.matchup_id
		JOIN league_seasons ls ON ls.id=m.league_season_id
		WHERE ls.external_league_id='league-sleeper' AND m.external_matchup_id='m'
	`).Scan(&matchupID, &savedStart, &savedEnd); err != nil {
		t.Fatal(err)
	}
	if !savedStart.Equal(startedAt) || !savedEnd.Equal(endedAt) {
		t.Fatalf("saved gap = %s to %s, want %s to %s", savedStart, savedEnd, startedAt, endedAt)
	}
	var gapStarters int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM win_chance_gap_starters
		WHERE matchup_id=$1 AND started_at=$2
	`, matchupID, startedAt).Scan(&gapStarters); err != nil {
		t.Fatal(err)
	}
	if gapStarters != 2 {
		t.Fatalf("saved gap starters = %d, want both matchup starters", gapStarters)
	}
	if changes := readLineupPointChanges(t, pool); len(changes) != 0 {
		t.Fatalf("stale tick recorded %d ordinary point changes, want none", len(changes))
	}

	var gapID pgtype.UUID
	if err := gapID.Scan(matchupID); err != nil {
		t.Fatal(err)
	}
	if !runner.requestWinChanceGapFill(gapID, startedAt) {
		t.Fatal("gap fill request was not accepted")
	}
	runner.wg.Wait()
	assertLiveGapFill(t, pool, matchupID, startedAt, endedAt)

	if !runner.requestWinChanceGapFill(gapID, startedAt) {
		t.Fatal("repeat gap fill request was not accepted")
	}
	runner.wg.Wait()
	assertLiveGapFill(t, pool, matchupID, startedAt, endedAt)
}

func assertLiveGapFill(t *testing.T, pool *pgxpool.Pool, matchupID string, startedAt, endedAt time.Time) {
	t.Helper()
	ctx := context.Background()
	var filledAt pgtype.Timestamptz
	if err := pool.QueryRow(ctx, `
		SELECT filled_at FROM win_chance_gaps
		WHERE matchup_id=$1 AND started_at=$2
	`, matchupID, startedAt).Scan(&filledAt); err != nil {
		t.Fatal(err)
	}
	if !filledAt.Valid || filledAt.Time.IsZero() {
		t.Fatal("gap was not marked filled")
	}
	var sampleCount, changeCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM win_chance_samples
		WHERE matchup_id=$1 AND observed_at > $2 AND observed_at < $3
	`, matchupID, startedAt, endedAt).Scan(&sampleCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM lineup_point_changes
		WHERE matchup_id=$1 AND observed_at >= $2 AND observed_at <= $3
	`, matchupID, startedAt, endedAt).Scan(&changeCount); err != nil {
		t.Fatal(err)
	}
	if sampleCount != 2 || changeCount != 3 {
		t.Fatalf("gap history has %d samples and %d changes, want 2 and 3", sampleCount, changeCount)
	}
	var pointsBefore, pointsAfter string
	var tdBefore, tdAfter *int32
	if err := pool.QueryRow(ctx, `
		SELECT c.points_before::text, c.points_after::text, c.touchdowns_before, c.touchdowns_after
		FROM lineup_point_changes c
		JOIN players p ON p.id=c.player_id
		WHERE c.matchup_id=$1 AND c.observed_at >= $2 AND p.display_name='Fixture Player'
		ORDER BY c.observed_at LIMIT 1
	`, matchupID, startedAt).Scan(&pointsBefore, &pointsAfter, &tdBefore, &tdAfter); err != nil {
		t.Fatal(err)
	}
	if pointsBefore != "17.0000" || pointsAfter != "18.0000" || tdBefore != nil || tdAfter != nil {
		t.Fatalf("gap change = %s to %s, TD counts %v/%v; want 17→18 and unknown counts", pointsBefore, pointsAfter, tdBefore, tdAfter)
	}
}
