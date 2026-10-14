package espn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/news"
)

func TestParsePlayerNewsFiltersAndMapsFeed(t *testing.T) {
	body, err := os.ReadFile("testdata/player_news.json")
	if err != nil {
		t.Fatal(err)
	}
	items, err := parsePlayerNews(body, map[string]string{
		"3916387": "league-player-jackson",
		"4241389": "league-player-lamb",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0].PlayerID != "league-player-jackson" || items[0].SourceItemID != "64220528" ||
		items[0].SourceLabel != "RotoWire via ESPN" || items[0].URL != "https://www.espn.com/nfl/player/_/id/3916387" ||
		items[0].Headline == "" || items[0].Summary == "" || !items[0].PublishedAt.Equal(time.Date(2026, 10, 12, 19, 13, 43, 0, time.UTC)) {
		t.Fatalf("first item = %+v", items[0])
	}
	if items[1].PlayerID != "league-player-lamb" || items[1].SourceItemID != "64150328" {
		t.Fatalf("second item = %+v", items[1])
	}
}

func TestPlayerNewsRepeatsPlayerIDsAndBatchesAt25(t *testing.T) {
	var requests [][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Query()["playerId"])
		if got := r.URL.Query().Get("limit"); got != "200" {
			t.Errorf("limit = %q, want 200", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"feed":[]}`))
	}))
	defer server.Close()
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: redirectTransport{target: serverURL}}
	players := make([]news.Player, 26)
	wantIDs := make([]string, len(players))
	for i := range players {
		wantIDs[i] = strconv.Itoa(1000 + i)
		players[i] = news.Player{ID: "player-" + wantIDs[i], ExternalIDs: map[string]string{"espn": wantIDs[i], "sleeper": "sleeper-" + wantIDs[i]}}
	}

	items, err := (NewsSource{HTTP: client}).PlayerNews(context.Background(), players)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 || len(requests) != 2 {
		t.Fatalf("items = %d, requests = %d; want no items and 2 batches", len(items), len(requests))
	}
	if !reflect.DeepEqual(requests[0], wantIDs[:25]) || !reflect.DeepEqual(requests[1], wantIDs[25:]) {
		t.Fatalf("repeated playerId params = %v; want first 25 and final ID", requests)
	}
}

func TestPlayerNewsDoesNotRequestUnknownPlayers(t *testing.T) {
	items, err := (NewsSource{}).PlayerNews(context.Background(), []news.Player{{ID: "only-sleeper", ExternalIDs: map[string]string{"sleeper": "s1"}}})
	if err != nil || len(items) != 0 {
		t.Fatalf("items = %v, err = %v; want no request and no items", items, err)
	}
}

type redirectTransport struct {
	target *url.URL
}

func (t redirectTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	redirected := request.Clone(request.Context())
	redirected.URL.Scheme = t.target.Scheme
	redirected.URL.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(redirected)
}
