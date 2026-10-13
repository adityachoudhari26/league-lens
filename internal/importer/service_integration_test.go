//go:build integration

package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	migrations "github.com/adityachoudhari26/league-lens/db"
	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
	"github.com/adityachoudhari26/league-lens/internal/sleeper"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"net/url"
	"os"
	"testing"
	"time"
)

func fixture(t *testing.T) sleeper.Data {
	t.Helper()
	d := sleeper.Data{Period: 2, ObservedAt: time.Now().UTC(), Weeks: map[int][]sleeper.Matchup{}, State: sleeper.State{Season: "2026", SeasonType: "regular", Week: 2}}
	d.User.ID = "owner"
	for f, out := range map[string]any{"league.json": &d.League, "rosters.json": &d.Rosters} {
		b, e := os.ReadFile("../sleeper/testdata/" + f)
		if e != nil {
			t.Fatal(e)
		}
		if e = json.Unmarshal(b, out); e != nil {
			t.Fatal(e)
		}
	}
	b, e := os.ReadFile("../sleeper/testdata/week.json")
	if e != nil {
		t.Fatal(e)
	}
	var week []sleeper.Matchup
	if e = json.Unmarshal(b, &week); e != nil {
		t.Fatal(e)
	}
	d.Weeks[1] = week
	return d
}

type fixtureSource struct {
	data sleeper.Data
	err  error
}

func (f *fixtureSource) Header(context.Context, string, string) (sleeper.Header, error) {
	return f.data.Header, nil
}
func (f *fixtureSource) Fetch(context.Context, sleeper.Header) (sleeper.Data, error) {
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
	req := Request{OwnerEmail: "local@example.test", Username: "test", LeagueID: "123"}
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
	src.data.Weeks[1][0].CustomPoints = &corrected
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
	saved := src.data.Weeks[1]
	src.data.Weeks[1] = nil
	if _, e = s.Import(ctx, req); e != nil {
		t.Fatal(e)
	}
	if count("matchups") != 1 || count("team_snapshots") != 4 {
		t.Fatal("missing history deleted evidence")
	}
	src.data.Weeks[1] = saved
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
	src.data.Rosters[0].Players = append(src.data.Rosters[0].Players, "new")
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
}

type blockedSource struct {
	fixtureSource
	entered chan struct{}
	release chan struct{}
}

func (b *blockedSource) Fetch(ctx context.Context, h sleeper.Header) (sleeper.Data, error) {
	close(b.entered)
	select {
	case <-b.release:
		return b.data, nil
	case <-ctx.Done():
		return sleeper.Data{}, ctx.Err()
	}
}
