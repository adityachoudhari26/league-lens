package sleeper

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

var _ fantasy.DefenseVsPositionRater = (*Client)(nil)

const sleeperSeasonStatsPath = "/stats/nfl/regular/"

var sleeperDefensePositions = map[string]string{
	"fan_pts_allow_qb":  "QB",
	"fan_pts_allow_rb":  "RB",
	"fan_pts_allow_wr":  "WR",
	"fan_pts_allow_te":  "TE",
	"fan_pts_allow_k":   "K",
	"fan_pts_allow_def": "DST",
}

type sleeperDefenseTeam struct {
	abbreviation string
	stats        map[string]float64
}

// DefenseVsPosition reads Sleeper's season totals and ranks opponents by allowed points per game.
func (c *Client) DefenseVsPosition(
	ctx context.Context,
	league fantasy.League,
) (fantasy.DefenseVsPositionTable, error) {
	if league.Ref.Provider != fantasy.ProviderSleeper ||
		league.Ref.Season < 1900 || league.Ref.Season > 2200 {
		return fantasy.DefenseVsPositionTable{}, fmt.Errorf("valid Sleeper league and season required")
	}
	path := sleeperSeasonStatsPath + strconv.Itoa(league.Ref.Season)
	var stats map[string]map[string]float64
	if err := c.getURL(ctx, c.BaseURL+path, path, &stats); err != nil {
		return fantasy.DefenseVsPositionTable{}, err
	}
	teams, err := selectSleeperDefenseTeams(stats)
	if err != nil {
		return fantasy.DefenseVsPositionTable{}, err
	}
	return buildSleeperDefenseTable(teams), nil
}

// selectSleeperDefenseTeams excludes player rows and requires 32 played teams.
func selectSleeperDefenseTeams(stats map[string]map[string]float64) ([]sleeperDefenseTeam, error) {
	teams := make([]sleeperDefenseTeam, 0, fantasy.NFLTeamCount)
	for abbreviation, row := range stats {
		if !isSleeperTeamAbbreviation(abbreviation) || row["gp"] <= 0 {
			continue
		}
		teams = append(teams, sleeperDefenseTeam{abbreviation: abbreviation, stats: row})
	}
	if len(teams) != fantasy.NFLTeamCount {
		return nil, fmt.Errorf("Sleeper season stats have %d played teams", len(teams))
	}
	return teams, nil
}

func isSleeperTeamAbbreviation(value string) bool {
	if value == "" || strings.ToUpper(value) != value {
		return false
	}
	for _, character := range value {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}

func buildSleeperDefenseTable(teams []sleeperDefenseTeam) fantasy.DefenseVsPositionTable {
	positions := make(map[string]fantasy.PositionDefense, len(sleeperDefensePositions))
	for stat, position := range sleeperDefensePositions {
		positions[position] = rateSleeperDefensePosition(teams, stat)
	}
	return fantasy.DefenseVsPositionTable{Scoring: "ppr", Positions: positions}
}

// rateSleeperDefensePosition computes per-game team values and the weighted league average.
func rateSleeperDefensePosition(teams []sleeperDefenseTeam, stat string) fantasy.PositionDefense {
	totals := 0.0
	games := 0.0
	pointsByTeam := make(map[string]float64, len(teams))
	for _, team := range teams {
		gamesPlayed := team.stats["gp"]
		points := team.stats[stat]
		totals += points
		games += gamesPlayed
		pointsByTeam[team.abbreviation] = points / gamesPlayed
	}
	return fantasy.PositionDefense{
		LeagueAverage: totals / games,
		ByTeam:        rankSleeperDefenseTeams(pointsByTeam),
	}
}

// rankSleeperDefenseTeams uses alphabetic team order to resolve equal per-game totals.
func rankSleeperDefenseTeams(pointsByTeam map[string]float64) map[string]fantasy.DefenseRating {
	teams := sleeperDefenseTeamNames(pointsByTeam)
	sort.Slice(teams, func(left, right int) bool {
		if pointsByTeam[teams[left]] == pointsByTeam[teams[right]] {
			return teams[left] < teams[right]
		}
		return pointsByTeam[teams[left]] < pointsByTeam[teams[right]]
	})
	ratings := make(map[string]fantasy.DefenseRating, len(teams))
	for index, team := range teams {
		ratings[team] = fantasy.DefenseRating{
			PointsAllowedPerGame: pointsByTeam[team],
			Rank:                 index + 1,
		}
	}
	return ratings
}

func sleeperDefenseTeamNames(pointsByTeam map[string]float64) []string {
	teams := make([]string, 0, len(pointsByTeam))
	for team := range pointsByTeam {
		teams = append(teams, team)
	}
	return teams
}
