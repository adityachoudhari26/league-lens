package sleeper

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestWeeklyStatsReadsFixtureWeek(t *testing.T) {
	fixture, err := os.ReadFile("testdata/weekly_stats_week5.json")
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests++
		if request.URL.Path != "/stats/nfl/2026/5" {
			t.Errorf("path = %s, want /stats/nfl/2026/5", request.URL.Path)
		}
		if request.URL.Query().Get("season_type") != "regular" {
			t.Errorf("query = %s, want season_type=regular", request.URL.RawQuery)
		}
		_, _ = w.Write(fixture)
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.Client())
	client.StatsURL = server.URL + "/stats/nfl"

	rows, err := client.WeeklyStats(context.Background(), 2026, 5)
	if err != nil {
		t.Fatal(err)
	}
	var puka WeeklyStat
	for _, row := range rows {
		if row.PlayerID == "9493" {
			puka = row
			break
		}
	}
	if puka.PlayerID == "" || puka.Team != "LAR" || puka.Position() != "WR" ||
		puka.Stats["off_snp"] != 70 || puka.Stats["tm_off_snp"] != 82 ||
		puka.Stats["rec_tgt"] != 24 || puka.Stats["rec_rz_tgt"] != 3 ||
		puka.Stats["rush_att"] != 2 || puka.Stats["rush_rz_att"] != 1 ||
		puka.Stats["rec_air_yd"] != 57 {
		t.Fatalf("Puka row = %+v", puka)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestWeeklyStatsRejectsInvalidSeasonOrWeekWithoutRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.Client())
	client.StatsURL = server.URL + "/stats/nfl"

	for _, test := range []struct {
		season int
		week   int
	}{
		{season: 0, week: 1},
		{season: 2026, week: 0},
		{season: 2026, week: 19},
	} {
		if _, err := client.WeeklyStats(context.Background(), test.season, test.week); err == nil {
			t.Errorf("WeeklyStats(%d, %d) succeeded, want error", test.season, test.week)
		}
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestWeeklyStatPositionHandlesMissingPlayer(t *testing.T) {
	var row WeeklyStat
	if err := json.Unmarshal([]byte(`{"player_id":"1"}`), &row); err != nil {
		t.Fatal(err)
	}
	if got := row.Position(); got != "" {
		t.Fatalf("Position() = %q, want empty", got)
	}
}

func TestWeeklyStatNameTrimsAndHandlesMissingPlayer(t *testing.T) {
	var namedRow WeeklyStat
	if err := json.Unmarshal([]byte(`{"player":{"first_name":"  Jaylen","last_name":"Warren  "}}`), &namedRow); err != nil {
		t.Fatal(err)
	}
	assertWeeklyStatName(t, namedRow, "Jaylen Warren")
	var missingRow WeeklyStat
	assertWeeklyStatName(t, missingRow, "")
}

func assertWeeklyStatName(t *testing.T, row WeeklyStat, want string) {
	t.Helper()
	if got := row.Name(); got != want {
		t.Fatalf("Name() = %q, want %q", got, want)
	}
}
