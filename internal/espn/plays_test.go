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

func TestParseNFLPlayPageWeekFiveFixture(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "plays_week5.json"))
	if err != nil {
		t.Fatal(err)
	}
	plays, pageCount, err := parseNFLPlayPage(body)
	if err != nil {
		t.Fatal(err)
	}
	if pageCount != 1 || len(plays) != 8 {
		t.Fatalf("page count/plays = %d/%d, want 1/8 valid plays", pageCount, len(plays))
	}
	for i := 1; i < len(plays); i++ {
		if plays[i].At.Before(plays[i-1].At) {
			t.Fatalf("plays are not sorted: %s precedes %s", plays[i-1].At, plays[i].At)
		}
	}
	if plays[0].Kind != fantasy.NFLPlayPassReception || plays[0].Yards != 8 {
		t.Errorf("first play = %+v, want 8-yard reception", plays[0])
	}
	if plays[1].Kind != fantasy.NFLPlayRush || plays[1].Yards != 4 ||
		plays[1].Participants[0] != (fantasy.NFLPlayParticipant{AthleteID: "4361579", Role: "rusher"}) {
		t.Errorf("rush = %+v", plays[1])
	}
	if touchdown := plays[2]; touchdown.Kind != fantasy.NFLPlayRushingTouchdown ||
		!touchdown.ExtraPointGood || touchdown.Clock != 651*time.Second {
		t.Errorf("rushing touchdown = %+v", touchdown)
	}
	if touchdown := plays[3]; touchdown.Kind != fantasy.NFLPlayPassingTouchdown || !touchdown.ExtraPointGood {
		t.Errorf("passing touchdown = %+v", touchdown)
	}
	if plays[4].Kind != fantasy.NFLPlayFieldGoalGood || plays[5].Kind != fantasy.NFLPlayInterception {
		t.Errorf("field goal/interception = %+v / %+v", plays[4], plays[5])
	}
	if touchdown := plays[6]; touchdown.Kind != fantasy.NFLPlayPassingTouchdown || touchdown.ExtraPointGood {
		t.Errorf("two-point passing touchdown = %+v", touchdown)
	}
	if plays[7].Kind != fantasy.NFLPlayEndOfGame {
		t.Errorf("end of game = %+v", plays[7])
	}
}

func TestFetchNFLGamePlaysFollowsPagesWithoutCookies(t *testing.T) {
	var pages []int
	client := &http.Client{Transport: scoreboardRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Cookie") != "" {
			t.Error("play-by-play request sent cookies")
		}
		if request.URL.Host != "sports.core.api.espn.com" || request.URL.Path != "/v2/sports/football/leagues/nfl/events/401872980/competitions/401872980/plays" {
			t.Errorf("play-by-play URL = %s", request.URL)
		}
		if request.URL.Query().Get("limit") != "400" {
			t.Errorf("page limit = %q, want 400", request.URL.Query().Get("limit"))
		}
		page := 1
		if value := request.URL.Query().Get("page"); value != "" {
			if value != "2" {
				t.Errorf("page query = %q, want 2", value)
			}
			page = 2
		}
		pages = append(pages, page)
		wallclock := "2026-10-09T00:20:00Z"
		if page == 1 {
			wallclock = "2026-10-09T00:21:00Z"
		}
		body := `{"pageCount":2,"items":[{"wallclock":"` + wallclock + `","type":{"id":"5"}}]}`
		return scoreboardHTTPResponse(request, http.StatusOK, body), nil
	})}

	plays, err := FetchNFLGamePlays(context.Background(), client, "401872980")
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 || pages[0] != 1 || pages[1] != 2 {
		t.Fatalf("requested pages = %v, want [1 2]", pages)
	}
	if len(plays) != 2 || !plays[0].At.Before(plays[1].At) {
		t.Fatalf("plays = %+v, want two plays sorted across pages", plays)
	}
}

func TestFetchNFLGamePlaysRejectsInvalidEventID(t *testing.T) {
	if _, err := FetchNFLGamePlays(context.Background(), nil, "../401872980"); err == nil {
		t.Fatal("invalid event ID accepted")
	}
}

func TestFetchNFLWeekScoreboardUsesHistoricalQuery(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "scoreboard_week5_final.json"))
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: scoreboardRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/apis/site/v2/sports/football/nfl/scoreboard" ||
			request.URL.Query().Get("dates") != "2026" ||
			request.URL.Query().Get("seasontype") != "2" ||
			request.URL.Query().Get("week") != "5" {
			t.Errorf("historical scoreboard URL = %s", request.URL)
		}
		if request.Header.Get("Cookie") != "" {
			t.Error("scoreboard request sent cookies")
		}
		return scoreboardHTTPResponse(request, http.StatusOK, string(fixture)), nil
	})}

	scoreboard, err := FetchNFLWeekScoreboard(context.Background(), client, 2026, 5)
	if err != nil {
		t.Fatal(err)
	}
	if scoreboard.Season != 2026 || scoreboard.Week != 5 || len(scoreboard.Games) == 0 {
		t.Fatalf("historical scoreboard = %+v", scoreboard)
	}
}

func TestFetchNFLGamePlaysRejectsOversizedBody(t *testing.T) {
	client := &http.Client{Transport: scoreboardRoundTripper(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", playBodyLimit+1))),
		}, nil
	})}
	if _, err := FetchNFLGamePlays(context.Background(), client, "401872980"); err == nil {
		t.Fatal("oversized ESPN play response accepted")
	}
}
