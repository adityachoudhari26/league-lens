//go:build integration

package importer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	migrations "github.com/adityachoudhari26/league-lens/db"
	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
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
	data        fantasy.LeagueData
	err         error
	describes   int
	lastRequest fantasy.FetchRequest
}

func (f *fixtureSource) DescribeLeague(context.Context, string) (fantasy.League, error) {
	f.describes++
	return f.data.League, nil
}

// FetchLeague records the request and, like a real provider, drops the skipped
// weeks' roster history, matchups, lineups, and complete-period marks.
func (f *fixtureSource) FetchLeague(_ context.Context, _ fantasy.League, request fantasy.FetchRequest) (fantasy.LeagueData, error) {
	f.lastRequest = request
	if f.err != nil {
		return fantasy.LeagueData{}, f.err
	}
	skip := map[fantasy.ScoringPeriod]bool{}
	for _, week := range request.SkipWeeks {
		skip[week] = true
	}
	data := f.data
	data.Rosters = nil
	for _, roster := range f.data.Rosters {
		if roster.Origin != fantasy.SnapshotHistory || !skip[roster.ScoringPeriod] {
			data.Rosters = append(data.Rosters, roster)
		}
	}
	data.Matchups = nil
	for _, matchup := range f.data.Matchups {
		covered := false
		for _, week := range matchup.ScoringPeriods {
			covered = covered || skip[week]
		}
		if !covered {
			data.Matchups = append(data.Matchups, matchup)
		}
	}
	data.Lineups = nil
	for _, lineup := range f.data.Lineups {
		if !skip[fantasy.ScoringPeriod(lineup.Period)] {
			data.Lineups = append(data.Lineups, lineup)
		}
	}
	data.CompleteMatchupPeriods = nil
	for _, period := range f.data.CompleteMatchupPeriods {
		if !skip[fantasy.ScoringPeriod(period)] {
			data.CompleteMatchupPeriods = append(data.CompleteMatchupPeriods, period)
		}
	}
	return data, nil
}

// openTestDatabase migrates a fresh schema in TEST_DATABASE_URL and returns a
// raw connection for assertions plus the application database handle.
func openTestDatabase(t *testing.T) (*sql.DB, *database.DB) {
	t.Helper()
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
	t.Cleanup(func() { admin.Close() })
	schema := fmt.Sprintf("import_%d", time.Now().UnixNano())
	if _, e = admin.Exec("CREATE SCHEMA " + schema); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { admin.Exec("DROP SCHEMA " + schema + " CASCADE") })
	cfg.RuntimeParams["search_path"] = schema
	sqlDB := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { sqlDB.Close() })
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
	t.Cleanup(db.Close)
	return sqlDB, db
}

