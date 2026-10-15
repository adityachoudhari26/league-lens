//go:build integration

package connections

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	migrations "github.com/adityachoudhari26/league-lens/db"
	"github.com/adityachoudhari26/league-lens/internal/dashboard"
	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/espn"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/leaguesync"
	"github.com/adityachoudhari26/league-lens/internal/secrets"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

type connectionTestProvider struct {
	league      fantasy.League
	data        fantasy.LeagueData
	describeErr error
	fetchErr    error
}

func (p connectionTestProvider) DescribeLeague(context.Context, string) (fantasy.League, error) {
	return p.league, p.describeErr
}

func (p connectionTestProvider) FetchLeague(context.Context, fantasy.League) (fantasy.LeagueData, error) {
	return p.data, p.fetchErr
}

func openConnectionTestDatabase(t *testing.T) *database.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL required")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test database URL")
	}
	admin := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { _ = admin.Close() })
	schema := fmt.Sprintf("connections_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE") })
	cfg.RuntimeParams["search_path"] = schema
	migrationDB := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { _ = migrationDB.Close() })
	if err = migrations.Migrate(context.Background(), migrationDB, "up"); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid test database URL")
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := database.Open(context.Background(), parsed.String())
	if err != nil {
		t.Fatal("could not open test database")
	}
	t.Cleanup(db.Close)
	return db
}

