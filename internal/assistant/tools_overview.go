package assistant

import (
	"context"
	"encoding/json"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
)

func overviewTools(reader assistantDashboard) []assistantTool {
	return []assistantTool{
		newAssistantTool(
			"get_alerts",
			"Returns the owner's current lineup alerts from imported league data. No IDs are required.",
			`{"type":"object","properties":{},"additionalProperties":false}`,
			fixedToolLabel("Checking alerts"),
			func(ctx context.Context, raw json.RawMessage) (string, error) {
				if _, err := decodeToolInput[struct{}](raw); err != nil {
					return "", err
				}
				response, err := reader.GetOverviewAttention(ctx, &v1.GetOverviewAttentionRequest{})
				if err != nil {
					return "", err
				}
				return marshalToolResponse(response)
			},
		),
		newAssistantTool(
			"get_player_news",
			"Returns recent news for players on the owner's selected teams. No IDs are required.",
			`{"type":"object","properties":{},"additionalProperties":false}`,
			fixedToolLabel("Reading player news"),
			func(ctx context.Context, raw json.RawMessage) (string, error) {
				if _, err := decodeToolInput[struct{}](raw); err != nil {
					return "", err
				}
				response, err := reader.GetOverviewPlayerUpdates(ctx, &v1.GetOverviewPlayerUpdatesRequest{})
				if err != nil {
					return "", err
				}
				return marshalToolResponse(response)
			},
		),
		newAssistantTool(
			"get_live_games",
			"Returns NFL games in progress, including possession and red-zone status. No IDs are required.",
			`{"type":"object","properties":{},"additionalProperties":false}`,
			fixedToolLabel("Checking live NFL games"),
			func(ctx context.Context, raw json.RawMessage) (string, error) {
				if _, err := decodeToolInput[struct{}](raw); err != nil {
					return "", err
				}
				response, err := reader.GetLiveGames(ctx, &v1.GetLiveGamesRequest{})
				if err != nil {
					return "", err
				}
				return marshalToolResponse(response)
			},
		),
	}
}