func TestImportRoundTrip(t *testing.T) {
	ctx := context.Background()
	sqlDB, db := openTestDatabase(t)
	var e error
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
	userTeams := count("user_teams")
	secondConnection := Request{OwnerEmail: req.OwnerEmail, ExternalAccountID: "second-account", Selection: fantasy.ConnectionSelection{League: req.Selection.League, SelectedTeamIDs: []string{"2"}}}
	if _, e = s.Import(ctx, secondConnection); e == nil {
		t.Fatal("second selected team in the same league was accepted")
	}
	if count("user_teams") != userTeams {
		t.Fatal("failed second-team import changed user teams")
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

func TestBeginLeavesSyncRunningUntilFinish(t *testing.T) {
	ctx := context.Background()
	sqlDB, db := openTestDatabase(t)
	src := &fixtureSource{data: fixture(t)}
	s := Service{DB: db, Source: src, Catalog: playerids.Catalog{}}
	req := Request{OwnerEmail: "local@example.test", ExternalAccountID: "owner", Selection: fantasy.ConnectionSelection{League: src.data.League.Ref, SelectedTeamIDs: []string{"1"}}}

	started, err := s.Begin(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	var attached int
	if err := sqlDB.QueryRow(`SELECT count(*) FROM connection_leagues WHERE detached_at IS NULL`).Scan(&attached); err != nil || attached != 1 {
		t.Fatalf("attached leagues after Begin = %d, %v", attached, err)
	}
	var runID, status string
	var completedAt sql.NullTime
	err = sqlDB.QueryRow(`SELECT id, status, completed_at FROM sync_runs ORDER BY started_at DESC LIMIT 1`).Scan(&runID, &status, &completedAt)
	if err != nil {
		t.Fatal(err)
	}
	if status != "running" || completedAt.Valid {
		t.Fatalf("sync run after Begin = status %s completed_at %v, want running with no completed_at", status, completedAt)
	}

	result, err := s.Finish(ctx, started)
	if err != nil {
		t.Fatal(err)
	}
	if result.SyncID.String() != runID {
		t.Fatalf("Finish SyncID = %s, want the Begin run %s", result.SyncID, runID)
	}
	if err = sqlDB.QueryRow(`SELECT status FROM sync_runs WHERE id=$1`, runID).Scan(&status); err != nil || status != "succeeded" {
		t.Fatalf("sync run after Finish = %s, %v", status, err)
	}
	var teams int
	if err = sqlDB.QueryRow(`SELECT count(*) FROM teams`).Scan(&teams); err != nil || teams != 2 {
		t.Fatalf("teams after Finish = %d, %v", teams, err)
	}
}

func TestFinishRecordsProviderFailureOnSyncRun(t *testing.T) {
	ctx := context.Background()
	sqlDB, db := openTestDatabase(t)
	src := &fixtureSource{data: fixture(t)}
	s := Service{DB: db, Source: src, Catalog: playerids.Catalog{}}
	req := Request{OwnerEmail: "local@example.test", ExternalAccountID: "owner", Selection: fantasy.ConnectionSelection{League: src.data.League.Ref, SelectedTeamIDs: []string{"1"}}}

	started, err := s.Begin(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	src.err = errors.New("upstream down")
	result, err := s.Finish(ctx, started)
	var recorded *RecordedSyncError
	if !errors.As(err, &recorded) || recorded.Code != "provider_fetch_failed" {
		t.Fatalf("Finish error = %v, want RecordedSyncError provider_fetch_failed", err)
	}
	var status, errorCode string
	var completedAt sql.NullTime
	if err = sqlDB.QueryRow(`SELECT status, error_code, completed_at FROM sync_runs WHERE id=$1`, result.SyncID).Scan(&status, &errorCode, &completedAt); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || errorCode != "provider_fetch_failed" || !completedAt.Valid {
		t.Fatalf("sync run after failed Finish = %s/%s completed %v", status, errorCode, completedAt)
	}
	var teams int
	if err = sqlDB.QueryRow(`SELECT count(*) FROM teams`).Scan(&teams); err != nil || teams != 0 {
		t.Fatalf("teams after failed Finish = %d, %v", teams, err)
	}
}

// A description supplied with the request skips the provider read; a stale or
// missing one is re-described.
func TestBeginUsesSuppliedLeagueDescription(t *testing.T) {
	ctx := context.Background()
	_, db := openTestDatabase(t)
	src := &fixtureSource{data: fixture(t)}
	s := Service{DB: db, Source: src, Catalog: playerids.Catalog{}}
	req := Request{OwnerEmail: "local@example.test", ExternalAccountID: "owner", Selection: fantasy.ConnectionSelection{League: src.data.League.Ref, SelectedTeamIDs: []string{"1"}}}

	withLeague := req
	withLeague.League = src.data.League
	if _, err := s.Begin(ctx, withLeague); err != nil {
		t.Fatal(err)
	}
	if src.describes != 0 {
		t.Fatalf("describe calls with matching League = %d, want 0", src.describes)
	}

	stale := withLeague
	stale.League = src.data.League
	stale.League.Ref.ExternalID = "other-league"
	if _, err := s.Begin(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if src.describes != 1 {
		t.Fatalf("describe calls after stale League = %d, want 1", src.describes)
	}

	if _, err := s.Begin(ctx, req); err != nil {
		t.Fatal(err)
	}
	if src.describes != 2 {
		t.Fatalf("describe calls without League = %d, want 2", src.describes)
	}
}

// A failed Begin writes nothing: no orphan running sync run and no attachment.
func TestBeginFailureLeavesNoSyncRun(t *testing.T) {
	ctx := context.Background()
	sqlDB, db := openTestDatabase(t)
	src := &fixtureSource{data: fixture(t)}
	s := Service{DB: db, Source: src, Catalog: playerids.Catalog{}}
	selection := fantasy.ConnectionSelection{League: src.data.League.Ref, SelectedTeamIDs: []string{"1"}}
	count := func(table string) int {
		t.Helper()
		var n int
		if err := sqlDB.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	badRequest := Request{OwnerEmail: "", ExternalAccountID: "owner", Selection: selection}
	if _, err := s.Begin(ctx, badRequest); err == nil {
		t.Fatal("Begin accepted an empty owner email")
	}
	if count("sync_runs") != 0 || count("connection_leagues") != 0 {
		t.Fatal("rejected Begin left a sync run or attachment")
	}

	src.data.League.Ref = fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "other-league", Season: 2026}
	_, err := s.Begin(ctx, Request{OwnerEmail: "local@example.test", ExternalAccountID: "owner", Selection: selection})
	if err == nil || !strings.Contains(err.Error(), "league selection changed") {
		t.Fatalf("Begin error = %v, want league selection changed", err)
	}
	if src.describes != 1 {
		t.Fatalf("describe calls = %d, want 1", src.describes)
	}
	if count("sync_runs") != 0 || count("connection_leagues") != 0 {
		t.Fatal("changed-selection Begin left a sync run or attachment")
	}
}

type blockedSource struct {
	fixtureSource
	entered chan struct{}
	release chan struct{}
}

func (b *blockedSource) FetchLeague(ctx context.Context, h fantasy.League, _ fantasy.FetchRequest) (fantasy.LeagueData, error) {
	close(b.entered)
	select {
	case <-b.release:
		return b.data, nil
	case <-ctx.Done():
		return fantasy.LeagueData{}, ctx.Err()
	}
}

// lineupFixture gives team 1 a projected starter with points, an IR player
// without points or projection, and a bench player with a real zero for both;
// team 2 has one starter whose provider slot is unknown and whose projection
// is negative.
func lineupFixture() []fantasy.MatchupLineup {
	twelve, zero, projected, negative := 12.5, 0.0, 14.25, -1.5
	return []fantasy.MatchupLineup{
		{Period: 1, MatchupID: "1", TeamID: "1", Entries: []fantasy.LineupEntry{
			{PlayerID: "10", ProviderSlotID: "0", Slot: fantasy.SlotQB, Starter: true, Points: &twelve, ProjectedPoints: &projected},
			{PlayerID: "20", ProviderSlotID: "21", Slot: fantasy.SlotIR},
			{PlayerID: "DET", ProviderSlotID: "20", Slot: fantasy.SlotBench, Points: &zero, ProjectedPoints: &zero},
		}},
		{Period: 1, MatchupID: "1", TeamID: "2", Entries: []fantasy.LineupEntry{
			{PlayerID: "30", Slot: fantasy.SlotQB, Starter: true, Points: &zero, ProjectedPoints: &negative},
		}},
	}
}

func TestImportMatchupLineups(t *testing.T) {
	ctx := context.Background()
	sqlDB, db := openTestDatabase(t)
	src := &fixtureSource{data: fixture(t)}
	src.data.Lineups = lineupFixture()
	s := Service{DB: db, Source: src, Catalog: playerids.Catalog{}}
	req := Request{OwnerEmail: "local@example.test", ExternalAccountID: "owner", Selection: fantasy.ConnectionSelection{League: src.data.League.Ref, SelectedTeamIDs: []string{"1"}}}
	importFixture := func() {
		t.Helper()
		if _, err := s.Import(ctx, req); err != nil {
			t.Fatal(err)
		}
	}
	team2Rows := map[string]string{"2/Fixture 30": "slot=<null> label=QB starter=true points=0.0000 projected=-1.5000"}

	importFixture()
	assertLineupRows(t, sqlDB, mergeRows(map[string]string{
		"1/Fixture 10":  "slot=0 label=QB starter=true points=12.5000 projected=14.2500",
		"1/Fixture 20":  "slot=21 label=IR starter=false points=<null> projected=<null>",
		"1/Fixture DET": "slot=20 label=BENCH starter=false points=0.0000 projected=0.0000",
	}, team2Rows))

	// A reported lineup replaces that side's rows, projections included.
	src.data.Players = append(src.data.Players, fantasy.Player{ExternalID: "40", Name: "Fixture 40", Kind: fantasy.PlayerPerson})
	src.data.Lineups[0].Entries[1] = fantasy.LineupEntry{PlayerID: "40", ProviderSlotID: "2", Slot: fantasy.SlotRB, Starter: true}
	updated := 15.75
	src.data.Lineups[0].Entries[0].ProjectedPoints = &updated
	importFixture()
	team1Rows := map[string]string{
		"1/Fixture 10":  "slot=0 label=QB starter=true points=12.5000 projected=15.7500",
		"1/Fixture 40":  "slot=2 label=RB starter=true points=<null> projected=<null>",
		"1/Fixture DET": "slot=20 label=BENCH starter=false points=0.0000 projected=0.0000",
	}
	assertLineupRows(t, sqlDB, mergeRows(team1Rows, team2Rows))

	// A missing lineup means unavailable, so team 2 keeps its stored rows.
	src.data.Lineups = src.data.Lineups[:1]
	importFixture()
	assertLineupRows(t, sqlDB, mergeRows(team1Rows, team2Rows))

	// When the matchup becomes a bye, the former opponent's rows are stale.
	src.data.Matchups[0].TeamB = nil
	src.data.Matchups[0].IsBye = true
	importFixture()
	assertLineupRows(t, sqlDB, team1Rows)

	// A matchup removed from a complete period takes its lineups with it.
	src.data.Matchups = nil
	src.data.Lineups = nil
	importFixture()
	assertLineupRows(t, sqlDB, map[string]string{})
}

func mergeRows(groups ...map[string]string) map[string]string {
	merged := map[string]string{}
	for _, group := range groups {
		maps.Copy(merged, group)
	}
	return merged
}

// assertLineupRows compares every stored lineup row, keyed by
// "team external ID/player display name".
func assertLineupRows(t *testing.T, sqlDB *sql.DB, want map[string]string) {
	t.Helper()
	got := readLineupRows(t, sqlDB)
	if !maps.Equal(got, want) {
		t.Fatalf("lineup rows:\n got %v\nwant %v", got, want)
	}
}

func readLineupRows(t *testing.T, sqlDB *sql.DB) map[string]string {
	t.Helper()
	rows, err := sqlDB.Query(`SELECT t.external_team_id||'/'||p.display_name,
	 'slot='||COALESCE(l.provider_slot_id,'<null>')||' label='||l.slot_label||' starter='||l.starter::text||' points='||COALESCE(l.points::text,'<null>')||' projected='||COALESCE(l.projected_points::text,'<null>')
	 FROM matchup_lineups l JOIN teams t ON t.id=l.team_id JOIN players p ON p.id=l.player_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var key, value string
		if err = rows.Scan(&key, &value); err != nil {
			t.Fatal(err)
		}
		got[key] = value
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return got
}

// settledWeeksFixture is a league at week 4: weeks 1-3 fully final with
// provider history for both teams, week 4 current with a matchup still open
// (and history snapshots, so only its non-final matchup keeps it unsettled).
func settledWeeksFixture(t *testing.T) fantasy.LeagueData {
	t.Helper()
	now := time.Now().UTC()
	league := fantasy.League{Ref: fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "settled-league", Season: 2026}, Name: "Settled League", TeamCount: 2}
	data := fantasy.LeagueData{League: league, FetchedAt: now, Teams: []fantasy.Team{{ExternalID: "1", Name: "Home"}, {ExternalID: "2", Name: "Away"}}, CompleteMatchupPeriods: []fantasy.MatchupPeriod{1, 2, 3, 4}}
	for _, id := range []string{"10", "30"} {
		data.Players = append(data.Players, fantasy.Player{ExternalID: id, Name: "Fixture " + id, Kind: fantasy.PlayerPerson})
	}
	for week := 1; week <= 4; week++ {
		score := float64(week)
		status := fantasy.MatchupFinal
		if week == 4 {
			status = fantasy.MatchupInProgress
		}
		data.Matchups = append(data.Matchups, fantasy.Matchup{
			ExternalID: fmt.Sprintf("m%d", week), Period: fantasy.MatchupPeriod(week), ScoringPeriods: []fantasy.ScoringPeriod{fantasy.ScoringPeriod(week)},
			TeamA: fantasy.MatchupSide{TeamID: "1", Score: &score}, TeamB: &fantasy.MatchupSide{TeamID: "2", Score: &score}, Status: status,
		})
		for _, team := range []string{"1", "2"} {
			player := "10"
			if team == "2" {
				player = "30"
			}
			origin := fantasy.SnapshotHistory
			if week == 4 {
				data.Rosters = append(data.Rosters, fantasy.RosterSnapshot{TeamID: team, ScoringPeriod: fantasy.ScoringPeriod(week), Origin: fantasy.SnapshotObserved, Coverage: fantasy.CoverageFull, ObservedAt: now, Entries: []fantasy.RosterEntry{{PlayerID: player, Slot: fantasy.SlotQB}}})
			}
			data.Rosters = append(data.Rosters, fantasy.RosterSnapshot{TeamID: team, ScoringPeriod: fantasy.ScoringPeriod(week), Origin: origin, Coverage: fantasy.CoverageFull, ObservedAt: now, Entries: []fantasy.RosterEntry{{PlayerID: player, Slot: fantasy.SlotQB}}})
		}
		points, projected := float64(week), float64(week)+0.5
		for _, side := range []struct{ team, player string }{{"1", "10"}, {"2", "30"}} {
			data.Lineups = append(data.Lineups, fantasy.MatchupLineup{
				Period: fantasy.MatchupPeriod(week), MatchupID: fmt.Sprintf("m%d", week), TeamID: side.team,
				Entries: []fantasy.LineupEntry{{PlayerID: side.player, Slot: fantasy.SlotQB, Starter: true, Points: &points, ProjectedPoints: &projected}},
			})
		}
	}
	return data
}

// An incremental sync re-reads only the newest settled week (stat corrections)
// and the current week; earlier settled weeks keep their stored rows, and a
// week with an open matchup is never settled. FullHistory is the rebuild path.
func TestFinishSkipsSettledWeeks(t *testing.T) {
	ctx := context.Background()
	sqlDB, db := openTestDatabase(t)
	src := &fixtureSource{data: settledWeeksFixture(t)}
	s := Service{DB: db, Source: src, Catalog: playerids.Catalog{}}
	req := Request{OwnerEmail: "local@example.test", ExternalAccountID: "owner", Selection: fantasy.ConnectionSelection{League: src.data.League.Ref, SelectedTeamIDs: []string{"1"}}}

	if _, err := s.Import(ctx, req); err != nil {
		t.Fatal(err)
	}
	if len(src.lastRequest.SkipWeeks) != 0 {
		t.Fatalf("first import SkipWeeks = %v, want none", src.lastRequest.SkipWeeks)
	}
	dump := func(query string) string {
		t.Helper()
		var digest string
		if err := sqlDB.QueryRow(`SELECT md5(coalesce(string_agg(r::text, '' ORDER BY r::text), '')) FROM (` + query + `) r`).Scan(&digest); err != nil {
			t.Fatal(err)
		}
		return digest
	}
	settledRows := func() string {
		return dump(`SELECT * FROM matchups WHERE period IN (1,2)`) +
			dump(`SELECT l.* FROM matchup_lineups l JOIN matchups m ON m.id = l.matchup_id WHERE m.period IN (1,2)`) +
			dump(`SELECT * FROM team_snapshots WHERE period IN (1,2)`) +
			dump(`SELECT e.* FROM roster_entries e JOIN team_snapshots ts ON ts.id = e.snapshot_id WHERE ts.period IN (1,2)`)
	}
	before := settledRows()

	if _, err := s.Import(ctx, req); err != nil {
		t.Fatal(err)
	}
	if len(src.lastRequest.SkipWeeks) != 2 || src.lastRequest.SkipWeeks[0] != 1 || src.lastRequest.SkipWeeks[1] != 2 {
		t.Fatalf("second import SkipWeeks = %v, want [1 2]", src.lastRequest.SkipWeeks)
	}
	if after := settledRows(); after != before {
		t.Fatal("skipped weeks' stored rows changed")
	}

	// Without week 1's provider history, week 1 is no longer settled and is
	// re-read; week 3 stays the correction window.
	if _, err := sqlDB.Exec(`DELETE FROM team_snapshots WHERE period = 1 AND origin = 'provider_history'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Import(ctx, req); err != nil {
		t.Fatal(err)
	}
	if len(src.lastRequest.SkipWeeks) != 1 || src.lastRequest.SkipWeeks[0] != 2 {
		t.Fatalf("third import SkipWeeks = %v, want [2]", src.lastRequest.SkipWeeks)
	}

	full := req
	full.FullHistory = true
	if _, err := s.Import(ctx, full); err != nil {
		t.Fatal(err)
	}
	if len(src.lastRequest.SkipWeeks) != 0 {
		t.Fatalf("full-history import SkipWeeks = %v, want none", src.lastRequest.SkipWeeks)
	}
}
