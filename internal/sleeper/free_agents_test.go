package sleeper

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
)

func TestListFreeAgentsSubtractsAllRosterListsAndScoresProjections(t *testing.T) {
	fixture, err := os.ReadFile("testdata/free_agents_projections.json")
	if err != nil {
		t.Fatal(err)
	}
	var projectionPath string
	var rosterPath string
	projections := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		projectionPath = r.URL.Path
		if !strings.Contains(r.URL.RawQuery, "position%5B%5D=QB") {
			t.Errorf("projection query = %s", r.URL.RawQuery)
		}
		_, _ = w.Write(fixture)
	}))
	defer projections.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rosterPath = r.URL.Path
		if r.URL.Path != "/league/league-1/rosters" {
			t.Errorf("unexpected Sleeper request %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[
			{"players":["13425"],"reserve":[],"taxi":["1373"]},
			{"players":[],"reserve":["421"],"taxi":[]}
		]`))
	}))
	defer api.Close()
	client := NewClient(api.Client())
	client.BaseURL = api.URL
	client.ProjectionsURL = projections.URL
	league := fantasy.League{
		Ref: fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "league-1", Season: 2026},
	}
	league.Settings.ProviderSettings, _ = json.Marshal(League{
		Scoring: map[string]json.RawMessage{
			"pass_yd": json.RawMessage("0.04"), "pass_td": json.RawMessage("4"),
		},
	})
	agents, err := client.ListFreeAgents(context.Background(), league, fantasy.FreeAgentQuery{
		Week: 6, Position: "QB", Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if projectionPath != "/2026/6" || rosterPath != "/league/league-1/rosters" {
		t.Fatalf("requested projection %q and rosters %q", projectionPath, rosterPath)
	}
	if len(agents) != 1 || agents[0].Name != "Sam Darnold" ||
		agents[0].ProjectedPoints == nil || *agents[0].ProjectedPoints != 12.9324 ||
		agents[0].Status != "available" || agents[0].Position != "QB" ||
		agents[0].NFLTeam != "SEA" || agents[0].SeasonPoints != nil ||
		agents[0].PercentRostered != nil {
		t.Fatalf("free agents = %+v", agents)
	}
	allAgents, err := client.ListFreeAgents(context.Background(), league, fantasy.FreeAgentQuery{
		Week: 6, Position: "QB", Limit: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(allAgents) != 2 || allAgents[0].Name != "Sam Darnold" || allAgents[1].Name != "Bo Nix" {
		t.Fatalf("rostered or unknown players were included: %+v", allAgents)
	}
}

func TestSleeperFreeAgentPositionsMapDST(t *testing.T) {
	positions, err := sleeperProjectionPositions("DST")
	if err != nil || len(positions) != 1 || positions[0] != "DEF" {
		t.Fatalf("DST projection positions = %v, %v", positions, err)
	}
	if _, err := sleeperProjectionPositions("FLEX"); err == nil {
		t.Fatal("unsupported position accepted")
	}
}

func TestListFreeAgentsUsesCatalogWhenProjectionMetadataIsMissing(t *testing.T) {
	catalog := playerids.Catalog{
		"500": {Name: "Catalog Player", Position: "QB", Team: "GB"},
	}
	agent, ok := sleeperFreeAgent(Projection{PlayerID: "500"}, nil, catalog)
	if !ok || agent.Name != "Catalog Player" || agent.Position != "QB" || agent.NFLTeam != "GB" {
		t.Fatalf("catalog fallback = %+v, found=%v", agent, ok)
	}
	if _, ok := sleeperFreeAgent(Projection{PlayerID: "unknown"}, nil, catalog); ok {
		t.Fatal("unknown player was not omitted")
	}
}
