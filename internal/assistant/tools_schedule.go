package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

const getScheduleSchema = `{
  "type": "object",
  "properties": {
    "league_season_id": {"type": "string", "format": "uuid", "description": "ID returned by list_leagues; adds the user's roster in that league"},
    "teams": {"type": "array", "minItems": 1, "maxItems": 8, "items": {"type": "string"}, "description": "NFL team abbreviations, e.g. KC"},
    "from_week": {"type": "integer", "minimum": 1, "maximum": 18, "description": "Defaults to the current NFL week"},
    "to_week": {"type": "integer", "minimum": 1, "maximum": 18, "description": "Defaults to from_week + 3; the range covers at most 6 weeks"}
  },
  "additionalProperties": false
}`

const (
	firstScheduleWeek   = 1
	lastScheduleWeek    = 18
	maxScheduleTeams    = 8
	maxScheduleSpan     = 6
	defaultScheduleSpan = 4
)

// scheduleTeamAliases maps common alternate abbreviations to the ones stored in nfl_games.
var scheduleTeamAliases = map[string]string{"WSH": "WAS", "JAC": "JAX"}

type scheduleToolInput struct {
	LeagueSeasonID string   `json:"league_season_id"`
	Teams          []string `json:"teams"`
	FromWeek       *int     `json:"from_week"`
	ToWeek         *int     `json:"to_week"`
}

// scheduleRequest is validated input; weeks stay optional until the current week is known.
type scheduleRequest struct {
	league   pgtype.UUID
	teams    []string
	fromWeek *int
	toWeek   *int
}

// scheduleScope is the season, current week, and (with a league) the owner's team to read.
type scheduleScope struct {
	season      int32
	currentWeek int32
	league      pgtype.UUID
	ownerTeam   pgtype.UUID
}

type scheduleToolResult struct {
	Season   int32                   `json:"season"`
	FromWeek int32                   `json:"from_week"`
	ToWeek   int32                   `json:"to_week"`
	Teams    []scheduleTeam          `json:"teams"`
	Roster   *[]scheduleRosterPlayer `json:"roster,omitempty"`
}

type scheduleTeam struct {
	Team    string         `json:"team"`
	ByeWeek *int32         `json:"bye_week,omitempty"`
	Games   []scheduleGame `json:"games"`
}

type scheduleGame struct {
	Week     int32     `json:"week"`
	Opponent string    `json:"opponent"`
	Kickoff  time.Time `json:"kickoff"`
}

type scheduleRosterPlayer struct {
	Name         string `json:"name"`
	Position     string `json:"position,omitempty"`
	NFLTeam      string `json:"nfl_team,omitempty"`
	ByeWeek      *int32 `json:"bye_week,omitempty"`
	OnByeInRange bool   `json:"on_bye_in_range"`
}

func scheduleTools(service *Service) []assistantTool {
	return []assistantTool{newAssistantTool(
		"get_schedule",
		"Upcoming NFL opponents, kickoff times and bye weeks. Pass league_season_id to see byes for the user's roster "+
			"in that league, or teams for specific NFL teams. Use it for bye-week planning and 'who plays when' questions.",
		getScheduleSchema,
		fixedToolLabel("Checking the NFL schedule"),
		func(ctx context.Context, raw json.RawMessage) (string, error) {
			input, err := decodeToolInput[scheduleToolInput](raw)
			if err != nil {
				return "", errInvalidToolInput
			}
			request, err := parseScheduleInput(input)
			if err != nil {
				return "", err
			}
			result, err := service.loadSchedule(ctx, request)
			if err != nil {
				return "", err
			}
			content, err := json.Marshal(result)
			return string(content), err
		},
	)}
}

// scheduleInputError is a validation message the model sees verbatim; toolErrorMessage
// passes tradeValidationError messages through.
func scheduleInputError(format string, args ...any) error {
	return tradeValidationError{message: fmt.Sprintf(format, args...)}
}

// parseScheduleInput checks everything that doesn't need the database: a league or
// teams, at most 8 distinct team abbreviations, and weeks within 1–18.
func parseScheduleInput(input scheduleToolInput) (scheduleRequest, error) {
	if input.LeagueSeasonID == "" && len(input.Teams) == 0 {
		return scheduleRequest{}, scheduleInputError("pass league_season_id or teams")
	}
	var request scheduleRequest
	if input.LeagueSeasonID != "" {
		league, err := parseTradeUUID(input.LeagueSeasonID)
		if err != nil {
			return scheduleRequest{}, errInvalidToolInput
		}
		request.league = league
	}
	teams, err := normalizeScheduleTeams(input.Teams)
	if err != nil {
		return scheduleRequest{}, err
	}
	request.teams = teams
	for _, week := range []*int{input.FromWeek, input.ToWeek} {
		if week != nil && (*week < firstScheduleWeek || *week > lastScheduleWeek) {
			return scheduleRequest{}, scheduleInputError("weeks must be between 1 and 18")
		}
	}
	request.fromWeek, request.toWeek = input.FromWeek, input.ToWeek
	return request, nil
}

