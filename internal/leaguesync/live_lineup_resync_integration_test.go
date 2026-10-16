//go:build integration

package leaguesync

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/importer"
)

type liveLineupResyncRun struct {
	id     string
	kind   string
	status string
}

func newLiveLineupResyncRunner(t *testing.T) (*Runner, *pgxpool.Pool, *fakeSource, *fakeLiveProvider) {
	t.Helper()
	db, pool := openDatabaseWithTestPool(t, true)
	data := liveLeagueFixture()
	provider := &fakeSource{data: data}
	importService := importer.Service{DB: db, Source: provider}
	if _, err := importService.Import(context.Background(), importer.Request{
		OwnerEmail: owner, ExternalAccountID: "account",
		Selection: fantasy.ConnectionSelection{League: data.League.Ref, SelectedTeamIDs: []string{"1"}},
	}); err != nil {
		t.Fatal(err)
	}

	liveProvider := &fakeLiveProvider{fakeSource: provider}
	sources := &liveScoreSources{
		fakeSources: fakeSources{fantasy.ProviderSleeper: liveProvider},
		board: fantasy.NFLScoreboard{
			Season: 2026, SeasonType: 2, Week: 5,
			Games: []fantasy.NFLGameStatus{{
				HomeTeam: "KC", AwayTeam: "SF", State: fantasy.NFLGameLive, Period: 1, Clock: 15 * time.Minute,
			}},
		},
	}
	runner := New(db, owner, sources, "")
	t.Cleanup(runner.Close)
	return runner, pool, provider, liveProvider
}

func runLiveLineupResyncTick(runner *Runner) {
	runner.runLiveScoreTick(context.Background(), map[string]time.Time{})
}

func waitForLiveLineupSync(t *testing.T, runner *Runner) {
	t.Helper()
	finished := waitUntilIdle(t, runner)
	status, found := finished[fantasy.ProviderSleeper]
	if !found || status.State != StateSucceeded {
		t.Fatalf("sync status = %+v, want succeeded", status)
	}
}

