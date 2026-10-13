//go:build integration

package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func TestSchemaAndQueries(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL must point to a disposable test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test database URL")
	}
	admin := stdlib.OpenDB(*cfg)
	defer admin.Close()
	schema := fmt.Sprintf("test_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE") }()
	cfg.RuntimeParams["search_path"] = schema
	conn := stdlib.OpenDB(*cfg)
	defer conn.Close()
	if err := Migrate(ctx, conn, "up"); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, conn, "up"); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	t.Run("queries prepare against migrated schema", func(t *testing.T) {
		files, err := filepath.Glob("queries/*.sql")
		if err != nil || len(files) == 0 {
			t.Fatal("no queries found")
		}
		for _, file := range files {
			content, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			for _, part := range strings.Split(string(content), "-- name: ")[1:] {
				header, query, _ := strings.Cut(part, "\n")
				stmt, err := conn.PrepareContext(ctx, query)
				if err != nil {
					t.Errorf("%s: %v", header, err)
					continue
				}
				_ = stmt.Close()
			}
		}
	})
	t.Run("identity access and season history constraints", func(t *testing.T) {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		exec := func(q string) {
			t.Helper()
			if _, err := tx.ExecContext(ctx, q); err != nil {
				t.Fatal(err)
			}
		}
		reject := func(q string) {
			t.Helper()
			exec("SAVEPOINT invalid_input")
			if _, err := tx.ExecContext(ctx, q); err == nil {
				t.Errorf("invalid record accepted: %s", q)
			}
			exec("ROLLBACK TO SAVEPOINT invalid_input")
		}
		exec(`INSERT INTO users(id,email) VALUES ('00000000-0000-0000-0000-000000000001','owner@example.test'),('00000000-0000-0000-0000-000000000002','other@example.test')`)
		exec(`INSERT INTO connections(id,user_id,provider) VALUES ('10000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000001','sleeper')`)
		exec(`INSERT INTO league_seasons(id,provider,external_league_id,season,name) VALUES ('20000000-0000-0000-0000-000000000001','sleeper','league-1',2026,'Test'),('20000000-0000-0000-0000-000000000002','sleeper','league-1',2025,'Past')`)
		reject(`INSERT INTO league_seasons(provider,external_league_id,season,name) VALUES ('sleeper','league-1',2026,'Duplicate')`)
		exec(`INSERT INTO connection_leagues(connection_id,league_season_id,user_id,provider) VALUES ('10000000-0000-0000-0000-000000000001','20000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000001','sleeper')`)
		reject(`INSERT INTO connection_leagues(connection_id,league_season_id,user_id,provider) VALUES ('10000000-0000-0000-0000-000000000001','20000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000002','sleeper')`)
		exec(`INSERT INTO teams(id,league_season_id,external_team_id,name) VALUES ('30000000-0000-0000-0000-000000000001','20000000-0000-0000-0000-000000000001','1','My team'),('30000000-0000-0000-0000-000000000002','20000000-0000-0000-0000-000000000002','1','Past team')`)
		reject(`INSERT INTO user_teams(user_id,connection_id,league_season_id,team_id) VALUES ('00000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000001','20000000-0000-0000-0000-000000000001','30000000-0000-0000-0000-000000000002')`)
		exec(`INSERT INTO players(id,display_name,entity_type) VALUES ('40000000-0000-0000-0000-000000000001','Example Player','person'),('40000000-0000-0000-0000-000000000002','Example D/ST','team_defense')`)
		exec(`INSERT INTO player_external_ids(provider,external_id,player_id,mapping_source) VALUES ('sleeper','123','40000000-0000-0000-0000-000000000001','provider')`)
		reject(`INSERT INTO player_external_ids(provider,external_id,player_id,mapping_source) VALUES ('sleeper','123','40000000-0000-0000-0000-000000000002','provider')`)
		exec(`INSERT INTO sync_runs(id,connection_id,league_season_id,kind) VALUES ('50000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000001','20000000-0000-0000-0000-000000000001','backfill')`)
		exec(`INSERT INTO team_snapshots(id,team_id,league_season_id,sync_run_id,period,origin,coverage,source_key,content_hash) VALUES ('60000000-0000-0000-0000-000000000001','30000000-0000-0000-0000-000000000001','20000000-0000-0000-0000-000000000001','50000000-0000-0000-0000-000000000001',3,'provider_history','lineup','week-3',repeat('a',64))`)
		exec(`INSERT INTO roster_entries(snapshot_id,player_id,provider_slot_id,slot_label) VALUES ('60000000-0000-0000-0000-000000000001','40000000-0000-0000-0000-000000000001','QB','QB')`)
		reject(`UPDATE team_snapshots SET team_id='30000000-0000-0000-0000-000000000002'`)
		reject(`UPDATE sync_runs SET status='succeeded' WHERE id='50000000-0000-0000-0000-000000000001'`)
		exec(`UPDATE sync_runs SET status='succeeded',completed_at=now() WHERE id='50000000-0000-0000-0000-000000000001'`)
		var effective sql.NullTime
		if err := tx.QueryRowContext(ctx, `SELECT effective_at FROM team_snapshots WHERE period=3`).Scan(&effective); err != nil {
			t.Fatal(err)
		}
		if effective.Valid {
			t.Fatal("invented an effective timestamp for historical data")
		}
		exec(`INSERT INTO matchups(league_season_id,external_matchup_id,period,team_a_id,status) VALUES ('20000000-0000-0000-0000-000000000001','bye-3',3,'30000000-0000-0000-0000-000000000001','scheduled')`)
		var score sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT score_a FROM matchups`).Scan(&score); err != nil {
			t.Fatal(err)
		}
		if score.Valid {
			t.Fatal("unknown score silently became zero")
		}
		reject(`UPDATE matchups SET team_b_id='30000000-0000-0000-0000-000000000002'`)

		queries := loadQueries(t)
		for _, user := range []struct {
			id   string
			want int
		}{
			{"00000000-0000-0000-0000-000000000001", 1},
			{"00000000-0000-0000-0000-000000000002", 0},
		} {
			rows, err := tx.QueryContext(ctx, queries["ListTeamsForUser"], user.id, "20000000-0000-0000-0000-000000000001")
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for rows.Next() {
				count++
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			if count != user.want {
				t.Fatalf("user %s sees %d teams; want %d", user.id, count, user.want)
			}
		}
		// Retry/correction of the same provider history must not append a duplicate.
		for _, hash := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64)} {
			_, err := tx.ExecContext(ctx, queries["UpsertTeamSnapshot"],
				"30000000-0000-0000-0000-000000000001", "20000000-0000-0000-0000-000000000001",
				"50000000-0000-0000-0000-000000000001", 3, "provider_history", "lineup", "week-3",
				nil, time.Now().UTC(), nil, hash)
			if err != nil {
				t.Fatal(err)
			}
		}
		var snapshotCount int
		var correctedHash string
		if err := tx.QueryRowContext(ctx, `SELECT count(*),min(content_hash) FROM team_snapshots`).Scan(&snapshotCount, &correctedHash); err != nil {
			t.Fatal(err)
		}
		if snapshotCount != 1 || correctedHash != strings.Repeat("b", 64) {
			t.Fatal("historical retry duplicated data or ignored correction")
		}
		rows, err := tx.QueryContext(ctx, queries["GetLatestObservedSnapshot"], "30000000-0000-0000-0000-000000000001", 3)
		if err != nil {
			t.Fatal(err)
		}
		if rows.Next() {
			t.Error("backfilled lineup treated as current observed roster")
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()

	})
	migrationFiles, err := filepath.Glob("migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for range migrationFiles {
		if err := Migrate(ctx, conn, "down"); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema=$1 AND table_name='players'`, schema).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("rollback left application tables")
	}
	if err := Migrate(ctx, conn, "up"); err != nil {
		t.Fatalf("reapply migration: %v", err)
	}
}

func loadQueries(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob("queries/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]string{}
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, part := range strings.Split(string(content), "-- name: ")[1:] {
			header, query, _ := strings.Cut(part, "\n")
			result[strings.Fields(header)[0]] = query
		}
	}
	return result
}
