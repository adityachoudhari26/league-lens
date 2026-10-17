package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/leaguesync"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

const getMatchupDifficultyDescription = "How many fantasy points each NFL defense allows per game to a position this season, with " +
	"rank 1 = fewest allowed (toughest matchup) to 32 = most allowed (easiest). By default rates this week's opponent " +
	"for every player on the user's roster in the league; pass teams (and optionally position) to rate specific defenses. " +
	"scoring says how the points are counted: league (the league's own scoring), ppr, half_ppr or standard. " +
	"Use it for start/sit and streaming questions."

const getMatchupDifficultySchema = `{
  "type": "object",
  "properties": {
    "league_season_id": {"type": "string", "format": "uuid", "description": "ID returned by list_leagues"},
    "week": {"type": "integer", "minimum": 1, "maximum": 18, "description": "Defaults to the current NFL week"},
    "teams": {"type": "array", "minItems": 1, "maxItems": 8, "items": {"type": "string"}, "description": "NFL team abbreviations, e.g. KC"},
    "position": {"type": "string", "enum": ["QB", "RB", "WR", "TE", "K", "DST"]}
  },
  "required": ["league_season_id"],
  "additionalProperties": false
}`

const (
	matchupDifficultyToolTimeout = 15 * time.Second
	matchupRatingsCacheTTL       = 6 * time.Hour
)

var (
	errMatchupRatingsUnavailable = errors.New("matchup ratings unavailable")
	matchupDifficultyPositions   = []string{"QB", "RB", "WR", "TE", "K", "DST"}
)

type matchupDifficultyToolInput struct {
	LeagueSeasonID string   `json:"league_season_id"`
	Week           *int     `json:"week"`
	Teams          []string `json:"teams"`
	Position       string   `json:"position"`
}

type matchupDifficultyRequest struct {
	leagueSeasonID string
	league         pgtype.UUID
	week           *int
	teams          []string
	position       string
}

type matchupDifficultyResult struct {
	Week     int                        `json:"week,omitempty"`
	Scoring  string                     `json:"scoring"`
	Players  []matchupDifficultyPlayer  `json:"players,omitempty"`
	Defenses []matchupDifficultyDefense `json:"defenses,omitempty"`
}

type matchupDifficultyPlayer struct {
	Name                 string   `json:"name"`
	Position             string   `json:"position"`
	NFLTeam              string   `json:"nfl_team,omitempty"`
	Opponent             string   `json:"opponent"`
	PointsAllowedPerGame *float64 `json:"points_allowed_per_game,omitempty"`
	LeagueAverage        *float64 `json:"league_average,omitempty"`
	Rank                 *int     `json:"rank,omitempty"`
}

type matchupDifficultyDefense struct {
	Team                 string   `json:"team"`
	Position             string   `json:"position"`
	PointsAllowedPerGame *float64 `json:"points_allowed_per_game,omitempty"`
	LeagueAverage        *float64 `json:"league_average,omitempty"`
	Rank                 *int     `json:"rank,omitempty"`
}

type matchupRatingsCacheEntry struct {
	table   fantasy.DefenseVsPositionTable
	expires time.Time
}

type matchupRatingsCache struct {
	mu      sync.Mutex
	entries map[string]matchupRatingsCacheEntry
}

type matchupDifficultyRosterWeek struct {
	week    int32
	players []store.MatchupDifficultyOwnerRosterRow
	games   []store.AssistantScheduleGamesRow
}

func matchupDifficultyTools(service *Service) []assistantTool {
	return []assistantTool{newAssistantTool(
		"get_matchup_difficulty",
		getMatchupDifficultyDescription,
		getMatchupDifficultySchema,
		fixedToolLabel("Checking matchups"),
		func(ctx context.Context, raw json.RawMessage) (string, error) {
			input, err := decodeToolInput[matchupDifficultyToolInput](raw)
			if err != nil {
				return "", errInvalidToolInput
			}
			request, err := parseMatchupDifficultyInput(input)
			if err != nil {
				return "", err
			}
			ctx, cancel := context.WithTimeout(ctx, matchupDifficultyToolTimeout)
			defer cancel()
			result, err := service.getMatchupDifficulty(ctx, request)
			if err != nil {
				return "", err
			}
			content, err := json.Marshal(result)
			return string(content), err
		},
	)}
}