// normalizeScheduleTeams uppercases, maps alternate abbreviations, and drops duplicates.
func normalizeScheduleTeams(values []string) ([]string, error) {
	if len(values) > maxScheduleTeams {
		return nil, scheduleInputError("pass at most %d teams", maxScheduleTeams)
	}
	teams := make([]string, 0, len(values))
	for _, value := range values {
		team := strings.ToUpper(strings.TrimSpace(value))
		if team == "" {
			return nil, errInvalidToolInput
		}
		if alias, ok := scheduleTeamAliases[team]; ok {
			team = alias
		}
		if !slices.Contains(teams, team) {
			teams = append(teams, team)
		}
	}
	return teams, nil
}

// resolveScheduleWeeks fills in defaults (the current week, then three more weeks,
// capped at week 18) and enforces from ≤ to and a span of at most six weeks.
func resolveScheduleWeeks(request scheduleRequest, currentWeek int32) (int32, int32, error) {
	var from int32
	switch {
	case request.fromWeek != nil:
		from = int32(*request.fromWeek)
	case currentWeek > 0:
		from = min(currentWeek, lastScheduleWeek)
	default:
		return 0, 0, scheduleInputError("pass from_week; no league has synced a current week yet")
	}
	to := min(from+defaultScheduleSpan-1, lastScheduleWeek)
	if request.toWeek != nil {
		to = int32(*request.toWeek)
	}
	if from > to {
		return 0, 0, scheduleInputError("from_week must be at or before to_week")
	}
	if to-from+1 > maxScheduleSpan {
		return 0, 0, scheduleInputError("pass at most %d weeks", maxScheduleSpan)
	}
	return from, to, nil
}

// loadSchedule reads the stored NFL schedule for the requested teams and, with a
// league, the owner's latest roster in that league. It never contacts a provider.
func (s *Service) loadSchedule(ctx context.Context, request scheduleRequest) (scheduleToolResult, error) {
	var result scheduleToolResult
	err := s.DB.InReadTx(ctx, func(q *store.Queries) error {
		owner, err := q.GetUserByEmail(ctx, s.OwnerEmail)
		if err != nil {
			return err
		}
		scope, err := loadScheduleScope(ctx, q, owner.ID, request.league)
		if err != nil {
			return err
		}
		result, err = readSchedule(ctx, q, request, scope)
		return err
	})
	if err != nil {
		return scheduleToolResult{}, scheduleLoadError(err)
	}
	return result, nil
}