func readLiveLineupResyncRuns(t *testing.T, pool *pgxpool.Pool) []liveLineupResyncRun {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT sr.id::text, sr.kind, sr.status
		FROM sync_runs sr
		JOIN league_seasons ls ON ls.id=sr.league_season_id
		WHERE ls.external_league_id='league-sleeper'
		ORDER BY sr.started_at
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var runs []liveLineupResyncRun
	for rows.Next() {
		var run liveLineupResyncRun
		if err := rows.Scan(&run.id, &run.kind, &run.status); err != nil {
			t.Fatal(err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return runs
}

func assertOneNewSucceededResync(t *testing.T, before, after []liveLineupResyncRun) {
	t.Helper()
	if len(after) != len(before)+1 {
		t.Fatalf("sync runs grew from %d to %d, want exactly one new run", len(before), len(after))
	}
	run := after[len(after)-1]
	if run.kind != "refresh" || run.status != StateSucceeded {
		t.Fatalf("new sync run = %+v, want succeeded refresh", run)
	}
}

func liveLineupWithHomeEntries(entries []fantasy.LineupEntry) fantasy.LiveMatchup {
	live := liveMatchupFixture("m")
	for i := range live.Lineups {
		switch live.Lineups[i].TeamID {
		case "1":
			live.Lineups[i].Entries = entries
		case "2":
			live.Lineups[i].Entries = []fantasy.LineupEntry{liveLineupEntry("q", true)}
		}
	}
	return live
}

func homeLineupWithEntries(data fantasy.LeagueData, entries []fantasy.LineupEntry) fantasy.LeagueData {
	for i := range data.Lineups {
		if data.Lineups[i].TeamID == "1" {
			data.Lineups[i].Entries = entries
		}
	}
	return data
}

func liveLineupEntry(playerID string, starter bool) fantasy.LineupEntry {
	return fantasy.LineupEntry{PlayerID: playerID, Starter: starter, Points: floatPtr(1)}
}

func readHomeLineupStarters(t *testing.T, pool *pgxpool.Pool) map[string]bool {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT x.external_id, ml.starter
		FROM matchup_lineups ml
		JOIN matchups m ON m.id=ml.matchup_id
		JOIN league_seasons ls ON ls.id=m.league_season_id
		JOIN teams tm ON tm.id=ml.team_id
		JOIN player_external_ids x ON x.player_id=ml.player_id AND x.provider='sleeper'
		WHERE ls.external_league_id='league-sleeper' AND m.external_matchup_id='m'
			AND tm.external_team_id='1'
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	starters := map[string]bool{}
	for rows.Next() {
		var playerID string
		var starter bool
		if err := rows.Scan(&playerID, &starter); err != nil {
			t.Fatal(err)
		}
		starters[playerID] = starter
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return starters
}

func TestLiveLineupSwapStartsOneResync(t *testing.T) {
	runner, pool, provider, liveProvider := newLiveLineupResyncRunner(t)
	before := readLiveLineupResyncRuns(t, pool)
	changedEntries := []fantasy.LineupEntry{
		{PlayerID: "p", Slot: fantasy.SlotBench, Starter: false},
		{PlayerID: "old", Slot: fantasy.SlotRB, Starter: true},
		{PlayerID: "bench", Slot: fantasy.SlotRB, Starter: true},
	}
	provider.data = homeLineupWithEntries(provider.data, changedEntries)
	liveProvider.live = liveLineupWithHomeEntries([]fantasy.LineupEntry{
		liveLineupEntry("p", false),
		liveLineupEntry("old", true),
		liveLineupEntry("bench", true),
	})

	runLiveLineupResyncTick(runner)
	waitForLiveLineupSync(t, runner)
	assertOneNewSucceededResync(t, before, readLiveLineupResyncRuns(t, pool))

	starters := readHomeLineupStarters(t, pool)
	if pStarter, found := starters["p"]; !found || pStarter {
		t.Fatalf("p starter flag after swap = %t, want a stored benched player; lineup = %+v", pStarter, starters)
	}
	if benchStarter, found := starters["bench"]; !found || !benchStarter {
		t.Fatalf("starter flags after swap = %+v, want p benched and bench player started", starters)
	}
}

func TestLiveLineupPickupStartsResyncAndImportsPlayer(t *testing.T) {
	runner, pool, provider, liveProvider := newLiveLineupResyncRunner(t)
	before := readLiveLineupResyncRuns(t, pool)
	provider.data.Players = append(provider.data.Players,
		fantasy.Player{ExternalID: "new", Name: "New Starter", Kind: fantasy.PlayerPerson},
	)
	provider.data = homeLineupWithEntries(provider.data, []fantasy.LineupEntry{
		{PlayerID: "p", Slot: fantasy.SlotRB, Starter: true},
		{PlayerID: "old", Slot: fantasy.SlotRB, Starter: true},
		{PlayerID: "bench", Slot: fantasy.SlotBench},
		{PlayerID: "new", Slot: fantasy.SlotWR, Starter: true},
	})
	liveProvider.live = liveLineupWithHomeEntries([]fantasy.LineupEntry{
		liveLineupEntry("p", true),
		liveLineupEntry("old", true),
		liveLineupEntry("bench", false),
		liveLineupEntry("new", true),
	})

	runLiveLineupResyncTick(runner)
	waitForLiveLineupSync(t, runner)
	assertOneNewSucceededResync(t, before, readLiveLineupResyncRuns(t, pool))

	if starters := readHomeLineupStarters(t, pool); !starters["new"] {
		t.Fatalf("new player's starter flag = %t, want true; lineup = %+v", starters["new"], starters)
	}
}

func TestLiveLineupDropStartsOneResync(t *testing.T) {
	runner, pool, provider, liveProvider := newLiveLineupResyncRunner(t)
	before := readLiveLineupResyncRuns(t, pool)
	provider.data = homeLineupWithEntries(provider.data, []fantasy.LineupEntry{
		{PlayerID: "p", Slot: fantasy.SlotRB, Starter: true},
		{PlayerID: "bench", Slot: fantasy.SlotBench},
	})
	liveProvider.live = liveLineupWithHomeEntries([]fantasy.LineupEntry{
		liveLineupEntry("p", true),
		liveLineupEntry("bench", false),
	})

	runLiveLineupResyncTick(runner)
	waitForLiveLineupSync(t, runner)
	assertOneNewSucceededResync(t, before, readLiveLineupResyncRuns(t, pool))

	starters := readHomeLineupStarters(t, pool)
	if _, found := starters["old"]; found {
		t.Fatalf("dropped player's row remains in matchup lineup: %+v", starters)
	}
}

func TestUnchangedLiveLineupDoesNotResync(t *testing.T) {
	runner, pool, _, liveProvider := newLiveLineupResyncRunner(t)
	before := readLiveLineupResyncRuns(t, pool)
	liveProvider.live = liveLineupWithHomeEntries([]fantasy.LineupEntry{
		liveLineupEntry("p", true),
		liveLineupEntry("old", true),
		liveLineupEntry("bench", false),
	})

	runLiveLineupResyncTick(runner)
	waitForLiveLineupSync(t, runner)

	if after := readLiveLineupResyncRuns(t, pool); len(after) != len(before) {
		t.Fatalf("unchanged lineup added %d sync runs, want none", len(after)-len(before))
	}
}

func TestLiveLineupResyncCooldownPreventsRepeatedSync(t *testing.T) {
	runner, pool, _, liveProvider := newLiveLineupResyncRunner(t)
	before := readLiveLineupResyncRuns(t, pool)
	now := time.Date(2026, time.October, 16, 12, 0, 0, 0, time.UTC)
	runner.now = func() time.Time { return now }
	liveProvider.live = liveLineupWithHomeEntries([]fantasy.LineupEntry{
		liveLineupEntry("p", false),
		liveLineupEntry("old", true),
		liveLineupEntry("bench", true),
	})

	runLiveLineupResyncTick(runner)
	waitForLiveLineupSync(t, runner)
	starters := readHomeLineupStarters(t, pool)
	pStarter, pFound := starters["p"]
	benchStarter, benchFound := starters["bench"]
	if !pFound || !pStarter || !benchFound || benchStarter {
		t.Fatalf("full sync did not retain the old lineup: %+v", starters)
	}
	now = now.Add(time.Minute)
	runLiveLineupResyncTick(runner)
	waitForLiveLineupSync(t, runner)

	assertOneNewSucceededResync(t, before, readLiveLineupResyncRuns(t, pool))
}
