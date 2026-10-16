package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/espn"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

const getInjuryReportDescription = "Current injury status, injury, expected return date and the latest news note " +
	"for NFL players. Pass league_season_id for injured players on the user's roster in that league, " +
	"or player_ids for specific players. Use it for start/sit, waiver and trade questions about health."

const getInjuryReportSchema = `{
  "type": "object",
  "properties": {
    "league_season_id": {
      "type": "string", "format": "uuid",
      "description": "ID returned by list_leagues; reports injured players on the user's roster in that league"
    },
    "player_ids": {
      "type": "array", "minItems": 1, "maxItems": 10, "items": {"type": "string", "format": "uuid"},
      "description": "League Lens player IDs returned by roster or player tools"
    }
  },
  "additionalProperties": false
}`

const (
	maxInjuryPlayerIDs = 10
	injuryToolTimeout  = 10 * time.Second
	// Questionable/Doubtful entries older than this are stale; long-term statuses never expire.
	shortTermInjuryMaxAge = 21 * 24 * time.Hour
	activeInjuryStatus    = "Active"
)

// shortTermInjuryStatuses are game-week designations that ESPN can leave behind after a player recovers.
var shortTermInjuryStatuses = []string{"Questionable", "Doubtful", "Probable", "Day-To-Day"}

// injuryStatusLabels shortens ESPN statuses that fantasy managers know by another name.
var injuryStatusLabels = map[string]string{"Injured Reserve": "IR"}

// InjurySource reads each ESPN athlete's newest public injury entry; nil means none.
type InjurySource interface {
	AthleteInjuries(ctx context.Context, season int, athleteIDs []int64) (map[int64]*espn.AthleteInjury, error)
}

type injuryToolInput struct {
	LeagueSeasonID string   `json:"league_season_id"`
	PlayerIDs      []string `json:"player_ids"`
}

// injuryRequest is validated input: an optional league and distinct requested players.
type injuryRequest struct {
	league    pgtype.UUID
	playerIDs []pgtype.UUID
}

// injuryPlayer is a stored player with its ESPN athlete ID (0 when unmapped).
// requested players are reported even when healthy; roster-only ones only when injured.
type injuryPlayer struct {
	name      string
	nflTeam   string
	sleeperID string
	athleteID int64
	requested bool
}

type injuryReportResult struct {
	Players []injuryReportPlayer `json:"players"`
}

type injuryReportPlayer struct {
	Name       string     `json:"name"`
	NFLTeam    string     `json:"nfl_team"`
	Status     string     `json:"status"`
	Injury     string     `json:"injury,omitempty"`
	ReturnDate string     `json:"return_date,omitempty"`
	Note       string     `json:"note,omitempty"`
	Updated    *time.Time `json:"updated,omitempty"`
}

func injuryTools(service *Service) []assistantTool {
	return []assistantTool{newAssistantTool(
		"get_injury_report",
		getInjuryReportDescription,
		getInjuryReportSchema,
		fixedToolLabel("Checking injury reports"),
		func(ctx context.Context, raw json.RawMessage) (string, error) {
			input, err := decodeToolInput[injuryToolInput](raw)
			if err != nil {
				return "", errInvalidToolInput
			}
			request, err := parseInjuryInput(input)
			if err != nil {
				return "", err
			}
			ctx, cancel := context.WithTimeout(ctx, injuryToolTimeout)
			defer cancel()
			result, err := service.injuryReport(ctx, request)
			if err != nil {
				return "", err
			}
			content, err := json.Marshal(result)
			return string(content), err
		},
	)}
}

func injuryInputError(format string, args ...any) error {
	return toolValidationError{message: fmt.Sprintf(format, args...)}
}

// parseInjuryInput requires a league, player IDs, or both.
func parseInjuryInput(input injuryToolInput) (injuryRequest, error) {
	if input.LeagueSeasonID == "" && len(input.PlayerIDs) == 0 {
		return injuryRequest{}, injuryInputError("pass league_season_id or player_ids")
	}
	league, err := parseInjuryLeague(input.LeagueSeasonID)
	if err != nil {
		return injuryRequest{}, err
	}
	playerIDs, err := parseInjuryPlayerIDs(input.PlayerIDs)
	if err != nil {
		return injuryRequest{}, err
	}
	return injuryRequest{league: league, playerIDs: playerIDs}, nil
}

