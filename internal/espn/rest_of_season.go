package espn

import (
	"context"
	"math"
	"strconv"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

var _ fantasy.RestOfSeasonProjector = (*Client)(nil)

// ProjectRestOfSeason reads weekly ESPN projections for requested players and excludes weeks with actual scoring.
func (c *Client) ProjectRestOfSeason(
	ctx context.Context,
	league fantasy.League,
	query fantasy.RestOfSeasonQuery,
) (map[string]fantasy.RestOfSeasonProjection, error) {
	if err := c.validatePlayerPoolQuery(league, query.Week); err != nil {
		return nil, err
	}
	playerIDs := numericExternalPlayerIDs(query.ExternalPlayerIDs)
	if len(playerIDs) == 0 {
		return map[string]fantasy.RestOfSeasonProjection{}, nil
	}
	filter := restOfSeasonFilter(playerIDs, query.Week)
	players, err := c.readPlayerPool(ctx, league.Ref.ExternalID, filter)
	if err != nil {
		return nil, err
	}
	return restOfSeasonProjections(players, league.Ref.Season, query.Week), nil
}

func numericExternalPlayerIDs(externalIDs []string) []int {
	playerIDs := make([]int, 0, len(externalIDs))
	seen := make(map[int]bool, len(externalIDs))
	for _, externalID := range externalIDs {
		playerID, err := strconv.Atoi(externalID)
		if err != nil || seen[playerID] {
			continue
		}
		seen[playerID] = true
		playerIDs = append(playerIDs, playerID)
	}
	return playerIDs
}

func restOfSeasonFilter(playerIDs []int, startWeek int) freeAgentFilter {
	return freeAgentFilter{Players: map[string]any{
		"filterIds": map[string]any{"value": playerIDs},
		"filterStatsForSplitTypeIds": map[string]any{
			"value": []int{1},
		},
		"filterStatsForCurrentSeasonScoringPeriodId": map[string]any{
			"value": restOfSeasonScoringPeriods(startWeek),
		},
	}}
}

func restOfSeasonScoringPeriods(startWeek int) []int {
	periods := make([]int, 0, fantasy.RestOfSeasonFinalWeek-startWeek+1)
	for week := startWeek; week <= fantasy.RestOfSeasonFinalWeek; week++ {
		periods = append(periods, week)
	}
	return periods
}

func restOfSeasonProjections(
	players []freeAgentEntry,
	season, startWeek int,
) map[string]fantasy.RestOfSeasonProjection {
	projections := make(map[string]fantasy.RestOfSeasonProjection, len(players))
	for _, entry := range players {
		points, weeks := restOfSeasonFromStats(entry.Player.Stats, season, startWeek)
		if points == nil || weeks == nil {
			continue
		}
		projections[strconv.Itoa(entry.Player.ID)] = fantasy.RestOfSeasonProjection{
			Points: *points,
			Weeks:  *weeks,
		}
	}
	return projections
}

// restOfSeasonFromStats excludes played weeks and incomplete projection rows from the remainder through week 17.
func restOfSeasonFromStats(stats []statResponse, season, startWeek int) (*float64, *int) {
	actualWeeks := actualRestOfSeasonWeeks(stats, season, startWeek)
	projectedWeeks := projectedRestOfSeasonWeeks(stats, season, startWeek)
	total, countedWeeks := 0.0, 0
	for week := startWeek; week <= fantasy.RestOfSeasonFinalWeek; week++ {
		if actualWeeks[week] {
			continue
		}
		points, found := projectedWeeks[week]
		if !found {
			continue
		}
		total += points
		countedWeeks++
	}
	if countedWeeks == 0 {
		return nil, nil
	}
	roundedPoints := math.Round(total*10) / 10
	return &roundedPoints, &countedWeeks
}

func actualRestOfSeasonWeeks(stats []statResponse, season, startWeek int) map[int]bool {
	weeks := make(map[int]bool)
	for _, stat := range stats {
		if stat.SourceID == 0 && stat.SplitTypeID == 1 && stat.SeasonID == season &&
			stat.ScoringPeriodID >= startWeek && stat.ScoringPeriodID <= fantasy.RestOfSeasonFinalWeek {
			weeks[stat.ScoringPeriodID] = true
		}
	}
	return weeks
}

func projectedRestOfSeasonWeeks(stats []statResponse, season, startWeek int) map[int]float64 {
	weeks := make(map[int]float64)
	for _, stat := range stats {
		if stat.SourceID == 1 && stat.SplitTypeID == 1 && stat.SeasonID == season &&
			stat.ScoringPeriodID >= startWeek && stat.ScoringPeriodID <= fantasy.RestOfSeasonFinalWeek &&
			len(stat.Stats) > 0 && stat.AppliedTotal != nil {
			weeks[stat.ScoringPeriodID] = *stat.AppliedTotal
		}
	}
	return weeks
}
