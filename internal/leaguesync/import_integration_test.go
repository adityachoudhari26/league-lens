//go:build integration

package leaguesync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/espn"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/news"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

type importRefreshSources struct {
	scheduleSources
	news news.Source
}

func (s *importRefreshSources) NewsSource() news.Source { return s.news }

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