// parseInjuryLeague returns an invalid UUID for an omitted league.
func parseInjuryLeague(value string) (pgtype.UUID, error) {
	if value == "" {
		return pgtype.UUID{}, nil
	}
	league, err := parseTradeUUID(value)
	if err != nil {
		return pgtype.UUID{}, errInvalidToolInput
	}
	return league, nil
}

// parseInjuryPlayerIDs accepts at most ten UUIDs and drops duplicates.
func parseInjuryPlayerIDs(values []string) ([]pgtype.UUID, error) {
	if len(values) > maxInjuryPlayerIDs {
		return nil, injuryInputError("pass at most %d player_ids", maxInjuryPlayerIDs)
	}
	playerIDs := make([]pgtype.UUID, 0, len(values))
	for _, value := range values {
		playerID, err := parseTradeUUID(value)
		if err != nil {
			return nil, errInvalidToolInput
		}
		if !slices.Contains(playerIDs, playerID) {
			playerIDs = append(playerIDs, playerID)
		}
	}
	return playerIDs, nil
}

// injuryReport loads the stored players, maps them to ESPN athletes, and reads
// ESPN's current-season injury lists for the mapped ones.
func (s *Service) injuryReport(ctx context.Context, request injuryRequest) (injuryReportResult, error) {
	players, err := s.loadInjuryPlayers(ctx, request)
	if err != nil {
		return injuryReportResult{}, err
	}
	s.mapSleeperInjuryAthletes(ctx, players)
	athleteIDs := injuryAthleteIDs(players)
	if len(athleteIDs) == 0 {
		return injuryReportResult{Players: []injuryReportPlayer{}}, nil
	}
	now := time.Now()
	injuries, err := s.Injuries.AthleteInjuries(ctx, fantasy.CurrentSeason(now), athleteIDs)
	if err != nil {
		return injuryReportResult{}, internalError("injury reports could not be loaded")
	}
	return buildInjuryReport(players, injuries, now), nil
}

// loadInjuryPlayers reads the requested players and, with a league, the owner's roster
// in it. An unknown requested player is a validation error the model can correct.
func (s *Service) loadInjuryPlayers(ctx context.Context, request injuryRequest) ([]injuryPlayer, error) {
	var players []injuryPlayer
	err := s.DB.InReadTx(ctx, func(q *store.Queries) error {
		owner, err := q.GetUserByEmail(ctx, s.OwnerEmail)
		if err != nil {
			return err
		}
		rosterIDs, err := readInjuryOwnerRoster(ctx, q, owner.ID, request.league)
		if err != nil {
			return err
		}
		rows, err := q.AssistantInjuryPlayers(ctx, unionPlayerIDs(request.playerIDs, rosterIDs))
		if err != nil {
			return err
		}
		if missing := missingInjuryPlayer(request.playerIDs, rows); missing.Valid {
			return injuryInputError("unknown player_id %s", missing.String())
		}
		players = injuryPlayersFromRows(rows, request.playerIDs)
		return nil
	})
	if err != nil {
		return nil, injuryLoadError(err)
	}
	return players, nil
}

// readInjuryOwnerRoster returns nil without a league, and the player IDs on the owner
// team's latest successful snapshot with one. Another owner's league is not found.
func readInjuryOwnerRoster(ctx context.Context, q *store.Queries, owner, league pgtype.UUID) ([]pgtype.UUID, error) {
	if !league.Valid {
		return nil, nil
	}
	team, err := q.MatchupDetailOwnerTeam(ctx, store.MatchupDetailOwnerTeamParams{
		UserID: owner, LeagueSeasonID: league,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, "league not found")
	}
	if err != nil {
		return nil, err
	}
	return q.AssistantInjuryOwnerRosterPlayerIDs(ctx, store.AssistantInjuryOwnerRosterPlayerIDsParams{
		LeagueSeasonID: league, TeamID: team.TeamID,
	})
}

func unionPlayerIDs(requested, roster []pgtype.UUID) []pgtype.UUID {
	playerIDs := slices.Clone(requested)
	for _, playerID := range roster {
		if !slices.Contains(playerIDs, playerID) {
			playerIDs = append(playerIDs, playerID)
		}
	}
	return playerIDs
}

