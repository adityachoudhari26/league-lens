package sleeper

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func TestFetchLiveMatchupIncludesScoresAndScoringCounts(t *testing.T) {
	matchupBody := sleeperLiveFixture(t, "live_matchup_week5.json")
	statsBody := sleeperLiveFixture(t, "live_stats_week5.json")
	client, statsCalls := sleeperLiveFixtureClient(t, matchupBody, statsBody, http.StatusOK)
	live, err := client.FetchLiveMatchup(context.Background(), sleeperLiveLeague(), sleeperLiveRequest("7"))
	if err != nil {
		t.Fatal(err)
	}
	if *statsCalls != 1 {
		t.Fatalf("stats requests = %d, want 1", *statsCalls)
	}
	if live.Matchup.Period != 5 || live.Matchup.ExternalID != "1" ||
		live.Matchup.TeamA.TeamID != "7" || live.Matchup.TeamA.Score == nil || *live.Matchup.TeamA.Score != 168.67 ||
		live.Matchup.TeamB == nil || live.Matchup.TeamB.TeamID != "8" || live.Matchup.TeamB.Score == nil || *live.Matchup.TeamB.Score != 148.99 {
		t.Fatalf("matchup = %+v", live.Matchup)
	}
	if len(live.Lineups) != 2 {
		t.Fatalf("got %d lineups, want both matchup sides", len(live.Lineups))
	}
	for _, lineup := range live.Lineups {
		starters := 0
		for _, entry := range lineup.Entries {
			if entry.Starter {
				starters++
				if entry.Scoring == nil {
					t.Errorf("starter %s scoring is nil after successful stats fetch", entry.PlayerID)
				}
			}
		}
		if starters != 10 {
			t.Errorf("team %s has %d starters, want 10", lineup.TeamID, starters)
		}
	}
	assertSleeperLiveEntry(t, live, "11564", 45.96, 5, 0)
	assertSleeperLiveEntry(t, live, "2747", 23, 0, 5)
	assertSleeperLiveEntry(t, live, "ATL", 14.66, 1, 0)
	assertSleeperLivePlayers(t, live)
}

func TestFetchLiveMatchupKeepsPointsWhenStatsRequestFails(t *testing.T) {
	matchupBody := sleeperLiveFixture(t, "live_matchup_week5.json")
	client, statsCalls := sleeperLiveFixtureClient(t, matchupBody, nil, http.StatusServiceUnavailable)
	live, err := client.FetchLiveMatchup(context.Background(), sleeperLiveLeague(), sleeperLiveRequest("7"))
	if err != nil {
		t.Fatal(err)
	}
	if *statsCalls != 1 {
		t.Fatalf("stats requests = %d, want 1", *statsCalls)
	}
	if live.Matchup.TeamA.Score == nil || *live.Matchup.TeamA.Score != 168.67 {
		t.Fatalf("owner score = %v, want 168.67", live.Matchup.TeamA.Score)
	}
	for _, lineup := range live.Lineups {
		for _, entry := range lineup.Entries {
			if entry.Scoring != nil {
				t.Errorf("entry %s scoring = %+v, want nil after stats failure", entry.PlayerID, entry.Scoring)
			}
		}
	}
}

func TestFetchLiveMatchupGivesStarterWithoutStatsZeroCounts(t *testing.T) {
	matchupBody := sleeperLiveFixture(t, "live_matchup_week5.json")
	statsBody := sleeperLiveFixture(t, "live_stats_week5.json")
	var rows []liveStatsRow
	if err := json.Unmarshal(statsBody, &rows); err != nil {
		t.Fatal(err)
	}
	filtered := rows[:0]
	for _, row := range rows {
		if row.PlayerID != "11564" {
			filtered = append(filtered, row)
		}
	}
	statsBody, err := json.Marshal(filtered)
	if err != nil {
		t.Fatal(err)
	}
	client, _ := sleeperLiveFixtureClient(t, matchupBody, statsBody, http.StatusOK)
	live, err := client.FetchLiveMatchup(context.Background(), sleeperLiveLeague(), sleeperLiveRequest("7"))
	if err != nil {
		t.Fatal(err)
	}
	entry := findSleeperLiveEntry(live, "11564")
	if entry == nil || !entry.Starter || entry.Points == nil || *entry.Points != 45.96 ||
		entry.Scoring == nil || entry.Scoring.Touchdowns != 0 || entry.Scoring.FieldGoals != 0 {
		t.Errorf("starter without stats row = %+v, want points and zero scoring counts", entry)
	}
}

