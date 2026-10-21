package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/sync/errgroup"

	"github.com/adityachoudhari26/league-lens/internal/playerids"
	"github.com/adityachoudhari26/league-lens/internal/sleeper"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

const getPlayerUsageDescription = "Recent snap share, targets, target share, carries, pass attempts and red-zone opportunities for NFL " +
	"players over the last completed NFL weeks. Each game also lists the same-team teammates at the player's position who played that " +
	"week (position_room), so you can see a backfield or receiver split and teammates returning from or missing games. Pass " +
	"league_season_id for the user's roster in that league, or player_ids for specific players. Use it for start/sit, waiver and trade " +
	"questions where a player's role and opportunity matter."

const getPlayerUsageSchema = `{
  "type": "object",
  "properties": {
    "league_season_id": {
      "type": "string", "format": "uuid",
      "description": "ID returned by list_leagues; reports usage for the user's roster in that league"
    },
    "player_ids": {
      "type": "array", "minItems": 1, "maxItems": 10, "items": {"type": "string", "format": "uuid"},
      "description": "League Lens player IDs returned by roster or player tools"
    },
    "weeks": {
      "type": "integer", "minimum": 1, "maximum": 6, "default": 3,
      "description": "Number of most recent completed NFL weeks"
    }
  },
  "additionalProperties": false
}`

const (
	defaultUsageWeeks        = 3
	maxUsageWeeks            = 6
	usageToolTimeout         = 15 * time.Second
	usageWeekCacheTTL        = 6 * time.Hour
	maxUsageFetchesInFlight  = 3
	maxPositionRoomTeammates = 3
)

// UsageSource reads all players' public stats for one Sleeper NFL week.
type UsageSource interface {
	WeeklyStats(ctx context.Context, season, week int) ([]sleeper.WeeklyStat, error)
}

type usageCacheEntry struct {
	data    usageWeek
	expires time.Time
}

type usageCacheKey struct {
	season int32
	week   int32
}

type usageCache struct {
	mu      sync.Mutex
	entries map[usageCacheKey]usageCacheEntry
	now     func() time.Time
}

type usageToolInput struct {
	LeagueSeasonID string   `json:"league_season_id"`
	PlayerIDs      []string `json:"player_ids"`
	Weeks          *int     `json:"weeks"`
}

type usageRequest struct {
	league    pgtype.UUID
	playerIDs []pgtype.UUID
	weeks     int
}

type usagePlayer struct {
	name      string
	espnID    string
	sleeperID string
	statsID   string
}

type usageScope struct {
	season  int32
	weeks   []int32
	players []usagePlayer
}

type usageWeek struct {
	players     map[string]sleeper.WeeklyStat
	rooms       map[usageRoomKey][]sleeper.WeeklyStat
	teamTargets map[string]float64
}

type usageRoomKey struct {
	team     string
	position string
}

type usageToolResult struct {
	Season  int32               `json:"season"`
	Weeks   []int32             `json:"weeks"`
	Players []usagePlayerResult `json:"players"`
}

type usagePlayerResult struct {
	Name     string       `json:"name"`
	Position string       `json:"position,omitempty"`
	NFLTeam  string       `json:"nfl_team,omitempty"`
	Games    []usageGame  `json:"games"`
	Average  usageAverage `json:"average"`
}

type usageGame struct {
	Week                int32           `json:"week"`
	Snaps               int             `json:"snaps"`
	TeamSnaps           int             `json:"team_snaps"`
	SnapPct             float64         `json:"snap_pct"`
	Targets             int             `json:"targets,omitempty"`
	TargetShare         float64         `json:"target_share,omitempty"`
	RedZoneTargets      int             `json:"red_zone_targets,omitempty"`
	Carries             int             `json:"carries,omitempty"`
	RedZoneCarries      int             `json:"red_zone_carries,omitempty"`
	PassAttempts        int             `json:"pass_attempts,omitempty"`
	RedZonePassAttempts int             `json:"red_zone_pass_attempts,omitempty"`
	AirYards            int             `json:"air_yards,omitempty"`
	PositionRoom        []usageTeammate `json:"position_room,omitempty"`
}

type usageTeammate struct {
	Name    string  `json:"name"`
	Snaps   int     `json:"snaps"`
	SnapPct float64 `json:"snap_pct"`
	Carries int     `json:"carries,omitempty"`
	Targets int     `json:"targets,omitempty"`
}