func parseMatchupDifficultyInput(input matchupDifficultyToolInput) (matchupDifficultyRequest, error) {
	league, err := parseTradeUUID(input.LeagueSeasonID)
	if err != nil {
		return matchupDifficultyRequest{}, errInvalidToolInput
	}
	teams, err := normalizeScheduleTeams(input.Teams)
	if err != nil {
		return matchupDifficultyRequest{}, err
	}
	if err := validateMatchupDifficultyWeek(input.Week); err != nil {
		return matchupDifficultyRequest{}, err
	}
	if err := validateMatchupDifficultyPosition(input.Position); err != nil {
		return matchupDifficultyRequest{}, err
	}
	return matchupDifficultyRequest{
		leagueSeasonID: input.LeagueSeasonID,
		league:         league,
		week:           input.Week,
		teams:          teams,
		position:       input.Position,
	}, nil
}

func validateMatchupDifficultyWeek(week *int) error {
	if week != nil && (*week < firstScheduleWeek || *week > lastScheduleWeek) {
		return matchupDifficultyInputError("week must be between 1 and 18")
	}
	return nil
}

func validateMatchupDifficultyPosition(position string) error {
	if position == "" {
		return nil
	}
	for _, supported := range matchupDifficultyPositions {
		if position == supported {
			return nil
		}
	}
	return matchupDifficultyInputError("position must be QB, RB, WR, TE, K, or DST")
}

func matchupDifficultyInputError(format string, args ...any) error {
	return toolValidationError{message: fmt.Sprintf(format, args...)}
}

// getMatchupDifficulty verifies ownership before loading provider ratings or private roster data.
func (s *Service) getMatchupDifficulty(
	ctx context.Context,
	request matchupDifficultyRequest,
) (matchupDifficultyResult, error) {
	target, err := s.ownerSyncTarget(ctx, request.leagueSeasonID, errMatchupRatingsUnavailable)
	if err != nil {
		return matchupDifficultyResult{}, err
	}
	if len(request.teams) > 0 {
		return s.matchupDifficultyForTeams(ctx, request, target)
	}
	return s.matchupDifficultyForRoster(ctx, request, target)
}

func (s *Service) matchupDifficultyForTeams(
	ctx context.Context,
	request matchupDifficultyRequest,
	target leaguesync.Target,
) (matchupDifficultyResult, error) {
	table, err := s.cachedMatchupRatings(ctx, request.leagueSeasonID, target)
	if err != nil {
		return matchupDifficultyResult{}, err
	}
	if unknown := unknownMatchupDifficultyTeam(request.teams, table); unknown != "" {
		return matchupDifficultyResult{}, matchupDifficultyInputError("unknown NFL team %s", unknown)
	}
	return matchupDifficultyResult{
		Scoring:  table.Scoring,
		Defenses: buildMatchupDifficultyDefenses(table, request.teams, request.position),
	}, nil
}

func (s *Service) matchupDifficultyForRoster(
	ctx context.Context,
	request matchupDifficultyRequest,
	target leaguesync.Target,
) (matchupDifficultyResult, error) {
	rosterWeek, err := s.loadMatchupDifficultyRoster(ctx, request)
	if err != nil {
		return matchupDifficultyResult{}, err
	}
	table, err := s.cachedMatchupRatings(ctx, request.leagueSeasonID, target)
	if err != nil {
		return matchupDifficultyResult{}, err
	}
	catalog, err := s.matchupDifficultyCatalog(ctx, rosterWeek.players)
	if err != nil {
		return matchupDifficultyResult{}, errMatchupRatingsUnavailable
	}
	return buildMatchupDifficultyResult(rosterWeek, table, catalog, request.position), nil
}

