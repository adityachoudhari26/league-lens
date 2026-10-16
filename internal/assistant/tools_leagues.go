package assistant

import (
	"context"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
)

func leagueTools(reader assistantDashboard) []assistantTool {
	return []assistantTool{
		readTool[struct{}](
			"list_leagues",
			"Lists leagues imported into League Lens. Use a league ID from this result as league_season_id in other tools.",
			`{"type":"object","properties":{},"additionalProperties":false}`,
			fixedToolLabel("Looking up your leagues"),
			func(ctx context.Context, _ struct{}) (*v1.ListLeaguesResponse, error) {
				return reader.ListLeagues(ctx, &v1.ListLeaguesRequest{PageSize: 100})
			},
		),
		readTool[leagueIDInput](
			"get_league",
			"Returns a league's teams, records, and available matchup periods. league_season_id comes from list_leagues.",
			`{"type":"object","properties":{
				"league_season_id":{"type":"string","description":"ID returned by list_leagues"}
			},"required":["league_season_id"],"additionalProperties":false}`,
			fixedToolLabel("Reading league standings"),
			func(ctx context.Context, input leagueIDInput) (*v1.GetLeagueResponse, error) {
				return reader.GetLeague(ctx, &v1.GetLeagueRequest{LeagueSeasonId: input.LeagueSeasonID})
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