type usageAverage struct {
	SnapPct             float64 `json:"snap_pct"`
	Targets             float64 `json:"targets,omitempty"`
	TargetShare         float64 `json:"target_share,omitempty"`
	RedZoneTargets      float64 `json:"red_zone_targets,omitempty"`
	Carries             float64 `json:"carries,omitempty"`
	RedZoneCarries      float64 `json:"red_zone_carries,omitempty"`
	PassAttempts        float64 `json:"pass_attempts,omitempty"`
	RedZonePassAttempts float64 `json:"red_zone_pass_attempts,omitempty"`
}

type usageGameRow struct {
	game        usageGame
	snapPct     float64
	targetShare float64
	position    string
	team        string
}

// usageTools registers the public Sleeper weekly stats lookup.
func usageTools(service *Service) []assistantTool {
	return []assistantTool{newAssistantTool(
		"get_player_usage",
		getPlayerUsageDescription,
		getPlayerUsageSchema,
		fixedToolLabel("Checking player usage"),
		func(ctx context.Context, raw json.RawMessage) (string, error) {
			input, err := decodeToolInput[usageToolInput](raw)
			if err != nil {
				return "", errInvalidToolInput
			}
			request, err := parseUsageInput(input)
			if err != nil {
				return "", err
			}
			ctx, cancel := context.WithTimeout(ctx, usageToolTimeout)
			defer cancel()
			result, err := service.playerUsage(ctx, request)
			if err != nil {
				return "", err
			}
			content, err := json.Marshal(result)
			return string(content), err
		},
	)}
}

// parseUsageInput validates the lookup scope and applies the completed-week default.
func parseUsageInput(input usageToolInput) (usageRequest, error) {
	if input.LeagueSeasonID == "" && len(input.PlayerIDs) == 0 {
		return usageRequest{}, injuryInputError("pass league_season_id or player_ids")
	}
	league, err := parseInjuryLeague(input.LeagueSeasonID)
	if err != nil {
		return usageRequest{}, err
	}
	playerIDs, err := parseInjuryPlayerIDs(input.PlayerIDs)
	if err != nil {
		return usageRequest{}, err
	}
	weeks, err := parseUsageWeeks(input.Weeks)
	if err != nil {
		return usageRequest{}, err
	}
	return usageRequest{league: league, playerIDs: playerIDs, weeks: weeks}, nil
}

// parseUsageWeeks selects the default window and rejects requests outside 1–6 weeks.
func parseUsageWeeks(value *int) (int, error) {
	if value == nil {
		return defaultUsageWeeks, nil
	}
	if *value < 1 || *value > maxUsageWeeks {
		return 0, injuryInputError("weeks must be between 1 and 6")
	}
	return *value, nil
}

// playerUsage loads the owner-scoped player and week scope before fetching cached weekly stats.
func (s *Service) playerUsage(ctx context.Context, request usageRequest) (usageToolResult, error) {
	scope, err := s.loadUsageScope(ctx, request)
	if err != nil {
		return usageToolResult{}, err
	}
	players := s.mapUsageSleeperIDs(ctx, scope.players)
	if len(players) == 0 || len(scope.weeks) == 0 {
		return usageToolResult{
			Season: scope.season, Weeks: append([]int32{}, scope.weeks...), Players: []usagePlayerResult{},
		}, nil
	}
	weekData, err := s.loadUsageWeeks(ctx, scope.season, scope.weeks)
	if err != nil {
		return usageToolResult{}, internalError("player usage could not be loaded")
	}
	return buildUsageResult(scope.season, scope.weeks, players, weekData), nil
}

// loadUsageScope reads the owner roster, requested player records and completed weeks in one snapshot.
func (s *Service) loadUsageScope(ctx context.Context, request usageRequest) (usageScope, error) {
	var scope usageScope
	err := s.DB.InReadTx(ctx, func(q *store.Queries) error {
		owner, err := q.GetUserByEmail(ctx, s.OwnerEmail)
		if err != nil {
			return err
		}
		schedule, err := loadScheduleScope(ctx, q, owner.ID, request.league)
		if err != nil {
			return err
		}
		rosterIDs, err := readUsageOwnerRoster(ctx, q, request.league, schedule.ownerTeam)
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
		weeks, err := readUsageCompletedWeeks(ctx, q, schedule, request.weeks)
		if err != nil {
			return err
		}
		scope = usageScope{
			season: schedule.season, weeks: weeks,
			players: usagePlayersFromRows(rows),
		}
		return nil
	})
	if err != nil {
		return usageScope{}, usageLoadError(err)
	}
	return scope, nil
}

