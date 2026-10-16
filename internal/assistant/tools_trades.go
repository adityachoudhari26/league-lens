package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"connectrpc.com/connect/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/store"
)

const evaluateTradeSchema = `{
  "type": "object",
  "properties": {
    "league_season_id": {"type": "string", "format": "uuid"},
    "give_player_ids": {"type": "array", "minItems": 1, "maxItems": 4, "items": {"type": "string", "format": "uuid"}},
    "get_player_ids": {"type": "array", "minItems": 1, "maxItems": 4, "items": {"type": "string", "format": "uuid"}}
  },
  "required": ["league_season_id", "give_player_ids", "get_player_ids"],
  "additionalProperties": false
}`

const tradePointsBasis = "league-scored points from synced matchups, last 4 weeks; latest projection"
const tradeLineupLimitation = "recent points unavailable for players on teams League Lens hasn't synced lineups for"

type evaluateTradeInput struct {
	LeagueSeasonID string   `json:"league_season_id"`
	GivePlayerIDs  []string `json:"give_player_ids"`
	GetPlayerIDs   []string `json:"get_player_ids"`
}

type tradeToolResult struct {
	Players           []tradePlayer `json:"players"`
	OwnerRosterCounts rosterCounts  `json:"owner_roster_counts"`
	Basis             string        `json:"basis"`
	Limitation        string        `json:"limitation,omitempty"`
}

type tradePlayer struct {
	PlayerID          string        `json:"player_id"`
	Side              string        `json:"side"`
	Name              string        `json:"name"`
	TeamName          string        `json:"team_name"`
	EligiblePositions []string      `json:"eligible_positions"`
	NFLTeam           string        `json:"nfl_team,omitempty"`
	InjuryStatus      string        `json:"injury_status,omitempty"`
	ByeWeek           *int32        `json:"bye_week,omitempty"`
	RecentPoints      []tradePoints `json:"recent_points"`
	AveragePoints     *float64      `json:"average_recent_points,omitempty"`
	ProjectedPoints   *float64      `json:"projected_points,omitempty"`
	primaryPosition   string
	teamID            string
}

type tradePoints struct {
	Week   int32    `json:"week"`
	Points *float64 `json:"points"`
}

type rosterCounts struct {
	Before map[string]int `json:"before"`
	After  map[string]int `json:"after"`
}

type tradeValidationError struct {
	message string
}

func (e tradeValidationError) Error() string { return e.message }

func tradeTools(service *Service) []assistantTool {
	return []assistantTool{newAssistantTool(
		"evaluate_trade",
		"Returns current roster context, recent league-scored points, projections, and owner position counts for a proposed trade. It does not judge the trade.",
		evaluateTradeSchema,
		fixedToolLabel("Evaluating trade"),
		func(ctx context.Context, raw json.RawMessage) (string, error) {
			input, err := decodeToolInput[evaluateTradeInput](raw)
			if err != nil {
				return "", errInvalidToolInput
			}
			result, err := service.evaluateTrade(ctx, input)
			if err != nil {
				return "", err
			}
			content, err := json.Marshal(result)
			return string(content), err
		},
	)}
}

