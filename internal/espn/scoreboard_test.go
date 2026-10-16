package espn

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func TestParseNFLScoreboardWeek6(t *testing.T) {
	scoreboard := parseScoreboardFixture(t, "scoreboard_week6_pre.json")
	if scoreboard.Season != 2026 || scoreboard.SeasonType != 2 || scoreboard.Week != 6 {
		t.Fatalf("scoreboard season/type/week = %d/%d/%d", scoreboard.Season, scoreboard.SeasonType, scoreboard.Week)
	}
	if len(scoreboard.Games) != 14 {
		t.Fatalf("got %d games, want 14", len(scoreboard.Games))
	}

	foundWashington := false
	foundKickoff := false
	for _, game := range scoreboard.Games {
		if game.State != fantasy.NFLGameScheduled || game.Period != 0 || game.Clock != 0 {
			t.Errorf("scheduled game = %+v", game)
		}
		if game.HomeTeam == "WAS" || game.AwayTeam == "WAS" {
			foundWashington = true
		}
		if game.HomeTeam == "WSH" || game.AwayTeam == "WSH" {
			t.Fatalf("scoreboard used ESPN's abbreviation instead of the roster abbreviation: %+v", game)
		}
		if game.ID == "401872995" {
			foundKickoff = true
			want := time.Date(2026, time.October, 16, 0, 15, 0, 0, time.UTC)
			if !game.KickoffAt.Equal(want) {
				t.Errorf("kickoff = %s, want %s", game.KickoffAt, want)
			}
		}
	}
	if !foundWashington {
		t.Error("week 6 scoreboard did not map Washington to WAS")
	}
	if !foundKickoff {
		t.Error("week 6 scoreboard did not include the game with a no-seconds kickoff")
	}
}

func TestParseNFLScoreboardWeek5Final(t *testing.T) {
	scoreboard := parseScoreboardFixture(t, "scoreboard_week5_final.json")
	if scoreboard.Season != 2026 || scoreboard.SeasonType != 2 || scoreboard.Week != 5 {
		t.Fatalf("scoreboard season/type/week = %d/%d/%d", scoreboard.Season, scoreboard.SeasonType, scoreboard.Week)
	}
	if len(scoreboard.Games) != 15 {
		t.Fatalf("got %d games, want 15", len(scoreboard.Games))
	}
	for _, game := range scoreboard.Games {
		if game.State != fantasy.NFLGameFinal {
			t.Errorf("game %s state = %q, want final", game.ID, game.State)
		}
	}
}

func TestParseNFLScoreboardLiveStatesAndSkippedGames(t *testing.T) {
	scoreboard := parseScoreboardFixture(t, "scoreboard_live.json")
	if scoreboard.Season != 2026 || scoreboard.SeasonType != 2 || scoreboard.Week != 6 {
		t.Fatalf("scoreboard season/type/week = %d/%d/%d", scoreboard.Season, scoreboard.SeasonType, scoreboard.Week)
	}
	if len(scoreboard.Games) != 3 {
		t.Fatalf("got %d games, want 3 valid games", len(scoreboard.Games))
	}
	if game := scoreboard.Games[0]; game.State != fantasy.NFLGameLive || game.Period != 3 || game.Clock != 462*time.Second {
		t.Errorf("live game = %+v", game)
	}
	if game := scoreboard.Games[0]; game.Possession != "" || game.RedZone {
		t.Errorf("live game with null situation = %+v", game)
	}
	if game := scoreboard.Games[1]; game.State != fantasy.NFLGameLive || game.Period != 2 || game.Clock != 0 {
		t.Errorf("halftime game = %+v", game)
	}
	if game := scoreboard.Games[1]; game.Possession != "" || game.RedZone {
		t.Errorf("halftime game with null situation = %+v", game)
	}
	if game := scoreboard.Games[2]; game.State != fantasy.NFLGamePostponed {
		t.Errorf("postponed game = %+v", game)
	}
}

func TestParseNFLScoreboardLiveSituation(t *testing.T) {
	scoreboard := parseScoreboardFixture(t, "scoreboard_live_situation.json")
	if len(scoreboard.Games) != 2 {
		t.Fatalf("got %d games, want 2", len(scoreboard.Games))
	}
	liveGame := scoreboard.Games[0]
	if liveGame.HomeTeam != "DEN" || liveGame.AwayTeam != "SEA" || liveGame.Possession != "DEN" || !liveGame.RedZone {
		t.Errorf("live game = %+v, want DEN/SEA with DEN possession in the red zone", liveGame)
	}
	preGame := scoreboard.Games[1]
	if preGame.State != fantasy.NFLGameScheduled || preGame.Possession != "" || preGame.RedZone {
		t.Errorf("pregame = %+v, want no possession or red zone", preGame)
	}
}

