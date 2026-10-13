//go:build integration

package importer

import (
	"context"
	"errors"
	"fmt"
	migrations "github.com/adityachoudhari26/league-lens/db"
	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"net/url"
	"os"
	"testing"
	"time"
)

func fixture(t *testing.T) fantasy.LeagueData {
	t.Helper()
	now := time.Now().UTC()
	zero, ten := 0.0, 10.0
	league := fantasy.League{Ref: fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "123", Season: 2026}, Name: "Fixture League", TeamCount: 2}
	data := fantasy.LeagueData{League: league, FetchedAt: now, Teams: []fantasy.Team{{ExternalID: "1", Name: "Home"}, {ExternalID: "2", Name: "Away"}}, CompleteMatchupPeriods: []fantasy.MatchupPeriod{1}}
	for _, id := range []string{"10", "20", "30", "DET"} {
		kind := fantasy.PlayerPerson
		if id == "DET" {
			kind = fantasy.PlayerTeamDefense
		}
		data.Players = append(data.Players, fantasy.Player{ExternalID: id, Name: "Fixture " + id, Kind: kind})
	}
	for _, week := range []fantasy.ScoringPeriod{2, 1} {
		origin := fantasy.SnapshotObserved
		if week == 1 {
			origin = fantasy.SnapshotHistory
		}
		data.Rosters = append(data.Rosters, fantasy.RosterSnapshot{TeamID: "1", ScoringPeriod: week, Origin: origin, Coverage: fantasy.CoverageFull, ObservedAt: now, Entries: []fantasy.RosterEntry{{PlayerID: "10", Slot: fantasy.SlotQB}, {PlayerID: "20", Slot: fantasy.SlotIR}, {PlayerID: "DET", Slot: fantasy.SlotBench}}}, fantasy.RosterSnapshot{TeamID: "2", ScoringPeriod: week, Origin: origin, Coverage: fantasy.CoverageFull, ObservedAt: now, Entries: []fantasy.RosterEntry{{PlayerID: "30", Slot: fantasy.SlotQB}}})
	}
	data.Matchups = []fantasy.Matchup{{ExternalID: "1", Period: 1, ScoringPeriods: []fantasy.ScoringPeriod{1}, TeamA: fantasy.MatchupSide{TeamID: "1", Score: &zero}, TeamB: &fantasy.MatchupSide{TeamID: "2", Score: &ten}, Status: fantasy.MatchupFinal}}
	return data
}

type fixtureSource struct {
	data fantasy.LeagueData
	err  error
}

