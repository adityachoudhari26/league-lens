package sleeper

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func TestDiscoverLeaguesUsesOwnedRosters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/user/Example":
			_, _ = w.Write([]byte(`{"user_id":"synthetic-user"}`))
		case "/v1/user/synthetic-user/leagues/nfl/2026":
			_, _ = w.Write([]byte(`[{"league_id":"league-1","name":"League One","season":"2026","sport":"nfl"}]`))
		case "/v1/league/league-1/rosters":
			_, _ = w.Write([]byte(`[{"roster_id":2,"owner_id":"another-user"},{"roster_id":7,"owner_id":"synthetic-user","co_owners":[]}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(server.Client())
	client.BaseURL = server.URL + "/v1"

	accountID, leagues, err := client.DiscoverLeagues(context.Background(), "Example", 2026)
	if err != nil {
		t.Fatal(err)
	}
	if accountID != "synthetic-user" {
		t.Fatalf("account ID = %q", accountID)
	}
	want := fantasy.DiscoveredLeague{
		Ref:      fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "league-1", Season: 2026},
		Name:     "League One",
		TeamID:   "7",
		TeamName: "",
	}
	if len(leagues) != 1 || leagues[0] != want {
		t.Fatalf("leagues = %+v, want %+v", leagues, []fantasy.DiscoveredLeague{want})
	}
}

func TestDiscoverLeaguesReturnsNotFoundForNullUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("null"))
	}))
	defer server.Close()
	client := NewClient(server.Client())
	client.BaseURL = server.URL + "/v1"

	_, _, err := client.DiscoverLeagues(context.Background(), "unknown", 2026)
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("error = %v, want ErrUserNotFound", err)
	}
}
