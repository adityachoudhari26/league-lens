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
