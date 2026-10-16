package espn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func TestListFreeAgentsUsesCurrentWeekFilterAndMapsScoring(t *testing.T) {
	fixture, err := os.ReadFile("testdata/free_agents_response.json")
	if err != nil {
		t.Fatal(err)
	}
	var receivedFilter freeAgentFilter
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/seasons/2026/segments/0/leagues/123456" ||
			r.URL.Query().Get("view") != "kona_player_info" ||
			r.Header.Get("Cookie") != "espn_s2=s2-test; SWID={swid-test}" {
			t.Errorf("request = %s, query=%v, cookie=%q", r.URL.Path, r.URL.Query(), r.Header.Get("Cookie"))
		}
		if err := json.Unmarshal([]byte(r.Header.Get("x-fantasy-filter")), &receivedFilter); err != nil {
			t.Errorf("invalid filter: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()
	client := NewClient(server.Client(), Credentials{S2: "s2-test", SWID: "swid-test"}, 2026)
	client.baseURL = server.URL
	agents, err := client.ListFreeAgents(context.Background(), fantasy.League{
		Ref: fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "123456", Season: 2026},
	}, fantasy.FreeAgentQuery{Week: 6, Position: "WR", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 || agents[0].Name != "Xavier Worthy" ||
		agents[0].ProjectedPoints == nil || *agents[0].ProjectedPoints != 11.04450452 ||
		agents[0].SeasonPoints == nil || *agents[0].SeasonPoints != 33.3 ||
		agents[0].PercentRostered == nil || *agents[0].PercentRostered != 70.83 ||
		agents[0].Position != "WR" || agents[0].NFLTeam != "KC" || agents[0].Status != "freeagent" ||
		agents[1].Status != "waivers" {
		t.Fatalf("free agents = %+v", agents)
	}
	if receivedFilter.Players["filterStatsForSplitTypeIds"] == nil ||
		receivedFilter.Players["filterStatsForTopScoringPeriodIds"] != nil {
		t.Fatalf("current-week stats filter missing: %+v", receivedFilter)
	}
	var periodsFilter struct {
		Players struct {
			Periods struct {
				Value []int `json:"value"`
			} `json:"filterStatsForCurrentSeasonScoringPeriodId"`
		} `json:"players"`
	}
	if err := json.Unmarshal(mustJSON(t, receivedFilter), &periodsFilter); err != nil {
		t.Fatal(err)
	}
	slot, ok := espnFreeAgentSlot("WR")
	if !ok || slot != 4 || !strings.Contains(string(mustJSON(t, receivedFilter)), `"value":[0,1]`) ||
		!slices.Equal(periodsFilter.Players.Periods.Value, []int{0, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17}) {
		t.Fatalf("split or slot filter = %+v", receivedFilter)
	}
	nextWeekAgents, err := client.ListFreeAgents(context.Background(), fantasy.League{
		Ref: fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "123456", Season: 2026},
	}, fantasy.FreeAgentQuery{Week: 7, Position: "WR", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, agent := range nextWeekAgents {
		if agent.ProjectedPoints != nil {
			t.Fatalf("week-6 projection used for week 7: %+v", agent)
		}
	}
}

func TestListFreeAgentsMapsRestOfSeasonFromWeeklyFixture(t *testing.T) {
	fixture, err := os.ReadFile("testdata/free_agents_rest_of_season.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()
	client := NewClient(server.Client(), Credentials{S2: "s2-test", SWID: "swid-test"}, 2026)
	client.baseURL = server.URL
	agents, err := client.ListFreeAgents(context.Background(), fantasy.League{
		Ref: fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "123456", Season: 2026},
	}, fantasy.FreeAgentQuery{Week: 6, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	agentsByID := make(map[string]fantasy.FreeAgent, len(agents))
	for _, agent := range agents {
		agentsByID[agent.ExternalPlayerID] = agent
	}
	tests := []struct {
		playerID string
		points   float64
		weeks    int
	}{
		{playerID: "4035538", points: 112.1, weeks: 10},
		{playerID: "4362081", points: 87.1, weeks: 11},
		{playerID: "4685512", points: 104.7, weeks: 9},
	}
	for _, test := range tests {
		agent, found := agentsByID[test.playerID]
		if !found || agent.RestOfSeasonPoints == nil || *agent.RestOfSeasonPoints != test.points ||
			agent.RestOfSeasonWeeks == nil || *agent.RestOfSeasonWeeks != test.weeks {
			t.Fatalf("player %s rest-of-season fields = %+v", test.playerID, agent)
		}
	}
	david, found := agentsByID["4035538"]
	if !found || david.Name != "David Montgomery" || david.Status != "waivers" ||
		david.Position != "RB" || david.InjuryStatus != "OUT" ||
		david.SeasonPoints == nil || *david.SeasonPoints != 45.5 ||
		david.ProjectedPoints == nil || *david.ProjectedPoints != 0 ||
		david.PercentRostered == nil || *david.PercentRostered != 84.74 {
		t.Fatalf("existing free-agent fields changed: %+v", david)
	}
}

func TestFreeAgentScoringPeriodsIncludesCurrentWeekAfterFinalWeek(t *testing.T) {
	if !slices.Equal(freeAgentScoringPeriods(18), []int{0, 18}) {
		t.Fatalf("week 18 scoring periods = %v", freeAgentScoringPeriods(18))
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
