//go:build integration

package livescore

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	migrations "github.com/adityachoudhari26/league-lens/db"
	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/leaguesync"
	"github.com/adityachoudhari26/league-lens/internal/news"
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

func (f fakeSources) SourceFor(target leaguesync.Target) (fantasy.Provider, error) {
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

type emptyNewsSource struct{}

func (emptyNewsSource) Name() string { return "empty" }

func (emptyNewsSource) PlayerNews(context.Context, []news.Player) ([]news.Item, error) {
	return nil, nil
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
	schema := fmt.Sprintf("livescore_%d", time.Now().UnixNano())
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
				{PlayerID: "q", Starter: true, Points: floatPtr(22), Scoring: &fantasy.ScoringCounts{Touchdowns: 1, FieldGoals: 2}},
			}},
			{TeamID: "1", Entries: []fantasy.LineupEntry{
				{PlayerID: "p", Starter: true, Points: floatPtr(17), Scoring: &fantasy.ScoringCounts{Touchdowns: 2, FieldGoals: 1}},
				{PlayerID: "old", Starter: true, Points: floatPtr(2)},
				{PlayerID: "bench", Points: floatPtr(4)},
			}},
		},
	}
}

func waitUntilIdle(t *testing.T, runner *leaguesync.Runner) map[fantasy.ProviderID]leaguesync.Status {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		statuses, err := runner.Status(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		byProvider := map[fantasy.ProviderID]leaguesync.Status{}
		syncing := false
		for _, status := range statuses {
			byProvider[status.Target.Selection.League.Provider] = status
			syncing = syncing || status.State == leaguesync.StateSyncing
		}
		if !syncing {
			return byProvider
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("sync did not finish")
	return nil
}
