//go:build integration

package dashboard_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	migrations "github.com/adityachoudhari26/league-lens/db"
	"github.com/adityachoudhari26/league-lens/internal/dashboard"
	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1/leaguelensv1connect"
	"github.com/adityachoudhari26/league-lens/internal/httpserver"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/stdlib"
)

type source struct{ data fantasy.LeagueData }

func (s source) DescribeLeague(context.Context, string) (fantasy.League, error) {
	return s.data.League, nil
}

func (s source) FetchLeague(context.Context, fantasy.League) (fantasy.LeagueData, error) {
	return s.data, nil
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
	schema := fmt.Sprintf("dashboard_%d", time.Now().UnixNano())
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

func importLeague(t *testing.T, db *database.DB, owner string, provider fantasy.ProviderID) {
	t.Helper()
	now := time.Now().UTC()
	zero := 0.0
	league := fantasy.League{Ref: fantasy.LeagueRef{Provider: provider, ExternalID: "fixture", Season: 2026}, Name: string(provider), TeamCount: 2, Settings: fantasy.LeagueSettings{RosterSlots: []fantasy.SlotDefinition{{Slot: fantasy.SlotQB, Count: 1}}}}
	data := fantasy.LeagueData{League: league, FetchedAt: now,
		Teams:   []fantasy.Team{{ExternalID: "1", Name: "Home", Record: &fantasy.TeamRecord{Wins: 1, PointsFor: &zero}}, {ExternalID: "2", Name: "Away"}},
		Players: []fantasy.Player{{ExternalID: "p", Name: "Fixture Player", Kind: fantasy.PlayerPerson}},
		Rosters: []fantasy.RosterSnapshot{
			{TeamID: "1", ScoringPeriod: 2, Origin: fantasy.SnapshotObserved, Coverage: fantasy.CoverageFull, ObservedAt: now, Entries: []fantasy.RosterEntry{{PlayerID: "p", Slot: fantasy.SlotBench}}},
			{TeamID: "1", ScoringPeriod: 1, Origin: fantasy.SnapshotHistory, Coverage: fantasy.CoverageLineup, ObservedAt: now.Add(time.Second)},
			{TeamID: "2", ScoringPeriod: 2, Origin: fantasy.SnapshotObserved, Coverage: fantasy.CoverageFull, ObservedAt: now},
		},
		Matchups:               []fantasy.Matchup{{ExternalID: "m", Period: 1, ScoringPeriods: []fantasy.ScoringPeriod{1, 2}, TeamA: fantasy.MatchupSide{TeamID: "1", Score: &zero}, TeamB: &fantasy.MatchupSide{TeamID: "2"}, Status: fantasy.MatchupUnknown}},
		CompleteMatchupPeriods: []fantasy.MatchupPeriod{1},
	}
	service := importer.Service{DB: db, Source: source{data}}
	_, err := service.Import(context.Background(), importer.Request{OwnerEmail: owner, ExternalAccountID: "account", Selection: fantasy.ConnectionSelection{League: league.Ref, SelectedTeamIDs: []string{"1"}}})
	if err != nil {
		t.Fatal(err)
	}
}

func clientFor(t *testing.T, db *database.DB, owner string) leaguelensv1connect.DashboardServiceClient {
	t.Helper()
	server := httptest.NewServer(httpserver.NewHandler(db.Ping, dashboard.New(db, owner), nil))
	t.Cleanup(server.Close)
	return leaguelensv1connect.NewDashboardServiceClient(connect.NewClient(connecthttp.NewTransport(server.Client(), server.URL, connecthttp.WithProtoJSON())))
}

func TestDashboardRoundTrip(t *testing.T) {
	db := openDatabase(t)
	ctx := context.Background()
	client := clientFor(t, db, "owner@test")
	empty, err := client.ListLeagues(ctx, &v1.ListLeaguesRequest{})
	if err != nil || len(empty.Leagues) != 0 {
		t.Fatalf("new owner: %v %v", empty, err)
	}
	importLeague(t, db, "owner@test", fantasy.ProviderESPN)
	importLeague(t, db, "owner@test", fantasy.ProviderSleeper)
	first, err := client.ListLeagues(ctx, &v1.ListLeaguesRequest{PageSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Leagues) != 1 || first.NextPageToken == "" {
		t.Fatalf("first page: %v", first)
	}
	second, err := client.ListLeagues(ctx, &v1.ListLeaguesRequest{PageSize: 1, PageToken: first.NextPageToken})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Leagues) != 1 || second.NextPageToken != "" || first.Leagues[0].Id == second.Leagues[0].Id {
		t.Fatalf("second page: %v", second)
	}
	for _, league := range []*v1.League{first.Leagues[0], second.Leagues[0]} {
		detail, err := client.GetLeague(ctx, &v1.GetLeagueRequest{LeagueSeasonId: league.Id})
		if err != nil {
			t.Fatal(err)
		}
		if len(detail.Teams) != 2 || len(detail.League.SelectedTeamIds) != 1 || detail.League.LastSuccessfulSyncAt == nil || len(detail.AvailableMatchupPeriods) != 1 {
			t.Fatalf("missing league data: %v", detail)
		}
		var home, away *v1.Team
		for _, team := range detail.Teams {
			if team.Selected {
				home = team
			} else {
				away = team
			}
		}
		if home == nil || home.Record == nil || home.Record.PointsFor == nil || *home.Record.PointsFor != 0 || away.Record != nil {
			t.Fatal("record or selection lost")
		}
		roster, err := client.GetTeamRoster(ctx, &v1.GetTeamRosterRequest{TeamId: home.Id})
		if err != nil {
			t.Fatal(err)
		}
		if roster.Snapshot == nil || roster.Snapshot.ScoringPeriod != 2 || roster.Snapshot.Origin != "observed" || len(roster.Snapshot.Entries) != 1 || roster.Snapshot.Entries[0].EligiblePositions != nil {
			t.Fatalf("latest observed roster: %v", roster)
		}
		week := int32(1)
		history, err := client.GetTeamRoster(ctx, &v1.GetTeamRosterRequest{TeamId: home.Id, ScoringPeriod: &week})
		if err != nil {
			t.Fatal(err)
		}
		if history.Snapshot == nil || history.Snapshot.Coverage != "lineup" {
			t.Fatal("history coverage lost")
		}
		week = 3
		missing, err := client.GetTeamRoster(ctx, &v1.GetTeamRosterRequest{TeamId: home.Id, ScoringPeriod: &week})
		if err != nil || missing.Snapshot != nil {
			t.Fatalf("invented missing roster: %v %v", missing, err)
		}
		knownEmpty, err := client.GetTeamRoster(ctx, &v1.GetTeamRosterRequest{TeamId: away.Id})
		if err != nil || knownEmpty.Snapshot == nil || len(knownEmpty.Snapshot.Entries) != 0 {
			t.Fatalf("lost empty roster: %v %v", knownEmpty, err)
		}
		matches, err := client.ListMatchups(ctx, &v1.ListMatchupsRequest{LeagueSeasonId: league.Id, MatchupPeriod: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(matches.Matchups) != 1 {
			t.Fatal("missing matchup")
		}
		match := matches.Matchups[0]
		if match.TeamA.Score == nil || *match.TeamA.Score != 0 || match.TeamB.Score != nil || len(match.ScoringPeriods.Values) != 2 {
			t.Fatalf("score/span lost: %v", match)
		}
	}
	// A guessed UUID never expands the configured owner's access.
	if _, err = db.Queries().CreateUser(ctx, "other@test"); err != nil {
		t.Fatal(err)
	}
	other := clientFor(t, db, "other@test")
	if _, err = other.GetLeague(ctx, &v1.GetLeagueRequest{LeagueSeasonId: first.Leagues[0].Id}); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("other owner access: %v", err)
	}
	if _, err = other.GetTeamRoster(ctx, &v1.GetTeamRosterRequest{TeamId: first.Leagues[0].SelectedTeamIds[0]}); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("other roster access: %v", err)
	}
	// Read transactions cannot accidentally write through a generated query.
	if err = db.InReadTx(ctx, func(q *store.Queries) error { _, e := q.CreateUser(ctx, "forbidden@test"); return e }); err == nil {
		t.Fatal("read transaction allowed mutation")
	}
	// Disable the selected connection; neither lists nor direct reads retain access.
	owner, err := db.Queries().GetUserByEmail(ctx, "owner@test")
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.Queries().FindProviderConnection(ctx, store.FindProviderConnectionParams{UserID: owner.ID, Provider: first.Leagues[0].Provider, ExternalUserID: pgtype.Text{String: "account", Valid: true}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Queries().UpdateConnectionForUser(ctx, store.UpdateConnectionForUserParams{ID: connection.ID, UserID: owner.ID, Status: "disabled"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.GetLeague(ctx, &v1.GetLeagueRequest{LeagueSeasonId: first.Leagues[0].Id}); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("disabled connection access: %v", err)
	}
	remaining, err := client.ListLeagues(ctx, &v1.ListLeaguesRequest{})
	if err != nil || len(remaining.Leagues) != 1 {
		t.Fatalf("disabled league listed: %v %v", remaining, err)
	}
}

func TestReadTransactionHasConsistentSnapshot(t *testing.T) {
	db := openDatabase(t)
	ctx := context.Background()
	if _, err := db.Queries().CreateUser(ctx, "initial@test"); err != nil {
		t.Fatal(err)
	}
	err := db.InReadTx(ctx, func(q *store.Queries) error {
		if _, err := q.GetUserByEmail(ctx, "initial@test"); err != nil {
			return err
		}
		if _, err := db.Queries().CreateUser(ctx, "concurrent@test"); err != nil {
			return err
		}
		_, err := q.GetUserByEmail(ctx, "concurrent@test")
		if err != pgx.ErrNoRows {
			t.Fatalf("read saw a concurrent commit: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Queries().GetUserByEmail(ctx, "concurrent@test"); err != nil {
		t.Fatal("concurrent write never committed")
	}
}

func TestUnsuccessfulSnapshotIsInvisible(t *testing.T) {
	db := openDatabase(t)
	ctx := context.Background()
	importLeague(t, db, "owner@test", fantasy.ProviderESPN)
	client := clientFor(t, db, "owner@test")
	leagues, err := client.ListLeagues(ctx, &v1.ListLeaguesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	league := leagues.Leagues[0]
	var leagueID, teamID pgtype.UUID
	if err = leagueID.Scan(league.Id); err != nil {
		t.Fatal(err)
	}
	if err = teamID.Scan(league.SelectedTeamIds[0]); err != nil {
		t.Fatal(err)
	}
	owner, err := db.Queries().GetUserByEmail(ctx, "owner@test")
	if err != nil {
		t.Fatal(err)
	}
	connection, err := db.Queries().FindProviderConnection(ctx, store.FindProviderConnectionParams{UserID: owner.ID, Provider: "espn", ExternalUserID: pgtype.Text{String: "account", Valid: true}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.Queries().StartSync(ctx, store.StartSyncParams{ConnectionID: connection.ID, LeagueSeasonID: leagueID, Kind: "refresh"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Queries().UpsertTeamSnapshot(ctx, store.UpsertTeamSnapshotParams{TeamID: teamID, LeagueSeasonID: leagueID, SyncRunID: run.ID, Period: 3, Origin: "observed", Coverage: "full", SourceKey: "incomplete", ObservedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, ContentHash: strings.Repeat("0", 64)})
	if err != nil {
		t.Fatal(err)
	}
	for _, failed := range []bool{false, true} {
		if failed {
			if _, err = db.Queries().FailSync(ctx, store.FailSyncParams{ID: run.ID, ErrorCode: pgtype.Text{String: "test", Valid: true}}); err != nil {
				t.Fatal(err)
			}
		}
		roster, err := client.GetTeamRoster(ctx, &v1.GetTeamRosterRequest{TeamId: teamID.String()})
		if err != nil {
			t.Fatal(err)
		}
		if roster.Snapshot == nil || roster.Snapshot.ScoringPeriod != 2 {
			t.Fatal("unsuccessful snapshot became current")
		}
		week := int32(3)
		unavailable, err := client.GetTeamRoster(ctx, &v1.GetTeamRosterRequest{TeamId: teamID.String(), ScoringPeriod: &week})
		if err != nil || unavailable.Snapshot != nil {
			t.Fatalf("unsuccessful history exposed: %v", err)
		}
	}
}

func TestOverviewSections(t *testing.T) {
	db := openDatabase(t)
	ctx := context.Background()
	client := clientFor(t, db, "owner@test")
	empty, err := client.GetOverviewMatchups(ctx, &v1.GetOverviewMatchupsRequest{})
	if err != nil || empty.Coverage.State != "empty" || len(empty.Matchups) != 0 {
		t.Fatalf("empty overview: %v %v", empty, err)
	}
	importLeague(t, db, "owner@test", fantasy.ProviderESPN)
	importLeague(t, db, "owner@test", fantasy.ProviderSleeper)
	matches, err := client.GetOverviewMatchups(ctx, &v1.GetOverviewMatchupsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches.Matchups) != 2 {
		t.Fatalf("expected selected team per league: %v", matches)
	}
	for _, m := range matches.Matchups {
		if m.Team.Name != "Home" || m.OpponentName != "Away" || m.Score == nil || *m.Score != 0 || m.OpponentScore != nil || m.GetMatchupPeriod() != 1 || m.Team.GetScoringPeriod() != 2 || m.FetchedAt == nil {
			t.Fatalf("score presence or multi-week mapping lost: %v", m)
		}
	}
	other := clientFor(t, db, "other@test")
	hidden, err := other.GetOverviewMatchups(ctx, &v1.GetOverviewMatchupsRequest{})
	if err != nil || len(hidden.Matchups) != 0 {
		t.Fatalf("owner isolation: %v %v", hidden, err)
	}
	attention, err := client.GetOverviewAttention(ctx, &v1.GetOverviewAttentionRequest{})
	if err != nil || attention.Coverage.State != "partial" || len(attention.Alerts) != 2 {
		t.Fatalf("attention: %v %v", attention, err)
	}
	newsPlayers, err := db.Queries().ListNewsPlayers(ctx, "owner@test")
	if err != nil || len(newsPlayers) == 0 {
		t.Fatalf("news players: %v %v", newsPlayers, err)
	}
	playerID := newsPlayers[0].PlayerID
	publishedAt := time.Now().UTC()
	for _, item := range []store.AddPlayerNewsParams{
		{
			Source: "espn", SourceItemID: "recent", PlayerID: playerID, Headline: "Placed on injured reserve",
			Summary: "The player was placed on IR.", Url: "https://www.espn.com/nfl/player/_/id/1",
			SourceLabel: "RotoWire via ESPN", PublishedAt: pgtype.Timestamptz{Time: publishedAt, Valid: true},
			FetchedAt: pgtype.Timestamptz{Time: publishedAt, Valid: true},
		},
		{
			Source: "espn", SourceItemID: "old", PlayerID: playerID, Headline: "Old story",
			Summary: "Outside the requested date range.", Url: "https://www.espn.com/nfl/player/_/id/1",
			SourceLabel: "RotoWire via ESPN", PublishedAt: pgtype.Timestamptz{Time: publishedAt.Add(-8 * 24 * time.Hour), Valid: true},
			FetchedAt: pgtype.Timestamptz{Time: publishedAt, Valid: true},
		},
	} {
		if err := db.Queries().AddPlayerNews(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	updates, err := client.GetOverviewPlayerUpdates(ctx, &v1.GetOverviewPlayerUpdatesRequest{})
	if err != nil || updates.Coverage.State != "partial" || len(updates.Updates) != 1 {
		t.Fatalf("player updates: %v %v", updates, err)
	}
	update := updates.Updates[0]
	if update.PlayerId != playerID.String() || update.Category != v1.NewsCategory_NEWS_CATEGORY_INJURY ||
		update.PlayerName != "Fixture Player" || len(update.Teams) != 1 || update.Teams[0].Name != "Home" ||
		update.Id != "espn:recent:"+playerID.String() {
		t.Fatalf("player update lost roster/news fields: %v", update)
	}
	hiddenUpdates, err := other.GetOverviewPlayerUpdates(ctx, &v1.GetOverviewPlayerUpdatesRequest{})
	if err != nil || len(hiddenUpdates.Updates) != 0 || hiddenUpdates.Coverage.State != "empty" {
		t.Fatalf("player news owner isolation: %v %v", hiddenUpdates, err)
	}
	ahead, err := client.GetOverviewLookingAhead(ctx, &v1.GetOverviewLookingAheadRequest{})
	if err != nil || ahead.Coverage.State != "unavailable" || len(ahead.Issues) != 0 {
		t.Fatalf("bye schedule falsely available: %v %v", ahead, err)
	}
}

func TestDismissLineupAlert(t *testing.T) {
	db := openDatabase(t)
	ctx := context.Background()
	importLeague(t, db, "owner@test", fantasy.ProviderESPN)
	importLeague(t, db, "owner@test", fantasy.ProviderSleeper)
	owner := clientFor(t, db, "owner@test")

	initial, err := owner.GetOverviewAttention(ctx, &v1.GetOverviewAttentionRequest{})
	if err != nil || len(initial.Alerts) != 2 {
		t.Fatalf("initial attention: %v %v", initial, err)
	}
	dismissedAlert := initial.Alerts[0]
	if dismissedAlert.Fingerprint == "" {
		t.Fatal("attention alert has no fingerprint")
	}

	ownerRow, err := db.Queries().GetUserByEmail(ctx, "owner@test")
	if err != nil {
		t.Fatal(err)
	}
	staleFingerprint := "stale:" + dismissedAlert.Fingerprint
	if err := db.Queries().DismissAlert(ctx, store.DismissAlertParams{UserID: ownerRow.ID, Fingerprint: staleFingerprint}); err != nil {
		t.Fatal(err)
	}
	currentDismissals, err := db.Queries().ListDismissedAlerts(ctx, store.ListDismissedAlertsParams{
		UserID: ownerRow.ID, Fingerprints: []string{dismissedAlert.Fingerprint},
	})
	if err != nil || len(currentDismissals) != 0 {
		t.Fatalf("stale dismissal matched current alert: %v %v", currentDismissals, err)
	}

	request := &v1.DismissLineupAlertRequest{Fingerprint: dismissedAlert.Fingerprint}
	for range 2 {
		if _, err := owner.DismissLineupAlert(ctx, request); err != nil {
			t.Fatalf("dismiss alert: %v", err)
		}
	}
	remaining, err := owner.GetOverviewAttention(ctx, &v1.GetOverviewAttentionRequest{})
	if err != nil || len(remaining.Alerts) != 1 || remaining.Alerts[0].Fingerprint == dismissedAlert.Fingerprint {
		t.Fatalf("dismissed alert remained visible: %v %v", remaining, err)
	}

	if _, err := db.Queries().CreateUser(ctx, "other@test"); err != nil {
		t.Fatal(err)
	}
	other := clientFor(t, db, "other@test")
	if _, err := other.DismissLineupAlert(ctx, request); err != nil {
		t.Fatalf("other owner dismiss: %v", err)
	}
	remaining, err = owner.GetOverviewAttention(ctx, &v1.GetOverviewAttentionRequest{})
	if err != nil || len(remaining.Alerts) != 1 {
		t.Fatalf("other owner's dismissal affected owner: %v %v", remaining, err)
	}

	if _, err := owner.RestoreLineupAlert(ctx, &v1.RestoreLineupAlertRequest{Fingerprint: dismissedAlert.Fingerprint}); err != nil {
		t.Fatalf("restore alert: %v", err)
	}
	restored, err := owner.GetOverviewAttention(ctx, &v1.GetOverviewAttentionRequest{})
	if err != nil || len(restored.Alerts) != 2 {
		t.Fatalf("restored attention: %v %v", restored, err)
	}

	if _, err := owner.DismissLineupAlert(ctx, &v1.DismissLineupAlertRequest{}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("empty dismiss fingerprint error = %v, want invalid argument", err)
	}
	if _, err := owner.RestoreLineupAlert(ctx, &v1.RestoreLineupAlertRequest{}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("empty restore fingerprint error = %v, want invalid argument", err)
	}
}

func TestOverviewMissingSnapshot(t *testing.T) {
	db := openDatabase(t)
	ctx := context.Background()
	league := fantasy.League{Ref: fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "no-snapshot", Season: 2026}, Name: "No snapshot", TeamCount: 1}
	data := fantasy.LeagueData{League: league, FetchedAt: time.Now(), Teams: []fantasy.Team{{ExternalID: "1", Name: "Only team"}}}
	service := importer.Service{DB: db, Source: source{data}}
	_, err := service.Import(ctx, importer.Request{OwnerEmail: "owner@test", ExternalAccountID: "missing", Selection: fantasy.ConnectionSelection{League: league.Ref, SelectedTeamIDs: []string{"1"}}})
	if err != nil {
		t.Fatal(err)
	}
	client := clientFor(t, db, "owner@test")
	result, err := client.GetOverviewMatchups(ctx, &v1.GetOverviewMatchupsRequest{})
	if err != nil || len(result.Matchups) != 1 || result.Matchups[0].Status != "unavailable" || result.Matchups[0].Score != nil {
		t.Fatalf("missing snapshot: %v %v", result, err)
	}
}

func TestMatchupDetail(t *testing.T) {
	db := openDatabase(t)
	ctx := context.Background()
	leagueSeasonID := importMatchupDetailFixture(t, db)
	now := time.Now().UTC()
	addMatchupDetailSchedule(t, db, now)

	client := clientFor(t, db, "matchup-owner@test")
	detail, err := client.GetMatchupDetail(ctx, &v1.GetMatchupDetailRequest{LeagueSeasonId: leagueSeasonID})
	if err != nil {
		t.Fatal(err)
	}
	assertDefaultMatchupDetail(t, detail.Matchup)

	period := int32(1)
	firstPeriod, err := client.GetMatchupDetail(ctx, &v1.GetMatchupDetailRequest{LeagueSeasonId: leagueSeasonID, MatchupPeriod: &period})
	if err != nil {
		t.Fatal(err)
	}
	assertMatchupWithoutSchedule(t, firstPeriod.Matchup)

	period = 3
	bye, err := client.GetMatchupDetail(ctx, &v1.GetMatchupDetailRequest{LeagueSeasonId: leagueSeasonID, MatchupPeriod: &period})
	if err != nil {
		t.Fatal(err)
	}
	assertByeMatchupDetail(t, bye.Matchup)

	period = 5
	missing, err := client.GetMatchupDetail(ctx, &v1.GetMatchupDetailRequest{LeagueSeasonId: leagueSeasonID, MatchupPeriod: &period})
	if err != nil || missing.Matchup != nil {
		t.Fatalf("missing round response = %v, error = %v", missing, err)
	}

	assertMatchupDetailAccessErrors(t, db, client, leagueSeasonID)
}

func addMatchupDetailSchedule(t *testing.T, db *database.DB, now time.Time) {
	t.Helper()
	for _, game := range []store.AddNFLGameParams{
		{
			Season: 2026, Week: 2, Team: "KC", Opponent: "BUF",
			KickoffAt: pgtype.Timestamptz{Time: now.Add(48 * time.Hour), Valid: true},
			FetchedAt: pgtype.Timestamptz{Time: now, Valid: true},
		},
		{
			Season: 2026, Week: 2, Team: "BUF", Opponent: "KC",
			KickoffAt: pgtype.Timestamptz{Time: now.Add(-48 * time.Hour), Valid: true},
			FetchedAt: pgtype.Timestamptz{Time: now, Valid: true},
		},
	} {
		if err := db.Queries().AddNFLGame(context.Background(), game); err != nil {
			t.Fatal(err)
		}
	}
}

func assertMatchupDetailAccessErrors(t *testing.T, db *database.DB, client leaguelensv1connect.DashboardServiceClient, leagueSeasonID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.Queries().CreateUser(ctx, "matchup-other@test"); err != nil {
		t.Fatal(err)
	}
	other := clientFor(t, db, "matchup-other@test")
	if _, err := other.GetMatchupDetail(ctx, &v1.GetMatchupDetailRequest{LeagueSeasonId: leagueSeasonID}); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("other owner access error = %v, want not found", err)
	}
	if _, err := client.GetMatchupDetail(ctx, &v1.GetMatchupDetailRequest{LeagueSeasonId: "bad-id"}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("invalid league ID error = %v, want invalid argument", err)
	}
}

func importMatchupDetailFixture(t *testing.T, db *database.DB) string {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	zero, eightAndHalf, twelveAndHalf := 0.0, 8.5, 12.5
	league := fantasy.League{
		Ref:       fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "matchup-detail", Season: 2026},
		Name:      "Matchup Detail",
		TeamCount: 2,
	}
	data := fantasy.LeagueData{
		League:    league,
		FetchedAt: now,
		Teams: []fantasy.Team{
			{ExternalID: "1", Name: "Home", Record: &fantasy.TeamRecord{Wins: 8, Losses: 2, PointsFor: &eightAndHalf}},
			{ExternalID: "2", Name: "Away"},
		},
		Players: []fantasy.Player{
			{ExternalID: "qb", Name: "Quarterback", Kind: fantasy.PlayerPerson},
			{ExternalID: "rb", Name: "Running Back", Kind: fantasy.PlayerPerson},
			{ExternalID: "wr", Name: "Dropped Receiver", Kind: fantasy.PlayerPerson},
			{ExternalID: "flex", Name: "Bye Week Starter", Kind: fantasy.PlayerPerson},
			{ExternalID: "bench", Name: "Bench Player", Kind: fantasy.PlayerPerson},
			{ExternalID: "ir", Name: "IR Player", Kind: fantasy.PlayerPerson},
		},
		Rosters: []fantasy.RosterSnapshot{
			{
				TeamID: "1", ScoringPeriod: 2, Origin: fantasy.SnapshotObserved, Coverage: fantasy.CoverageFull, ObservedAt: now,
				Entries: []fantasy.RosterEntry{
					{PlayerID: "qb", Slot: fantasy.SlotQB, InjuryStatus: fantasy.InjuryQuestionable, ProTeam: "KC"},
					{PlayerID: "rb", Slot: fantasy.SlotRB, InjuryStatus: fantasy.InjuryActive, ProTeam: "BUF"},
					{PlayerID: "flex", Slot: fantasy.SlotFlex, InjuryStatus: fantasy.InjuryActive, ProTeam: "NYJ"},
					{PlayerID: "bench", Slot: fantasy.SlotBench},
					{PlayerID: "ir", Slot: fantasy.SlotIR},
				},
			},
			{TeamID: "2", ScoringPeriod: 2, Origin: fantasy.SnapshotObserved, Coverage: fantasy.CoverageFull, ObservedAt: now},
		},
		Matchups: []fantasy.Matchup{
			{
				ExternalID: "period-2", Period: 2, ScoringPeriods: []fantasy.ScoringPeriod{2},
				TeamA: fantasy.MatchupSide{TeamID: "2", Score: &twelveAndHalf},
				TeamB: &fantasy.MatchupSide{TeamID: "1", Score: &zero}, Status: fantasy.MatchupInProgress,
			},
			{
				ExternalID: "period-1", Period: 1, ScoringPeriods: []fantasy.ScoringPeriod{1},
				TeamA: fantasy.MatchupSide{TeamID: "1"}, TeamB: &fantasy.MatchupSide{TeamID: "2"}, Status: fantasy.MatchupScheduled,
			},
			{
				ExternalID: "period-3", Period: 3, ScoringPeriods: []fantasy.ScoringPeriod{3},
				TeamA: fantasy.MatchupSide{TeamID: "1"}, IsBye: true, Status: fantasy.MatchupScheduled,
			},
		},
		Lineups: []fantasy.MatchupLineup{
			{
				Period: 2, MatchupID: "period-2", TeamID: "1",
				Entries: []fantasy.LineupEntry{
					{PlayerID: "qb", Slot: fantasy.SlotQB, Starter: true, Points: &zero},
					{PlayerID: "rb", Slot: fantasy.SlotRB, Starter: true, Points: &eightAndHalf},
					{PlayerID: "wr", Slot: fantasy.SlotWR, Starter: true},
					{PlayerID: "flex", Slot: fantasy.SlotFlex, Starter: true},
					{PlayerID: "bench", Slot: fantasy.SlotBench},
					{PlayerID: "ir", Slot: fantasy.SlotIR, Points: &zero},
				},
			},
			{
				Period: 1, MatchupID: "period-1", TeamID: "1",
				Entries: []fantasy.LineupEntry{{PlayerID: "qb", Slot: fantasy.SlotQB, Starter: true}},
			},
		},
		CompleteMatchupPeriods: []fantasy.MatchupPeriod{1, 2, 3},
	}
	service := importer.Service{DB: db, Source: source{data}}
	if _, err := service.Import(ctx, importer.Request{
		OwnerEmail: "matchup-owner@test", ExternalAccountID: "matchup-detail-account",
		Selection: fantasy.ConnectionSelection{League: league.Ref, SelectedTeamIDs: []string{"1"}},
	}); err != nil {
		t.Fatal(err)
	}
	client := clientFor(t, db, "matchup-owner@test")
	leagues, err := client.ListLeagues(ctx, &v1.ListLeaguesRequest{})
	if err != nil || len(leagues.Leagues) != 1 {
		t.Fatalf("fixture league lookup = %v, error = %v", leagues, err)
	}
	return leagues.Leagues[0].Id
}

func assertDefaultMatchupDetail(t *testing.T, detail *v1.MatchupDetail) {
	t.Helper()
	if detail == nil {
		t.Fatal("default matchup is absent")
	}
	if detail.MatchupPeriod != 2 || detail.Status != v1.MatchupStatus_MATCHUP_STATUS_IN_PROGRESS ||
		detail.IsBye || detail.Id == "" || detail.FetchedAt == nil {
		t.Fatalf("matchup fields = %v", detail)
	}
	if detail.Team == nil || detail.Team.TeamName != "Home" || detail.Team.Score == nil || *detail.Team.Score != 0 ||
		detail.Team.Record == nil || detail.Team.Record.Wins != 8 || detail.Team.Record.PointsFor == nil ||
		*detail.Team.Record.PointsFor != 8.5 {
		t.Fatalf("owner orientation or record lost: %v", detail.Team)
	}
	if detail.Opponent == nil || detail.Opponent.TeamName != "Away" || detail.Opponent.Score == nil ||
		*detail.Opponent.Score != 12.5 || detail.Opponent.Record != nil {
		t.Fatalf("opponent orientation or record lost: %v", detail.Opponent)
	}
	assertHomeMatchupLineup(t, detail.Team)
	if len(detail.Opponent.Starters) != 0 || len(detail.Opponent.Bench) != 0 {
		t.Fatalf("missing opponent lineup was invented: %v", detail.Opponent)
	}
}

func assertHomeMatchupLineup(t *testing.T, team *v1.MatchupLineup) {
	t.Helper()
	if team.StartersLeftToPlay != 1 {
		t.Fatalf("starters left to play = %d, want 1", team.StartersLeftToPlay)
	}
	if len(team.Starters) != 4 {
		t.Fatalf("starter count = %d, want 4", len(team.Starters))
	}
	assertLineupSlots(t, team.Starters, []string{"QB", "RB", "WR", "FLEX"})
	assertLineupPlayer(t, team.Starters[0], "Quarterback", "KC", v1.InjuryStatus_INJURY_STATUS_QUESTIONABLE, v1.GameState_GAME_STATE_NOT_STARTED, true, 0)
	assertLineupPlayer(t, team.Starters[1], "Running Back", "BUF", v1.InjuryStatus_INJURY_STATUS_ACTIVE, v1.GameState_GAME_STATE_KICKED_OFF, true, 8.5)
	if player := team.Starters[2]; player.PlayerName != "Dropped Receiver" || player.PlayerId == "" || player.ProTeam != "" ||
		player.InjuryStatus != v1.InjuryStatus_INJURY_STATUS_UNSPECIFIED || player.GameState != v1.GameState_GAME_STATE_UNKNOWN ||
		player.Points != nil || player.KickoffAt != nil {
		t.Fatalf("dropped player current metadata or nil score lost: %v", player)
	}
	if player := team.Starters[3]; player.PlayerName != "Bye Week Starter" || player.GameState != v1.GameState_GAME_STATE_BYE {
		t.Fatalf("no-game starter state = %v, want bye", player)
	}
	if team.Starters[0].KickoffAt == nil || !team.Starters[0].KickoffAt.AsTime().After(time.Now()) {
		t.Fatalf("future kickoff missing: %v", team.Starters[0].KickoffAt)
	}
	if len(team.Bench) != 2 {
		t.Fatalf("bench count = %d, want 2", len(team.Bench))
	}
	assertLineupSlots(t, team.Bench, []string{"BENCH", "IR"})
	if team.Bench[0].PlayerName != "Bench Player" || team.Bench[0].Points != nil {
		t.Fatalf("nil bench points lost: %v", team.Bench[0])
	}
	if team.Bench[1].PlayerName != "IR Player" || team.Bench[1].Points == nil || *team.Bench[1].Points != 0 {
		t.Fatalf("zero IR points lost: %v", team.Bench[1])
	}
}

func assertMatchupWithoutSchedule(t *testing.T, detail *v1.MatchupDetail) {
	t.Helper()
	if detail == nil || detail.MatchupPeriod != 1 || detail.Team == nil || len(detail.Team.Starters) != 1 {
		t.Fatalf("period one matchup missing: %v", detail)
	}
	player := detail.Team.Starters[0]
	if player.GameState != v1.GameState_GAME_STATE_UNKNOWN || player.KickoffAt != nil {
		t.Fatalf("missing schedule state = %v, kickoff = %v", player.GameState, player.KickoffAt)
	}
	if detail.Team.Score != nil || detail.Opponent == nil || detail.Opponent.Score != nil {
		t.Fatalf("unknown scores became present: %v", detail)
	}
}

func assertByeMatchupDetail(t *testing.T, detail *v1.MatchupDetail) {
	t.Helper()
	if detail == nil || !detail.IsBye || detail.Opponent != nil || detail.Team == nil || detail.Team.TeamName != "Home" {
		t.Fatalf("bye matchup detail = %v", detail)
	}
}

func assertLineupSlots(t *testing.T, players []*v1.LineupPlayer, want []string) {
	t.Helper()
	if len(players) != len(want) {
		t.Fatalf("lineup player count = %d, want %d", len(players), len(want))
	}
	for index, player := range players {
		if player.Slot != want[index] {
			t.Fatalf("lineup slot[%d] = %q, want %q", index, player.Slot, want[index])
		}
	}
}

func assertLineupPlayer(t *testing.T, player *v1.LineupPlayer, name, proTeam string, injury v1.InjuryStatus, game v1.GameState, hasPoints bool, points float64) {
	t.Helper()
	if player.PlayerId == "" || player.PlayerName != name || player.ProTeam != proTeam ||
		player.InjuryStatus != injury || player.GameState != game {
		t.Fatalf("lineup player fields lost: %v", player)
	}
	if (player.Points != nil) != hasPoints || (hasPoints && *player.Points != points) {
		t.Fatalf("lineup player points = %v, want present %t value %v", player.Points, hasPoints, points)
	}
}
