package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
)

func matchupTools(reader assistantDashboard) []assistantTool {
	return []assistantTool{
		readTool[teamRosterInput](
			"get_team_roster",
			"Returns a team's latest roster or its roster for a scoring week. team_id comes from get_league.",
			`{"type":"object","properties":{
				"team_id":{"type":"string","description":"ID returned by get_league"},
				"week":{"type":"integer","description":"Optional scoring week"}
			},"required":["team_id"],"additionalProperties":false}`,
			fixedToolLabel("Reading a roster"),
			func(ctx context.Context, input teamRosterInput) (*v1.GetTeamRosterResponse, error) {
				return reader.GetTeamRoster(ctx, &v1.GetTeamRosterRequest{
					TeamId: input.TeamID, ScoringPeriod: input.Week,
				})
			},
		),
		readTool[leagueWeekInput](
			"list_matchups",
			"Lists matchups for a league round. league_season_id comes from list_leagues; "+
				"week is the provider's matchup period. When omitted, the latest available period is used.",
			`{"type":"object","properties":{
				"league_season_id":{"type":"string","description":"ID returned by list_leagues"},
				"week":{"type":"integer","description":"Optional matchup period"}
			},"required":["league_season_id"],"additionalProperties":false}`,
			fixedToolLabel("Reading matchups"),
			func(ctx context.Context, input leagueWeekInput) (*v1.ListMatchupsResponse, error) {
				period := input.Week
				if period == nil {
					latest, err := latestMatchupPeriod(ctx, reader, input.LeagueSeasonID)
					if err != nil {
						return nil, err
					}
					period = latest
				}
				return reader.ListMatchups(ctx, &v1.ListMatchupsRequest{
					LeagueSeasonId: input.LeagueSeasonID, MatchupPeriod: *period,
				})
			},
		),
		readTool[leagueWeekInput](
			"get_my_matchup",
			"Returns the selected owner's team and opponent for a league round. league_season_id comes from list_leagues; week is the provider's matchup period.",
			`{"type":"object","properties":{
				"league_season_id":{"type":"string","description":"ID returned by list_leagues"},
				"week":{"type":"integer","description":"Optional matchup period"}
			},"required":["league_season_id"],"additionalProperties":false}`,
			matchupToolLabel,
			func(ctx context.Context, input leagueWeekInput) (*v1.GetMatchupDetailResponse, error) {
				return reader.GetMatchupDetail(ctx, &v1.GetMatchupDetailRequest{
					LeagueSeasonId: input.LeagueSeasonID, MatchupPeriod: input.Week,
				})
			},
		),
		readTool[matchupIDInput](
			"get_win_chance",
			"Returns the selected owner's win-chance samples and key moments for a matchup. matchup_id comes from get_my_matchup.",
			`{"type":"object","properties":{
				"matchup_id":{"type":"string","description":"ID returned by get_my_matchup"}
			},"required":["matchup_id"],"additionalProperties":false}`,
			fixedToolLabel("Checking win chance"),
			func(ctx context.Context, input matchupIDInput) (*v1.GetMatchupWinChanceResponse, error) {
				response, err := reader.GetMatchupWinChance(ctx, &v1.GetMatchupWinChanceRequest{
					MatchupId: input.MatchupID,
				})
				if err != nil {
					return nil, err
				}
				return trimWinChanceResponse(response), nil
			},
		),
	}
}

type teamRosterInput struct {
	TeamID string `json:"team_id"`
	Week   *int32 `json:"week"`
}

type leagueWeekInput struct {
	LeagueSeasonID string `json:"league_season_id"`
	Week           *int32 `json:"week"`
}

type matchupIDInput struct {
	MatchupID string `json:"matchup_id"`
}

func matchupToolLabel(raw json.RawMessage) string {
	input, err := decodeToolInput[leagueWeekInput](raw)
	if err == nil && input.Week != nil {
		return fmt.Sprintf("Reading your matchup (week %d)", *input.Week)
	}
	return "Reading your matchup"
}

func latestMatchupPeriod(ctx context.Context, reader assistantDashboard, leagueID string) (*int32, error) {
	league, err := reader.GetLeague(ctx, &v1.GetLeagueRequest{LeagueSeasonId: leagueID})
	if err != nil {
		return nil, err
	}
	if len(league.AvailableMatchupPeriods) == 0 {
		return nil, errors.New("no matchup periods are available")
	}
	latest := league.AvailableMatchupPeriods[0]
	for _, period := range league.AvailableMatchupPeriods[1:] {
		if period > latest {
			latest = period
		}
	}
	return &latest, nil
}

func trimWinChanceResponse(response *v1.GetMatchupWinChanceResponse) *v1.GetMatchupWinChanceResponse {
	if response == nil || response.WinChance == nil || len(response.WinChance.Samples) <= 12 {
		return response
	}
	trimmed := proto.Clone(response).(*v1.GetMatchupWinChanceResponse)
	chance := trimmed.WinChance
	samples := response.WinChance.Samples
	chance.Samples = append([]*v1.WinChanceSample{samples[0]}, samples[len(samples)-11:]...)
	return trimmed
}
