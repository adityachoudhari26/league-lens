package sleeper

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

var ErrUserNotFound = errors.New("sleeper user not found")

// DiscoverLeagues returns a Sleeper account's NFL leagues and its owned teams.
func (c *Client) DiscoverLeagues(ctx context.Context, username string, season int) (string, []fantasy.DiscoveredLeague, error) {
	if username == "" {
		return "", nil, fmt.Errorf("username required")
	}
	if season < 1900 || season > 2200 {
		return "", nil, fmt.Errorf("valid NFL season required")
	}

	userPath := "/user/" + url.PathEscape(username)
	var user User
	if err := c.getURLAllowNull(ctx, c.BaseURL+userPath, userPath, &user); err != nil {
		return "", nil, err
	}
	if user.ID == "" {
		return "", nil, ErrUserNotFound
	}

	leaguePath := "/user/" + url.PathEscape(user.ID) + "/leagues/nfl/" + strconv.Itoa(season)
	var leagues []League
	if err := c.get(ctx, leaguePath, &leagues); err != nil {
		return "", nil, err
	}

	discovered, err := c.discoverOwnedLeagues(ctx, leagues, user.ID, season)
	if err != nil {
		return "", nil, err
	}
	return user.ID, discovered, nil
}

func (c *Client) discoverOwnedLeagues(ctx context.Context, leagues []League, userID string, season int) ([]fantasy.DiscoveredLeague, error) {
	var discovered []fantasy.DiscoveredLeague
	for _, league := range leagues {
		leagueSeason, err := strconv.Atoi(league.Season)
		if err != nil || league.ID == "" || leagueSeason != season || league.Sport != "nfl" {
			continue
		}
		rosterPath := "/league/" + url.PathEscape(league.ID) + "/rosters"
		var rosters []Roster
		if err = c.get(ctx, rosterPath, &rosters); err != nil {
			return nil, err
		}
		for _, roster := range rosters {
			if roster.ID <= 0 || !rosterOwnedBy(roster, userID) {
				continue
			}
			discovered = append(discovered, fantasy.DiscoveredLeague{
				Ref:      fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: league.ID, Season: leagueSeason},
				Name:     league.Name,
				TeamID:   strconv.Itoa(roster.ID),
				TeamName: "",
			})
		}
	}
	return discovered, nil
}
