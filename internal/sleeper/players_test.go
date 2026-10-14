package sleeper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func TestAnnotateAvailabilityUsesDailyDirectory(t *testing.T) {
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/players/nfl" {
			http.NotFound(w, r)
			return
		}
		downloads.Add(1)
		_, _ = w.Write([]byte(`{"1":{"team":"KC","injury_status":"Out","full_name":"A"},"2":{"team":"BUF","injury_status":null},"3":{"team":null,"injury_status":"NA"}}`))
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), BaseURL: server.URL, PlayersCachePath: filepath.Join(t.TempDir(), "players.json")}

	for range 2 {
		data := fantasy.LeagueData{Rosters: []fantasy.RosterSnapshot{
			{Origin: fantasy.SnapshotObserved, Entries: []fantasy.RosterEntry{{PlayerID: "1"}, {PlayerID: "2"}, {PlayerID: "3"}, {PlayerID: "missing"}}},
			{Origin: fantasy.SnapshotHistory, Entries: []fantasy.RosterEntry{{PlayerID: "1"}}},
		}}
		client.annotateAvailability(context.Background(), &data)

		observed := data.Rosters[0].Entries
		want := []fantasy.RosterEntry{
			{PlayerID: "1", InjuryStatus: fantasy.InjuryOut, ProTeam: "KC"},
			{PlayerID: "2", InjuryStatus: fantasy.InjuryActive, ProTeam: "BUF"},
			{PlayerID: "3", InjuryStatus: fantasy.InjuryUnknown},
			{PlayerID: "missing", InjuryStatus: fantasy.InjuryUnknown},
		}
		for i := range want {
			if observed[i].InjuryStatus != want[i].InjuryStatus || observed[i].ProTeam != want[i].ProTeam {
				t.Fatalf("entry %d = %+v, want %+v", i, observed[i], want[i])
			}
		}
		if history := data.Rosters[1].Entries[0]; history.InjuryStatus != "" || history.ProTeam != "" {
			t.Fatalf("history entry annotated: %+v", history)
		}
	}
	if got := downloads.Load(); got != 1 {
		t.Fatalf("directory downloads = %d, want 1 (cached)", got)
	}
}

func TestAnnotateAvailabilityToleratesDirectoryFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), BaseURL: server.URL, PlayersCachePath: filepath.Join(t.TempDir(), "players.json")}

	data := fantasy.LeagueData{Rosters: []fantasy.RosterSnapshot{{Origin: fantasy.SnapshotObserved, Entries: []fantasy.RosterEntry{{PlayerID: "1"}}}}}
	client.annotateAvailability(context.Background(), &data)
	if entry := data.Rosters[0].Entries[0]; entry.InjuryStatus != "" {
		t.Fatalf("entry = %+v, want availability omitted", entry)
	}
}
