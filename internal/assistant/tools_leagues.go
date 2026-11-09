package assistant

import (
	"context"
	"encoding/json"
	"time"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
)

type listLeaguesResult struct {
	Leagues []listedLeague `json:"leagues"`
}

type listedLeague struct {
	ID                   string      `json:"id"`
	Name                 string      `json:"name"`
	Provider             string      `json:"provider"`
	Season               int32       `json:"season"`
	LastSuccessfulSyncAt string      `json:"last_successful_sync_at,omitempty"`
	MyTeam               *listedTeam `json:"my_team,omitempty"`
}

type listedTeam struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func leagueTools(reader assistantDashboard) []assistantTool {
	return []assistantTool{
		newAssistantTool(
			"list_leagues",
			"Lists leagues imported into League Lens, with the user's own team in each (my_team). "+
				"The user may refer to a league by their team's name. Use a league ID from this result as league_season_id in other tools.",
			`{"type":"object","properties":{},"additionalProperties":false}`,
			fixedToolLabel("Looking up your leagues"),
			func(ctx context.Context, raw json.RawMessage) (string, error) {
				if _, err := decodeToolInput[struct{}](raw); err != nil {
					return "", err
				}
				result, err := listLeagues(ctx, reader)
				if err != nil {
					return "", err
				}
				content, err := json.Marshal(result)
				if err != nil {
					return "", err
				}
				return truncateToolResult(string(content)), nil
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

// listLeagues loads imported leagues and adds their selected team names for model-facing lookup.
func listLeagues(ctx context.Context, reader assistantDashboard) (listLeaguesResult, error) {
	response, err := reader.ListLeagues(ctx, &v1.ListLeaguesRequest{PageSize: 100})
	if err != nil {
		return listLeaguesResult{}, err
	}
	return listLeaguesBuildResult(ctx, reader, response.GetLeagues())
}

func listLeaguesBuildResult(
	ctx context.Context,
	reader assistantDashboard,
	leagues []*v1.League,
) (listLeaguesResult, error) {
	result := listLeaguesResult{Leagues: make([]listedLeague, 0, len(leagues))}
	for _, league := range leagues {
		listed := listedLeagueFrom(league)
		myTeam, err := listLeaguesSelectedTeam(ctx, reader, league)
		if err != nil {
			return listLeaguesResult{}, err
		}
		listed.MyTeam = myTeam
		result.Leagues = append(result.Leagues, listed)
	}
	return result, nil
}

func listedLeagueFrom(league *v1.League) listedLeague {
	listed := listedLeague{
		ID: league.GetId(), Name: league.GetName(), Provider: league.GetProvider(),
		Season: league.GetSeason(),
	}
	if syncedAt := league.GetLastSuccessfulSyncAt(); syncedAt != nil {
		listed.LastSuccessfulSyncAt = syncedAt.AsTime().Format(time.RFC3339)
	}
	return listed
}

// listLeaguesSelectedTeam fetches the selected team; a GetLeague error fails the entire list.
func listLeaguesSelectedTeam(ctx context.Context, reader assistantDashboard, league *v1.League) (*listedTeam, error) {
	selectedTeamIDs := league.GetSelectedTeamIds()
	if len(selectedTeamIDs) == 0 {
		return nil, nil
	}
	response, err := reader.GetLeague(ctx, &v1.GetLeagueRequest{LeagueSeasonId: league.GetId()})
	if err != nil {
		return nil, err
	}
	return findListedTeam(response.GetTeams(), selectedTeamIDs[0]), nil
}

func findListedTeam(teams []*v1.Team, id string) *listedTeam {
	for _, team := range teams {
		if team.GetId() == id {
			return &listedTeam{ID: team.GetId(), Name: team.GetName()}
		}
	}
	return nil
}

type leagueIDInput struct {
	LeagueSeasonID string `json:"league_season_id"`
}

func fixedToolLabel(label string) func(json.RawMessage) string {
	return func(json.RawMessage) string { return label }
}
