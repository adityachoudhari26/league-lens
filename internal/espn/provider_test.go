package espn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func fixture(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile("testdata/league.json")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err = json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func fixtureClient(t *testing.T, change func(map[string]any, *http.Request)) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/seasons/2026/segments/0/leagues/123" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		cookie, err := r.Cookie("espn_s2")
		if err != nil || cookie.Value != "private-token" {
			t.Error("missing authentication")
		}
		v := fixture(t)
		if week := r.URL.Query().Get("scoringPeriodId"); week != "" {
			n, _ := strconv.Atoi(week)
			v["scoringPeriodId"] = n
		}
		if change != nil {
			change(v, r)
		}
		json.NewEncoder(w).Encode(v)
	}))
	t.Cleanup(server.Close)
	c := NewClient(server.Client(), Credentials{S2: "private-token", SWID: "{account}"}, 2026)
	c.baseURL = server.URL
	return c
}
func TestFetchNormalizedLeague(t *testing.T) {
	c := fixtureClient(t, nil)
	league, err := c.DescribeLeague(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	data, err := c.FetchLeague(context.Background(), league)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Teams) != 2 || len(data.Players) != 2 || len(data.Rosters) != 6 || len(data.Matchups) != 2 {
		t.Fatalf("unexpected counts: %d %d %d %d", len(data.Teams), len(data.Players), len(data.Rosters), len(data.Matchups))
	}
	if len(data.League.Settings.ScoringRules) != 1 {
		t.Fatal("position-specific scoring must remain supplemental")
	}
	m := data.Matchups[0]
	if len(m.ScoringPeriods) != 2 || m.ScoringPeriods[1] != 2 || m.TeamA.Score == nil || *m.TeamA.Score != 0 || m.Status != fantasy.MatchupFinal {
		t.Fatalf("bad multiweek matchup: %+v", m)
	}
	if data.Matchups[1].Status != fantasy.MatchupUnknown {
		t.Fatal("undecided winner does not establish whether games have started")
	}
	for _, r := range data.Rosters {
		if r.Origin == fantasy.SnapshotHistory && r.Record != nil {
			t.Fatal("copied current record into history")
		}
	}
	found := false
	for _, p := range data.Players {
		if p.Kind == fantasy.PlayerTeamDefense {
			found = true
			if len(p.KnownIDs) != 1 || p.KnownIDs[0].ExternalID != "PHI" {
				t.Fatalf("bad defense alias: %+v", p)
			}
		}
	}
	if !found {
		t.Fatal("missing team defense")
	}
}
func TestRejectsWrongHistoricalWeek(t *testing.T) {
	c := fixtureClient(t, func(v map[string]any, r *http.Request) { v["scoringPeriodId"] = 3 })
	league, err := c.DescribeLeague(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.FetchLeague(context.Background(), league); err == nil {
		t.Fatal("accepted current roster as history")
	}
}
func TestUnavailableRosterIsNotEmptySnapshot(t *testing.T) {
	c := fixtureClient(t, func(v map[string]any, r *http.Request) {
		if r.URL.Query().Get("scoringPeriodId") == "1" {
			delete(v["teams"].([]any)[0].(map[string]any), "roster")
		}
	})
	league, _ := c.DescribeLeague(context.Background(), "123")
	data, err := c.FetchLeague(context.Background(), league)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Rosters) != 5 {
		t.Fatalf("invented missing roster: %d", len(data.Rosters))
	}
}
func TestRejectsIncompleteTeams(t *testing.T) {
	c := fixtureClient(t, func(v map[string]any, r *http.Request) { v["teams"] = v["teams"].([]any)[:1] })
	if _, err := c.DescribeLeague(context.Background(), "123"); err == nil {
		t.Fatal("accepted incomplete league")
	}
}
func TestHTTPFailuresDoNotLeakSecrets(t *testing.T) {
	for _, code := range []int{401, 403, 500, 200, 302} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			reached := false
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
			defer destination.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", destination.URL)
				w.WriteHeader(code)
				w.Write([]byte("private-token"))
			}))
			defer server.Close()
			c := NewClient(server.Client(), Credentials{S2: "private-token", SWID: "{account}"}, 2026)
			c.baseURL = server.URL
			_, err := c.DescribeLeague(context.Background(), "123")
			if err == nil || strings.Contains(err.Error(), "private-token") || reached {
				t.Fatalf("unsafe failure: %v, redirected %v", err, reached)
			}
		})
	}
}

func TestPartialScheduleCannotDeleteStoredPairings(t *testing.T) {
	c := fixtureClient(t, func(v map[string]any, r *http.Request) { v["schedule"].([]any)[0].(map[string]any)["away"] = nil })
	league, _ := c.DescribeLeague(context.Background(), "123")
	data, err := c.FetchLeague(context.Background(), league)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.CompleteMatchupPeriods) != 1 || data.CompleteMatchupPeriods[0] != 2 {
		t.Fatal("partial period incorrectly marked complete")
	}
	if data.Matchups[0].IsBye {
		t.Fatal("missing opponent is not a confirmed bye")
	}
}
func TestUnknownSlotAndEmptyRoster(t *testing.T) {
	c := fixtureClient(t, func(v map[string]any, r *http.Request) {
		teams := v["teams"].([]any)
		teams[0].(map[string]any)["roster"].(map[string]any)["entries"].([]any)[0].(map[string]any)["lineupSlotId"] = 999
		teams[1].(map[string]any)["roster"].(map[string]any)["entries"] = []any{}
	})
	league, _ := c.DescribeLeague(context.Background(), "123")
	data, err := c.FetchLeague(context.Background(), league)
	if err != nil {
		t.Fatal(err)
	}
	entry := data.Rosters[0].Entries[0]
	if entry.Slot != fantasy.SlotUnknown || entry.ProviderSlotID != "999" {
		t.Fatal("unknown slot lost")
	}
	if len(data.Rosters) != 6 || len(data.Rosters[1].Entries) != 0 || data.Rosters[1].Coverage != fantasy.CoverageFull {
		t.Fatal("explicit empty roster lost")
	}
}
func TestCancellation(t *testing.T) {
	c := fixtureClient(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.DescribeLeague(ctx, "123"); err != context.Canceled {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestDecidedCurrentPeriodIsFinal(t *testing.T) {
	c := fixtureClient(t, func(v map[string]any, r *http.Request) {
		v["settings"].(map[string]any)["scheduleSettings"].(map[string]any)["matchupPeriods"].(map[string]any)["2"] = []any{3}
		v["schedule"].([]any)[1].(map[string]any)["winner"] = "TIE"
	})
	league, _ := c.DescribeLeague(context.Background(), "123")
	data, err := c.FetchLeague(context.Background(), league)
	if err != nil {
		t.Fatal(err)
	}
	if data.Matchups[1].Status != fantasy.MatchupFinal {
		t.Fatal("decided current matchup was not final")
	}
}
