package assistant

import (
	"context"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
)

func playerTools(reader assistantDashboard) []assistantTool {
	return []assistantTool{
		readTool[playerIDInput](
			"get_player",
			"Returns a League Lens player card with available profile and news details. player_id comes from get_team_roster or get_my_matchup.",
			`{"type":"object","properties":{
				"player_id":{"type":"string","description":"ID returned by get_team_roster or get_my_matchup"}
			},"required":["player_id"],"additionalProperties":false}`,
			fixedToolLabel("Reading a player card"),
			func(ctx context.Context, input playerIDInput) (*v1.GetPlayerCardResponse, error) {
				return reader.GetPlayerCard(ctx, &v1.GetPlayerCardRequest{PlayerId: input.PlayerID})
			},
		),
	}
}

type playerIDInput struct {
	PlayerID string `json:"player_id"`
}
