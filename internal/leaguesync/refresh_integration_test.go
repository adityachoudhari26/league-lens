//go:build integration

package leaguesync

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/importer"
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

const otherOwner = "other-owner@test"

func TestPlayerNewsRefreshKeepsOtherOwnersNews(t *testing.T) {
	db := openDatabase(t)
	seedTwoOwnerLeague(t, db)
	ownerPlayer := onlyNewsPlayer(t, db, owner)
	otherPlayer := onlyNewsPlayer(t, db, otherOwner)
	saveNewsItem(t, db, ownerPlayer, "owner-old", "Owner old news")
	saveNewsItem(t, db, otherPlayer, "other-saved", "Other owner news")

	runner := New(db, owner, fakeNewsSources{news: fakeNewsSource{headline: "Owner fresh news"}}, "")
	t.Cleanup(runner.Close)
	runner.RefreshPlayerNews(context.Background())

	assertNewsHeadlines(t, db, ownerPlayer, "Owner fresh news")
	assertNewsHeadlines(t, db, otherPlayer, "Other owner news")
}

// seedTwoOwnerLeague imports one league for two owners: owner selects team 1
// (player p) and otherOwner selects team 2 (player q).
func seedTwoOwnerLeague(t *testing.T, db *database.DB) {
	t.Helper()
	data := leagueData(fantasy.ProviderSleeper, "Home")
	data.Players = append(data.Players, fantasy.Player{ExternalID: "q", Name: "Other Player", Kind: fantasy.PlayerPerson})
	data.Rosters[1].Entries = []fantasy.RosterEntry{{PlayerID: "q", Slot: fantasy.SlotBench}}

	for _, selection := range []struct{ email, account, team string }{
		{owner, "account", "1"},
		{otherOwner, "other-account", "2"},
	} {
		service := importer.Service{DB: db, Source: &fakeSource{data: data}}
		_, err := service.Import(context.Background(), importer.Request{
			OwnerEmail: selection.email, ExternalAccountID: selection.account,
			Selection: fantasy.ConnectionSelection{League: data.League.Ref, SelectedTeamIDs: []string{selection.team}},
		})
		if err != nil {
			t.Fatalf("import for %s: %v", selection.email, err)
		}
	}
}

func onlyNewsPlayer(t *testing.T, db *database.DB, email string) pgtype.UUID {
	t.Helper()
	rows, err := db.Queries().ListNewsPlayers(context.Background(), email)
	if err != nil || len(rows) != 1 {
		t.Fatalf("news players for %s = %v, %v", email, rows, err)
	}
	return rows[0].PlayerID
}

func saveNewsItem(t *testing.T, db *database.DB, playerID pgtype.UUID, itemID, headline string) {
	t.Helper()
	now := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	if err := db.Queries().AddPlayerNews(context.Background(), store.AddPlayerNewsParams{
		Source: "fake", SourceItemID: itemID, PlayerID: playerID, Headline: headline,
		Url: "https://example.test/" + itemID, SourceLabel: "Fixture source", PublishedAt: now, FetchedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
}

func assertNewsHeadlines(t *testing.T, db *database.DB, playerID pgtype.UUID, want ...string) {
	t.Helper()
	rows, err := db.Queries().ListRecentPlayerNews(context.Background(), store.ListRecentPlayerNewsParams{
		PlayerIds: []pgtype.UUID{playerID},
		Since:     pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(rows))
	for _, row := range rows {
		got = append(got, row.Headline)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("news headlines = %v, want %v", got, want)
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
