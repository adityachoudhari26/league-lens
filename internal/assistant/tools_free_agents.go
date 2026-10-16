package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/leaguesync"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

var (
	errFreeAgentsESPN             = errors.New("ESPN free-agent lookup failed")
	errFreeAgentsSleeper          = errors.New("sleeper free-agent lookup failed")
	errFreeAgentTargetUnavailable = errors.New("free-agent target unavailable")
)

const getFreeAgentsSchema = `{
  "type": "object",
  "properties": {
    "league_season_id": {"type": "string"},
    "position": {"type": "string", "enum": ["QB", "RB", "WR", "TE", "K", "DST"]},
    "limit": {"type": "integer", "minimum": 1, "maximum": 25, "default": 15}
  },
  "required": ["league_season_id"],
  "additionalProperties": false
}`

type freeAgentToolInput struct {
	LeagueSeasonID string `json:"league_season_id"`
	Position       string `json:"position"`
	Limit          *int   `json:"limit"`
}

type freeAgentToolResult struct {
	Source     string              `json:"source"`
	Week       int                 `json:"week"`
	FreeAgents []fantasy.FreeAgent `json:"free_agents"`
	provider   fantasy.ProviderID
}

func freeAgentTools(reader assistantDashboard, service *Service) []assistantTool {
	return []assistantTool{newAssistantTool(
		"get_free_agents",
		"Returns currently available players for one league, ranked by the current week's league-scored projections. "+
			"Each player may include rest_of_season_points, league-scored projected points "+
			"from the next unplayed week through week 17, and rest_of_season_weeks, the number of projected games it covers.",
		getFreeAgentsSchema,
		fixedToolLabel("Checking free agents"),
		func(ctx context.Context, raw json.RawMessage) (string, error) {
			input, err := decodeToolInput[freeAgentToolInput](raw)
			if err != nil {
				return "", errInvalidToolInput
			}
			input, limit, err := validateFreeAgentInput(input)
			if err != nil {
				return "", err
			}
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			result, err := service.listFreeAgents(ctx, reader, input.LeagueSeasonID, input.Position, limit)
			if err != nil {
				return "", err
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				return "", errFreeAgentError(result.provider)
			}
			return string(encoded), nil
		},
	)}
}

func validateFreeAgentInput(input freeAgentToolInput) (freeAgentToolInput, int, error) {
	if input.LeagueSeasonID == "" {
		return input, 0, errInvalidToolInput
	}
	limit := 15
	if input.Limit != nil {
		limit = *input.Limit
	}
	if limit < 1 || limit > 25 {
		return input, 0, errInvalidToolInput
	}
	switch input.Position {
	case "", "QB", "RB", "WR", "TE", "K", "DST":
	default:
		return input, 0, errInvalidToolInput
	}
	return input, limit, nil
}

func (s *Service) listFreeAgents(
	ctx context.Context,
	reader assistantDashboard,
	leagueSeasonID, position string,
	limit int,
) (freeAgentToolResult, error) {
	target, err := s.freeAgentTarget(ctx, leagueSeasonID)
	if err != nil {
		return freeAgentToolResult{}, err
	}
	providerID := target.Selection.League.Provider
	leaguePage, err := reader.GetLeague(ctx, &v1.GetLeagueRequest{LeagueSeasonId: leagueSeasonID})
	if err != nil {
		return freeAgentToolResult{}, errFreeAgentError(providerID)
	}
	week, err := latestSelectedScoringWeek(ctx, reader, leaguePage)
	if err != nil {
		return freeAgentToolResult{}, errFreeAgentError(providerID)
	}
	lister, league, err := s.freeAgentSource(ctx, target)
	if err != nil {
		return freeAgentToolResult{}, errFreeAgentError(providerID)
	}
	agents, err := lister.ListFreeAgents(ctx, league, fantasy.FreeAgentQuery{
		Week: week, Position: position, Limit: limit,
	})
	if err != nil {
		return freeAgentToolResult{}, errFreeAgentError(providerID)
	}
	return freeAgentToolResult{
		Source: freeAgentSourceName(providerID), Week: week, FreeAgents: agents, provider: providerID,
	}, nil
}

func (s *Service) freeAgentSource(
	ctx context.Context,
	target leaguesync.Target,
) (fantasy.FreeAgentLister, fantasy.League, error) {
	source, err := s.ProviderSources.SourceFor(target)
	if err != nil {
		return nil, fantasy.League{}, err
	}
	lister, ok := source.(fantasy.FreeAgentLister)
	if !ok {
		return nil, fantasy.League{}, errors.New("provider does not list free agents")
	}
	league, err := source.DescribeLeague(ctx, target.Selection.League.ExternalID)
	if err != nil {
		return nil, fantasy.League{}, err
	}
	return lister, league, nil
}

func freeAgentSourceName(provider fantasy.ProviderID) string {
	if provider == fantasy.ProviderSleeper {
		return "Sleeper projections minus rostered players"
	}
	return "ESPN player pool"
}

func (s *Service) freeAgentTarget(ctx context.Context, leagueSeasonID string) (leaguesync.Target, error) {
	var id pgtype.UUID
	if err := id.Scan(leagueSeasonID); err != nil {
		return leaguesync.Target{}, connect.NewError(connect.CodeInvalidArgument, errInvalidToolInput.Error())
	}
	var target leaguesync.Target
	err := s.DB.InReadTx(ctx, func(q *store.Queries) error {
		owner, err := q.GetUserByEmail(ctx, s.OwnerEmail)
		if err != nil {
			return err
		}
		rows, err := q.ListSyncTargets(ctx, owner.ID)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.LeagueSeasonID == id {
				target = leaguesync.TargetFromRow(row)
				return nil
			}
		}
		return pgx.ErrNoRows
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return leaguesync.Target{}, connect.NewError(connect.CodeNotFound, "league not found")
	}
	if err != nil {
		return leaguesync.Target{}, connect.NewError(
			connect.CodeInternal,
			"free-agent league could not be loaded",
		).WithCause(errFreeAgentTargetUnavailable)
	}
	return target, nil
}

func latestSelectedScoringWeek(ctx context.Context, reader assistantDashboard, league *v1.GetLeagueResponse) (int, error) {
	if league == nil {
		return 0, fmt.Errorf("league was not returned")
	}
	for _, team := range league.GetTeams() {
		if !team.GetSelected() {
			continue
		}
		roster, err := reader.GetTeamRoster(ctx, &v1.GetTeamRosterRequest{TeamId: team.GetId()})
		if err != nil {
			return 0, err
		}
		if week := int(roster.GetSnapshot().GetScoringPeriod()); week > 0 {
			return week, nil
		}
	}
	return 0, fmt.Errorf("selected team has no observed scoring week")
}

func errFreeAgentError(provider fantasy.ProviderID) error {
	if provider == fantasy.ProviderESPN {
		return errFreeAgentsESPN
	}
	return errFreeAgentsSleeper
}
