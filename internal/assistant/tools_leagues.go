package assistant

import (
	"context"
	"encoding/json"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
)

func leagueTools(reader assistantDashboard) []assistantTool {
	return []assistantTool{
		newAssistantTool(
			"list_leagues",
			"Lists leagues imported into League Lens. Use a league ID from this result as league_season_id in other tools.",
			`{"type":"object","properties":{},"additionalProperties":false}`,
			fixedToolLabel("Looking up your leagues"),
			func(ctx context.Context, raw json.RawMessage) (string, error) {
				if _, err := decodeToolInput[struct{}](raw); err != nil {
					return "", err
				}
				response, err := reader.ListLeagues(ctx, &v1.ListLeaguesRequest{PageSize: 100})
				if err != nil {
					return "", err
				}
				return marshalToolResponse(response)
			},
		),
		newAssistantTool(
			"get_league",
			"Returns a league's teams, records, and available matchup periods. league_season_id comes from list_leagues.",
			`{"type":"object","properties":{
				"league_season_id":{"type":"string","description":"ID returned by list_leagues"}
			},"required":["league_season_id"],"additionalProperties":false}`,
			fixedToolLabel("Reading league standings"),
			func(ctx context.Context, raw json.RawMessage) (string, error) {
				input, err := decodeToolInput[leagueIDInput](raw)
				if err != nil {
					return "", err
				}
				response, err := reader.GetLeague(ctx, &v1.GetLeagueRequest{LeagueSeasonId: input.LeagueSeasonID})
				if err != nil {
					return "", err
				}
				return marshalToolResponse(response)
			},
		),
	}
}

type leagueIDInput struct {
	LeagueSeasonID string `json:"league_season_id"`
}

func fixedToolLabel(label string) func(json.RawMessage) string {
	return func(json.RawMessage) string { return label }
}
