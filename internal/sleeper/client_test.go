package sleeper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var file string
		switch r.URL.Path {
		case "/user/test":
			w.Write([]byte(`{"user_id":"owner"}`))
			return
		case "/league/123":
			file = "league.json"
		case "/league/123/rosters":
			file = "rosters.json"
		case "/league/123/users":
			w.Write([]byte(`[{"user_id":"owner","display_name":"Owner","metadata":{"team_name":"My Team"}}]`))
			return
		case "/state/nfl":
			w.Write([]byte(`{"season":"2026","season_type":"regular","week":2}`))
			return
		case "/league/123/matchups/1", "/league/123/matchups/2":
			file = "week.json"
		case "/projections/nfl/2026/1":
			query := r.URL.Query()
			if query.Get("season_type") != "regular" || len(query["position[]"]) != len(projectionPositions) {
				t.Errorf("projections query = %s", r.URL.RawQuery)
			}
			w.Write([]byte(`[{"player_id":"10","stats":{"rec":2}}]`))
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
		w.Write(b)
	}))
	defer srv.Close()
	c := NewClient(srv.Client())
	c.BaseURL = srv.URL
	c.ProjectionsURL = srv.URL + "/projections/nfl"
	head, err := c.Header(context.Background(), "test", "123")
	if err != nil {
		t.Fatal(err)
	}
	data, err := c.Fetch(context.Background(), head)
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
	payload, err := c.FetchLeague(context.Background(), league)
	if err != nil || len(payload.Teams) != 2 || len(payload.Matchups) != 2 {
		t.Fatalf("contract fetch failed: %v", err)
	}
	if data.Weeks[1][0].CustomPoints == nil || *data.Weeks[1][0].CustomPoints != 0 {
		t.Fatal("zero score override lost")
	}
}

func TestRejectBadResponses(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `not json`, `{"league_id":"wrong"}`} {
		t.Run(body, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
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
