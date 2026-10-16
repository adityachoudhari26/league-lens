package sleeper

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

var _ fantasy.Provider = (*Client)(nil)

func (c *Client) readLeague(ctx context.Context, id string) (League, error) {
	var league League
	if id == "" {
		return league, fmt.Errorf("league ID required")
	}
	if err := c.get(ctx, "/league/"+url.PathEscape(id), &league); err != nil {
		return league, err
	}
	year, err := strconv.Atoi(league.Season)
	if err != nil ||
		year < 1900 || year > 2200 ||
		league.ID != id ||
		league.Sport != "nfl" ||
		league.SeasonType != "regular" ||
		league.TotalRosters < 1 || league.TotalRosters > 100 {
		return league, fmt.Errorf("invalid or unsupported NFL league")
	}
	return league, nil
}

func (c *Client) DescribeLeague(ctx context.Context, id string) (fantasy.League, error) {
	league, err := c.readLeague(ctx, id)
	if err != nil {
		return fantasy.League{}, err
	}
	return normalizeLeague(league)
}

func (c *Client) FetchLeague(ctx context.Context, league fantasy.League, request fantasy.FetchRequest) (fantasy.LeagueData, error) {
	raw, err := c.readLeague(ctx, league.Ref.ExternalID)
	if err != nil {
		return fantasy.LeagueData{}, err
	}
	described, err := normalizeLeague(raw)
	if err != nil {
		return fantasy.LeagueData{}, err
	}
	if described.Ref != league.Ref {
		return fantasy.LeagueData{}, fmt.Errorf("league identity changed during import")
	}
	data, err := c.Fetch(ctx, Header{League: raw}, request)
	if err != nil {
		return fantasy.LeagueData{}, err
	}
	normalized, err := normalizeData(data)
	if err != nil {
		return fantasy.LeagueData{}, err
	}
	c.annotateAvailability(ctx, &normalized)
	return normalized, nil
}

// ResolveSelection is onboarding logic, separate from fetching league data.
// Public username lookup selects a team; it is not application authentication.
func (c *Client) ResolveSelection(ctx context.Context, username string, league fantasy.League) (string, fantasy.ConnectionSelection, error) {
	selection := fantasy.ConnectionSelection{League: league.Ref}
	var user User
	if username == "" {
		return "", selection, fmt.Errorf("username required")
	}
	if err := c.get(ctx, "/user/"+url.PathEscape(username), &user); err != nil {
		return "", selection, err
	}
	if user.ID == "" {
		return "", selection, fmt.Errorf("Sleeper user not found")
	}
	var rosters []Roster
	if err := c.get(ctx, "/league/"+url.PathEscape(league.Ref.ExternalID)+"/rosters", &rosters); err != nil {
		return "", selection, err
	}
	for _, roster := range rosters {
		if rosterOwnedBy(roster, user.ID) {
			selection.SelectedTeamIDs = append(selection.SelectedTeamIDs, strconv.Itoa(roster.ID))
		}
	}
	if len(selection.SelectedTeamIDs) == 0 {
		return "", selection, fmt.Errorf("Sleeper user does not own a team in this league")
	}
	return user.ID, selection, nil
}

func rosterOwnedBy(roster Roster, userID string) bool {
	if roster.OwnerID == userID {
		return true
	}
	for _, owner := range roster.CoOwners {
		if owner == userID {
			return true
		}
	}
	return false
}