func TestConnectionImportDetachAndReattach(t *testing.T) {
	ctx := context.Background()
	db := openConnectionTestDatabase(t)
	key := bytes.Repeat([]byte{3}, 32)
	box, err := secrets.NewBox(key)
	if err != nil {
		t.Fatal(err)
	}
	const s2 = "synthetic-espn-cookie"
	const swid = "{synthetic-espn-id}"
	season := fantasy.CurrentSeason(time.Now())
	ref := fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "league-123", Season: season}
	league := fantasy.League{
		Ref: ref, Name: "Synthetic League", TeamCount: 2,
		Settings: fantasy.LeagueSettings{RosterSlots: []fantasy.SlotDefinition{{Slot: fantasy.SlotQB, Count: 1}}},
	}
	now := time.Now().UTC()
	zero := 0.0
	data := fantasy.LeagueData{
		League: league, FetchedAt: now,
		Teams:   []fantasy.Team{{ExternalID: "1", Name: "Home"}, {ExternalID: "2", Name: "Away"}},
		Players: []fantasy.Player{{ExternalID: "synthetic-player", Name: "Fixture Player", Kind: fantasy.PlayerPerson}},
		Rosters: []fantasy.RosterSnapshot{
			{TeamID: "1", ScoringPeriod: 1, Origin: fantasy.SnapshotObserved, Coverage: fantasy.CoverageFull, ObservedAt: now, Entries: []fantasy.RosterEntry{{PlayerID: "synthetic-player", Slot: fantasy.SlotQB}}},
			{TeamID: "2", ScoringPeriod: 1, Origin: fantasy.SnapshotObserved, Coverage: fantasy.CoverageFull, ObservedAt: now},
		},
		Matchups: []fantasy.Matchup{{
			ExternalID: "matchup-1", Period: 1, ScoringPeriods: []fantasy.ScoringPeriod{1},
			TeamA: fantasy.MatchupSide{TeamID: "1", Score: &zero},
			TeamB: &fantasy.MatchupSide{TeamID: "2", Score: &zero}, Status: fantasy.MatchupFinal,
		}},
		CompleteMatchupPeriods: []fantasy.MatchupPeriod{1},
	}
	credentials := espn.Credentials{S2: s2, SWID: swid}
	accountID := espn.NewClient(nil, credentials, season).AccountID()
	service := &Service{
		DB: db, Box: box, OwnerEmail: "connect@example.test",
		DiscoverESPN: func(context.Context, espn.Credentials) ([]fantasy.DiscoveredLeague, error) {
			return []fantasy.DiscoveredLeague{{Ref: ref, Name: league.Name, TeamID: "1", TeamName: "Home"}}, nil
		},
		NewSource: func(_ context.Context, spec ImportSpec) (fantasy.Provider, string, fantasy.ConnectionSelection, error) {
			selection := fantasy.ConnectionSelection{League: ref, SelectedTeamIDs: []string{fmt.Sprint(spec.TeamID)}}
			return connectionTestProvider{league: league, data: data}, accountID, selection, nil
		},
	}

	connected, err := service.ConnectESPN(ctx, &v1.ConnectESPNRequest{S2: s2, Swid: swid})
	if err != nil {
		t.Fatal(err)
	}
	var owner store.User
	owner, err = db.Queries().GetUserByEmail(ctx, service.OwnerEmail)
	if err != nil {
		t.Fatal(err)
	}
	connectionID, ok := parseUUID(connected.ConnectionId)
	if !ok {
		t.Fatal("connection ID was invalid")
	}
	connection, err := db.Queries().GetConnectionForUser(ctx, store.GetConnectionForUserParams{ID: connectionID, UserID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(connection.EncryptedCredentials) == 0 {
		t.Fatal("encrypted credentials were not stored")
	}
	if bytes.Contains(connection.EncryptedCredentials, []byte(s2)) {
		t.Fatal("stored credentials contain the synthetic cookie")
	}
	if !connection.ID.Valid || !connection.UserID.Valid || connection.Status != "active" {
		t.Fatal("connection was not activated")
	}

	imported, err := service.ImportLeagues(ctx, &v1.ImportLeaguesRequest{
		ConnectionId: connected.ConnectionId,
		Picks:        []*v1.ImportPick{{ExternalLeagueId: ref.ExternalID, TeamId: "1"}},
	})
	if err != nil || len(imported.Results) != 1 || !imported.Results[0].Succeeded {
		t.Fatalf("import result: %+v, %v", imported, err)
	}
	rows, err := db.Queries().ListSyncTargets(ctx, owner.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("sync targets = %d, %v", len(rows), err)
	}
	if !rows[0].ConnectionID.Valid || rows[0].ConnectionID.Bytes != connection.ID.Bytes {
		t.Fatal("sync target returned a different connection")
	}
	if len(rows[0].EncryptedCredentials) == 0 {
		t.Fatal("sync target omitted encrypted credentials")
	}
	target := leaguesync.Target{
		ConnectionID: rows[0].ConnectionID, EncryptedCredentials: rows[0].EncryptedCredentials,
		ExternalAccountID: rows[0].ExternalUserID.String,
		Selection: fantasy.ConnectionSelection{
			League:          fantasy.LeagueRef{Provider: fantasy.ProviderID(rows[0].Provider), ExternalID: rows[0].ExternalLeagueID, Season: int(rows[0].Season)},
			SelectedTeamIDs: rows[0].SelectedTeamIds,
		},
	}
	if _, err = (leaguesync.ProviderSources{Box: box}).SourceFor(target); err != nil {
		t.Fatalf("stored credentials were not usable for sync: %v", err)
	}

	dashboardService := dashboard.New(db, service.OwnerEmail, nil)
	visible, err := dashboardService.ListLeagues(ctx, &v1.ListLeaguesRequest{})
	if err != nil || len(visible.Leagues) != 1 {
		t.Fatalf("league before detach: %v, %v", visible, err)
	}
	removed, err := service.RemoveLeague(ctx, &v1.RemoveLeagueRequest{LeagueId: imported.Results[0].LeagueId})
	if err != nil || !removed.Removed {
		t.Fatalf("remove result: %v, %v", removed, err)
	}
	visible, err = dashboardService.ListLeagues(ctx, &v1.ListLeaguesRequest{})
	if err != nil || len(visible.Leagues) != 0 {
		t.Fatalf("detached league remained visible: %v, %v", visible, err)
	}
	rows, err = db.Queries().ListSyncTargets(ctx, owner.ID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("detached sync targets = %d, %v", len(rows), err)
	}
	if _, err = service.ImportLeagues(ctx, &v1.ImportLeaguesRequest{
		ConnectionId: connected.ConnectionId,
		Picks:        []*v1.ImportPick{{ExternalLeagueId: ref.ExternalID, TeamId: "1"}},
	}); err != nil {
		t.Fatal(err)
	}
	visible, err = dashboardService.ListLeagues(ctx, &v1.ListLeaguesRequest{})
	if err != nil || len(visible.Leagues) != 1 {
		t.Fatalf("re-import did not reattach league: %v, %v", visible, err)
	}
	rows, err = db.Queries().ListSyncTargets(ctx, owner.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("re-attached sync targets = %d, %v", len(rows), err)
	}
}

func TestConnectESPNWithoutFootballLeaguesDoesNotCreateConnection(t *testing.T) {
	ctx := context.Background()
	db := openConnectionTestDatabase(t)
	box, err := secrets.NewBox(bytes.Repeat([]byte{4}, 32))
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{
		DB: db, Box: box, OwnerEmail: "empty-espn@example.test",
		DiscoverESPN: func(context.Context, espn.Credentials) ([]fantasy.DiscoveredLeague, error) {
			return nil, nil
		},
	}
	_, err = service.ConnectESPN(ctx, &v1.ConnectESPNRequest{S2: "synthetic-s2", Swid: "{synthetic-swid}"})
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("ConnectESPN error = %v, want failed precondition", err)
	}
	if !strings.Contains(err.Error(), "No ESPN football leagues found for this account") {
		t.Fatalf("ConnectESPN error = %v, want no-football-leagues message", err)
	}
	listed, err := service.ListConnections(ctx, &v1.ListConnectionsRequest{})
	if err != nil || len(listed.GetConnections()) != 0 {
		t.Fatalf("connections after empty discovery = %v, %v", listed, err)
	}
}

