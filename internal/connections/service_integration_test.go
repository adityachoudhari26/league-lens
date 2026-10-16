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
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/leaguesync"
	"github.com/adityachoudhari26/league-lens/internal/secrets"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

type fakeImportStarter struct {
	jobs []leaguesync.ImportJob
}

func (f *fakeImportStarter) StartImports(jobs []leaguesync.ImportJob) {
	f.jobs = append(f.jobs, jobs...)
}

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
		Teams: []fantasy.Team{{ExternalID: "1", Name: "Home"}, {ExternalID: "2", Name: "Away"}},
		Players: []fantasy.Player{
			{ExternalID: "synthetic-player", Name: "Fixture Player", Kind: fantasy.PlayerPerson},
			{ExternalID: "opponent-player", Name: "Opponent Fixture Player", Kind: fantasy.PlayerPerson},
		},
		Rosters: []fantasy.RosterSnapshot{
			{TeamID: "1", ScoringPeriod: 1, Origin: fantasy.SnapshotObserved, Coverage: fantasy.CoverageFull, ObservedAt: now, Entries: []fantasy.RosterEntry{{
				PlayerID: "synthetic-player", Slot: fantasy.SlotQB, ProTeam: "NE", InjuryStatus: fantasy.InjuryOut,
			}}},
			{TeamID: "2", ScoringPeriod: 1, Origin: fantasy.SnapshotObserved, Coverage: fantasy.CoverageFull, ObservedAt: now, Entries: []fantasy.RosterEntry{{
				PlayerID: "opponent-player", Slot: fantasy.SlotRB, ProTeam: "BUF", InjuryStatus: fantasy.InjuryQuestionable,
			}}},
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
	starter := &fakeImportStarter{}
	service := &Service{
		DB: db, Box: box, OwnerEmail: "connect@example.test",
		Imports: starter,
		DiscoverESPN: func(context.Context, espn.Credentials) ([]fantasy.DiscoveredLeague, error) {
			return []fantasy.DiscoveredLeague{{Ref: ref, Name: league.Name, TeamID: "1", TeamName: "Home"}}, nil
		},
		NewSource: func(_ context.Context, spec ImportSpec) (PreparedSource, error) {
			selection := fantasy.ConnectionSelection{League: ref, SelectedTeamIDs: []string{fmt.Sprint(spec.TeamID)}}
			return PreparedSource{
				Provider: connectionTestProvider{league: league, data: data}, AccountID: accountID,
				Selection: selection, League: league,
			}, nil
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
	if err != nil || len(imported.Results) != 1 || !imported.Results[0].Started {
		t.Fatalf("import result: %+v, %v", imported, err)
	}
	if imported.Results[0].LeagueId == "" {
		t.Fatal("started import returned no league ID")
	}
	if len(starter.jobs) != 1 || starter.jobs[0].Target.ID != imported.Results[0].LeagueId {
		t.Fatalf("recorded import jobs = %+v", starter.jobs)
	}
	// Finish the begun import so the saved-data checks below still hold.
	job := starter.jobs[0]
	if _, err = (&importer.Service{DB: db, Source: job.Source}).Finish(ctx, job.Started); err != nil {
		t.Fatal(err)
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

	dashboardService := dashboard.New(db, service.OwnerEmail, nil, nil, nil)
	visible, err := dashboardService.ListLeagues(ctx, &v1.ListLeaguesRequest{})
	if err != nil || len(visible.Leagues) != 1 {
		t.Fatalf("league before detach: %v, %v", visible, err)
	}
	matchups, err := dashboardService.GetOverviewMatchups(ctx, &v1.GetOverviewMatchupsRequest{})
	if err != nil || len(matchups.Matchups) != 1 {
		t.Fatalf("overview before detach: %v, %v", matchups, err)
	}
	newsPlayers, err := db.Queries().ListNewsPlayers(ctx, service.OwnerEmail)
	if err != nil || len(newsPlayers) != 1 {
		t.Fatalf("news players before detach: %v, %v", newsPlayers, err)
	}
	player, err := db.Queries().FindPlayerByExternalID(ctx, store.FindPlayerByExternalIDParams{
		Provider: string(fantasy.ProviderESPN), ExternalID: "opponent-player",
	})
	if err != nil {
		t.Fatal(err)
	}
	cardPlayer, err := db.Queries().PlayerCardPlayer(ctx, store.PlayerCardPlayerParams{
		ID: player.ID, UserID: owner.ID,
	})
	if err != nil || cardPlayer.ProTeam != "BUF" || cardPlayer.InjuryStatus != string(fantasy.InjuryQuestionable) {
		t.Fatalf("player card before detach: %+v, %v", cardPlayer, err)
	}
	removed, err := service.RemoveLeague(ctx, &v1.RemoveLeagueRequest{LeagueId: imported.Results[0].LeagueId})
	if err != nil || !removed.Removed {
		t.Fatalf("remove result: %v, %v", removed, err)
	}
	visible, err = dashboardService.ListLeagues(ctx, &v1.ListLeaguesRequest{})
	if err != nil || len(visible.Leagues) != 0 {
		t.Fatalf("detached league remained visible: %v, %v", visible, err)
	}
	matchups, err = dashboardService.GetOverviewMatchups(ctx, &v1.GetOverviewMatchupsRequest{})
	if err != nil || len(matchups.Matchups) != 0 {
		t.Fatalf("detached league remained in overview matchups: %v, %v", matchups, err)
	}
	attention, err := dashboardService.GetOverviewAttention(ctx, &v1.GetOverviewAttentionRequest{})
	if err != nil || attention.Coverage.State != "empty" || len(attention.Alerts) != 0 {
		t.Fatalf("detached league remained in overview attention: %v, %v", attention, err)
	}
	updates, err := dashboardService.GetOverviewPlayerUpdates(ctx, &v1.GetOverviewPlayerUpdatesRequest{})
	if err != nil || updates.Coverage.State != "empty" || len(updates.Updates) != 0 {
		t.Fatalf("detached league remained in player updates: %v, %v", updates, err)
	}
	newsPlayers, err = db.Queries().ListNewsPlayers(ctx, service.OwnerEmail)
	if err != nil || len(newsPlayers) != 0 {
		t.Fatalf("detached league remained in news player lookup: %v, %v", newsPlayers, err)
	}
	cardPlayer, err = db.Queries().PlayerCardPlayer(ctx, store.PlayerCardPlayerParams{
		ID: player.ID, UserID: owner.ID,
	})
	if err != nil || cardPlayer.ProTeam != "" || cardPlayer.InjuryStatus != "" {
		t.Fatalf("detached league data remained in player card: %+v, %v", cardPlayer, err)
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
	matchups, err = dashboardService.GetOverviewMatchups(ctx, &v1.GetOverviewMatchupsRequest{})
	if err != nil || len(matchups.Matchups) != 1 {
		t.Fatalf("re-import did not restore overview: %v, %v", matchups, err)
	}
	newsPlayers, err = db.Queries().ListNewsPlayers(ctx, service.OwnerEmail)
	if err != nil || len(newsPlayers) != 1 {
		t.Fatalf("re-import did not restore news players: %v, %v", newsPlayers, err)
	}
	cardPlayer, err = db.Queries().PlayerCardPlayer(ctx, store.PlayerCardPlayerParams{
		ID: player.ID, UserID: owner.ID,
	})
	if err != nil || cardPlayer.ProTeam != "BUF" || cardPlayer.InjuryStatus != string(fantasy.InjuryQuestionable) {
		t.Fatalf("re-import did not restore player card data: %+v, %v", cardPlayer, err)
	}
	rows, err = db.Queries().ListSyncTargets(ctx, owner.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("re-attached sync targets = %d, %v", len(rows), err)
	}
}

func TestImportLeaguesMixedPicksStartsOnlyValidJobs(t *testing.T) {
	ctx := context.Background()
	db := openConnectionTestDatabase(t)
	box, err := secrets.NewBox(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	season := fantasy.CurrentSeason(time.Now())
	ref := fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "league-123", Season: season}
	league := fantasy.League{Ref: ref, Name: "Synthetic League", TeamCount: 2}
	now := time.Now().UTC()
	data := fantasy.LeagueData{
		League: league, FetchedAt: now,
		Teams: []fantasy.Team{{ExternalID: "1", Name: "Home"}, {ExternalID: "2", Name: "Away"}},
	}
	credentials := espn.Credentials{S2: "synthetic-s2", SWID: "{synthetic-swid}"}
	accountID := espn.NewClient(nil, credentials, season).AccountID()
	starter := &fakeImportStarter{}
	service := &Service{
		DB: db, Box: box, OwnerEmail: "mixed@example.test",
		Imports: starter,
		DiscoverESPN: func(context.Context, espn.Credentials) ([]fantasy.DiscoveredLeague, error) {
			return []fantasy.DiscoveredLeague{{Ref: ref, TeamID: "1"}}, nil
		},
		NewSource: func(_ context.Context, spec ImportSpec) (PreparedSource, error) {
			selection := fantasy.ConnectionSelection{League: ref, SelectedTeamIDs: []string{fmt.Sprint(spec.TeamID)}}
			if spec.LeagueID == "other-league" {
				return PreparedSource{Provider: connectionTestProvider{league: league, data: data}, AccountID: "different-account", Selection: selection, League: league}, nil
			}
			return PreparedSource{
				Provider: connectionTestProvider{league: league, data: data}, AccountID: accountID,
				Selection: selection, League: league,
			}, nil
		},
	}
	connected, err := service.ConnectESPN(ctx, &v1.ConnectESPNRequest{S2: credentials.S2, Swid: credentials.SWID})
	if err != nil {
		t.Fatal(err)
	}

	imported, err := service.ImportLeagues(ctx, &v1.ImportLeaguesRequest{
		ConnectionId: connected.ConnectionId,
		Picks: []*v1.ImportPick{
			{ExternalLeagueId: "", TeamId: "1"},
			{ExternalLeagueId: ref.ExternalID, TeamId: "abc"},
			{ExternalLeagueId: "other-league", TeamId: "1"},
			{ExternalLeagueId: ref.ExternalID, TeamId: "1"},
		},
	})
	if err != nil || len(imported.GetResults()) != 4 {
		t.Fatalf("import results = %+v, %v", imported, err)
	}
	wantCodes := []string{"invalid_selection", "invalid_selection", "account_mismatch", ""}
	for index, want := range wantCodes {
		if got := imported.Results[index].ErrorCode; got != want {
			t.Fatalf("result %d error code = %q, want %q", index, got, want)
		}
	}
	valid := imported.Results[3]
	if !valid.Started || valid.LeagueId == "" {
		t.Fatalf("valid pick result = %+v, want started with a league ID", valid)
	}
	for index, result := range imported.Results[:3] {
		if result.Started || result.LeagueId != "" {
			t.Fatalf("failed pick %d result = %+v, want not started", index, result)
		}
	}
	if len(starter.jobs) != 1 || starter.jobs[0].Target.ID != valid.LeagueId {
		t.Fatalf("recorded import jobs = %+v, want exactly the valid pick's job", starter.jobs)
	}
}

// Stored credentials that can no longer be decrypted fail every pick the same
// way an expired-cookie import does: needs_reconnect, marked connection, no job.
func TestImportLeaguesUndecryptableCredentialsNeedsReconnect(t *testing.T) {
	ctx := context.Background()
	db := openConnectionTestDatabase(t)
	storeBox, err := secrets.NewBox(bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	season := fantasy.CurrentSeason(time.Now())
	ref := fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "locked-league", Season: season}
	storeService := &Service{
		DB: db, Box: storeBox, OwnerEmail: "locked@example.test",
		DiscoverESPN: func(context.Context, espn.Credentials) ([]fantasy.DiscoveredLeague, error) {
			return []fantasy.DiscoveredLeague{{Ref: ref, TeamID: "1"}}, nil
		},
	}
	credentials := espn.Credentials{S2: "synthetic-s2", SWID: "{synthetic-swid}"}
	connected, err := storeService.ConnectESPN(ctx, &v1.ConnectESPNRequest{S2: credentials.S2, Swid: credentials.SWID})
	if err != nil {
		t.Fatal(err)
	}

	otherBox, err := secrets.NewBox(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	starter := &fakeImportStarter{}
	service := &Service{
		DB: db, Box: otherBox, OwnerEmail: storeService.OwnerEmail,
		Imports: starter,
		NewSource: func(context.Context, ImportSpec) (PreparedSource, error) {
			t.Fatal("NewSource ran for undecryptable credentials")
			return PreparedSource{}, nil
		},
	}
	imported, err := service.ImportLeagues(ctx, &v1.ImportLeaguesRequest{
		ConnectionId: connected.ConnectionId,
		Picks: []*v1.ImportPick{
			{ExternalLeagueId: ref.ExternalID, TeamId: "1"},
			{ExternalLeagueId: ref.ExternalID, TeamId: "2"},
		},
	})
	if err != nil || len(imported.GetResults()) != 2 {
		t.Fatalf("import results = %+v, %v", imported, err)
	}
	for index, result := range imported.Results {
		if result.Started || result.ErrorCode != "needs_reconnect" {
			t.Fatalf("pick %d result = %+v, want needs_reconnect", index, result)
		}
	}
	if len(starter.jobs) != 0 {
		t.Fatalf("recorded import jobs = %d, want 0", len(starter.jobs))
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
			starter := &fakeImportStarter{}
			service := &Service{
				DB: db, Box: box, OwnerEmail: "unauthorized@example.test",
				Imports: starter,
				DiscoverESPN: func(context.Context, espn.Credentials) ([]fantasy.DiscoveredLeague, error) {
					return []fantasy.DiscoveredLeague{{Ref: ref, TeamID: "1"}}, nil
				},
				NewSource: func(_ context.Context, spec ImportSpec) (PreparedSource, error) {
					selection := fantasy.ConnectionSelection{League: ref, SelectedTeamIDs: []string{fmt.Sprint(spec.TeamID)}}
					if failSource {
						return PreparedSource{}, espn.ErrUnauthorized
					}
					// League left unset so Begin describes the league again and hits the unauthorized provider.
					return PreparedSource{
						Provider: connectionTestProvider{league: league, describeErr: espn.ErrUnauthorized}, AccountID: accountID,
						Selection: selection,
					}, nil
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
			if len(starter.jobs) != 0 {
				t.Fatalf("recorded import jobs after failed import = %d, want 0", len(starter.jobs))
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
