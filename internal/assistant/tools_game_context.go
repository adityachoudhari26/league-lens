package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

var (
	errGameContextUnavailable = errors.New("game context unavailable")
	gameContextTeamPattern    = regexp.MustCompile(`^[A-Za-z]{2,3}$`)
)

const (
	maxGameContextWeek  = 18
	maxGameContextTeams = 8
)

const getGameContextDescription = "Spread, over/under and weather for NFL games in a week. Use it for start/sit calls: " +
	"a high total or bad weather can tip a close decision. Omit week for the current week; " +
	"pass teams (NFL abbreviations) to get only their games."

const getGameContextSchema = `{
  "type": "object",
  "properties": {
    "week": {"type": "integer", "minimum": 1, "maximum": 18},
    "teams": {
      "type": "array",
      "minItems": 1,
      "maxItems": 8,
      "items": {"type": "string", "pattern": "^[A-Za-z]{2,3}$"}
    }
  },
  "additionalProperties": false
}`

// GameContextSource loads current or selected NFL-week betting lines and forecasts.
type GameContextSource interface {
	NFLGameContext(ctx context.Context, week int) (fantasy.NFLWeekGameContext, error)
}

type gameContextInput struct {
	Week  *int     `json:"week"`
	Teams []string `json:"teams"`
}

type gameContextResult struct {
	Season int               `json:"season"`
	Week   int               `json:"week"`
	Games  []gameContextGame `json:"games"`
}

type gameContextGame struct {
	Away       string              `json:"away"`
	Home       string              `json:"home"`
	Kickoff    string              `json:"kickoff"`
	Status     string              `json:"status"`
	Line       string              `json:"line,omitempty"`
	OverUnder  *float64            `json:"over_under,omitempty"`
	LineSource string              `json:"line_source,omitempty"`
	Weather    *gameContextWeather `json:"weather,omitempty"`
}

type gameContextWeather struct {
	Condition    string `json:"condition,omitempty"`
	TemperatureF *int   `json:"temperature_f,omitempty"`
}

func gameContextTools(source GameContextSource) []assistantTool {
	return []assistantTool{newAssistantTool(
		"get_game_context",
		getGameContextDescription,
		getGameContextSchema,
		fixedToolLabel("Checking game lines and weather"),
		runGameContextTool(source),
	)}
}

func runGameContextTool(source GameContextSource) func(context.Context, json.RawMessage) (string, error) {
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		input, err := decodeToolInput[gameContextInput](raw)
		if err != nil {
			return "", errInvalidToolInput
		}
		week, teams, err := validateGameContextInput(input)
		if err != nil {
			return "", err
		}
		gameContext, err := loadGameContext(ctx, source, week)
		if err != nil {
			return "", err
		}
		result := gameContextResultFrom(gameContext, teams)
		encoded, err := json.Marshal(result)
		if err != nil {
			return "", errGameContextUnavailable
		}
		return string(encoded), nil
	}
}

func gameContextResultFrom(
	gameContext fantasy.NFLWeekGameContext,
	teams map[string]struct{},
) gameContextResult {
	return gameContextResult{
		Season: gameContext.Season,
		Week:   int(gameContext.Week),
		Games:  gameContextGames(gameContext.Games, teams),
	}
}

// loadGameContext applies a 10-second timeout and maps source errors to errGameContextUnavailable.
func loadGameContext(
	ctx context.Context,
	source GameContextSource,
	week int,
) (fantasy.NFLWeekGameContext, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	gameContext, err := source.NFLGameContext(ctx, week)
	if err != nil {
		return fantasy.NFLWeekGameContext{}, errGameContextUnavailable
	}
	return gameContext, nil
}

// validateGameContextInput treats week 0 as current, nil teams as no filter, and returns teams as an uppercase set.
func validateGameContextInput(input gameContextInput) (int, map[string]struct{}, error) {
	week, err := validateGameContextWeek(input.Week)
	if err != nil {
		return 0, nil, err
	}
	teams, err := normalizeGameContextTeams(input.Teams)
	if err != nil {
		return 0, nil, err
	}
	return week, teams, nil
}

func validateGameContextWeek(week *int) (int, error) {
	if week == nil {
		return 0, nil
	}
	if *week < 1 || *week > maxGameContextWeek {
		return 0, errInvalidToolInput
	}
	return *week, nil
}

func normalizeGameContextTeams(teams []string) (map[string]struct{}, error) {
	if teams == nil {
		return nil, nil
	}
	if len(teams) < 1 || len(teams) > maxGameContextTeams {
		return nil, errInvalidToolInput
	}
	normalized := make(map[string]struct{}, len(teams))
	for _, team := range teams {
		team = strings.TrimSpace(team)
		if !gameContextTeamPattern.MatchString(team) {
			return nil, errInvalidToolInput
		}
		normalized[strings.ToUpper(team)] = struct{}{}
	}
	return normalized, nil
}

func gameContextGames(games []fantasy.NFLGameContext, teams map[string]struct{}) []gameContextGame {
	selectedGames := filterGamesByTeam(games, teams)
	result := make([]gameContextGame, 0, len(selectedGames))
	for _, game := range selectedGames {
		result = append(result, gameContextGameFrom(game))
	}
	return result
}

// filterGamesByTeam keeps games involving a requested team; an empty set leaves all games unchanged.
func filterGamesByTeam(games []fantasy.NFLGameContext, teams map[string]struct{}) []fantasy.NFLGameContext {
	if len(teams) == 0 {
		return games
	}
	selected := make([]fantasy.NFLGameContext, 0, len(games))
	for _, game := range games {
		_, homeSelected := teams[game.HomeTeam]
		_, awaySelected := teams[game.AwayTeam]
		if homeSelected || awaySelected {
			selected = append(selected, game)
		}
	}
	return selected
}

func gameContextGameFrom(game fantasy.NFLGameContext) gameContextGame {
	result := gameContextGame{
		Away: game.AwayTeam, Home: game.HomeTeam,
		Kickoff: game.KickoffAt.UTC().Format(time.RFC3339),
		Status:  gameStatusLabel(game.State),
	}
	if game.Odds != nil {
		result.Line = game.Odds.Line
		result.OverUnder = game.Odds.OverUnder
		result.LineSource = game.Odds.Source
	}
	if game.Weather != nil {
		result.Weather = &gameContextWeather{
			Condition: game.Weather.Condition, TemperatureF: game.Weather.TemperatureF,
		}
	}
	return result
}

// gameStatusLabel maps fantasy states to tool strings, including "postponed".
func gameStatusLabel(state fantasy.NFLGameState) string {
	switch state {
	case fantasy.NFLGameScheduled:
		return "scheduled"
	case fantasy.NFLGameLive:
		return "in_progress"
	case fantasy.NFLGameFinal:
		return "final"
	case fantasy.NFLGamePostponed:
		return "postponed"
	default:
		return ""
	}
}