func TestImportUnauthorizedMarksConnectionForReconnect(t *testing.T) {
	for _, failSource := range []bool{true, false} {
		name := "import"
		if failSource {
			name = "source setup"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db := openConnectionTestDatabase(t)
			box, err := secrets.NewBox(bytes.Repeat([]byte{5}, 32))
			if err != nil {
				t.Fatal(err)
			}
			season := fantasy.CurrentSeason(time.Now())
			ref := fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "unauthorized-league", Season: season}
			league := fantasy.League{Ref: ref, Name: "Synthetic League", TeamCount: 2}
			credentials := espn.Credentials{S2: "synthetic-s2", SWID: "{synthetic-swid}"}
			accountID := espn.NewClient(nil, credentials, season).AccountID()
			service := &Service{
				DB: db, Box: box, OwnerEmail: "unauthorized@example.test",
				DiscoverESPN: func(context.Context, espn.Credentials) ([]fantasy.DiscoveredLeague, error) {
					return []fantasy.DiscoveredLeague{{Ref: ref, TeamID: "1"}}, nil
				},
				NewSource: func(_ context.Context, spec ImportSpec) (fantasy.Provider, string, fantasy.ConnectionSelection, error) {
					selection := fantasy.ConnectionSelection{League: ref, SelectedTeamIDs: []string{fmt.Sprint(spec.TeamID)}}
					if failSource {
						return nil, "", selection, espn.ErrUnauthorized
					}
					return connectionTestProvider{league: league, describeErr: espn.ErrUnauthorized}, accountID, selection, nil
				},
			}
			connected, err := service.ConnectESPN(ctx, &v1.ConnectESPNRequest{S2: credentials.S2, Swid: credentials.SWID})
			if err != nil {
				t.Fatal(err)
			}
			imported, err := service.ImportLeagues(ctx, &v1.ImportLeaguesRequest{
				ConnectionId: connected.ConnectionId,
				Picks:        []*v1.ImportPick{{ExternalLeagueId: ref.ExternalID, TeamId: "1"}},
			})
			if err != nil || len(imported.GetResults()) != 1 || imported.Results[0].ErrorCode != "needs_reconnect" {
				t.Fatalf("import result = %v, %v", imported, err)
			}
			connectionID, ok := parseUUID(connected.ConnectionId)
			if !ok {
				t.Fatal("connection ID was invalid")
			}
			owner, err := db.Queries().GetUserByEmail(ctx, service.OwnerEmail)
			if err != nil {
				t.Fatal(err)
			}
			connection, err := db.Queries().GetConnectionForUser(ctx, store.GetConnectionForUserParams{
				ID: connectionID, UserID: owner.ID,
			})
			if err != nil || connection.Status != "needs_reconnect" {
				t.Fatalf("connection status = %q, %v", connection.Status, err)
			}
		})
	}
}
