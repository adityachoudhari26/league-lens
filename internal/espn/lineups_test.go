package espn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func boxscoreFixtureClient(t *testing.T, change func(map[string]any, *http.Request)) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/seasons/2026/segments/0/leagues/123" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		cookie, err := r.Cookie("espn_s2")
		if err != nil || cookie.Value != "private-token" {
			t.Error("missing authentication")
		}
		week := 2
		if r.URL.Query().Get("scoringPeriodId") == "1" {
			week = 1
		}
		body, err := os.ReadFile(fmt.Sprintf("testdata/boxscore_week%d.json", week))
		if err != nil {
			t.Errorf("read week %d fixture: %v", week, err)
			http.Error(w, "fixture unavailable", http.StatusInternalServerError)
			return
		}
		var value map[string]any
		if err := json.Unmarshal(body, &value); err != nil {
			t.Errorf("decode week %d fixture: %v", week, err)
			http.Error(w, "invalid fixture", http.StatusInternalServerError)
			return
		}
		if change != nil {
			change(value, r)
		}
		json.NewEncoder(w).Encode(value)
	}))
	t.Cleanup(server.Close)
	client := NewClient(server.Client(), Credentials{S2: "private-token", SWID: "{account}"}, 2026)
	client.baseURL = server.URL
	return client
}

