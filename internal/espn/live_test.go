package espn

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func TestFetchLiveMatchup(t *testing.T) {
	body, err := os.ReadFile("testdata/live_matchup_week5.json")
	if err != nil {
		t.Fatal(err)
	}
	client := liveMatchupFixtureClient(t, body, func(request *http.Request) {
		if got := request.URL.Query()["view"]; len(got) != 2 || got[0] != "mMatchupScore" || got[1] != "mBoxscore" {
			t.Errorf("views = %v, want mMatchupScore and mBoxscore", got)
		}
		if got := request.URL.Query().Get("scoringPeriodId"); got != "5" {
			t.Errorf("scoringPeriodId = %q, want 5", got)
		}
		cookies := request.Cookies()
		if len(cookies) != 2 || cookies[0].Name != "espn_s2" || cookies[0].Value != "test-s2" ||
			cookies[1].Name != "SWID" || cookies[1].Value != "{test-swid}" {
			t.Errorf("request cookies = %v, want the configured ESPN credentials", cookies)
		}
		var filter struct {
			Schedule struct {
				Periods struct {
					Value []int `json:"value"`
				} `json:"filterMatchupPeriodIds"`
				Teams struct {
					Value []int `json:"value"`
				} `json:"filterTeamIds"`
			} `json:"schedule"`
		}
		if err := json.Unmarshal([]byte(request.Header.Get("x-fantasy-filter")), &filter); err != nil {
			t.Errorf("decode x-fantasy-filter: %v", err)
			return
		}
		if len(filter.Schedule.Periods.Value) != 1 || filter.Schedule.Periods.Value[0] != 5 ||
			len(filter.Schedule.Teams.Value) != 1 || filter.Schedule.Teams.Value[0] != 4 {
			t.Errorf("x-fantasy-filter = %+v, want period 5/team 4", filter)
		}
	})
	league := fantasy.League{Ref: fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "1002879736", Season: 2026}}
	live, err := client.FetchLiveMatchup(context.Background(), league, fantasy.LiveMatchupRequest{
		Week:   5,
		Period: 5,
		TeamID: "4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if live.Matchup.ExternalID != "21" || live.Matchup.Period != 5 || live.Matchup.Status != fantasy.MatchupFinal {
		t.Fatalf("matchup = %+v", live.Matchup)
	}
	if live.Matchup.TeamA.TeamID != "4" || live.Matchup.TeamA.Score == nil || *live.Matchup.TeamA.Score != 133.24 ||
		live.Matchup.TeamB == nil || live.Matchup.TeamB.TeamID != "2" || live.Matchup.TeamB.Score == nil || *live.Matchup.TeamB.Score != 175.84 {
		t.Fatalf("matchup scores = %+v", live.Matchup)
	}
	if len(live.Lineups) != 2 {
		t.Fatalf("got %d lineups, want both matchup sides", len(live.Lineups))
	}
	lineups := map[string]fantasy.MatchupLineup{}
	for _, lineup := range live.Lineups {
		lineups[lineup.TeamID] = lineup
		starters := 0
		for _, entry := range lineup.Entries {
			if entry.Starter {
				starters++
			}
		}
		if starters == 0 {
			t.Errorf("lineup for team %s has no starters", lineup.TeamID)
		}
	}
	if len(lineups["4"].Entries) != 18 || len(lineups["2"].Entries) != 18 {
		t.Fatalf("lineup entry counts = %d/%d, want 18/18", len(lineups["4"].Entries), len(lineups["2"].Entries))
	}
	assertESPNLiveEntry(t, lineups["4"], "4362628", 19.7, 1, 0)
	assertESPNLiveEntry(t, lineups["2"], "4567104", 16, 0, 3)
	assertESPNLiveEntry(t, lineups["4"], "4249087", 4, 0, 1)
	assertESPNLiveEntry(t, lineups["2"], "-16007", 14, 0, 0)
	if entry := findESPNLineupEntry(lineups, "4695883"); entry == nil || entry.Scoring != nil {
		t.Errorf("no-stat-line player scoring = %+v, want nil", entry)
	}
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

func TestFetchLiveMatchupReturnsErrorWhenNoMatchupFound(t *testing.T) {
	body, err := os.ReadFile("testdata/live_matchup_week5.json")
	if err != nil {
		t.Fatal(err)
	}
	client := liveMatchupFixtureClient(t, body, nil)
	league := fantasy.League{Ref: fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "1002879736", Season: 2026}}
	if _, err := client.FetchLiveMatchup(context.Background(), league, fantasy.LiveMatchupRequest{
		Week: 5, Period: 5, TeamID: "99",
	}); err == nil {
		t.Fatal("missing matchup did not return an error")
	}
}

func liveMatchupFixtureClient(t *testing.T, body []byte, checkRequest func(*http.Request)) *Client {
	t.Helper()
	client := NewClient(&http.Client{Transport: scoreboardRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/apis/v3/games/ffl/seasons/2026/segments/0/leagues/1002879736" {
			t.Errorf("request path = %s", request.URL.Path)
		}
		if checkRequest != nil {
			checkRequest(request)
		}
		return scoreboardHTTPResponse(request, http.StatusOK, string(body)), nil
	})}, Credentials{S2: "test-s2", SWID: "test-swid"}, 2026)
	client.baseURL = "https://example.test/apis/v3/games/ffl"
	return client
}

func assertESPNLiveEntry(t *testing.T, lineup fantasy.MatchupLineup, playerID string, points float64, touchdowns, fieldGoals int) {
	t.Helper()
	entry := findESPNLineupEntry(map[string]fantasy.MatchupLineup{lineup.TeamID: lineup}, playerID)
	if entry == nil {
		t.Fatalf("player %s not found in lineup for team %s", playerID, lineup.TeamID)
	}
	if entry.Points == nil || *entry.Points != points {
		t.Errorf("player %s points = %v, want %v", playerID, entry.Points, points)
	}
	if entry.Scoring == nil || entry.Scoring.Touchdowns != touchdowns || entry.Scoring.FieldGoals != fieldGoals {
		t.Errorf("player %s scoring = %+v, want touchdowns/field goals %d/%d", playerID, entry.Scoring, touchdowns, fieldGoals)
	}
}

func findESPNLineupEntry(lineups map[string]fantasy.MatchupLineup, playerID string) *fantasy.LineupEntry {
	for _, lineup := range lineups {
		for index := range lineup.Entries {
			if lineup.Entries[index].PlayerID == playerID {
				return &lineup.Entries[index]
			}
		}
	}
	return nil
}
