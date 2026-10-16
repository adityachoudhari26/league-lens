package sleeper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var file string
		switch r.URL.Path {
		case "/user/test":
			_, _ = w.Write([]byte(`{"user_id":"owner"}`))
			return
		case "/league/123":
			file = "league.json"
		case "/league/123/rosters":
			file = "rosters.json"
		case "/league/123/users":
			_, _ = w.Write([]byte(`[{"user_id":"owner","display_name":"Owner","metadata":{"team_name":"My Team"}}]`))
			return
		case "/state/nfl":
			_, _ = w.Write([]byte(`{"season":"2026","season_type":"regular","week":2}`))
			return
		case "/league/123/matchups/1", "/league/123/matchups/2":
			file = "week.json"
		case "/projections/nfl/2026/1":
			query := r.URL.Query()
			if query.Get("season_type") != "regular" || len(query["position[]"]) != len(projectionPositions) {
				t.Errorf("projections query = %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"player_id":"10","stats":{"rec":2}}]`))
			return
		case "/projections/nfl/2026/2":
			w.WriteHeader(http.StatusInternalServerError)
			return
		default:
			http.NotFound(w, r)
			return
		}
		b, err := os.ReadFile("testdata/" + file)
		if err != nil {
			t.Error(err)
		}
		_, _ = w.Write(b)
	}))
	defer srv.Close()
	c := NewClient(srv.Client())
	c.BaseURL = srv.URL
	c.ProjectionsURL = srv.URL + "/projections/nfl"
	head, err := c.Header(context.Background(), "test", "123")
	if err != nil {
		t.Fatal(err)
	}
	data, err := c.Fetch(context.Background(), head, fantasy.FetchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Rosters) != 2 || len(data.Weeks) != 2 || data.Period != 2 {
		t.Fatalf("unexpected data: %+v", data)
	}
	if _, failed := data.Projections[2]; failed || len(data.Projections[1]) != 1 || data.Projections[1][0].Stats["rec"] != 2 {
		t.Fatalf("projections = %+v, want week 1 only", data.Projections)
	}
	league, err := c.DescribeLeague(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	account, selection, err := c.ResolveSelection(context.Background(), "test", league)
	if err != nil || account != "owner" || len(selection.SelectedTeamIDs) != 1 || selection.SelectedTeamIDs[0] != "1" {
		t.Fatalf("selection failed: %v", err)
	}
	payload, err := c.FetchLeague(context.Background(), league, fantasy.FetchRequest{})
	if err != nil || len(payload.Teams) != 2 || len(payload.Matchups) != 2 {
		t.Fatalf("contract fetch failed: %v", err)
	}
	if data.Weeks[1][0].CustomPoints == nil || *data.Weeks[1][0].CustomPoints != 0 {
		t.Fatal("zero score override lost")
	}
}

// Weeks the request marks as settled are not read at all, so their matchups,
// lineups, projections, and complete-period marks are absent from the payload.
// The current period is fetched even when listed in SkipWeeks.
func TestFetchSkipsSettledWeeks(t *testing.T) {
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		var file string
		switch r.URL.Path {
		case "/user/test":
			_, _ = w.Write([]byte(`{"user_id":"owner"}`))
			return
		case "/league/123":
			file = "league.json"
		case "/league/123/rosters":
			file = "rosters.json"
		case "/league/123/users":
			_, _ = w.Write([]byte(`[{"user_id":"owner","display_name":"Owner","metadata":{"team_name":"My Team"}}]`))
			return
		case "/state/nfl":
			_, _ = w.Write([]byte(`{"season":"2026","season_type":"regular","week":4}`))
			return
		case "/league/123/matchups/1", "/league/123/matchups/2", "/league/123/matchups/3", "/league/123/matchups/4":
			file = "week.json"
		default:
			if strings.HasPrefix(r.URL.Path, "/projections/nfl/2026/") {
				_, _ = w.Write([]byte(`[{"player_id":"10","stats":{"rec":2}}]`))
				return
			}
			http.NotFound(w, r)
			return
		}
		b, err := os.ReadFile("testdata/" + file)
		if err != nil {
			t.Error(err)
		}
		_, _ = w.Write(b)
	}))
	defer srv.Close()
	c := NewClient(srv.Client())
	c.BaseURL = srv.URL
	c.ProjectionsURL = srv.URL + "/projections/nfl"
	head, err := c.Header(context.Background(), "test", "123")
	if err != nil {
		t.Fatal(err)
	}
	forbidden := func() {
		t.Helper()
		for _, path := range requested {
			if strings.HasSuffix(path, "/matchups/1") || strings.HasSuffix(path, "/matchups/2") ||
				path == "/projections/nfl/2026/1" || path == "/projections/nfl/2026/2" {
				t.Fatalf("settled week requested: %s (all: %v)", path, requested)
			}
		}
	}
	requested = nil
	data, err := c.Fetch(context.Background(), head, fantasy.FetchRequest{SkipWeeks: []fantasy.ScoringPeriod{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	forbidden()
	if len(data.Weeks) != 2 || data.Weeks[3] == nil || data.Weeks[4] == nil {
		t.Fatalf("weeks = %v, want 3 and 4", data.Weeks)
	}
	if len(data.Projections) != 2 || data.Projections[3] == nil || data.Projections[4] == nil {
		t.Fatalf("projections = %v, want weeks 3 and 4", data.Projections)
	}

	league, err := c.DescribeLeague(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := c.FetchLeague(context.Background(), league, fantasy.FetchRequest{SkipWeeks: []fantasy.ScoringPeriod{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range payload.Matchups {
		if m.Period < 3 {
			t.Fatalf("skipped week matchup in payload: %+v", m)
		}
	}
	for _, l := range payload.Lineups {
		if l.Period < 3 {
			t.Fatalf("skipped week lineup in payload: %+v", l)
		}
	}
	for _, r := range payload.Rosters {
		if r.Origin == fantasy.SnapshotHistory && r.ScoringPeriod < 3 {
			t.Fatalf("skipped week history in payload: %+v", r)
		}
	}
	if len(payload.CompleteMatchupPeriods) != 2 || payload.CompleteMatchupPeriods[0] != 3 || payload.CompleteMatchupPeriods[1] != 4 {
		t.Fatalf("complete periods = %v, want [3 4]", payload.CompleteMatchupPeriods)
	}

	// Listing the current week changes nothing: it is always read.
	requested = nil
	data, err = c.Fetch(context.Background(), head, fantasy.FetchRequest{SkipWeeks: []fantasy.ScoringPeriod{1, 2, 4}})
	if err != nil {
		t.Fatal(err)
	}
	forbidden()
	if data.Weeks[4] == nil {
		t.Fatal("current week skipped by request")
	}
}

func TestRejectBadResponses(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `not json`, `{"league_id":"wrong"}`} {
		t.Run(body, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer s.Close()
			c := NewClient(s.Client())
			c.BaseURL = s.URL
			if _, err := c.Header(context.Background(), "test", "123"); err == nil {
				t.Fatal("accepted missing/invalid user")
			}
		})
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429) }))
	defer s.Close()
	c := NewClient(s.Client())
	c.BaseURL = s.URL
	if _, err := c.Header(context.Background(), "test", "123"); err == nil {
		t.Fatal("accepted rate limit")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Header(ctx, "test", "123"); err == nil {
		t.Fatal("ignored cancellation")
	}
}
