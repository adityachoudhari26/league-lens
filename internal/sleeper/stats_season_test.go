package sleeper

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func TestDefenseVsPositionReadsSleeperSeasonStats(t *testing.T) {
	fixture := readSleeperSeasonFixture(t)
	server := newSleeperSeasonServer(t, http.StatusOK, fixture)
	defer server.Close()
	client := NewClient(server.Client())
	client.BaseURL = server.URL
	client.StatsURL = server.URL

	table, err := client.DefenseVsPosition(context.Background(), fantasy.League{
		Ref: fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "league", Season: 2026},
	})
	if err != nil {
		t.Fatal(err)
	}
	if table.Scoring != "ppr" || len(table.Positions) != 6 {
		t.Fatalf("scoring or position count = %q/%d", table.Scoring, len(table.Positions))
	}
	wr := table.Positions["WR"]
	if len(wr.ByTeam) != fantasy.NFLTeamCount ||
		math.Abs(wr.ByTeam["NE"].PointsAllowedPerGame-31.36) > 0.001 ||
		wr.ByTeam["KC"].PointsAllowedPerGame != 20.95 || wr.ByTeam["KC"].Rank != 1 ||
		wr.ByTeam["BUF"].Rank != fantasy.NFLTeamCount {
		t.Fatalf("WR ratings = %+v", wr)
	}
	if math.Abs(wr.LeagueAverage-sleeperSeasonExpectedAverage(t, fixture, "fan_pts_allow_wr")) > 0.000001 {
		t.Fatalf("WR league average = %v", wr.LeagueAverage)
	}
	if table.Positions["DST"].ByTeam["NE"].PointsAllowedPerGame == 0 {
		t.Fatalf("DST opponent points were not included: %+v", table.Positions["DST"])
	}
}

func TestSleeperDefenseRatingsTreatMissingStatsAsZeroAndBreakTiesByTeam(t *testing.T) {
	fixture := readSleeperSeasonFixture(t)
	var stats map[string]map[string]float64
	if err := json.Unmarshal(fixture, &stats); err != nil {
		t.Fatal(err)
	}
	delete(stats["NE"], "fan_pts_allow_def")
	teams, err := selectSleeperDefenseTeams(stats)
	if err != nil {
		t.Fatal(err)
	}
	table := buildSleeperDefenseTable(teams)
	if table.Positions["DST"].ByTeam["NE"].PointsAllowedPerGame != 0 {
		t.Fatalf("missing DST stats = %+v", table.Positions["DST"].ByTeam["NE"])
	}
	tied := rankSleeperDefenseTeams(map[string]float64{"KC": 10, "BUF": 10, "ATL": 20})
	if tied["BUF"].Rank != 1 || tied["KC"].Rank != 2 || tied["ATL"].Rank != 3 {
		t.Fatalf("tie ranks = %+v", tied)
	}
}

func TestDefenseVsPositionRejectsIncompleteSleeperStats(t *testing.T) {
	fixture := readSleeperSeasonFixture(t)
	tests := []struct {
		name   string
		status int
		body   []byte
	}{
		{name: "HTTP error", status: http.StatusInternalServerError, body: fixture},
		{name: "31 team rows", status: http.StatusOK, body: mutateSleeperSeasonFixture(t, fixture, func(stats map[string]map[string]float64) {
			delete(stats, "NE")
		})},
		{name: "zero games played", status: http.StatusOK, body: mutateSleeperSeasonFixture(t, fixture, func(stats map[string]map[string]float64) {
			stats["NE"]["gp"] = 0
		})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newSleeperSeasonServer(t, test.status, test.body)
			defer server.Close()
			client := NewClient(server.Client())
			client.BaseURL = server.URL
			client.StatsURL = server.URL
			_, err := client.DefenseVsPosition(context.Background(), fantasy.League{
				Ref: fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "league", Season: 2026},
			})
			if err == nil {
				t.Fatal("incomplete Sleeper stats were accepted")
			}
		})
	}
}

func TestDefenseVsPositionRejectsInvalidSleeperLeague(t *testing.T) {
	client := NewClient(http.DefaultClient)
	tests := []struct {
		name   string
		league fantasy.League
	}{
		{name: "wrong provider", league: fantasy.League{
			Ref: fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "1", Season: 2026},
		}},
		{name: "insane season", league: fantasy.League{
			Ref: fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "league", Season: 1800},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := client.DefenseVsPosition(context.Background(), test.league); err == nil {
				t.Fatal("invalid Sleeper league was accepted")
			}
		})
	}
}

func readSleeperSeasonFixture(t *testing.T) []byte {
	t.Helper()
	fixture, err := os.ReadFile("testdata/stats_season.json")
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func newSleeperSeasonServer(t *testing.T, status int, fixture []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stats/nfl/regular/2026" {
			t.Errorf("season stats path = %q", r.URL.Path)
		}
		w.WriteHeader(status)
		_, _ = w.Write(fixture)
	}))
}

func mutateSleeperSeasonFixture(
	t *testing.T,
	fixture []byte,
	mutate func(map[string]map[string]float64),
) []byte {
	t.Helper()
	var stats map[string]map[string]float64
	if err := json.Unmarshal(fixture, &stats); err != nil {
		t.Fatal(err)
	}
	mutate(stats)
	encoded, err := json.Marshal(stats)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func sleeperSeasonExpectedAverage(t *testing.T, fixture []byte, stat string) float64 {
	t.Helper()
	var stats map[string]map[string]float64
	if err := json.Unmarshal(fixture, &stats); err != nil {
		t.Fatal(err)
	}
	total := 0.0
	games := 0.0
	for team, row := range stats {
		if !isSleeperTeamAbbreviation(team) || row["gp"] <= 0 {
			continue
		}
		total += row[stat]
		games += row["gp"]
	}
	return total / games
}
