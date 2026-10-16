package espn

import (
	"context"
	"encoding/json"
	"errors"
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
		if err := json.NewEncoder(w).Encode(v); err != nil {
			t.Error(err)
		}
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
	data, err := c.FetchLeague(context.Background(), league, fantasy.FetchRequest{})
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

// A sync that already holds weeks 1-2 as settled must not re-read them: the
// only per-week request is the correction window (week 3), while matchups still
// come from the season schedule in the current-week response.
func TestSkipsSettledWeeks(t *testing.T) {
	var requested []string
	c := fixtureClient(t, func(v map[string]any, r *http.Request) {
		if week := r.URL.Query().Get("scoringPeriodId"); week != "" {
			requested = append(requested, week)
			return
		}
		v["scoringPeriodId"] = 4
		v["status"].(map[string]any)["latestScoringPeriod"] = 4
	})
	league, err := c.DescribeLeague(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	data, err := c.FetchLeague(context.Background(), league, fantasy.FetchRequest{SkipWeeks: []fantasy.ScoringPeriod{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(requested) != 1 || requested[0] != "3" {
		t.Fatalf("per-week requests = %v, want only week 3", requested)
	}
	history := map[fantasy.ScoringPeriod]bool{}
	observed := 0
	for _, r := range data.Rosters {
		if r.Origin == fantasy.SnapshotHistory {
			history[r.ScoringPeriod] = true
		} else {
			observed++
		}
	}
	if len(history) != 1 || !history[3] || observed != 2 {
		t.Fatalf("roster history = %v observed %d, want week 3 only", history, observed)
	}
	if len(data.Matchups) != 2 {
		t.Fatalf("matchups = %d, want the full season schedule", len(data.Matchups))
	}
}

// DescribeLeague is the light describe used before every sync: settings and
// teams only, not the five views a full fetch needs.
func TestDescribeLeagueRequestsMinimalViews(t *testing.T) {
	var views []string
	c := fixtureClient(t, func(_ map[string]any, r *http.Request) { views = r.URL.Query()["view"] })
	if _, err := c.DescribeLeague(context.Background(), "123"); err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 || views[0] != "mSettings" || views[1] != "mTeam" {
		t.Fatalf("describe views = %v, want [mSettings mTeam]", views)
	}
}

func TestFetchLeagueUsesLiveTotalsForUndecidedCurrentWeek(t *testing.T) {
	c := fixtureClient(t, func(v map[string]any, r *http.Request) {
		if r.URL.Query().Get("scoringPeriodId") != "" {
			return
		}
		var includesMatchupScore bool
		for _, view := range r.URL.Query()["view"] {
			if view == "mMatchupScore" {
				includesMatchupScore = true
				break
			}
		}
		for _, item := range v["schedule"].([]any) {
			matchup := item.(map[string]any)
			if matchup["matchupPeriodId"] != float64(2) {
				continue
			}
			home := matchup["home"].(map[string]any)
			away := matchup["away"].(map[string]any)
			home["totalPoints"] = 0.0
			away["totalPoints"] = 0.0
			delete(home, "totalPointsLive")
			delete(away, "totalPointsLive")
			if includesMatchupScore {
				home["totalPointsLive"] = 11.0
				away["totalPointsLive"] = 13.6
			}
			return
		}
		t.Fatal("current matchup not found")
	})
	league, err := c.DescribeLeague(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	data, err := c.FetchLeague(context.Background(), league, fantasy.FetchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, matchup := range data.Matchups {
		if matchup.ExternalID != "2" {
			continue
		}
		var scoreA, scoreB float64
		if matchup.TeamA.Score != nil {
			scoreA = *matchup.TeamA.Score
		}
		if matchup.TeamB != nil && matchup.TeamB.Score != nil {
			scoreB = *matchup.TeamB.Score
		}
		if scoreA != 11.0 || scoreB != 13.6 {
			t.Fatalf("current matchup scores = %.1f and %.1f, want 11.0 and 13.6", scoreA, scoreB)
		}
		return
	}
	t.Fatal("current matchup not returned")
}

func TestRejectsWrongHistoricalWeek(t *testing.T) {
	c := fixtureClient(t, func(v map[string]any, r *http.Request) { v["scoringPeriodId"] = 3 })
	league, err := c.DescribeLeague(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.FetchLeague(context.Background(), league, fantasy.FetchRequest{}); err == nil {
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
	data, err := c.FetchLeague(context.Background(), league, fantasy.FetchRequest{})
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
				_, _ = w.Write([]byte("private-token"))
			}))
			defer server.Close()
			c := NewClient(server.Client(), Credentials{S2: "private-token", SWID: "{account}"}, 2026)
			c.baseURL = server.URL
			_, err := c.DescribeLeague(context.Background(), "123")
			if err == nil || strings.Contains(err.Error(), "private-token") || reached {
				t.Fatalf("unsafe failure: %v, redirected %v", err, reached)
			}
			if code == http.StatusUnauthorized && !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("401 error does not wrap ErrUnauthorized: %v", err)
			}
		})
	}
}

func TestPartialScheduleCannotDeleteStoredPairings(t *testing.T) {
	c := fixtureClient(t, func(v map[string]any, r *http.Request) { v["schedule"].([]any)[0].(map[string]any)["away"] = nil })
	league, _ := c.DescribeLeague(context.Background(), "123")
	data, err := c.FetchLeague(context.Background(), league, fantasy.FetchRequest{})
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
	data, err := c.FetchLeague(context.Background(), league, fantasy.FetchRequest{})
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
	if _, err := c.DescribeLeague(ctx, "123"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestDecidedCurrentPeriodIsFinal(t *testing.T) {
	c := fixtureClient(t, func(v map[string]any, r *http.Request) {
		v["settings"].(map[string]any)["scheduleSettings"].(map[string]any)["matchupPeriods"].(map[string]any)["2"] = []any{3}
		v["schedule"].([]any)[1].(map[string]any)["winner"] = "TIE"
	})
	league, _ := c.DescribeLeague(context.Background(), "123")
	data, err := c.FetchLeague(context.Background(), league, fantasy.FetchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if data.Matchups[1].Status != fantasy.MatchupFinal {
		t.Fatal("decided current matchup was not final")
	}
}

func TestNewClientRestoresSWIDBraces(t *testing.T) {
	braced := NewClient(nil, Credentials{S2: "private-token", SWID: "{ACCOUNT}"}, 2026)
	bare := NewClient(nil, Credentials{S2: "private-token", SWID: " ACCOUNT "}, 2026)
	if bare.credentials.SWID != "{ACCOUNT}" {
		t.Fatalf("SWID = %q, want braces restored", bare.credentials.SWID)
	}
	if bare.AccountID() != braced.AccountID() {
		t.Fatal("bare and braced SWID must identify the same account")
	}
}