func (s *Service) loadMatchupDifficultyRoster(
	ctx context.Context,
	request matchupDifficultyRequest,
) (matchupDifficultyRosterWeek, error) {
	var rosterWeek matchupDifficultyRosterWeek
	err := s.DB.InReadTx(ctx, func(q *store.Queries) error {
		owner, err := q.GetUserByEmail(ctx, s.OwnerEmail)
		if err != nil {
			return err
		}
		team, err := q.MatchupDetailOwnerTeam(ctx, store.MatchupDetailOwnerTeamParams{
			UserID: owner.ID, LeagueSeasonID: request.league,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return connect.NewError(connect.CodeNotFound, "league not found")
		}
		if err != nil {
			return err
		}
		week := team.ObservedPeriod
		if request.week != nil {
			week = int32(*request.week)
		}
		if week == 0 {
			return matchupDifficultyInputError("pass week; this league hasn't synced a current week yet")
		}
		if err := requireScheduleLoaded(ctx, q, team.Season); err != nil {
			return err
		}
		players, err := q.MatchupDifficultyOwnerRoster(ctx, store.MatchupDifficultyOwnerRosterParams{
			LeagueSeasonID: request.league, TeamID: team.TeamID,
		})
		if err != nil {
			return err
		}
		games, err := q.AssistantScheduleGames(ctx, store.AssistantScheduleGamesParams{
			Season: team.Season, Teams: matchupDifficultyRosterTeams(players),
			FromWeek: week, ToWeek: week,
		})
		if err != nil {
			return err
		}
		rosterWeek = matchupDifficultyRosterWeek{
			week: week, players: players, games: games,
		}
		return nil
	})
	if err != nil {
		if asToolValidationError(err) || connect.CodeOf(err) == connect.CodeNotFound {
			return matchupDifficultyRosterWeek{}, err
		}
		return matchupDifficultyRosterWeek{}, errMatchupRatingsUnavailable
	}
	return rosterWeek, nil
}

func matchupDifficultyRosterTeams(players []store.MatchupDifficultyOwnerRosterRow) []string {
	teams := make([]string, 0, len(players))
	for _, player := range players {
		if player.ProTeam == "" || containsMatchupDifficultyTeam(teams, player.ProTeam) {
			continue
		}
		teams = append(teams, player.ProTeam)
	}
	return teams
}

func containsMatchupDifficultyTeam(teams []string, requested string) bool {
	for _, team := range teams {
		if team == requested {
			return true
		}
	}
	return false
}

func (s *Service) matchupDifficultyCatalog(
	ctx context.Context,
	players []store.MatchupDifficultyOwnerRosterRow,
) (playerids.Catalog, error) {
	if !matchupDifficultyNeedsCatalog(players) {
		return nil, nil
	}
	return s.trendingCatalog(ctx)
}

func matchupDifficultyNeedsCatalog(players []store.MatchupDifficultyOwnerRosterRow) bool {
	for _, player := range players {
		if matchupPositionNeedsCatalog(player.Position, player.SleeperID) {
			return true
		}
	}
	return false
}

func matchupPositionNeedsCatalog(stored, sleeperID string) bool {
	if normalizeMatchupPosition(stored) != "" || (sleeperID != "" && !isNumericSleeperID(sleeperID)) {
		return false
	}
	return true
}

func isNumericSleeperID(sleeperID string) bool {
	_, err := strconv.Atoi(sleeperID)
	return err == nil
}

// matchupPosition prefers stored positions and resolves Sleeper-only IDs from the catalog.
func matchupPosition(stored, sleeperID string, catalog playerids.Catalog) string {
	if position := normalizeMatchupPosition(stored); position != "" {
		return position
	}
	if sleeperID != "" && !isNumericSleeperID(sleeperID) {
		return "DST"
	}
	player, found := catalog.Lookup("sleeper", sleeperID)
	if !found {
		return ""
	}
	return normalizeMatchupPosition(player.Position)
}

func normalizeMatchupPosition(position string) string {
	position = strings.ToUpper(strings.TrimSpace(position))
	switch position {
	case "QB", "RB", "WR", "TE", "K":
		return position
	case "DEF", "D/ST", "DST":
		return "DST"
	case "PK":
		return "K"
	default:
		return ""
	}
}

func buildMatchupDifficultyResult(
	rosterWeek matchupDifficultyRosterWeek,
	table fantasy.DefenseVsPositionTable,
	catalog playerids.Catalog,
	positionFilter string,
) matchupDifficultyResult {
	return matchupDifficultyResult{
		Week: int(rosterWeek.week), Scoring: table.Scoring,
		Players: buildMatchupDifficultyPlayers(rosterWeek.players, rosterWeek.games, table, catalog, positionFilter),
	}
}

func buildMatchupDifficultyPlayers(
	roster []store.MatchupDifficultyOwnerRosterRow,
	games []store.AssistantScheduleGamesRow,
	table fantasy.DefenseVsPositionTable,
	catalog playerids.Catalog,
	positionFilter string,
) []matchupDifficultyPlayer {
	opponents := matchupDifficultyOpponents(games)
	players := make([]matchupDifficultyPlayer, 0, len(roster))
	for _, rosterPlayer := range roster {
		position := matchupPosition(rosterPlayer.Position, rosterPlayer.SleeperID, catalog)
		if position == "" || positionFilter != "" && position != positionFilter {
			continue
		}
		opponent := "BYE"
		if rosterPlayer.ProTeam != "" {
			if scheduledOpponent, found := opponents[rosterPlayer.ProTeam]; found {
				opponent = scheduledOpponent
			}
		}
		player := matchupDifficultyPlayer{
			Name: rosterPlayer.PlayerName, Position: position,
			NFLTeam: rosterPlayer.ProTeam, Opponent: opponent,
		}
		if opponent != "BYE" {
			addMatchupDifficultyRating(&player.PointsAllowedPerGame, &player.LeagueAverage, &player.Rank,
				table.Positions[position], opponent)
		}
		players = append(players, player)
	}
	if len(players) == 0 {
		return nil
	}
	return players
}

func matchupDifficultyOpponents(games []store.AssistantScheduleGamesRow) map[string]string {
	opponents := make(map[string]string, len(games))
	for _, game := range games {
		opponents[game.Team] = game.Opponent
	}
	return opponents
}

func addMatchupDifficultyRating(
	pointsAllowed, leagueAverage **float64,
	rank **int,
	position fantasy.PositionDefense,
	opponent string,
) {
	rating, found := position.ByTeam[opponent]
	if !found {
		return
	}
	roundedPoints := roundMatchupDifficultyPoints(rating.PointsAllowedPerGame)
	roundedAverage := roundMatchupDifficultyPoints(position.LeagueAverage)
	*pointsAllowed = &roundedPoints
	*leagueAverage = &roundedAverage
	*rank = &rating.Rank
}

func buildMatchupDifficultyDefenses(
	table fantasy.DefenseVsPositionTable,
	teams []string,
	positionFilter string,
) []matchupDifficultyDefense {
	defenses := make([]matchupDifficultyDefense, 0, len(teams))
	for _, team := range teams {
		defenses = append(defenses, matchupDifficultyDefensesForTeam(table, team, positionFilter)...)
	}
	if len(defenses) == 0 {
		return nil
	}
	return defenses
}

func matchupDifficultyDefensesForTeam(
	table fantasy.DefenseVsPositionTable,
	team, positionFilter string,
) []matchupDifficultyDefense {
	positions := matchupDifficultyPositions
	if positionFilter != "" {
		positions = []string{positionFilter}
	}
	defenses := make([]matchupDifficultyDefense, 0, len(positions))
	for _, position := range positions {
		defense := matchupDifficultyDefense{Team: team, Position: position}
		addMatchupDifficultyRating(
			&defense.PointsAllowedPerGame, &defense.LeagueAverage, &defense.Rank,
			table.Positions[position], team,
		)
		defenses = append(defenses, defense)
	}
	return defenses
}

func unknownMatchupDifficultyTeam(teams []string, table fantasy.DefenseVsPositionTable) string {
	for _, team := range teams {
		if !hasMatchupDifficultyTeam(table, team) {
			return team
		}
	}
	return ""
}

func hasMatchupDifficultyTeam(table fantasy.DefenseVsPositionTable, team string) bool {
	for _, position := range matchupDifficultyPositions {
		if _, found := table.Positions[position].ByTeam[team]; found {
			return true
		}
	}
	return false
}

func roundMatchupDifficultyPoints(points float64) float64 {
	return math.Round(points*10) / 10
}

// cachedMatchupRatings caches only complete provider results for six hours.
func (s *Service) cachedMatchupRatings(
	ctx context.Context,
	leagueSeasonID string,
	target leaguesync.Target,
) (fantasy.DefenseVsPositionTable, error) {
	s.matchupRatings.mu.Lock()
	defer s.matchupRatings.mu.Unlock()
	if s.matchupRatings.entries == nil {
		s.matchupRatings.entries = make(map[string]matchupRatingsCacheEntry)
	}
	if entry, found := s.matchupRatings.entries[leagueSeasonID]; found &&
		time.Now().Before(entry.expires) {
		return entry.table, nil
	}
	table, err := s.fetchMatchupRatings(ctx, target)
	if err != nil {
		return fantasy.DefenseVsPositionTable{}, errMatchupRatingsUnavailable
	}
	s.matchupRatings.entries[leagueSeasonID] = matchupRatingsCacheEntry{
		table: table, expires: time.Now().Add(matchupRatingsCacheTTL),
	}
	return table, nil
}

// fetchMatchupRatings resolves the provider league and its defense-vs-position data.
func (s *Service) fetchMatchupRatings(
	ctx context.Context,
	target leaguesync.Target,
) (fantasy.DefenseVsPositionTable, error) {
	source, err := s.ProviderSources.SourceFor(target)
	if err != nil {
		return fantasy.DefenseVsPositionTable{}, err
	}
	rater, ok := source.(fantasy.DefenseVsPositionRater)
	if !ok {
		return fantasy.DefenseVsPositionTable{}, fmt.Errorf("provider does not rate defense matchups")
	}
	league, err := source.DescribeLeague(ctx, target.Selection.League.ExternalID)
	if err != nil {
		return fantasy.DefenseVsPositionTable{}, err
	}
	return rater.DefenseVsPosition(ctx, league)
}
