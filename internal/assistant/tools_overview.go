package assistant

import (
	"context"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
)

func overviewTools(reader assistantDashboard) []assistantTool {
	return []assistantTool{
		readTool[struct{}](
			"get_alerts",
			"Returns the owner's current lineup alerts from imported league data. No IDs are required.",
			`{"type":"object","properties":{},"additionalProperties":false}`,
			fixedToolLabel("Checking alerts"),
			func(ctx context.Context, _ struct{}) (*v1.GetOverviewAttentionResponse, error) {
				return reader.GetOverviewAttention(ctx, &v1.GetOverviewAttentionRequest{})
			},
		),
		readTool[struct{}](
			"get_player_news",
			"Returns recent news for players on the owner's selected teams. No IDs are required.",
			`{"type":"object","properties":{},"additionalProperties":false}`,
			fixedToolLabel("Reading player news"),
			func(ctx context.Context, _ struct{}) (*v1.GetOverviewPlayerUpdatesResponse, error) {
				return reader.GetOverviewPlayerUpdates(ctx, &v1.GetOverviewPlayerUpdatesRequest{})
			},
		),
		readTool[struct{}](
			"get_live_games",
			"Returns NFL games in progress, including possession and red-zone status. No IDs are required.",
			`{"type":"object","properties":{},"additionalProperties":false}`,
			fixedToolLabel("Checking live NFL games"),
			func(ctx context.Context, _ struct{}) (*v1.GetLiveGamesResponse, error) {
				return reader.GetLiveGames(ctx, &v1.GetLiveGamesRequest{})
			},
		),
	}
}
