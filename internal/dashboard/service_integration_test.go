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
	updates, err := client.GetOverviewPlayerUpdates(ctx, &v1.GetOverviewPlayerUpdatesRequest{})
	if err != nil || updates.Coverage.State != "unavailable" || len(updates.Updates) != 0 {
		t.Fatalf("news falsely available: %v %v", updates, err)
	}
	ahead, err := client.GetOverviewLookingAhead(ctx, &v1.GetOverviewLookingAheadRequest{})
	if err != nil || ahead.Coverage.State != "unavailable" || len(ahead.Issues) != 0 {
		t.Fatalf("bye schedule falsely available: %v %v", ahead, err)
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
