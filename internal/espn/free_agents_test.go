package espn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
		receivedFilter.Players["filterStatsForCurrentSeasonScoringPeriodId"] != nil ||
		receivedFilter.Players["filterStatsForTopScoringPeriodIds"] != nil {
		t.Fatalf("current-week stats filter missing: %+v", receivedFilter)
	}
	slot, ok := espnFreeAgentSlot("WR")
	if !ok || slot != 4 || !strings.Contains(string(mustJSON(t, receivedFilter)), `"value":[0,1]`) {
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

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