func (f *fixtureSource) DescribeLeague(context.Context, string) (fantasy.League, error) {
	return f.data.League, nil
}
func (f *fixtureSource) FetchLeague(context.Context, fantasy.League) (fantasy.LeagueData, error) {
	return f.data, f.err
}
func TestImportRoundTrip(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL required")
	}
	cfg, e := pgx.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	admin := stdlib.OpenDB(*cfg)
	defer admin.Close()
	schema := fmt.Sprintf("import_%d", time.Now().UnixNano())
	if _, e = admin.Exec("CREATE SCHEMA " + schema); e != nil {
		t.Fatal(e)
	}
	defer admin.Exec("DROP SCHEMA " + schema + " CASCADE")
	cfg.RuntimeParams["search_path"] = schema
	sqlDB := stdlib.OpenDB(*cfg)
	defer sqlDB.Close()
	if e = migrations.Migrate(ctx, sqlDB, "up"); e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(dsn)
	v := u.Query()
	v.Set("search_path", schema)
	u.RawQuery = v.Encode()
	db, e := database.Open(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	src := &fixtureSource{data: fixture(t)}
	s := Service{DB: db, Source: src, Catalog: playerids.Catalog{"10": {Name: "Test QB", IDs: map[string]string{"sleeper": "10", "espn": "100"}}}}
	req := Request{OwnerEmail: "local@example.test", ExternalAccountID: "owner", Selection: fantasy.ConnectionSelection{League: src.data.League.Ref, SelectedTeamIDs: []string{"1"}}}
	count := func(table string) int {
		t.Helper()
		var n int
		if e := sqlDB.QueryRow("SELECT count(*) FROM " + table).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	if _, e = s.Import(ctx, req); e != nil {
		t.Fatal(e)
	}
	if count("players") != 4 || count("teams") != 2 || count("team_snapshots") != 4 || count("matchups") != 1 {
		t.Fatal("unexpected persisted counts")
	}
	if _, e = s.Import(ctx, req); e != nil {
		t.Fatal(e)
	}
	if count("players") != 4 || count("connections") != 1 || count("team_snapshots") != 4 {
		t.Fatal("repeat duplicated data")
	}
	var score string
	if e = sqlDB.QueryRow("SELECT score_a::text FROM matchups").Scan(&score); e != nil || score != "0.0000" {
		t.Fatalf("override lost: %s %v", score, e)
	}
	// Provider corrections replace the historical source, keeping its snapshot ID/count.
	corrected := 17.5
	src.data.Matchups[0].TeamA.Score = &corrected
	if _, e = s.Import(ctx, req); e != nil {
		t.Fatal(e)
	}
	if count("team_snapshots") != 4 {
		t.Fatal("correction duplicated history")
	}
	if e = sqlDB.QueryRow("SELECT score_a::text FROM matchups").Scan(&score); e != nil || score != "17.5000" {
		t.Fatalf("correction lost: %s %v", score, e)
	}
	// Missing history is not a deletion request; retain previously imported evidence.
	saved := src.data.Matchups
	src.data.Matchups = nil
	src.data.CompleteMatchupPeriods = nil
	if _, e = s.Import(ctx, req); e != nil {
		t.Fatal(e)
	}
	if count("matchups") != 1 || count("team_snapshots") != 4 {
		t.Fatal("missing history deleted evidence")
	}
	src.data.Matchups = saved
	src.data.CompleteMatchupPeriods = []fantasy.MatchupPeriod{1}
	src.err = errors.New("upstream down")
	if _, e = s.Import(ctx, req); e == nil {
		t.Fatal("failure hidden")
	}
	src.err = nil
	if count("team_snapshots") != 4 {
		t.Fatal("failed import changed data")
	}
	var failures int
	sqlDB.QueryRow("SELECT count(*) FROM sync_runs WHERE status='failed'").Scan(&failures)
	if failures != 1 {
		t.Fatal("failure not recorded")
	}
	// Force a database failure after team and identity writes; entire write phase rolls back.
	_, e = sqlDB.Exec("CREATE FUNCTION reject_matchup() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test failure'; END $$; CREATE TRIGGER reject_matchup BEFORE INSERT OR UPDATE ON matchups FOR EACH ROW EXECUTE FUNCTION reject_matchup()")
	if e != nil {
		t.Fatal(e)
	}
	src.data.Players = append(src.data.Players, fantasy.Player{ExternalID: "new", Name: "New", Kind: fantasy.PlayerPerson})
	src.data.Rosters[0].Entries = append(src.data.Rosters[0].Entries, fantasy.RosterEntry{PlayerID: "new", Slot: fantasy.SlotBench})
	if _, e = s.Import(ctx, req); e == nil {
		t.Fatal("database failure hidden")
	}
	if count("players") != 4 || count("team_snapshots") != 4 {
		t.Fatal("partial writes survived rollback")
	}

	if _, e = sqlDB.Exec("DROP TRIGGER reject_matchup ON matchups"); e != nil {
		t.Fatal(e)
	}
	// Fetches overlap without holding a DB transaction. Older runs cannot replace
	// the result of a newer run that already committed.
	blocked := &blockedSource{fixtureSource: fixtureSource{data: fixture(t)}, entered: make(chan struct{}), release: make(chan struct{})}
	older := Service{DB: db, Source: blocked, Catalog: s.Catalog}
	done := make(chan error, 1)
	go func() { _, err := older.Import(ctx, req); done <- err }()
	select {
	case <-blocked.entered:
	case <-time.After(5 * time.Second):
		close(blocked.release)
		t.Fatal("older import did not reach fetch")
	}
	src.data = fixture(t)
	_, newerErr := s.Import(ctx, req)
	close(blocked.release)
	if newerErr != nil {
		t.Fatal(newerErr)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("older run overwrote newer import")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("older import stuck")
	}
	// An explicit empty, complete period removes stale pairings. Missing history above did not.
	src.data.Matchups = nil
	if _, e = s.Import(ctx, req); e != nil {
		t.Fatal(e)
	}
	if count("matchups") != 0 {
		t.Fatal("complete empty schedule retained stale pairings")
	}
	src.data = fixture(t)
	src.data.Rosters[0].Entries[0].PlayerID = "missing"
	if _, e = s.Import(ctx, req); e == nil {
		t.Fatal("unresolved roster reference accepted")
	}
	if count("players") != 4 {
		t.Fatal("invalid payload wrote data")
	}
	// A second provider with opaque team IDs uses the same writer and shared player.
	src.data = fixture(t)
	src.data.League.Ref.Provider = fantasy.ProviderESPN
	src.data.League.Ref.ExternalID = "opaque-league"
	for index := range src.data.Teams {
		src.data.Teams[index].ExternalID = "team-" + src.data.Teams[index].ExternalID
	}
	aliases := map[string]string{"10": "100", "20": "200", "30": "300", "DET": "defense"}
	for index := range src.data.Players {
		src.data.Players[index].ExternalID = aliases[src.data.Players[index].ExternalID]
	}
	for index := range src.data.Rosters {
		roster := &src.data.Rosters[index]
		roster.TeamID = "team-" + roster.TeamID
		for entry := range roster.Entries {
			roster.Entries[entry].PlayerID = aliases[roster.Entries[entry].PlayerID]
		}
	}
	src.data.Matchups[0].TeamA.TeamID = "team-1"
	src.data.Matchups[0].TeamB.TeamID = "team-2"
	src.data.Matchups[0].ScoringPeriods = []fantasy.ScoringPeriod{1, 2}
	other := Request{OwnerEmail: req.OwnerEmail, ExternalAccountID: "other-account", Selection: fantasy.ConnectionSelection{League: src.data.League.Ref, SelectedTeamIDs: []string{"team-1"}}}
	if _, e = s.Import(ctx, other); e != nil {
		t.Fatal(e)
	}
	if count("players") != 7 {
		t.Fatal("cross-provider identity was duplicated")
	}
	var span string
	if e = sqlDB.QueryRow("SELECT scoring_periods_json::text FROM matchups").Scan(&span); e != nil || span != "[1, 2]" {
		t.Fatalf("span lost: %s %v", span, e)
	}

}

type blockedSource struct {
	fixtureSource
	entered chan struct{}
	release chan struct{}
}

func (b *blockedSource) FetchLeague(ctx context.Context, h fantasy.League) (fantasy.LeagueData, error) {
	close(b.entered)
	select {
	case <-b.release:
		return b.data, nil
	case <-ctx.Done():
		return fantasy.LeagueData{}, ctx.Err()
	}
}