// readUsageOwnerRoster returns the selected owner's roster IDs when a league is supplied.
func readUsageOwnerRoster(
	ctx context.Context,
	q *store.Queries,
	league, ownerTeam pgtype.UUID,
) ([]pgtype.UUID, error) {
	if !league.Valid {
		return nil, nil
	}
	return q.AssistantInjuryOwnerRosterPlayerIDs(ctx, store.AssistantInjuryOwnerRosterPlayerIDsParams{
		LeagueSeasonID: league, TeamID: ownerTeam,
	})
}

// readUsageCompletedWeeks returns the latest completed, non-current weeks in ascending order.
func readUsageCompletedWeeks(
	ctx context.Context,
	q *store.Queries,
	schedule scheduleScope,
	limit int,
) ([]int32, error) {
	weeks, err := q.AssistantUsageCompletedWeeks(ctx, store.AssistantUsageCompletedWeeksParams{
		Season: schedule.season, CurrentWeek: schedule.currentWeek,
		AsOf: pgtype.Timestamptz{Time: time.Now(), Valid: true}, WeekLimit: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	slices.Reverse(weeks)
	return weeks, nil
}

func usagePlayersFromRows(rows []store.AssistantInjuryPlayersRow) []usagePlayer {
	players := make([]usagePlayer, 0, len(rows))
	for _, row := range rows {
		players = append(players, usagePlayer{
			name: row.DisplayName, espnID: row.EspnID, sleeperID: row.SleeperID,
		})
	}
	return players
}

// mapUsageSleeperIDs resolves ESPN-only stored identities through the shared catalog and drops unresolved IDs.
func (s *Service) mapUsageSleeperIDs(ctx context.Context, players []usagePlayer) []usagePlayer {
	var catalog playerids.Catalog
	if usagePlayersNeedCatalog(players) {
		catalog, _ = s.trendingCatalog(ctx)
	}
	mapped := make([]usagePlayer, 0, len(players))
	for _, player := range players {
		player.statsID = usageStatsID(player, catalog)
		if player.statsID != "" {
			mapped = append(mapped, player)
		}
	}
	return mapped
}

// usageStatsID prefers a stored numeric Sleeper ID, then an ESPN-to-Sleeper catalog mapping; it never matches names.
func usageStatsID(player usagePlayer, catalog playerids.Catalog) string {
	if isNumericSleeperID(player.sleeperID) {
		return player.sleeperID
	}
	if player.espnID == "" {
		return ""
	}
	entry, found := catalog.Lookup("espn", player.espnID)
	if !found || !isNumericSleeperID(entry.IDs["sleeper"]) {
		return ""
	}
	return entry.IDs["sleeper"]
}

func usagePlayersNeedCatalog(players []usagePlayer) bool {
	for _, player := range players {
		if !isNumericSleeperID(player.sleeperID) && player.espnID != "" {
			return true
		}
	}
	return false
}

func isNumericSleeperID(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

// loadUsageWeeks returns cached and fetched indexed stats, failing the whole request after a fetch error.
func (s *Service) loadUsageWeeks(ctx context.Context, season int32, weeks []int32) (map[int32]usageWeek, error) {
	cached, missing := s.usage.cachedWeeks(season, weeks)
	if len(missing) == 0 {
		return cached, nil
	}
	fetched, err := fetchUsageWeeks(ctx, s.Usage, season, missing)
	if err != nil {
		return nil, err
	}
	s.usage.cacheWeeks(season, fetched)
	for week, data := range fetched {
		cached[week] = data
	}
	return cached, nil
}

func (cache *usageCache) cachedWeeks(season int32, weeks []int32) (map[int32]usageWeek, []int32) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.entries == nil {
		cache.entries = make(map[usageCacheKey]usageCacheEntry)
	}
	now := cache.currentTime()
	hits := make(map[int32]usageWeek, len(weeks))
	missing := make([]int32, 0, len(weeks))
	for _, week := range weeks {
		key := usageCacheKey{season: season, week: week}
		entry, found := cache.entries[key]
		if found && now.Before(entry.expires) {
			hits[week] = entry.data
			continue
		}
		delete(cache.entries, key)
		missing = append(missing, week)
	}
	return hits, missing
}

func (cache *usageCache) cacheWeeks(season int32, weeks map[int32]usageWeek) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.entries == nil {
		cache.entries = make(map[usageCacheKey]usageCacheEntry)
	}
	expires := cache.currentTime().Add(usageWeekCacheTTL)
	for week, data := range weeks {
		cache.entries[usageCacheKey{season: season, week: week}] = usageCacheEntry{
			data: data, expires: expires,
		}
	}
}

func (cache *usageCache) currentTime() time.Time {
	if cache.now == nil {
		return time.Now()
	}
	return cache.now()
}

// fetchUsageWeeks limits concurrent requests to three and fails the whole call if any week fails.
func fetchUsageWeeks(ctx context.Context, source UsageSource, season int32, weeks []int32) (map[int32]usageWeek, error) {
	var resultMu sync.Mutex
	result := make(map[int32]usageWeek, len(weeks))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(maxUsageFetchesInFlight)
	for _, week := range weeks {
		group.Go(func() error {
			rows, err := fetchUsageWeekWithRetry(groupCtx, source, int(season), int(week))
			if err != nil {
				return err
			}
			resultMu.Lock()
			result[week] = indexUsageWeek(rows)
			resultMu.Unlock()
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return result, nil
}

// fetchUsageWeekWithRetry retries one public stats read once unless its context has ended.
func fetchUsageWeekWithRetry(ctx context.Context, source UsageSource, season, week int) ([]sleeper.WeeklyStat, error) {
	rows, err := source.WeeklyStats(ctx, season, week)
	if !shouldRetryUsageRead(ctx, err) {
		return rows, err
	}
	return source.WeeklyStats(ctx, season, week)
}

// shouldRetryUsageRead skips retries for cancellation and deadline errors.
func shouldRetryUsageRead(ctx context.Context, err error) bool {
	return err != nil && ctx.Err() == nil &&
		!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// indexUsageWeek separates real numeric player rows from team totals and defenses, then indexes position rooms.
func indexUsageWeek(rows []sleeper.WeeklyStat) usageWeek {
	week := usageWeek{
		players:     make(map[string]sleeper.WeeklyStat, len(rows)),
		rooms:       make(map[usageRoomKey][]sleeper.WeeklyStat),
		teamTargets: make(map[string]float64),
	}
	for _, row := range rows {
		if !isNumericSleeperID(row.PlayerID) || row.Position() == "DEF" || row.Position() == "TEAM" {
			continue
		}
		week.players[row.PlayerID] = row
		roomKey := usageRoomKey{team: row.Team, position: row.Position()}
		week.rooms[roomKey] = append(week.rooms[roomKey], row)
		week.teamTargets[row.Team] += row.Stats["rec_tgt"]
	}
	return week
}

// buildUsageResult computes game-level opportunity and per-game averages for mapped players.
func buildUsageResult(
	season int32,
	weeks []int32,
	players []usagePlayer,
	weekData map[int32]usageWeek,
) usageToolResult {
	result := usageToolResult{
		Season: season, Weeks: append([]int32{}, weeks...), Players: []usagePlayerResult{},
	}
	for _, player := range players {
		usage, found := buildUsagePlayer(player, weeks, weekData)
		if found {
			result.Players = append(result.Players, usage)
		}
	}
	return result
}

// buildUsagePlayer excludes kickers, defenses and players without a game in the selected weeks.
func buildUsagePlayer(
	player usagePlayer,
	weeks []int32,
	weekData map[int32]usageWeek,
) (usagePlayerResult, bool) {
	games, skip := collectUsageGames(player, weeks, weekData)
	if skip || len(games) == 0 {
		return usagePlayerResult{}, false
	}
	result := usagePlayerResult{
		Name: player.name, Games: make([]usageGame, 0, len(games)),
		Average: averageUsageGames(games),
	}
	for _, row := range games {
		result.Games = append(result.Games, row.game)
	}
	newestGame := games[len(games)-1]
	result.Position, result.NFLTeam = newestGame.position, newestGame.team
	return result, true
}

// collectUsageGames builds each requested player's games using that week's team and position context.
func collectUsageGames(
	player usagePlayer,
	weeks []int32,
	weekData map[int32]usageWeek,
) ([]usageGameRow, bool) {
	games := make([]usageGameRow, 0, len(weeks))
	for _, week := range weeks {
		data, found := weekData[week]
		if !found {
			continue
		}
		row, found := data.players[player.statsID]
		if !found {
			continue
		}
		if row.Position() == "K" || row.Position() == "DEF" {
			return nil, true
		}
		if row.Stats["gp"] <= 0 {
			continue
		}
		game, snapPct, targetShare := buildUsageGame(week, row, data.teamTargets[row.Team])
		game.PositionRoom = buildUsagePositionRoom(
			row.PlayerID, data.rooms[usageRoomKey{team: row.Team, position: row.Position()}],
		)
		games = append(games, usageGameRow{
			game: game, snapPct: snapPct, targetShare: targetShare,
			position: row.Position(), team: row.Team,
		})
	}
	return games, false
}

// buildUsagePositionRoom reports the leading same-team players at a position who took offensive snaps.
func buildUsagePositionRoom(playerID string, rows []sleeper.WeeklyStat) []usageTeammate {
	teammates := make([]usageTeammate, 0, len(rows))
	for _, row := range rows {
		if row.PlayerID == playerID || row.Stats["off_snp"] <= 0 {
			continue
		}
		teammates = append(teammates, buildUsageTeammate(row))
	}
	slices.SortFunc(teammates, compareUsageTeammates)
	if len(teammates) > maxPositionRoomTeammates {
		teammates = teammates[:maxPositionRoomTeammates]
	}
	return teammates
}

// buildUsageTeammate converts one weekly row into the usage fields shown for a position-room teammate.
func buildUsageTeammate(row sleeper.WeeklyStat) usageTeammate {
	snaps := int(row.Stats["off_snp"])
	teamSnaps := int(row.Stats["tm_off_snp"])
	snapPct := 0.0
	if teamSnaps > 0 {
		snapPct = float64(snaps) * 100 / float64(teamSnaps)
	}
	return usageTeammate{
		Name: row.Name(), Snaps: snaps, SnapPct: roundUsage(snapPct),
		Carries: int(row.Stats["rush_att"]), Targets: int(row.Stats["rec_tgt"]),
	}
}

func compareUsageTeammates(left, right usageTeammate) int {
	if left.Snaps > right.Snaps {
		return -1
	}
	if left.Snaps < right.Snaps {
		return 1
	}
	return strings.Compare(left.Name, right.Name)
}

func averageUsageGames(games []usageGameRow) usageAverage {
	var totalSnapPct, totalTargets, totalTargetShare float64
	var totalRedZoneTargets, totalCarries, totalRedZoneCarries float64
	var totalPassAttempts, totalRedZonePassAttempts float64
	for _, row := range games {
		totalSnapPct += row.snapPct
		totalTargets += float64(row.game.Targets)
		totalTargetShare += row.targetShare
		totalRedZoneTargets += float64(row.game.RedZoneTargets)
		totalCarries += float64(row.game.Carries)
		totalRedZoneCarries += float64(row.game.RedZoneCarries)
		totalPassAttempts += float64(row.game.PassAttempts)
		totalRedZonePassAttempts += float64(row.game.RedZonePassAttempts)
	}
	count := float64(len(games))
	return usageAverage{
		SnapPct: roundUsage(totalSnapPct / count), Targets: roundUsage(totalTargets / count),
		TargetShare: roundUsage(totalTargetShare / count), RedZoneTargets: roundUsage(totalRedZoneTargets / count),
		Carries: roundUsage(totalCarries / count), RedZoneCarries: roundUsage(totalRedZoneCarries / count),
		PassAttempts:        roundUsage(totalPassAttempts / count),
		RedZonePassAttempts: roundUsage(totalRedZonePassAttempts / count),
	}
}

// buildUsageGame derives that week's percentages from the player's own team and game row.
func buildUsageGame(week int32, row sleeper.WeeklyStat, teamTargets float64) (usageGame, float64, float64) {
	snaps := int(row.Stats["off_snp"])
	teamSnaps := int(row.Stats["tm_off_snp"])
	snapPct := 0.0
	if teamSnaps > 0 {
		snapPct = float64(snaps) * 100 / float64(teamSnaps)
	}
	targets := int(row.Stats["rec_tgt"])
	targetShare := 0.0
	if teamTargets > 0 {
		targetShare = float64(targets) * 100 / teamTargets
	}
	return usageGame{
		Week: week, Snaps: snaps, TeamSnaps: teamSnaps, SnapPct: roundUsage(snapPct),
		Targets: targets, TargetShare: roundUsage(targetShare),
		RedZoneTargets: int(row.Stats["rec_rz_tgt"]), Carries: int(row.Stats["rush_att"]),
		RedZoneCarries: int(row.Stats["rush_rz_att"]), PassAttempts: int(row.Stats["pass_att"]),
		RedZonePassAttempts: int(row.Stats["pass_rz_att"]), AirYards: int(row.Stats["rec_air_yd"]),
	}, snapPct, targetShare
}

// roundUsage rounds a percentage or per-game average to one decimal place.
func roundUsage(value float64) float64 {
	return math.Round(value*10) / 10
}

// usageLoadError preserves user-visible scope errors and hides unexpected database failures.
func usageLoadError(err error) error {
	if asToolValidationError(err) || connect.CodeOf(err) == connect.CodeNotFound {
		return err
	}
	return internalError("player usage players could not be loaded")
}
