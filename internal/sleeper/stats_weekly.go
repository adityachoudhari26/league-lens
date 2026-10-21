package sleeper

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

const (
	firstWeeklyStatsSeason = 1
	lastWeeklyStatsWeek    = 18
)

// WeeklyStat is one player row from Sleeper's public weekly NFL stats endpoint.
type WeeklyStat struct {
	PlayerID string             `json:"player_id"`
	Team     string             `json:"team"`
	Stats    map[string]float64 `json:"stats"`
	Player   *weeklyStatPlayer  `json:"player"`
}

type weeklyStatPlayer struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Position  string `json:"position"`
}

// Position returns the row's provider position, or an empty string when omitted.
func (row WeeklyStat) Position() string {
	if row.Player == nil {
		return ""
	}
	return row.Player.Position
}

// Name returns the player's trimmed first and last name, or an empty string when omitted.
func (row WeeklyStat) Name() string {
	if row.Player == nil {
		return ""
	}
	return strings.TrimSpace(row.Player.FirstName + " " + row.Player.LastName)
}

// WeeklyStats reads all positions for one regular-season week.
func (c *Client) WeeklyStats(ctx context.Context, season, week int) ([]WeeklyStat, error) {
	if season < firstWeeklyStatsSeason || week < 1 || week > lastWeeklyStatsWeek {
		return nil, fmt.Errorf("valid Sleeper weekly stats season and week required")
	}
	path := "/" + strconv.Itoa(season) + "/" + strconv.Itoa(week)
	var rows []WeeklyStat
	if err := c.getURL(ctx, c.StatsURL+path+"?season_type=regular", "/stats/nfl"+path, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}