func TestParseNFLScoreboardUnknownPossessionDoesNotSkipGame(t *testing.T) {
	for _, possessionID := range []string{"not-a-team", "999"} {
		t.Run(possessionID, func(t *testing.T) {
			body := []byte(`{
				"season":{"year":2026,"type":2},
				"week":{"number":6},
				"events":[{"id":"unknown-possession","date":"2026-10-16T00:15Z","status":{"type":{"state":"in"}},
					"competitions":[{"competitors":[{"homeAway":"home","team":{"id":"7"}},{"homeAway":"away","team":{"id":"26"}}],
						"situation":{"possession":"` + possessionID + `","isRedZone":true}}]}]
			}`)
			scoreboard, err := parseNFLScoreboard(body)
			if err != nil || len(scoreboard.Games) != 1 {
				t.Fatalf("scoreboard = %+v, %v; want one valid live game", scoreboard, err)
			}
			if game := scoreboard.Games[0]; game.Possession != "" || game.RedZone {
				t.Errorf("game with unknown possession = %+v", game)
			}
		})
	}
}

func TestParseNFLScoreboardEmptyAndInvalidResponses(t *testing.T) {
	empty, err := parseNFLScoreboard([]byte(`{"season":{"year":2026,"type":2},"week":{"number":6},"events":[]}`))
	if err != nil || len(empty.Games) != 0 {
		t.Fatalf("empty scoreboard = %+v, %v", empty, err)
	}
	if _, err := parseNFLScoreboard([]byte(`{`)); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if _, err := parseNFLScoreboard([]byte(`{"season":{"year":2026},"week":{},"events":[]}`)); err == nil {
		t.Fatal("missing week accepted")
	}
	if _, err := parseNFLScoreboard([]byte(`{"season":{},"week":{"number":6},"events":[]}`)); err == nil {
		t.Fatal("missing season year accepted")
	}
}

func TestParseNFLScoreboardSkipsInvalidEvents(t *testing.T) {
	body := []byte(`{
		"season":{"year":2026,"type":2},
		"week":{"number":6},
		"events":[
			{"id":"missing-home","date":"2026-10-16T00:15Z","status":{"type":{"state":"pre"}},
			 "competitions":[{"competitors":[{"homeAway":"away","team":{"id":"26"}}]}]},
			{"id":"invalid-date","date":"not-a-date","status":{"type":{"state":"pre"}},
			 "competitions":[{"competitors":[{"homeAway":"home","team":{"id":"7"}},{"homeAway":"away","team":{"id":"26"}}]}]},
			{"id":"nonnumeric-team-id","date":"2026-10-16T00:15Z","status":{"type":{"state":"pre"}},
			 "competitions":[{"competitors":[{"homeAway":"home","team":{"id":"abc"}},{"homeAway":"away","team":{"id":"26"}}]}]}
		]
	}`)
	scoreboard, err := parseNFLScoreboard(body)
	if err != nil || len(scoreboard.Games) != 0 {
		t.Fatalf("scoreboard = %+v, %v; want no games", scoreboard, err)
	}
}

func TestFetchNFLScoreboardUsesPublicEndpointWithoutCookies(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "scoreboard_week6_pre.json"))
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: scoreboardRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet {
			t.Errorf("request method = %s, want GET", request.Method)
		}
		if request.URL.String() != nflScoreboardURL {
			t.Errorf("request URL = %s, want %s", request.URL, nflScoreboardURL)
		}
		if request.Header.Get("Cookie") != "" {
			t.Error("scoreboard request sent cookies")
		}
		return scoreboardHTTPResponse(request, http.StatusOK, string(fixture)), nil
	})}

	scoreboard, err := FetchNFLScoreboard(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if scoreboard.Week != 6 || len(scoreboard.Games) != 14 {
		t.Fatalf("scoreboard week/games = %d/%d", scoreboard.Week, len(scoreboard.Games))
	}
}

func TestFetchNFLScoreboardRejectsNonOKStatus(t *testing.T) {
	client := &http.Client{Transport: scoreboardRoundTripper(func(request *http.Request) (*http.Response, error) {
		return scoreboardHTTPResponse(request, http.StatusServiceUnavailable, ""), nil
	})}
	if _, err := FetchNFLScoreboard(context.Background(), client); err == nil || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("FetchNFLScoreboard error = %v, want HTTP 503", err)
	}
}

func parseScoreboardFixture(t *testing.T, name string) fantasy.NFLScoreboard {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	scoreboard, err := parseNFLScoreboard(body)
	if err != nil {
		t.Fatal(err)
	}
	return scoreboard
}

type scoreboardRoundTripper func(*http.Request) (*http.Response, error)

func (roundTripper scoreboardRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

func scoreboardHTTPResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    request,
	}
}