func TestFetchLiveMatchupReturnsErrorForUnknownRoster(t *testing.T) {
	matchupBody := sleeperLiveFixture(t, "live_matchup_week5.json")
	client, _ := sleeperLiveFixtureClient(t, matchupBody, nil, http.StatusOK)
	if _, err := client.FetchLiveMatchup(context.Background(), sleeperLiveLeague(), sleeperLiveRequest("999")); err == nil {
		t.Fatal("unknown roster did not return an error")
	}
}

func sleeperLiveFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func sleeperLiveFixtureClient(t *testing.T, matchupBody, statsBody []byte, statsStatus int) (*Client, *int) {
	t.Helper()
	statsCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/league/1388361934532780032/matchups/5":
			_, _ = w.Write(matchupBody)
		case "/stats/nfl/2026/5":
			statsCalls++
			if request.URL.Query().Get("season_type") != "regular" {
				t.Errorf("stats query = %s, want season_type=regular", request.URL.RawQuery)
			}
			if statsStatus != http.StatusOK {
				w.WriteHeader(statsStatus)
				return
			}
			_, _ = w.Write(statsBody)
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.Client())
	client.BaseURL = server.URL + "/v1"
	client.StatsURL = server.URL + "/stats/nfl"
	return client, &statsCalls
}

func sleeperLiveLeague() fantasy.League {
	return fantasy.League{
		Ref: fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "1388361934532780032", Season: 2026},
		Settings: fantasy.LeagueSettings{
			ProviderSettings: []byte(`{"roster_positions":["QB","RB","RB","WR","WR","TE","FLEX","FLEX","SUPER_FLEX","K","DEF","BN","BN","BN","BN","BN","BN"]}`),
		},
	}
}

func sleeperLiveRequest(rosterID string) fantasy.LiveMatchupRequest {
	return fantasy.LiveMatchupRequest{
		Week:   5,
		Period: 5,
		TeamID: rosterID,
	}
}

func assertSleeperLiveEntry(t *testing.T, live fantasy.LiveMatchup, playerID string, points float64, touchdowns, fieldGoals int) {
	t.Helper()
	entry := findSleeperLiveEntry(live, playerID)
	if entry == nil {
		t.Fatalf("player %s not found", playerID)
	}
	if entry.Points == nil || *entry.Points != points {
		t.Errorf("player %s points = %v, want %v", playerID, entry.Points, points)
	}
	if entry.Scoring == nil || entry.Scoring.Touchdowns != touchdowns || entry.Scoring.FieldGoals != fieldGoals {
		t.Errorf("player %s scoring = %+v, want touchdowns/field goals %d/%d", playerID, entry.Scoring, touchdowns, fieldGoals)
	}
}

func findSleeperLiveEntry(live fantasy.LiveMatchup, playerID string) *fantasy.LineupEntry {
	for lineupIndex := range live.Lineups {
		for entryIndex := range live.Lineups[lineupIndex].Entries {
			if live.Lineups[lineupIndex].Entries[entryIndex].PlayerID == playerID {
				return &live.Lineups[lineupIndex].Entries[entryIndex]
			}
		}
	}
	return nil
}

func assertSleeperLivePlayers(t *testing.T, live fantasy.LiveMatchup) {
	t.Helper()
	players := map[string]bool{}
	for _, player := range live.Players {
		players[player.ExternalID] = true
	}
	for _, lineup := range live.Lineups {
		for _, entry := range lineup.Entries {
			if !players[entry.PlayerID] {
				t.Errorf("lineup player %s missing from LiveMatchup.Players", entry.PlayerID)
			}
		}
	}
}