// evaluateTrade validates a proposed exchange against the owner's latest roster snapshots.
func (s *Service) evaluateTrade(ctx context.Context, input evaluateTradeInput) (tradeToolResult, error) {
	leagueID, err := parseTradeUUID(input.LeagueSeasonID)
	if err != nil || len(input.GivePlayerIDs) < 1 || len(input.GivePlayerIDs) > 4 ||
		len(input.GetPlayerIDs) < 1 || len(input.GetPlayerIDs) > 4 {
		return tradeToolResult{}, errInvalidToolInput
	}
	giveIDs, err := parseTradePlayerIDs(input.GivePlayerIDs)
	if err != nil {
		return tradeToolResult{}, err
	}
	getIDs, err := parseTradePlayerIDs(input.GetPlayerIDs)
	if err != nil {
		return tradeToolResult{}, err
	}
	allIDs := append(append([]pgtype.UUID{}, giveIDs...), getIDs...)
	if duplicateUUID(allIDs) {
		return tradeToolResult{}, errInvalidToolInput
	}

	var result tradeToolResult
	err = s.DB.InReadTx(ctx, func(q *store.Queries) error {
		owner, err := q.GetUserByEmail(ctx, s.OwnerEmail)
		if err != nil {
			return err
		}
		team, err := q.AssistantTradeOwnerTeam(ctx, store.AssistantTradeOwnerTeamParams{
			UserID: owner.ID, LeagueSeasonID: leagueID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return connect.NewError(connect.CodeNotFound, "league not found")
		}
		if err != nil {
			return err
		}
		result, err = loadTradeResult(ctx, q, leagueID, team.TeamID, giveIDs, getIDs, allIDs)
		return err
	})
	if err != nil {
		if asTradeValidationError(err) {
			return tradeToolResult{}, err
		}
		if connect.CodeOf(err) != connect.CodeNotFound {
			return tradeToolResult{}, internalError("trade data could not be loaded")
		}
		return tradeToolResult{}, err
	}
	return result, nil
}

func loadTradeResult(
	ctx context.Context,
	q *store.Queries,
	leagueID, ownerTeamID pgtype.UUID,
	giveIDs, getIDs, allIDs []pgtype.UUID,
) (tradeToolResult, error) {
	rows, err := q.AssistantTradeCurrentPlayers(ctx, store.AssistantTradeCurrentPlayersParams{
		LeagueSeasonID: leagueID, PlayerIds: allIDs,
	})
	if err != nil {
		return tradeToolResult{}, err
	}
	players := make(map[string]tradePlayer, len(rows))
	for _, row := range rows {
		player := tradePlayer{
			PlayerID: row.PlayerID, Name: row.PlayerName, TeamName: row.TeamName,
			NFLTeam: row.ProTeam, InjuryStatus: row.InjuryStatus,
		}
		if row.TeamID.Valid {
			player.teamID = row.TeamID.String()
		}
		var positions []string
		if err := json.Unmarshal(row.EligiblePositionsJson, &positions); err != nil {
			return tradeToolResult{}, err
		}
		player.EligiblePositions = positions
		player.primaryPosition = "UNKNOWN"
		if len(positions) > 0 && strings.TrimSpace(positions[0]) != "" {
			player.primaryPosition = strings.ToUpper(positions[0])
		}
		if row.ByeWeek > 0 {
			player.ByeWeek = &row.ByeWeek
		}
		player.ProjectedPoints = numericFloat(row.ProjectedPoints)
		players[player.PlayerID] = player
	}
	givePlayers, err := findTradePlayers(giveIDs, players, ownerTeamID, true)
	if err != nil {
		return tradeToolResult{}, err
	}
	getPlayers, err := findTradePlayers(getIDs, players, ownerTeamID, false)
	if err != nil {
		return tradeToolResult{}, err
	}
	opponentTeamID := getPlayers[0].teamID
	for _, player := range getPlayers[1:] {
		if player.teamID != opponentTeamID {
			return tradeToolResult{}, tradeValidationError{message: "get players must all come from the same other team"}
		}
	}
	opponentID, err := parseTradeUUID(opponentTeamID)
	if err != nil {
		return tradeToolResult{}, err
	}

	points, err := q.AssistantTradeRecentPoints(ctx, store.AssistantTradeRecentPointsParams{
		LeagueSeasonID: leagueID, PlayerIds: allIDs,
	})
	if err != nil {
		return tradeToolResult{}, err
	}
	pointsByPlayer := make(map[string][]tradePoints, len(allIDs))
	for _, row := range points {
		pointsByPlayer[row.PlayerID] = append(pointsByPlayer[row.PlayerID], tradePoints{
			Week: row.Period, Points: numericFloat(row.Points),
		})
	}
	for id, player := range players {
		player.RecentPoints = pointsByPlayer[id]
		player.AveragePoints = averageTradePoints(player.RecentPoints)
		players[id] = player
	}
	for i := range givePlayers {
		givePlayers[i] = players[givePlayers[i].PlayerID]
	}
	for i := range getPlayers {
		getPlayers[i] = players[getPlayers[i].PlayerID]
	}

	countRows, err := q.AssistantTradeOwnerRosterCounts(ctx, store.AssistantTradeOwnerRosterCountsParams{
		LeagueSeasonID: leagueID, TeamID: ownerTeamID,
	})
	if err != nil {
		return tradeToolResult{}, err
	}
	before := make(map[string]int, len(countRows))
	for _, row := range countRows {
		before[row.PrimaryPosition] = int(row.PlayerCount)
	}
	after := copyRosterCounts(before)
	for _, player := range givePlayers {
		after[player.primaryPosition]--
	}
	for _, player := range getPlayers {
		after[player.primaryPosition]++
	}

	coverage, err := q.AssistantTradeLineupCoverage(ctx, store.AssistantTradeLineupCoverageParams{
		LeagueSeasonID: leagueID, TeamIds: []pgtype.UUID{ownerTeamID, opponentID},
	})
	if err != nil {
		return tradeToolResult{}, err
	}
	result := tradeToolResult{
		Players:           make([]tradePlayer, 0, len(givePlayers)+len(getPlayers)),
		OwnerRosterCounts: rosterCounts{Before: before, After: after},
		Basis:             tradePointsBasis,
	}
	for _, player := range givePlayers {
		player.Side = "give"
		result.Players = append(result.Players, player)
	}
	for _, player := range getPlayers {
		player.Side = "get"
		result.Players = append(result.Players, player)
	}
	for _, row := range coverage {
		if !row.HasLineup {
			result.Limitation = tradeLineupLimitation
			break
		}
	}
	return result, nil
}

func findTradePlayers(ids []pgtype.UUID, players map[string]tradePlayer, ownerTeamID pgtype.UUID, giving bool) ([]tradePlayer, error) {
	found := make([]tradePlayer, 0, len(ids))
	for _, id := range ids {
		player, ok := players[id.String()]
		if !ok || player.teamID == "" {
			return nil, tradeValidationError{message: fmt.Sprintf("player %s is not on a current roster", id.String())}
		}
		if giving && player.teamID != ownerTeamID.String() {
			return nil, tradeValidationError{message: fmt.Sprintf("%s is not on your team to give", player.Name)}
		}
		if !giving && player.teamID == ownerTeamID.String() {
			return nil, tradeValidationError{message: fmt.Sprintf("%s is on your team; choose a player from another team", player.Name)}
		}
		found = append(found, player)
	}
	return found, nil
}

func parseTradePlayerIDs(values []string) ([]pgtype.UUID, error) {
	ids := make([]pgtype.UUID, 0, len(values))
	for _, value := range values {
		id, err := parseTradeUUID(value)
		if err != nil {
			return nil, errInvalidToolInput
		}
		ids = append(ids, id)
	}
	if duplicateUUID(ids) {
		return nil, errInvalidToolInput
	}
	return ids, nil
}

func parseTradeUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	err := id.Scan(strings.TrimSpace(value))
	return id, err
}

func duplicateUUID(ids []pgtype.UUID) bool {
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id.String()] {
			return true
		}
		seen[id.String()] = true
	}
	return false
}

func numericFloat(value pgtype.Numeric) *float64 {
	if !value.Valid {
		return nil
	}
	converted, err := value.Float64Value()
	if err != nil || !converted.Valid {
		return nil
	}
	return &converted.Float64
}

func averageTradePoints(points []tradePoints) *float64 {
	var total float64
	var count int
	for _, point := range points {
		if point.Points == nil {
			continue
		}
		total += *point.Points
		count++
	}
	if count == 0 {
		return nil
	}
	average := math.Round(total/float64(count)*100) / 100
	return &average
}

func copyRosterCounts(counts map[string]int) map[string]int {
	copied := make(map[string]int, len(counts))
	for position, count := range counts {
		copied[position] = count
	}
	return copied
}

func asTradeValidationError(err error) bool {
	var validation tradeValidationError
	return errors.As(err, &validation)
}