// loadScheduleScope uses the league's season and the owner team's latest synced week
// when a league is given, otherwise the current season and the owner's newest synced week.
func loadScheduleScope(ctx context.Context, q *store.Queries, owner, league pgtype.UUID) (scheduleScope, error) {
	if !league.Valid {
		season := int32(fantasy.CurrentSeason(time.Now()))
		week, err := q.AssistantScheduleOwnerWeek(ctx, store.AssistantScheduleOwnerWeekParams{
			Season: season, UserID: owner,
		})
		return scheduleScope{season: season, currentWeek: week}, err
	}
	team, err := q.MatchupDetailOwnerTeam(ctx, store.MatchupDetailOwnerTeamParams{
		UserID: owner, LeagueSeasonID: league,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return scheduleScope{}, connect.NewError(connect.CodeNotFound, "league not found")
	}
	if err != nil {
		return scheduleScope{}, err
	}
	return scheduleScope{
		season: team.Season, currentWeek: team.ObservedPeriod, league: league, ownerTeam: team.TeamID,
	}, nil
}

// readSchedule reads one owner-scoped schedule: it refuses an unloaded season, resolves
// the week range, and returns the requested and roster teams' byes and games.
func readSchedule(
	ctx context.Context,
	q *store.Queries,
	request scheduleRequest,
	scope scheduleScope,
) (scheduleToolResult, error) {
	if err := requireScheduleLoaded(ctx, q, scope.season); err != nil {
		return scheduleToolResult{}, err
	}
	from, to, err := resolveScheduleWeeks(request, scope.currentWeek)
	if err != nil {
		return scheduleToolResult{}, err
	}
	roster, err := readScheduleRoster(ctx, q, scope)
	if err != nil {
		return scheduleToolResult{}, err
	}
	byes, games, err := readScheduleTeams(ctx, q, scope.season, request.teams, roster, from, to)
	if err != nil {
		return scheduleToolResult{}, err
	}
	return buildScheduleResult(scope.season, from, to, byes, games, roster), nil
}

// requireScheduleLoaded rejects a season with no nfl_games rows, which would otherwise
// make every team look like it is on bye.
func requireScheduleLoaded(ctx context.Context, q *store.Queries, season int32) error {
	loaded, err := q.AssistantScheduleLoaded(ctx, season)
	if err != nil {
		return err
	}
	if !loaded {
		return scheduleInputError("the NFL schedule hasn't loaded yet; refresh leagues")
	}
	return nil
}

// readScheduleTeams reads byes and in-range games for the requested teams plus the roster's
// NFL teams. A requested team with no game in the season is a validation error.
func readScheduleTeams(
	ctx context.Context,
	q *store.Queries,
	season int32,
	requested []string,
	roster []store.AssistantScheduleOwnerRosterRow,
	from, to int32,
) ([]store.AssistantScheduleTeamByesRow, []store.AssistantScheduleGamesRow, error) {
	byes, err := q.AssistantScheduleTeamByes(ctx, store.AssistantScheduleTeamByesParams{
		Season: season, Teams: scheduleTeamsWithRoster(requested, roster),
	})
	if err != nil {
		return nil, nil, err
	}
	if unknown := unknownRequestedTeam(requested, byes); unknown != "" {
		return nil, nil, scheduleInputError("unknown NFL team %s", unknown)
	}
	games, err := q.AssistantScheduleGames(ctx, store.AssistantScheduleGamesParams{
		Season: season, Teams: knownScheduleTeams(byes), FromWeek: from, ToWeek: to,
	})
	return byes, games, err
}

// readScheduleRoster returns nil without a league, and the owner's roster (possibly empty) with one.
func readScheduleRoster(ctx context.Context, q *store.Queries, scope scheduleScope) ([]store.AssistantScheduleOwnerRosterRow, error) {
	if !scope.league.Valid {
		return nil, nil
	}
	return q.AssistantScheduleOwnerRoster(ctx, store.AssistantScheduleOwnerRosterParams{
		LeagueSeasonID: scope.league, TeamID: scope.ownerTeam,
	})
}

func scheduleTeamsWithRoster(requested []string, roster []store.AssistantScheduleOwnerRosterRow) []string {
	teams := slices.Clone(requested)
	for _, player := range roster {
		if player.ProTeam != "" && !slices.Contains(teams, player.ProTeam) {
			teams = append(teams, player.ProTeam)
		}
	}
	return teams
}

func unknownRequestedTeam(requested []string, byes []store.AssistantScheduleTeamByesRow) string {
	for _, row := range byes {
		if !row.Known && slices.Contains(requested, row.Team) {
			return row.Team
		}
	}
	return ""
}

func knownScheduleTeams(byes []store.AssistantScheduleTeamByesRow) []string {
	teams := make([]string, 0, len(byes))
	for _, row := range byes {
		if row.Known {
			teams = append(teams, row.Team)
		}
	}
	return teams
}

// buildScheduleResult groups games by team and marks roster players whose team's bye
// falls inside the range. Teams without a game in the season (roster players with an
// unrecognized NFL team) are left out, and those players get no bye week.
func buildScheduleResult(
	season, from, to int32,
	byes []store.AssistantScheduleTeamByesRow,
	games []store.AssistantScheduleGamesRow,
	roster []store.AssistantScheduleOwnerRosterRow,
) scheduleToolResult {
	result := scheduleToolResult{Season: season, FromWeek: from, ToWeek: to, Teams: []scheduleTeam{}}
	byeWeeks := make(map[string]*int32, len(byes))
	for _, row := range byes {
		if !row.Known {
			continue
		}
		team := scheduleTeam{Team: row.Team, ByeWeek: positiveWeek(row.ByeWeek), Games: scheduleTeamGames(row.Team, games)}
		byeWeeks[row.Team] = team.ByeWeek
		result.Teams = append(result.Teams, team)
	}
	if roster != nil {
		players := scheduleRosterPlayers(roster, byeWeeks, from, to)
		result.Roster = &players
	}
	return result
}

func scheduleTeamGames(team string, games []store.AssistantScheduleGamesRow) []scheduleGame {
	teamGames := []scheduleGame{}
	for _, game := range games {
		if game.Team == team {
			teamGames = append(teamGames, scheduleGame{
				Week: game.Week, Opponent: game.Opponent, Kickoff: game.KickoffAt.Time.UTC(),
			})
		}
	}
	return teamGames
}

func scheduleRosterPlayers(
	roster []store.AssistantScheduleOwnerRosterRow,
	byeWeeks map[string]*int32,
	from, to int32,
) []scheduleRosterPlayer {
	players := make([]scheduleRosterPlayer, 0, len(roster))
	for _, row := range roster {
		bye := byeWeeks[row.ProTeam]
		players = append(players, scheduleRosterPlayer{
			Name: row.PlayerName, Position: row.Position, NFLTeam: row.ProTeam, ByeWeek: bye,
			OnByeInRange: bye != nil && *bye >= from && *bye <= to,
		})
	}
	return players
}

func positiveWeek(week int32) *int32 {
	if week <= 0 {
		return nil
	}
	return &week
}

func scheduleLoadError(err error) error {
	if asTradeValidationError(err) || connect.CodeOf(err) == connect.CodeNotFound {
		return err
	}
	return internalError("schedule data could not be loaded")
}
