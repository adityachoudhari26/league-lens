package sleeper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func TestAnnotateAvailabilityLooksUpSelectedTeamPlayers(t *testing.T) {
	var lookups atomic.Int32
	players := map[string]string{
		"1": `{"team":"KC","injury_status":"Out"}`,
		"2": `{"team":"BUF","injury_status":null}`,
		"3": `{"team":null,"injury_status":"NA"}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lookups.Add(1)
		body, found := players[strings.TrimPrefix(r.URL.Path, "/players/nfl/")]
		if !found {
			_, _ = w.Write([]byte("null"))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), BaseURL: server.URL, AvailabilityTeamIDs: []string{"7"}}

	data := fantasy.LeagueData{Rosters: []fantasy.RosterSnapshot{
		{TeamID: "7", Origin: fantasy.SnapshotObserved, Entries: []fantasy.RosterEntry{{PlayerID: "1"}, {PlayerID: "2"}, {PlayerID: "3"}, {PlayerID: "missing"}, {PlayerID: "1"}}},
		{TeamID: "7", Origin: fantasy.SnapshotHistory, Entries: []fantasy.RosterEntry{{PlayerID: "1"}}},
		{TeamID: "8", Origin: fantasy.SnapshotObserved, Entries: []fantasy.RosterEntry{{PlayerID: "2"}}},
	}}
	client.annotateAvailability(context.Background(), &data)

	want := []fantasy.RosterEntry{
		{PlayerID: "1", InjuryStatus: fantasy.InjuryOut, ProTeam: "KC"},
		{PlayerID: "2", InjuryStatus: fantasy.InjuryActive, ProTeam: "BUF"},
		{PlayerID: "3", InjuryStatus: fantasy.InjuryUnknown},
		{PlayerID: "missing", InjuryStatus: fantasy.InjuryUnknown},
		{PlayerID: "1", InjuryStatus: fantasy.InjuryOut, ProTeam: "KC"},
	}
	for i, entry := range data.Rosters[0].Entries {
		if entry.InjuryStatus != want[i].InjuryStatus || entry.ProTeam != want[i].ProTeam {
			t.Fatalf("entry %d = %+v, want %+v", i, entry, want[i])
		}
	}
	for _, untouched := range []fantasy.RosterEntry{data.Rosters[1].Entries[0], data.Rosters[2].Entries[0]} {
		if untouched.InjuryStatus != "" || untouched.ProTeam != "" {
			t.Fatalf("history or unselected entry annotated: %+v", untouched)
		}
	}
	if got := lookups.Load(); got != 4 {
		t.Fatalf("lookups = %d, want 4 (one per distinct selected player)", got)
	}
}

func TestAnnotateAvailabilityToleratesLookupFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), BaseURL: server.URL, AvailabilityTeamIDs: []string{"7"}}

	data := fantasy.LeagueData{Rosters: []fantasy.RosterSnapshot{{TeamID: "7", Origin: fantasy.SnapshotObserved, Entries: []fantasy.RosterEntry{{PlayerID: "1"}}}}}
	client.annotateAvailability(context.Background(), &data)
	if entry := data.Rosters[0].Entries[0]; entry.InjuryStatus != fantasy.InjuryUnknown || entry.ProTeam != "" {
		t.Fatalf("entry = %+v, want unknown availability", entry)
	}
}

func TestAnnotateAvailabilityDisabledWithoutTeams(t *testing.T) {
	client := &Client{BaseURL: "http://unused.invalid"}
	data := fantasy.LeagueData{Rosters: []fantasy.RosterSnapshot{{TeamID: "7", Origin: fantasy.SnapshotObserved, Entries: []fantasy.RosterEntry{{PlayerID: "1"}}}}}
	client.annotateAvailability(context.Background(), &data)
	if entry := data.Rosters[0].Entries[0]; entry.InjuryStatus != "" {
		t.Fatalf("entry = %+v, want no lookup", entry)
	}
}
