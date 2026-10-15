//go:build integration

package leaguesync_test

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
	"github.com/jackc/pgx/v5/stdlib"

	migrations "github.com/adityachoudhari26/league-lens/db"
	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/leaguesync"
	"github.com/adityachoudhari26/league-lens/internal/news"
	"github.com/adityachoudhari26/league-lens/internal/secrets"
	"github.com/adityachoudhari26/league-lens/internal/store"
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

func (s *fakeSource) FetchLeague(ctx context.Context, _ fantasy.League) (fantasy.LeagueData, error) {
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

func (fakeSources) NewsSource() news.Source { return emptyNewsSource{} }

func (f fakeSources) SourceFor(target leaguesync.Target) (fantasy.Provider, error) {
	return f[target.Selection.League.Provider], nil
}

type storedCredentialSources struct {
	leaguesync.ProviderSources
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
	return db
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
	runner := leaguesync.New(db, owner, sources, "")
	t.Cleanup(runner.Close)

	started, err := runner.Start(context.Background(), false)
	if err != nil || len(started) != 2 {
		t.Fatalf("start = %d statuses, %v", len(started), err)
	}
	for _, status := range started {
		if status.State != leaguesync.StateSyncing {
			t.Fatalf("started league state = %s, want syncing", status.State)
		}
	}

	finished := waitUntilIdle(t, runner)
	if got := finished[fantasy.ProviderSleeper].State; got != leaguesync.StateSucceeded {
		t.Errorf("sleeper state = %s, want succeeded", got)
	}
	if got := homeTeamName(t, db, fantasy.ProviderSleeper); got != "Home Renamed" {
		t.Errorf("sleeper team = %q, want refreshed name", got)
	}
	espnStatus := finished[fantasy.ProviderESPN]
	if espnStatus.State != leaguesync.StateFailed || espnStatus.ErrorCode != "provider_fetch_failed" || espnStatus.FailedAt.IsZero() {
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
	runner := leaguesync.New(db, owner, fakeSources{fantasy.ProviderSleeper: source}, "")
	t.Cleanup(runner.Close)

	statuses, err := runner.Start(context.Background(), true)
	if err != nil || len(statuses) != 1 || statuses[0].State != leaguesync.StateSucceeded {
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
	runner := leaguesync.New(db, owner, fakeSources{fantasy.ProviderSleeper: source}, "")
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
	runner := leaguesync.New(db, owner, leaguesync.ProviderSources{}, "")
	t.Cleanup(runner.Close)

	if _, err := runner.Start(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	status := waitUntilIdle(t, runner)[fantasy.ProviderESPN]
	if status.State != leaguesync.StateFailed || status.ErrorCode != "espn_credentials_missing" {
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
	runner := leaguesync.New(db, owner, storedCredentialSources{
		ProviderSources: leaguesync.ProviderSources{Box: box},
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
	runner := leaguesync.New(db, owner, fakeNewsSources{
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
	runner := leaguesync.New(db, owner, fakeNewsSources{
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
	if got := statuses[fantasy.ProviderSleeper].State; got != leaguesync.StateSucceeded {
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
