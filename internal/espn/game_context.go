package espn

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

type nflGameContextScoreboard struct {
	Season struct {
		Year *int `json:"year"`
	} `json:"season"`
	Week struct {
		Number *int `json:"number"`
	} `json:"week"`
	Events []nflGameContextEvent `json:"events"`
}

type nflGameContextEvent struct {
	Date   string `json:"date"`
	Status struct {
		Type struct {
			State     string `json:"state"`
			Completed bool   `json:"completed"`
		} `json:"type"`
	} `json:"status"`
	Weather      *nflGameContextWeather      `json:"weather"`
	Competitions []nflGameContextCompetition `json:"competitions"`
}

type nflGameContextCompetition struct {
	Competitors []scoreboardCompetitor `json:"competitors"`
	Odds        []nflGameContextOdds   `json:"odds"`
}

type nflGameContextOdds struct {
	Provider struct {
		Name string `json:"name"`
	} `json:"provider"`
	Details   string   `json:"details"`
	OverUnder *float64 `json:"overUnder"`
}

type nflGameContextWeather struct {
	DisplayValue string   `json:"displayValue"`
	Temperature  *float64 `json:"temperature"`
}

// FetchNFLGameContext reads lines and forecasts from ESPN's public scoreboard.
// Week 0 means ESPN's current week; 1–18 is a regular-season week of the current season.
// It needs no league cookies and sends none.
func FetchNFLGameContext(ctx context.Context, client *http.Client, week int) (fantasy.NFLWeekGameContext, error) {
	endpoint, err := nflGameContextURL(week)
	if err != nil {
		return fantasy.NFLWeekGameContext{}, err
	}
	body, err := fetchScoreboardBody(ctx, client, endpoint)
	if err != nil {
		return fantasy.NFLWeekGameContext{}, err
	}
	return parseNFLGameContext(body)
}

func nflGameContextURL(week int) (string, error) {
	if week == 0 {
		return nflScoreboardURL, nil
	}
	if week < 1 || week > 18 {
		return "", fmt.Errorf("valid NFL week required")
	}
	query := url.Values{
		"seasontype": {"2"},
		"week":       {strconv.Itoa(week)},
	}
	return nflScoreboardURL + "?" + query.Encode(), nil
}

// parseNFLGameContext parses ESPN's scoreboard. A missing season year or week is an error;
// games with an unknown state, date or team are skipped rather than guessed.
func parseNFLGameContext(body []byte) (fantasy.NFLWeekGameContext, error) {
	var raw nflGameContextScoreboard
	if err := json.Unmarshal(body, &raw); err != nil {
		return fantasy.NFLWeekGameContext{}, fmt.Errorf("invalid ESPN NFL scoreboard: %w", err)
	}
	if raw.Season.Year == nil || *raw.Season.Year == 0 {
		return fantasy.NFLWeekGameContext{}, fmt.Errorf("ESPN NFL scoreboard is missing the season year")
	}
	if raw.Week.Number == nil || *raw.Week.Number == 0 {
		return fantasy.NFLWeekGameContext{}, fmt.Errorf("ESPN NFL scoreboard is missing the week number")
	}

	gameContext := fantasy.NFLWeekGameContext{
		Season: *raw.Season.Year,
		Week:   fantasy.ScoringPeriod(*raw.Week.Number),
	}
	for _, event := range raw.Events {
		game, ok := parseNFLGameContextEvent(event)
		if ok {
			gameContext.Games = append(gameContext.Games, game)
		}
	}
	return gameContext, nil
}

func parseNFLGameContextEvent(event nflGameContextEvent) (fantasy.NFLGameContext, bool) {
	state, ok := scoreboardGameState(event.Status.Type.State, event.Status.Type.Completed)
	if !ok || len(event.Competitions) == 0 {
		return fantasy.NFLGameContext{}, false
	}
	kickoffAt, err := parseScoreboardDate(event.Date)
	if err != nil {
		return fantasy.NFLGameContext{}, false
	}
	homeTeam, awayTeam, ok := scoreboardTeams(event.Competitions[0].Competitors)
	if !ok {
		return fantasy.NFLGameContext{}, false
	}

	return fantasy.NFLGameContext{
		HomeTeam: homeTeam, AwayTeam: awayTeam, KickoffAt: kickoffAt, State: state,
		Odds:    parseNFLGameOdds(event.Competitions[0].Odds),
		Weather: parseNFLGameWeather(event.Weather),
	}, true
}

// parseNFLGameOdds keeps ESPN's first odds entry. The line is ESPN's display string
// (e.g. "JAX -2.5"); the numeric spread is ignored because its sign doesn't follow the favorite.
func parseNFLGameOdds(odds []nflGameContextOdds) *fantasy.NFLGameOdds {
	if len(odds) == 0 {
		return nil
	}
	latest := odds[0]
	if latest.Details == "" && latest.OverUnder == nil {
		return nil
	}
	return &fantasy.NFLGameOdds{
		Line: latest.Details, OverUnder: latest.OverUnder, Source: latest.Provider.Name,
	}
}

func parseNFLGameWeather(weather *nflGameContextWeather) *fantasy.NFLGameWeather {
	if weather == nil || (weather.DisplayValue == "" && weather.Temperature == nil) {
		return nil
	}
	var temperature *int
	if weather.Temperature != nil {
		rounded := int(math.Round(*weather.Temperature))
		temperature = &rounded
	}
	return &fantasy.NFLGameWeather{
		Condition: weather.DisplayValue, TemperatureF: temperature,
	}
}
