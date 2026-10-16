package sleeper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
)

func TestTrendingPlayers(t *testing.T) {
	fixture, err := os.ReadFile("testdata/trending_add.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/players/nfl/trending/add" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("lookback_hours") != "24" || r.URL.Query().Get("limit") != "10" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.BaseURL = server.URL
	rows, err := client.TrendingPlayers(context.Background(), "add", 24, 10)
	if err != nil {
		t.Fatal(err)
	}
	expected := []TrendingPlayer{
		{PlayerID: "9230", Count: 1058240},
		{PlayerID: "8129", Count: 498184},
		{PlayerID: "1166", Count: 375344},
		{PlayerID: "650", Count: 351540},
		{PlayerID: "13150", Count: 333571},
		{PlayerID: "96", Count: 210744},
		{PlayerID: "8132", Count: 196375},
		{PlayerID: "9758", Count: 183260},
		{PlayerID: "9480", Count: 179739},
		{PlayerID: "11439", Count: 170795},
	}
	if len(rows) != len(expected) {
		t.Fatalf("got %d rows, want %d", len(rows), len(expected))
	}
	for index := range expected {
		if rows[index] != expected[index] {
			t.Errorf("row %d = %+v, want %+v", index, rows[index], expected[index])
		}
	}
}

func TestTrendingPlayersRejectsInvalidArgumentsWithoutRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	client := NewClient(server.Client())
	client.BaseURL = server.URL

	tests := []struct {
		name          string
		kind          string
		lookbackHours int
		limit         int
	}{
		{name: "kind", kind: "foo", lookbackHours: 24, limit: 10},
		{name: "zero hours", kind: "add", lookbackHours: 0, limit: 10},
		{name: "too many hours", kind: "add", lookbackHours: 169, limit: 10},
		{name: "zero limit", kind: "add", lookbackHours: 24, limit: 0},
		{name: "too many results", kind: "add", lookbackHours: 24, limit: 51},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := client.TrendingPlayers(
				context.Background(), test.kind, test.lookbackHours, test.limit,
			)
			if err == nil {
				t.Fatal("expected invalid arguments to fail")
			}
		})
	}
	if requests.Load() != 0 {
		t.Fatalf("made %d requests for invalid arguments", requests.Load())
	}
}
