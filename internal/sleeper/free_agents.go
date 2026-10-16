package sleeper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
)

var _ fantasy.FreeAgentLister = (*Client)(nil)

// ListFreeAgents subtracts every rostered player from the current-week projections.
func (c *Client) ListFreeAgents(ctx context.Context, league fantasy.League, query fantasy.FreeAgentQuery) ([]fantasy.FreeAgent, error) {
	if league.Ref.Provider != fantasy.ProviderSleeper || league.Ref.ExternalID == "" ||
		league.Ref.Season < 1900 || league.Ref.Season > 2200 ||
		query.Week < 1 || query.Week > 18 {
		return nil, fmt.Errorf("valid Sleeper league and week required")
	}
	var rawLeague League
	if err := json.Unmarshal(league.Settings.ProviderSettings, &rawLeague); err != nil {
		return nil, fmt.Errorf("Sleeper scoring settings are unavailable")
	}
	positions, err := sleeperProjectionPositions(query.Position)
	if err != nil {
		return nil, err
	}
	projections, err := c.readFreeAgentProjections(ctx, league.Ref.Season, query.Week, positions)
	if err != nil {
		return nil, err
	}
	rosters, err := c.readLeagueRosters(ctx, league.Ref.ExternalID)
	if err != nil {
		return nil, err
	}
	rostered := rosteredPlayerIDs(rosters)
	scoring := scoringValues(rawLeague.Scoring)
	catalog := c.PlayerCatalog
	if catalog == nil && c.CatalogPath != "" {
		catalog, _, _ = playerids.Load(ctx, c.CatalogPath)
	}
	agents := make([]fantasy.FreeAgent, 0, len(projections))
	for _, projection := range projections {
		if projection.PlayerID == "" || rostered[projection.PlayerID] {
			continue
		}
		agent, ok := sleeperFreeAgent(projection, scoring, catalog)
		if ok {
			agents = append(agents, agent)
		}
	}
	return fantasy.SortFreeAgents(agents, query.Limit), nil
}

func (c *Client) readFreeAgentProjections(ctx context.Context, season, week int, positions []string) ([]Projection, error) {
	path := "/" + strconv.Itoa(season) + "/" + strconv.Itoa(week)
	query := url.Values{"season_type": {"regular"}, "position[]": positions}
	var rows []Projection
	if err := c.getURL(ctx, c.ProjectionsURL+path+"?"+query.Encode(), "/projections/nfl"+path, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func (c *Client) readLeagueRosters(ctx context.Context, leagueID string) ([]Roster, error) {
	var rosters []Roster
	if err := c.get(ctx, "/league/"+url.PathEscape(leagueID)+"/rosters", &rosters); err != nil {
		return nil, err
	}
	return rosters, nil
}

func sleeperProjectionPositions(position string) ([]string, error) {
	switch strings.ToUpper(position) {
	case "":
		return []string{"QB", "RB", "WR", "TE", "K", "DEF"}, nil
	case "QB", "RB", "WR", "TE", "K":
		return []string{strings.ToUpper(position)}, nil
	case "DST":
		return []string{"DEF"}, nil
	default:
		return nil, fmt.Errorf("unsupported Sleeper fantasy position")
	}
}

func rosteredPlayerIDs(rosters []Roster) map[string]bool {
	rostered := make(map[string]bool)
	for _, roster := range rosters {
		for _, ids := range [][]string{roster.Players, roster.Reserve, roster.Taxi} {
			for _, id := range ids {
				if id != "" {
					rostered[id] = true
				}
			}
		}
	}
	return rostered
}

func sleeperFreeAgent(projection Projection, scoring map[string]float64, catalog playerids.Catalog) (fantasy.FreeAgent, bool) {
	name, position, team, injury := "", "", "", ""
	if player := projection.Player; player != nil {
		name = player.Name
		if name == "" {
			name = strings.TrimSpace(player.FirstName + " " + player.LastName)
		}
		position, team, injury = player.Position, player.Team, player.InjuryStatus
	}
	if entry, found := catalog.Lookup("sleeper", projection.PlayerID); found {
		if name == "" {
			name = entry.Name
		}
		if position == "" {
			position = entry.Position
		}
		if team == "" {
			team = entry.Team
		}
	}
	if name == "" {
		return fantasy.FreeAgent{}, false
	}
	if strings.EqualFold(position, "DEF") {
		position = "DST"
	}
	return fantasy.FreeAgent{
		ExternalPlayerID: projection.PlayerID,
		Name:             name,
		Position:         position,
		NFLTeam:          team,
		InjuryStatus:     injury,
		Status:           "available",
		ProjectedPoints:  projectedPoints(projection.Stats, scoring),
	}, true
}
