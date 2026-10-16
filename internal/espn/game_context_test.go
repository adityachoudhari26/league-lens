package espn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func TestParseNFLGameContextFixture(t *testing.T) {
	gameContext := parseNFLGameContextFixture(t)
	if gameContext.Season != 2026 || gameContext.Week != 6 || len(gameContext.Games) != 4 {
		t.Fatalf("season/week/games = %d/%d/%d", gameContext.Season, gameContext.Week, len(gameContext.Games))
	}

	assertJaguarsOddsWithoutWeather(t, gameContextByTeams(t, gameContext, "JAX", "HOU"))
	assertBearsOddsAndWeather(t, gameContextByTeams(t, gameContext, "ATL", "CHI"))
	assertFinalGameHasNoOddsOrWeather(t, gameContextByTeams(t, gameContext, "DEN", "SEA"))
	assertWashingtonMappedToWAS(t, gameContextByTeams(t, gameContext, "SF", "WAS"))
}

func assertJaguarsOddsWithoutWeather(t *testing.T, game fantasy.NFLGameContext) {
	t.Helper()
	if !game.KickoffAt.Equal(time.Date(2026, time.October, 18, 13, 30, 0, 0, time.UTC)) {
		t.Errorf("JAX/HOU kickoff = %s", game.KickoffAt)
	}
	if game.Odds == nil || game.Odds.Line != "JAX -2.5" ||
		game.Odds.OverUnder == nil || *game.Odds.OverUnder != 42.5 ||
		game.Odds.Source != "Draft Kings" || game.Weather != nil {
		t.Errorf("JAX/HOU context = %+v", game)
	}
}

func assertBearsOddsAndWeather(t *testing.T, game fantasy.NFLGameContext) {
	t.Helper()
	if game.Odds == nil || game.Odds.Line != "CHI -1.5" ||
		game.Odds.OverUnder == nil || *game.Odds.OverUnder != 51.5 ||
		game.Weather == nil || game.Weather.Condition != "Sunny" ||
		game.Weather.TemperatureF == nil || *game.Weather.TemperatureF != 60 {
		t.Errorf("ATL/CHI context = %+v", game)
	}
}

func assertFinalGameHasNoOddsOrWeather(t *testing.T, game fantasy.NFLGameContext) {
	t.Helper()
	if game.State != fantasy.NFLGameFinal || game.Odds != nil || game.Weather != nil {
		t.Errorf("DEN/SEA context = %+v", game)
	}
}

func assertWashingtonMappedToWAS(t *testing.T, game fantasy.NFLGameContext) {
	t.Helper()
	if game.AwayTeam != "WAS" {
		t.Errorf("SF opponent = %q, want WAS", game.AwayTeam)
	}
}

func TestParseNFLGameContextRequiresSeasonAndWeek(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing week",
			body: `{"season":{"year":2026},"week":{},"events":[]}`,
		},
		{
			name: "missing season year",
			body: `{"season":{},"week":{"number":6},"events":[]}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseNFLGameContext([]byte(test.body)); err == nil {
				t.Fatal("missing season or week accepted")
			}
		})
	}
}

func TestNFLGameContextURL(t *testing.T) {
	current, err := nflGameContextURL(0)
	if err != nil || current != nflScoreboardURL {
		t.Fatalf("current URL = %q, %v", current, err)
	}
	weekURL, err := nflGameContextURL(7)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(weekURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("seasontype") != "2" || parsed.Query().Get("week") != "7" ||
		parsed.Query().Has("dates") {
		t.Fatalf("week 7 URL query = %s", parsed.RawQuery)
	}
	for _, week := range []int{19, -1} {
		if _, err := nflGameContextURL(week); err == nil {
			t.Errorf("week %d accepted", week)
		}
	}
}

func TestFetchScoreboardBodyRejectsNonOKStatusWithoutBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte("private response body"))
	}))
	defer server.Close()

	_, err := fetchScoreboardBody(context.Background(), server.Client(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") ||
		strings.Contains(err.Error(), "private response body") {
		t.Fatalf("fetchScoreboardBody error = %v", err)
	}
}

func TestParseNFLGameContextRoundsDecimalWeather(t *testing.T) {
	temperature := 60.5
	weather := parseNFLGameWeather(&nflGameContextWeather{
		DisplayValue: "Sunny", Temperature: &temperature,
	})
	if weather == nil || weather.TemperatureF == nil || *weather.TemperatureF != 61 {
		t.Fatalf("rounded weather = %+v", weather)
	}
}

func parseNFLGameContextFixture(t *testing.T) fantasy.NFLWeekGameContext {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "scoreboard_game_context.json"))
	if err != nil {
		t.Fatal(err)
	}
	gameContext, err := parseNFLGameContext(body)
	if err != nil {
		t.Fatal(err)
	}
	return gameContext
}

func gameContextByTeams(t *testing.T, gameContext fantasy.NFLWeekGameContext, home, away string) fantasy.NFLGameContext {
	t.Helper()
	for _, game := range gameContext.Games {
		if game.HomeTeam == home && game.AwayTeam == away {
			return game
		}
	}
	t.Fatalf("game %s/%s not found", home, away)
	return fantasy.NFLGameContext{}
}
