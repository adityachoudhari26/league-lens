package assistant

import (
	"context"
	"encoding/json"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
)

func playerTools(reader assistantDashboard) []assistantTool {
	return []assistantTool{
		newAssistantTool(
			"get_player",
			"Returns a League Lens player card with available profile and news details. player_id comes from get_team_roster or get_my_matchup.",
			`{"type":"object","properties":{
				"player_id":{"type":"string","description":"ID returned by get_team_roster or get_my_matchup"}
			},"required":["player_id"],"additionalProperties":false}`,
			fixedToolLabel("Reading a player card"),
			func(ctx context.Context, raw json.RawMessage) (string, error) {
				input, err := decodeToolInput[playerIDInput](raw)
				if err != nil {
					return "", err
				}
				response, err := reader.GetPlayerCard(ctx, &v1.GetPlayerCardRequest{PlayerId: input.PlayerID})
				if err != nil {
					return "", err
				}
				return marshalToolResponse(response)
			},
		),
	}
}

type playerIDInput struct {
	PlayerID string `json:"player_id"`
}
