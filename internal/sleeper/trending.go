package sleeper

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// TrendingPlayer is one row of Sleeper's public trending add/drop list.
type TrendingPlayer struct {
	PlayerID string `json:"player_id"`
	Count    int    `json:"count"`
}

// TrendingPlayers reads Sleeper's most-added or most-dropped players.
func (c *Client) TrendingPlayers(ctx context.Context, kind string, lookbackHours, limit int) ([]TrendingPlayer, error) {
	if kind != "add" && kind != "drop" {
		return nil, fmt.Errorf("invalid trending kind %q", kind)
	}
	if lookbackHours < 1 || lookbackHours > 168 {
		return nil, fmt.Errorf("invalid trending lookback hours %d", lookbackHours)
	}
	if limit < 1 || limit > 50 {
		return nil, fmt.Errorf("invalid trending limit %d", limit)
	}

	path := "/players/nfl/trending/" + kind
	query := url.Values{
		"lookback_hours": {strconv.Itoa(lookbackHours)},
		"limit":          {strconv.Itoa(limit)},
	}.Encode()
	var rows []TrendingPlayer
	if err := c.getURL(ctx, c.BaseURL+path+"?"+query, path, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}
