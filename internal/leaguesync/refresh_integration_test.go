//go:build integration

package leaguesync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/news"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

type fakeNewsSources struct {
	fakeSources
	news news.Source
}

func (f fakeNewsSources) NewsSource() news.Source { return f.news }

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
