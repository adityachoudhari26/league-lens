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

type gameContextSource interface {
	NFLGameContext(context.Context, int) (fantasy.NFLWeekGameContext, error)
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

func gameContextTools(source gameContextSource) []assistantTool {
	return []assistantTool{newAssistantTool(
		"get_game_context",
		getGameContextDescription,
		getGameContextSchema,
		fixedToolLabel("Checking game lines and weather"),
		func(ctx context.Context, raw json.RawMessage) (string, error) {
			input, err := decodeToolInput[gameContextInput](raw)
			if err != nil {
				return "", errInvalidToolInput
			}
			week, teams, err := validateGameContextInput(input)
			if err != nil {
				return "", err
			}
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			gameContext, err := source.NFLGameContext(ctx, week)
			if err != nil {
				return "", errGameContextUnavailable
			}
			result := gameContextResult{
				Season: gameContext.Season,
				Week:   int(gameContext.Week),
				Games:  gameContextGames(gameContext.Games, teams),
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				return "", errGameContextUnavailable
			}
			return string(encoded), nil
		},
	)}
}

func validateGameContextInput(input gameContextInput) (int, map[string]struct{}, error) {
	week := 0
	if input.Week != nil {
		week = *input.Week
		if week < 1 || week > 18 {
			return 0, nil, errInvalidToolInput
		}
	}
	if input.Teams == nil {
		return week, nil, nil
	}
	if len(input.Teams) < 1 || len(input.Teams) > 8 {
		return 0, nil, errInvalidToolInput
	}
	teams := make(map[string]struct{}, len(input.Teams))
	for _, team := range input.Teams {
		team = strings.TrimSpace(team)
		if !gameContextTeamPattern.MatchString(team) {
			return 0, nil, errInvalidToolInput
		}
		teams[strings.ToUpper(team)] = struct{}{}
	}
	return week, teams, nil
}

func gameContextGames(games []fantasy.NFLGameContext, teams map[string]struct{}) []gameContextGame {
	selectedGames := filterGamesByTeam(games, teams)
	result := make([]gameContextGame, 0, len(selectedGames))
	for _, game := range selectedGames {
		result = append(result, gameContextGameFrom(game))
	}
	return result
}

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