func missingInjuryPlayer(requested []pgtype.UUID, rows []store.AssistantInjuryPlayersRow) pgtype.UUID {
	for _, playerID := range requested {
		found := slices.ContainsFunc(rows, func(row store.AssistantInjuryPlayersRow) bool {
			return row.PlayerID == playerID
		})
		if !found {
			return playerID
		}
	}
	return pgtype.UUID{}
}

func injuryPlayersFromRows(rows []store.AssistantInjuryPlayersRow, requested []pgtype.UUID) []injuryPlayer {
	players := make([]injuryPlayer, 0, len(rows))
	for _, row := range rows {
		players = append(players, injuryPlayer{
			name: row.DisplayName, nflTeam: row.ProTeam, sleeperID: row.SleeperID,
			athleteID: espnAthleteID(row.EspnID), requested: slices.Contains(requested, row.PlayerID),
		})
	}
	return players
}

// espnAthleteID returns 0 for empty or non-positive IDs; ESPN D/ST IDs are negative
// and have no athlete injury list.
func espnAthleteID(value string) int64 {
	athleteID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || athleteID <= 0 {
		return 0
	}
	return athleteID
}

// mapSleeperInjuryAthletes fills missing ESPN IDs from the shared player catalog's
// Sleeper namespace. Without a catalog those players stay unmapped and are skipped.
func (s *Service) mapSleeperInjuryAthletes(ctx context.Context, players []injuryPlayer) {
	if !slices.ContainsFunc(players, needsCatalogAthleteID) {
		return
	}
	catalog, err := s.trendingCatalog(ctx)
	if err != nil {
		return
	}
	for index := range players {
		if needsCatalogAthleteID(players[index]) {
			players[index].athleteID = catalogAthleteID(catalog, players[index].sleeperID)
		}
	}
}

func needsCatalogAthleteID(player injuryPlayer) bool {
	return player.athleteID == 0 && player.sleeperID != ""
}

func catalogAthleteID(catalog playerids.Catalog, sleeperID string) int64 {
	entry, found := catalog.Lookup(string(fantasy.PlayerIDSleeper), sleeperID)
	if !found {
		return 0
	}
	return espnAthleteID(entry.IDs["espn"])
}

func injuryAthleteIDs(players []injuryPlayer) []int64 {
	athleteIDs := make([]int64, 0, len(players))
	for _, player := range players {
		if player.athleteID != 0 && !slices.Contains(athleteIDs, player.athleteID) {
			athleteIDs = append(athleteIDs, player.athleteID)
		}
	}
	return athleteIDs
}

// buildInjuryReport keeps players ESPN answered for: injured ones always, and healthy
// requested ones as "Active". Unmapped players and failed lookups are left out.
func buildInjuryReport(players []injuryPlayer, injuries map[int64]*espn.AthleteInjury, now time.Time) injuryReportResult {
	result := injuryReportResult{Players: []injuryReportPlayer{}}
	for _, player := range players {
		injury, answered := injuries[player.athleteID]
		if player.athleteID == 0 || !answered {
			continue
		}
		current := isCurrentInjury(injury, now)
		if current {
			result.Players = append(result.Players, injuredReportPlayer(player, injury))
		} else if player.requested {
			result.Players = append(result.Players, injuryReportPlayer{
				Name: player.name, NFLTeam: player.nflTeam, Status: activeInjuryStatus,
			})
		}
	}
	return result
}

// isCurrentInjury treats ESPN's newest entry as current unless it is Active, or a
// short-term designation that hasn't been updated in three weeks.
func isCurrentInjury(injury *espn.AthleteInjury, now time.Time) bool {
	if injury == nil || injury.Status == activeInjuryStatus {
		return false
	}
	shortTerm := slices.Contains(shortTermInjuryStatuses, injury.Status)
	return !shortTerm || now.Sub(injury.Updated) <= shortTermInjuryMaxAge
}

func injuredReportPlayer(player injuryPlayer, injury *espn.AthleteInjury) injuryReportPlayer {
	status := injury.Status
	if label, found := injuryStatusLabels[status]; found {
		status = label
	}
	updated := injury.Updated
	return injuryReportPlayer{
		Name: player.name, NFLTeam: player.nflTeam, Status: status, Injury: injury.Injury,
		ReturnDate: injury.ReturnDate, Note: injury.Note, Updated: &updated,
	}
}

func injuryLoadError(err error) error {
	if asToolValidationError(err) || connect.CodeOf(err) == connect.CodeNotFound {
		return err
	}
	return internalError("injury report players could not be loaded")
}
