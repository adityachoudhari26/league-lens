//go:build integration

package leaguesync

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/news"
	"github.com/adityachoudhari26/league-lens/internal/secrets"
	"github.com/adityachoudhari26/league-lens/internal/sleeper"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

type storedCredentialSources struct {
	ProviderSources
}

func (storedCredentialSources) NFLSchedule(context.Context, int) ([]fantasy.NFLGame, error) {
	return nil, nil
}

func (storedCredentialSources) NewsSource() news.Source { return emptyNewsSource{} }

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
	runner := New(db, owner, sources, "")
	t.Cleanup(runner.Close)

	started, err := runner.Start(context.Background(), false)
	if err != nil || len(started) != 2 {
		t.Fatalf("start = %d statuses, %v", len(started), err)
	}
	for _, status := range started {
		if status.State != StateSyncing {
			t.Fatalf("started league state = %s, want syncing", status.State)
		}
	}

	finished := waitUntilIdle(t, runner)
	if got := finished[fantasy.ProviderSleeper].State; got != StateSucceeded {
		t.Errorf("sleeper state = %s, want succeeded", got)
	}
	if got := homeTeamName(t, db, fantasy.ProviderSleeper); got != "Home Renamed" {
		t.Errorf("sleeper team = %q, want refreshed name", got)
	}
	espnStatus := finished[fantasy.ProviderESPN]
	if espnStatus.State != StateFailed || espnStatus.ErrorCode != "provider_fetch_failed" || espnStatus.FailedAt.IsZero() {
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
	runner := New(db, owner, fakeSources{fantasy.ProviderSleeper: source}, "")
	t.Cleanup(runner.Close)

	statuses, err := runner.Start(context.Background(), true)
	if err != nil || len(statuses) != 1 || statuses[0].State != StateSucceeded {
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
	runner := New(db, owner, fakeSources{fantasy.ProviderSleeper: source}, "")
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
	runner := New(db, owner, ProviderSources{}, "")
	t.Cleanup(runner.Close)

	if _, err := runner.Start(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	status := waitUntilIdle(t, runner)[fantasy.ProviderESPN]
	if status.State != StateFailed || status.ErrorCode != "espn_credentials_missing" {
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
	runner := New(db, owner, storedCredentialSources{
		ProviderSources: ProviderSources{Box: box},
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

// With saved teams present, the newest-season fallback stays unused: a league
// attached by Begin alone in a newer season does not become a sync target.
func TestListSyncTargetsKeepsSavedSeasonWhenTeamsExist(t *testing.T) {
	db := openDatabase(t)
	seedLeague(t, db, fantasy.ProviderSleeper)
	source := &fakeSource{data: leagueData(fantasy.ProviderSleeper, "Next Season")}
	source.data.League.Ref = fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "league-sleeper-next", Season: 2027}
	beginRefresh(t, db, source, source.data.League.Ref)
	runner := New(db, owner, fakeSources{}, "")
	defer runner.Close()

	statuses, err := runner.Status(context.Background())
	if err != nil || len(statuses) != 1 {
		t.Fatalf("statuses = %+v, %v; want only the saved season's league", statuses, err)
	}
	if statuses[0].Target.Selection.League.ExternalID != "league-sleeper" {
		t.Fatalf("target league = %q, want the league with saved teams", statuses[0].Target.Selection.League.ExternalID)
	}
}

// A league attached by Begin appears as a sync target before its first import
// saves any teams: the target listing falls back to the newest attached season.
func TestListSyncTargetsIncludesFirstImportBeforeSave(t *testing.T) {
	db := openDatabase(t)
	source := &fakeSource{data: leagueData(fantasy.ProviderSleeper, "Home")}
	started, err := (&importer.Service{DB: db, Source: source}).Begin(context.Background(), importer.Request{
		OwnerEmail: owner, ExternalAccountID: "account",
		Selection: fantasy.ConnectionSelection{League: source.data.League.Ref, SelectedTeamIDs: []string{"1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := New(db, owner, fakeSources{}, "")
	defer runner.Close()

	statuses, err := runner.Status(context.Background())
	if err != nil || len(statuses) != 1 {
		t.Fatalf("statuses = %+v, %v", statuses, err)
	}
	if statuses[0].Target.ID != started.LeagueSeasonID().String() || statuses[0].State != StateNeverSynced {
		t.Fatalf("status = %+v, want the begun league as never_synced", statuses[0])
	}
}

// A Sleeper sync looks up availability only for the listed teams, and the
// league's roster rows are shared, so each owner's sync must list every owner's team.
func TestSyncTargetLooksUpAvailabilityForEveryOwnersTeam(t *testing.T) {
	db := openDatabase(t)
	seedTwoOwnerLeague(t, db)

	target := onlySyncTarget(t, db, owner)
	if !slices.Equal(target.Selection.SelectedTeamIDs, []string{"1"}) {
		t.Fatalf("selected teams = %v, want [1]", target.Selection.SelectedTeamIDs)
	}
	source, err := ProviderSources{}.SourceFor(target)
	if err != nil {
		t.Fatal(err)
	}
	client, ok := source.(*sleeper.Client)
	if !ok || !slices.Equal(client.AvailabilityTeamIDs, []string{"1", "2"}) {
		t.Fatalf("availability teams = %v, want [1 2]", client.AvailabilityTeamIDs)
	}

	detachLeague(t, db, otherOwner, target.ID)
	if got := onlySyncTarget(t, db, owner).AvailabilityTeamIDs; !slices.Equal(got, []string{"1"}) {
		t.Fatalf("availability teams after detach = %v, want [1]", got)
	}
}

func onlySyncTarget(t *testing.T, db *database.DB, email string) Target {
	t.Helper()
	user, err := db.Queries().GetUserByEmail(context.Background(), email)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.Queries().ListSyncTargets(context.Background(), user.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("sync targets for %s = %d, %v", email, len(rows), err)
	}
	return TargetFromRow(rows[0])
}

func detachLeague(t *testing.T, db *database.DB, email, leagueSeasonID string) {
	t.Helper()
	user, err := db.Queries().GetUserByEmail(context.Background(), email)
	if err != nil {
		t.Fatal(err)
	}
	var leagueID pgtype.UUID
	if err = leagueID.Scan(leagueSeasonID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Queries().DetachLeagueForUser(context.Background(), store.DetachLeagueForUserParams{UserID: user.ID, LeagueSeasonID: leagueID}); err != nil {
		t.Fatal(err)
	}
}