func fetchFixtureLeague(t *testing.T, client *Client) fantasy.LeagueData {
	t.Helper()
	league, err := client.DescribeLeague(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	data, err := client.FetchLeague(context.Background(), league)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestFetchLineups(t *testing.T) {
	client := boxscoreFixtureClient(t, nil)
	data := fetchFixtureLeague(t, client)
	if len(data.Lineups) != 4 {
		t.Fatalf("got %d lineups, want both sides in each week", len(data.Lineups))
	}

	assertLineupsReferenceMatchupsAndPlayers(t, data)
	players := playerIDs(data.Players)
	if !players["108"] {
		t.Fatal("dropped player from historical lineup missing from LeagueData.Players")
	}

	expected := map[string]expectedLineupEntry{
		"101": {floatPointer(10), floatPointer(12.5), true, fantasy.SlotQB}, // finished week keeps its projection
		"102": {floatPointer(5), nil, true, fantasy.SlotFlex},
		"103": {floatPointer(4), nil, false, fantasy.SlotBench},
		"104": {floatPointer(2), nil, false, fantasy.SlotIR},
		"105": {floatPointer(0), nil, true, fantasy.SlotRB},
		"106": {nil, nil, false, fantasy.SlotBench},
		"108": {nil, nil, false, fantasy.SlotBench},
		"111": {floatPointer(8), nil, true, fantasy.SlotQB},
		"112": {floatPointer(4), floatPointer(-1.5), true, fantasy.SlotFlex}, // season line ignored; negative kept
		"201": {nil, floatPointer(18.25), true, fantasy.SlotQB},
		"202": {nil, floatPointer(0), true, fantasy.SlotFlex}, // zero projection is real
		"211": {nil, nil, true, fantasy.SlotQB},               // projected line without appliedTotal
		"212": {nil, nil, true, fantasy.SlotFlex},
	}
	assertLineupEntries(t, data.Lineups, expected)
	assertStartersSumToMatchupScore(t, data, 1)
}

func TestMissingLineupSideIsOmitted(t *testing.T) {
	client := boxscoreFixtureClient(t, func(value map[string]any, request *http.Request) {
		if request.URL.Query().Get("scoringPeriodId") != "1" {
			return
		}
		match := value["schedule"].([]any)[0].(map[string]any)
		delete(match["home"].(map[string]any), "rosterForCurrentScoringPeriod")
	})
	data := fetchFixtureLeague(t, client)
	if _, exists := lineupForTeam(data.Lineups, 1, "1", "1"); exists {
		t.Fatal("missing home lineup block produced a lineup")
	}
	if _, exists := lineupForTeam(data.Lineups, 1, "1", "2"); !exists {
		t.Fatal("available away lineup was omitted")
	}
	if len(data.Lineups) != 3 {
		t.Fatalf("got %d lineups, want three", len(data.Lineups))
	}
}

// A history response whose schedule names a team outside the current matchup
// must not produce a lineup.
func TestLineupOutsideMatchupSidesIsSkipped(t *testing.T) {
	client := boxscoreFixtureClient(t, func(value map[string]any, request *http.Request) {
		if request.URL.Query().Get("scoringPeriodId") == "1" {
			match := value["schedule"].([]any)[0].(map[string]any)
			match["home"].(map[string]any)["teamId"] = 999
		}
	})
	data := fetchFixtureLeague(t, client)
	if _, exists := lineupForTeam(data.Lineups, 1, "1", "999"); exists {
		t.Fatal("lineup with a team outside the matchup was added")
	}
}

func TestMultiweekMatchupsHaveNoLineups(t *testing.T) {
	client := fixtureClient(t, nil)
	data := fetchFixtureLeague(t, client)
	if len(data.Lineups) != 0 {
		t.Fatalf("got %d lineups for multiweek fixture, want none", len(data.Lineups))
	}
}

type expectedLineupEntry struct {
	points    *float64
	projected *float64
	starter   bool
	slot      fantasy.RosterSlot
}

func matchupForLineup(matchups []fantasy.Matchup, lineup fantasy.MatchupLineup) (fantasy.Matchup, bool) {
	for _, matchup := range matchups {
		if matchup.Period == lineup.Period && matchup.ExternalID == lineup.MatchupID {
			return matchup, true
		}
	}
	return fantasy.Matchup{}, false
}

func isMatchupSide(matchup fantasy.Matchup, teamID string) bool {
	return matchup.TeamA.TeamID == teamID || matchup.TeamB != nil && matchup.TeamB.TeamID == teamID
}

func lineupForTeam(lineups []fantasy.MatchupLineup, period fantasy.MatchupPeriod, matchupID, teamID string) (fantasy.MatchupLineup, bool) {
	for _, lineup := range lineups {
		if lineup.Period == period && lineup.MatchupID == matchupID && lineup.TeamID == teamID {
			return lineup, true
		}
	}
	return fantasy.MatchupLineup{}, false
}

func playerIDs(players []fantasy.Player) map[string]bool {
	ids := make(map[string]bool, len(players))
	for _, player := range players {
		ids[player.ExternalID] = true
	}
	return ids
}

func pointsEqual(got, want *float64) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

func floatPointer(value float64) *float64 {
	return &value
}

func assertLineupsReferenceMatchupsAndPlayers(t *testing.T, data fantasy.LeagueData) {
	t.Helper()
	players := playerIDs(data.Players)
	for _, lineup := range data.Lineups {
		matchup, exists := matchupForLineup(data.Matchups, lineup)
		if !exists || !isMatchupSide(matchup, lineup.TeamID) {
			t.Fatalf("lineup references unknown matchup side: %+v", lineup)
		}
		for _, entry := range lineup.Entries {
			if !players[entry.PlayerID] {
				t.Errorf("lineup player %s missing from LeagueData.Players", entry.PlayerID)
			}
		}
	}
}

func assertLineupEntries(t *testing.T, lineups []fantasy.MatchupLineup, expected map[string]expectedLineupEntry) {
	t.Helper()
	seen := map[string]bool{}
	for _, lineup := range lineups {
		for _, entry := range lineup.Entries {
			want, exists := expected[entry.PlayerID]
			if !exists {
				t.Errorf("unexpected lineup player %s", entry.PlayerID)
				continue
			}
			seen[entry.PlayerID] = true
			if !pointsEqual(entry.Points, want.points) {
				t.Errorf("player %s points = %v, want %v", entry.PlayerID, entry.Points, want.points)
			}
			if !pointsEqual(entry.ProjectedPoints, want.projected) {
				t.Errorf("player %s projected points = %v, want %v", entry.PlayerID, entry.ProjectedPoints, want.projected)
			}
			if entry.Starter != want.starter || entry.Slot != want.slot {
				t.Errorf("player %s starter/slot = %v/%s, want %v/%s", entry.PlayerID, entry.Starter, entry.Slot, want.starter, want.slot)
			}
		}
	}
	for playerID := range expected {
		if !seen[playerID] {
			t.Errorf("player %s missing from lineups", playerID)
		}
	}
}

func assertStartersSumToMatchupScore(t *testing.T, data fantasy.LeagueData, period fantasy.MatchupPeriod) {
	t.Helper()
	for _, matchup := range data.Matchups {
		if matchup.Period != period {
			continue
		}
		for _, side := range []*fantasy.MatchupSide{&matchup.TeamA, matchup.TeamB} {
			if side == nil {
				continue
			}
			lineup, exists := lineupForTeam(data.Lineups, matchup.Period, matchup.ExternalID, side.TeamID)
			if !exists || side.Score == nil {
				t.Fatalf("missing lineup or matchup score for team %s", side.TeamID)
			}
			var starterPoints float64
			for _, entry := range lineup.Entries {
				if entry.Starter {
					if entry.Points == nil {
						t.Fatalf("starter %s has unknown points", entry.PlayerID)
					}
					starterPoints += *entry.Points
				}
			}
			if starterPoints != *side.Score {
				t.Errorf("team %s starter sum %.2f != matchup score %.2f", side.TeamID, starterPoints, *side.Score)
			}
		}
	}
}
