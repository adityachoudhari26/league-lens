package espn

import (
	"context"
	"fmt"
	"strconv"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

var _ fantasy.DefenseVsPositionRater = (*Client)(nil)

type positionAgainstOpponentResponse struct {
	PositionalRatings map[string]positionalRating `json:"positionalRatings"`
}

type positionalRating struct {
	Average           float64                   `json:"average"`
	RatingsByOpponent map[string]opponentRating `json:"ratingsByOpponent"`
}

type opponentRating struct {
	Average float64 `json:"average"`
	Rank    int     `json:"rank"`
}

type espnDefensePosition struct {
	id       string
	position string
}

var espnDefensePositions = []espnDefensePosition{
	{id: "1", position: "QB"},
	{id: "2", position: "RB"},
	{id: "3", position: "WR"},
	{id: "4", position: "TE"},
	{id: "5", position: "K"},
	{id: "16", position: "DST"},
}

// DefenseVsPosition reads ESPN's league-scored positional ratings for the season.
func (c *Client) DefenseVsPosition(
	ctx context.Context,
	league fantasy.League,
) (fantasy.DefenseVsPositionTable, error) {
	leagueID, parseErr := strconv.ParseUint(league.Ref.ExternalID, 10, 64)
	if league.Ref.Provider != fantasy.ProviderESPN || parseErr != nil || leagueID == 0 ||
		league.Ref.Season < 1900 || league.Ref.Season > 2200 {
		return fantasy.DefenseVsPositionTable{}, fmt.Errorf("valid ESPN league and season required")
	}
	if c.credentials.S2 == "" || c.credentials.SWID == "" {
		return fantasy.DefenseVsPositionTable{}, fmt.Errorf("ESPN credentials required")
	}
	if league.Ref.Season != c.season {
		return fantasy.DefenseVsPositionTable{}, fmt.Errorf("ESPN season mismatch")
	}

	response, err := c.readLeagueWithViews(
		ctx, league.Ref.ExternalID, 0, []string{"mPositionalRatings"}, nil,
	)
	if err != nil {
		return fantasy.DefenseVsPositionTable{}, err
	}
	return mapESPNDefenseRatings(response.PositionAgainstOpponent)
}

// mapESPNDefenseRatings requires every supported position before returning a table.
func mapESPNDefenseRatings(response positionAgainstOpponentResponse) (fantasy.DefenseVsPositionTable, error) {
	ratings := make(map[string]fantasy.PositionDefense, len(espnDefensePositions))
	for _, position := range espnDefensePositions {
		raw, found := response.PositionalRatings[position.id]
		if !found {
			return fantasy.DefenseVsPositionTable{}, fmt.Errorf("ESPN positional ratings missing %s", position.position)
		}
		mapped, err := mapESPNPositionRatings(position.position, raw)
		if err != nil {
			return fantasy.DefenseVsPositionTable{}, err
		}
		ratings[position.position] = mapped
	}
	return fantasy.DefenseVsPositionTable{Scoring: "league", Positions: ratings}, nil
}

// mapESPNPositionRatings maps one provider position and rejects incomplete team data.
func mapESPNPositionRatings(position string, raw positionalRating) (fantasy.PositionDefense, error) {
	byTeam := make(map[string]fantasy.DefenseRating, len(raw.RatingsByOpponent))
	for teamID, rating := range raw.RatingsByOpponent {
		numericID, err := strconv.Atoi(teamID)
		if err != nil {
			return fantasy.PositionDefense{}, fmt.Errorf("invalid ESPN team ID for %s", position)
		}
		team := proTeamAbbreviation(numericID)
		if team == "" {
			return fantasy.PositionDefense{}, fmt.Errorf("unknown ESPN team ID for %s", position)
		}
		if rating.Rank < 1 || rating.Rank > fantasy.NFLTeamCount {
			return fantasy.PositionDefense{}, fmt.Errorf("invalid ESPN rank for %s", position)
		}
		byTeam[team] = fantasy.DefenseRating{
			PointsAllowedPerGame: rating.Average,
			Rank:                 rating.Rank,
		}
	}
	if len(byTeam) != fantasy.NFLTeamCount {
		return fantasy.PositionDefense{}, fmt.Errorf("ESPN positional ratings have %d teams for %s", len(byTeam), position)
	}
	return fantasy.PositionDefense{LeagueAverage: raw.Average, ByTeam: byTeam}, nil
}
