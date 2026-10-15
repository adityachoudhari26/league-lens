package espn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

const nflScoreboardURL = "https://site.api.espn.com/apis/site/v2/sports/football/nfl/scoreboard"

type scoreboardResponse struct {
	Season struct {
		Year *int `json:"year"`
		Type int  `json:"type"`
	} `json:"season"`
	Week struct {
		Number *int `json:"number"`
	} `json:"week"`
	Events []scoreboardEvent `json:"events"`
}

type scoreboardCompetitor struct {
	HomeAway string `json:"homeAway"`
	Team     struct {
		ID string `json:"id"`
	} `json:"team"`
}

type scoreboardCompetition struct {
	Competitors []scoreboardCompetitor `json:"competitors"`
}

type scoreboardEvent struct {
	ID     string `json:"id"`
	Date   string `json:"date"`
	Status struct {
		Clock  float64 `json:"clock"`
		Period int     `json:"period"`
		Type   struct {
			Name      string `json:"name"`
			State     string `json:"state"`
			Completed bool   `json:"completed"`
		} `json:"type"`
	} `json:"status"`
	Competitions []scoreboardCompetition `json:"competitions"`
}

// FetchNFLScoreboard reads ESPN's public NFL scoreboard for the current week.
// It needs no league cookies and sends none.
func FetchNFLScoreboard(ctx context.Context, client *http.Client) (fantasy.NFLScoreboard, error) {
	return fetchNFLScoreboard(ctx, client, nflScoreboardURL)
}

// FetchNFLWeekScoreboard reads ESPN's public regular-season scoreboard for a historical week.
func FetchNFLWeekScoreboard(ctx context.Context, client *http.Client, season, week int) (fantasy.NFLScoreboard, error) {
	if season < 1900 || season > 2200 || week < 1 || week > 30 {
		return fantasy.NFLScoreboard{}, fmt.Errorf("valid NFL season and week required")
	}
	query := url.Values{
		"dates":      {strconv.Itoa(season)},
		"seasontype": {"2"},
		"week":       {strconv.Itoa(week)},
	}
	return fetchNFLScoreboard(ctx, client, nflScoreboardURL+"?"+query.Encode())
}

func fetchNFLScoreboard(ctx context.Context, client *http.Client, endpoint string) (fantasy.NFLScoreboard, error) {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fantasy.NFLScoreboard{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return fantasy.NFLScoreboard{}, fmt.Errorf("ESPN scoreboard request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fantasy.NFLScoreboard{}, fmt.Errorf("ESPN scoreboard returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return fantasy.NFLScoreboard{}, fmt.Errorf("failed to read ESPN scoreboard: %w", err)
	}
	return parseNFLScoreboard(body)
}

// parseNFLScoreboard parses ESPN's response. A missing season year or week is an error;
// zero events is a valid empty board, and invalid games are skipped rather than guessed.
func parseNFLScoreboard(body []byte) (fantasy.NFLScoreboard, error) {
	var raw scoreboardResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return fantasy.NFLScoreboard{}, fmt.Errorf("invalid ESPN NFL scoreboard: %w", err)
	}
	if raw.Season.Year == nil || *raw.Season.Year == 0 {
		return fantasy.NFLScoreboard{}, fmt.Errorf("ESPN NFL scoreboard is missing the season year")
	}
	if raw.Week.Number == nil || *raw.Week.Number == 0 {
		return fantasy.NFLScoreboard{}, fmt.Errorf("ESPN NFL scoreboard is missing the week number")
	}

	scoreboard := fantasy.NFLScoreboard{
		Season:     *raw.Season.Year,
		SeasonType: raw.Season.Type,
		Week:       fantasy.ScoringPeriod(*raw.Week.Number),
	}
	for _, event := range raw.Events {
		game, ok := parseScoreboardEvent(event)
		if ok {
			scoreboard.Games = append(scoreboard.Games, game)
		}
	}
	return scoreboard, nil
}

func parseScoreboardEvent(event scoreboardEvent) (fantasy.NFLGameStatus, bool) {
	state, ok := scoreboardGameState(event.Status.Type.State, event.Status.Type.Completed)
	if !ok || len(event.Competitions) == 0 {
		return fantasy.NFLGameStatus{}, false
	}
	kickoffAt, err := parseScoreboardDate(event.Date)
	if err != nil {
		return fantasy.NFLGameStatus{}, false
	}
	homeTeam, awayTeam, ok := scoreboardTeams(event.Competitions[0].Competitors)
	if !ok {
		return fantasy.NFLGameStatus{}, false
	}
	return fantasy.NFLGameStatus{
		ID:        event.ID,
		HomeTeam:  homeTeam,
		AwayTeam:  awayTeam,
		KickoffAt: kickoffAt,
		State:     state,
		Period:    event.Status.Period,
		Clock:     time.Duration(event.Status.Clock * float64(time.Second)),
	}, true
}

func scoreboardGameState(state string, completed bool) (fantasy.NFLGameState, bool) {
	switch state {
	case "pre":
		return fantasy.NFLGameScheduled, true
	case "in":
		return fantasy.NFLGameLive, true
	case "post":
		if completed {
			return fantasy.NFLGameFinal, true
		}
		return fantasy.NFLGamePostponed, true
	default:
		return "", false
	}
}

func parseScoreboardDate(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04Z07:00"} {
		kickoffAt, err := time.Parse(layout, value)
		if err == nil {
			return kickoffAt, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid ESPN scoreboard date %q", value)
}

func scoreboardTeams(competitors []scoreboardCompetitor) (string, string, bool) {
	var homeTeam, awayTeam string
	for _, competitor := range competitors {
		teamID, err := strconv.Atoi(competitor.Team.ID)
		if err != nil {
			return "", "", false
		}
		abbreviation := proTeamAbbreviation(teamID)
		if abbreviation == "" {
			return "", "", false
		}
		switch competitor.HomeAway {
		case "home":
			homeTeam = abbreviation
		case "away":
			awayTeam = abbreviation
		}
	}
	if homeTeam == "" || awayTeam == "" {
		return "", "", false
	}
	return homeTeam, awayTeam, true
}
