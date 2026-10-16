package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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

type tradeIDs struct {
	league pgtype.UUID
	give   []pgtype.UUID
	get    []pgtype.UUID
	all    []pgtype.UUID
}

type tradeToolResult struct {
	Players           []tradePlayer `json:"players"`
	OwnerRosterCounts rosterCounts  `json:"owner_roster_counts"`
	Basis             string        `json:"basis"`
	Limitation        string        `json:"limitation,omitempty"`
}

type tradePlayer struct {
	PlayerID           string        `json:"player_id"`
	Side               string        `json:"side"`
	Name               string        `json:"name"`
	TeamName           string        `json:"team_name"`
	EligiblePositions  []string      `json:"eligible_positions"`
	NFLTeam            string        `json:"nfl_team,omitempty"`
	InjuryStatus       string        `json:"injury_status,omitempty"`
	ByeWeek            *int32        `json:"bye_week,omitempty"`
	RecentPoints       []tradePoints `json:"recent_points"`
	AveragePoints      *float64      `json:"average_recent_points,omitempty"`
	ProjectedPoints    *float64      `json:"projected_points,omitempty"`
	RestOfSeasonPoints *float64      `json:"rest_of_season_points,omitempty"`
	RestOfSeasonWeeks  *int          `json:"rest_of_season_weeks,omitempty"`
	primaryPosition    string
	teamID             string
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
		"Returns current roster context, recent league-scored points, projections, and owner position counts for a proposed trade. It does not judge the trade. "+
			"Each player may include rest_of_season_points, league-scored projected points "+
			"from the next unplayed week through week 17, and rest_of_season_weeks, the number of projected games it covers.",
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
	ids, err := parseTradeInput(input)
	if err != nil {
		return tradeToolResult{}, err
	}

	var result tradeToolResult
	err = s.DB.InReadTx(ctx, func(q *store.Queries) error {
		owner, err := q.GetUserByEmail(ctx, s.OwnerEmail)
		if err != nil {
			return err
		}
		team, err := q.AssistantTradeOwnerTeam(ctx, store.AssistantTradeOwnerTeamParams{
			UserID: owner.ID, LeagueSeasonID: ids.league,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return connect.NewError(connect.CodeNotFound, "league not found")
		}
		if err != nil {
			return err
		}
		result, err = loadTradeResult(ctx, q, ids, team.TeamID)
		return err
	})
	if err != nil {
		return tradeToolResult{}, tradeLoadError(err)
	}
	s.addTradeRestOfSeason(ctx, input.LeagueSeasonID, &result)
	return result, nil
}

func parseTradeInput(input evaluateTradeInput) (tradeIDs, error) {
	leagueID, err := parseTradeUUID(input.LeagueSeasonID)
	if err != nil || len(input.GivePlayerIDs) < 1 || len(input.GivePlayerIDs) > 4 ||
		len(input.GetPlayerIDs) < 1 || len(input.GetPlayerIDs) > 4 {
		return tradeIDs{}, errInvalidToolInput
	}
	giveIDs, err := parseTradePlayerIDs(input.GivePlayerIDs)
	if err != nil {
		return tradeIDs{}, err
	}
	getIDs, err := parseTradePlayerIDs(input.GetPlayerIDs)
	if err != nil {
		return tradeIDs{}, err
	}
	allIDs := append(append([]pgtype.UUID{}, giveIDs...), getIDs...)
	if duplicateUUID(allIDs) {
		return tradeIDs{}, errInvalidToolInput
	}
	return tradeIDs{league: leagueID, give: giveIDs, get: getIDs, all: allIDs}, nil
}

func tradeLoadError(err error) error {
	if asTradeValidationError(err) || connect.CodeOf(err) == connect.CodeNotFound {
		return err
	}
	return internalError("trade data could not be loaded")
}

func loadTradeResult(
	ctx context.Context,
	q *store.Queries,
	ids tradeIDs,
	ownerTeamID pgtype.UUID,
) (tradeToolResult, error) {
	rows, err := q.AssistantTradeCurrentPlayers(ctx, store.AssistantTradeCurrentPlayersParams{
		LeagueSeasonID: ids.league, PlayerIds: ids.all,
	})
	if err != nil {
		return tradeToolResult{}, err
	}
	players, err := tradePlayersByID(rows)
	if err != nil {
		return tradeToolResult{}, err
	}
	if err := addTradeRecentPoints(ctx, q, ids, players); err != nil {
		return tradeToolResult{}, err
	}

	givePlayers, err := findTradePlayers(ids.give, players, ownerTeamID, true)
	if err != nil {
		return tradeToolResult{}, err
	}
	getPlayers, err := findTradePlayers(ids.get, players, ownerTeamID, false)
	if err != nil {
		return tradeToolResult{}, err
	}
	opponentID, err := tradeOpponent(getPlayers)
	if err != nil {
		return tradeToolResult{}, err
	}

	counts, err := ownerRosterCounts(ctx, q, ids.league, ownerTeamID, givePlayers, getPlayers)
	if err != nil {
		return tradeToolResult{}, err
	}
	coverage, err := q.AssistantTradeLineupCoverage(ctx, store.AssistantTradeLineupCoverageParams{
		LeagueSeasonID: ids.league, TeamIds: []pgtype.UUID{ownerTeamID, opponentID},
	})
	if err != nil {
		return tradeToolResult{}, err
	}
	return tradeToolResult{
		Players:           tradeResultPlayers(givePlayers, getPlayers),
		OwnerRosterCounts: counts,
		Basis:             tradePointsBasis,
		Limitation:        tradeLimitation(coverage),
	}, nil
}

func tradePlayersByID(rows []store.AssistantTradeCurrentPlayersRow) (map[string]tradePlayer, error) {
	players := make(map[string]tradePlayer, len(rows))
	for _, row := range rows {
		player, err := tradePlayerFromRow(row)
		if err != nil {
			return nil, err
		}
		players[player.PlayerID] = player
	}
	return players, nil
}

func tradePlayerFromRow(row store.AssistantTradeCurrentPlayersRow) (tradePlayer, error) {
	player := tradePlayer{
		PlayerID: row.PlayerID, Name: row.PlayerName, TeamName: row.TeamName,
		NFLTeam: row.ProTeam, InjuryStatus: row.InjuryStatus,
	}
	if row.TeamID.Valid {
		player.teamID = row.TeamID.String()
	}
	if err := json.Unmarshal(row.EligiblePositionsJson, &player.EligiblePositions); err != nil {
		return tradePlayer{}, err
	}
	player.primaryPosition = "UNKNOWN"
	if len(player.EligiblePositions) > 0 && strings.TrimSpace(player.EligiblePositions[0]) != "" {
		player.primaryPosition = strings.ToUpper(player.EligiblePositions[0])
	}
	if row.ByeWeek > 0 {
		player.ByeWeek = &row.ByeWeek
	}
	player.ProjectedPoints = numericFloat(row.ProjectedPoints)
	return player, nil
}

func addTradeRecentPoints(
	ctx context.Context,
	q *store.Queries,
	ids tradeIDs,
	players map[string]tradePlayer,
) error {
	rows, err := q.AssistantTradeRecentPoints(ctx, store.AssistantTradeRecentPointsParams{
		LeagueSeasonID: ids.league, PlayerIds: ids.all,
	})
	if err != nil {
		return err
	}
	pointsByPlayer := make(map[string][]tradePoints, len(ids.all))
	for _, row := range rows {
		pointsByPlayer[row.PlayerID] = append(pointsByPlayer[row.PlayerID], tradePoints{
			Week: row.Period, Points: numericFloat(row.Points),
		})
	}
	for playerID, player := range players {
		player.RecentPoints = pointsByPlayer[playerID]
		player.AveragePoints = averageTradePoints(player.RecentPoints)
		players[playerID] = player
	}
	return nil
}

func tradeOpponent(getPlayers []tradePlayer) (pgtype.UUID, error) {
	teamID := getPlayers[0].teamID
	for _, player := range getPlayers[1:] {
		if player.teamID != teamID {
			return pgtype.UUID{}, tradeValidationError{message: "get players must all come from the same other team"}
		}
	}
	return parseTradeUUID(teamID)
}

func ownerRosterCounts(
	ctx context.Context,
	q *store.Queries,
	league, ownerTeam pgtype.UUID,
	give, get []tradePlayer,
) (rosterCounts, error) {
	rows, err := q.AssistantTradeOwnerRosterCounts(ctx, store.AssistantTradeOwnerRosterCountsParams{
		LeagueSeasonID: league, TeamID: ownerTeam,
	})
	if err != nil {
		return rosterCounts{}, err
	}
	before := make(map[string]int, len(rows))
	for _, row := range rows {
		before[row.PrimaryPosition] = int(row.PlayerCount)
	}
	after := maps.Clone(before)
	for _, player := range give {
		after[player.primaryPosition]--
	}
	for _, player := range get {
		after[player.primaryPosition]++
	}
	return rosterCounts{Before: before, After: after}, nil
}

func tradeLimitation(coverage []store.AssistantTradeLineupCoverageRow) string {
	for _, row := range coverage {
		if !row.HasLineup {
			return tradeLineupLimitation
		}
	}
	return ""
}

func tradeResultPlayers(give, get []tradePlayer) []tradePlayer {
	players := make([]tradePlayer, 0, len(give)+len(get))
	for _, player := range give {
		player.Side = "give"
		players = append(players, player)
	}
	for _, player := range get {
		player.Side = "get"
		players = append(players, player)
	}
	return players
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

func asTradeValidationError(err error) bool {
	var validation tradeValidationError
	return errors.As(err, &validation)
}
