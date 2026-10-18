package espn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func TestDefenseVsPositionMapsESPNRatings(t *testing.T) {
	fixture := readESPNDefenseFixture(t)
	server := newESPNDefenseServer(t, http.StatusOK, fixture)
	defer server.Close()
	client := NewClient(server.Client(), Credentials{S2: "s2-test", SWID: "swid-test"}, 2026)
	client.baseURL = server.URL

	table, err := client.DefenseVsPosition(context.Background(), fantasy.League{
		Ref: fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "1002879736", Season: 2026},
	})
	if err != nil {
		t.Fatal(err)
	}
	if table.Scoring != "league" || len(table.Positions) != 6 {
		t.Fatalf("scoring or position count = %q/%d", table.Scoring, len(table.Positions))
	}
	for _, position := range []string{"QB", "RB", "WR", "TE", "K", "DST"} {
		if len(table.Positions[position].ByTeam) != fantasy.NFLTeamCount {
			t.Fatalf("%s team count = %d", position, len(table.Positions[position].ByTeam))
		}
	}
	wr := table.Positions["WR"]
	if !closeESPNDefenseValue(wr.LeagueAverage, 31.179) ||
		!closeESPNDefenseValue(wr.ByTeam["BUF"].PointsAllowedPerGame, 45.06) || wr.ByTeam["BUF"].Rank != 32 ||
		!closeESPNDefenseValue(wr.ByTeam["KC"].PointsAllowedPerGame, 20.95) || wr.ByTeam["KC"].Rank != 1 ||
		!closeESPNDefenseValue(wr.ByTeam["WAS"].PointsAllowedPerGame, 39.55) || wr.ByTeam["WAS"].Rank != 29 ||
		wr.ByTeam["JAX"].PointsAllowedPerGame == 0 {
		t.Fatalf("mapped WR ratings = %+v", wr)
	}
	if table.Positions["DST"].LeagueAverage == 0 {
		t.Fatalf("DST ratings were not read from position ID 16: %+v", table.Positions["DST"])
	}
}

func TestDefenseVsPositionRejectsIncompleteESPNRatings(t *testing.T) {
	fixture := readESPNDefenseFixture(t)
	tests := []struct {
		name   string
		status int
		body   []byte
	}{
		{name: "non-200", status: http.StatusInternalServerError, body: fixture},
		{name: "missing position", status: http.StatusOK, body: mutateESPNDefenseFixture(t, fixture, func(root map[string]json.RawMessage) {
			var against positionAgainstOpponentResponse
			_ = json.Unmarshal(root["positionAgainstOpponent"], &against)
			delete(against.PositionalRatings, "5")
			root["positionAgainstOpponent"], _ = json.Marshal(against)
		})},
		{name: "unknown team", status: http.StatusOK, body: mutateESPNDefenseFixture(t, fixture, func(root map[string]json.RawMessage) {
			var against map[string]map[string]positionalRating
			_ = json.Unmarshal(root["positionAgainstOpponent"], &against)
			rating := against["positionalRatings"]["3"]
			rating.RatingsByOpponent["99"] = opponentRating{Average: 1, Rank: 1}
			against["positionalRatings"]["3"] = rating
			root["positionAgainstOpponent"], _ = json.Marshal(against)
		})},
		{name: "out of range rank", status: http.StatusOK, body: mutateESPNDefenseFixture(t, fixture, func(root map[string]json.RawMessage) {
			var against map[string]map[string]positionalRating
			_ = json.Unmarshal(root["positionAgainstOpponent"], &against)
			rating := against["positionalRatings"]["3"]
			entry := rating.RatingsByOpponent["1"]
			entry.Rank = fantasy.NFLTeamCount + 1
			rating.RatingsByOpponent["1"] = entry
			against["positionalRatings"]["3"] = rating
			root["positionAgainstOpponent"], _ = json.Marshal(against)
		})},
		{name: "31 teams", status: http.StatusOK, body: mutateESPNDefenseFixture(t, fixture, func(root map[string]json.RawMessage) {
			var against map[string]map[string]positionalRating
			_ = json.Unmarshal(root["positionAgainstOpponent"], &against)
			rating := against["positionalRatings"]["3"]
			delete(rating.RatingsByOpponent, "1")
			against["positionalRatings"]["3"] = rating
			root["positionAgainstOpponent"], _ = json.Marshal(against)
		})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := newESPNDefenseServer(t, test.status, test.body)
			defer server.Close()
			client := NewClient(server.Client(), Credentials{S2: "s2-test", SWID: "swid-test"}, 2026)
			client.baseURL = server.URL
			_, err := client.DefenseVsPosition(context.Background(), fantasy.League{
				Ref: fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "1002879736", Season: 2026},
			})
			if err == nil {
				t.Fatal("incomplete ESPN ratings were accepted")
			}
		})
	}
}

func TestDefenseVsPositionRejectsInvalidESPNLeague(t *testing.T) {
	client := NewClient(http.DefaultClient, Credentials{S2: "s2-test", SWID: "swid-test"}, 2026)
	valid := fantasy.League{
		Ref: fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "1002879736", Season: 2026},
	}
	tests := []struct {
		name   string
		league fantasy.League
		client *Client
	}{
		{name: "wrong provider", league: fantasy.League{Ref: fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "1", Season: 2026}}, client: client},
		{name: "non-numeric ID", league: fantasy.League{Ref: fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "league", Season: 2026}}, client: client},
		{
			name: "season mismatch",
			league: fantasy.League{Ref: fantasy.LeagueRef{
				Provider: fantasy.ProviderESPN, ExternalID: "1002879736", Season: 2025,
			}},
			client: client,
		},
		{name: "missing credentials", league: valid, client: NewClient(http.DefaultClient, Credentials{}, 2026)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.client.DefenseVsPosition(context.Background(), test.league); err == nil {
				t.Fatal("invalid ESPN league was accepted")
			}
		})
	}
}

func readESPNDefenseFixture(t *testing.T) []byte {
	t.Helper()
	fixture, err := os.ReadFile("testdata/positional_ratings.json")
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func newESPNDefenseServer(t *testing.T, status int, fixture []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/seasons/2026/segments/0/leagues/1002879736" ||
			r.URL.Query().Get("view") != "mPositionalRatings" ||
			r.URL.Query().Has("scoringPeriodId") ||
			r.Header.Get("Cookie") != "espn_s2=s2-test; SWID={swid-test}" {
			t.Errorf("unexpected ESPN request: path=%q query=%v", r.URL.Path, r.URL.Query())
		}
		w.WriteHeader(status)
		_, _ = w.Write(fixture)
	}))
}

func mutateESPNDefenseFixture(
	t *testing.T,
	fixture []byte,
	mutate func(map[string]json.RawMessage),
) []byte {
	t.Helper()
	var root map[string]json.RawMessage
	if err := json.Unmarshal(fixture, &root); err != nil {
		t.Fatal(err)
	}
	mutate(root)
	encoded, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func closeESPNDefenseValue(left, right float64) bool {
	const tolerance = 0.01
	return left-right < tolerance && right-left < tolerance
}
