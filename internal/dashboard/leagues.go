package dashboard

import (
	"context"

	"connectrpc.com/connect/v2"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// ListLeagues pages through leagues accessible to the configured local owner.
// Before the first import, a missing owner produces an empty list.
func (s *Service) ListLeagues(ctx context.Context, request *v1.ListLeaguesRequest) (*v1.ListLeaguesResponse, error) {
	size := request.PageSize
	if size < 0 || size > 100 {
		return nil, connect.NewError(connect.CodeInvalidArgument, "page size must be between 1 and 100")
	}
	if size == 0 {
		size = 25
	}
	var after pgtype.UUID
	if request.PageToken != "" {
		id, err := parseID(request.PageToken)
		if err != nil {
			return nil, err
		}
		after = id
	}
	response := &v1.ListLeaguesResponse{}
	err := s.read(ctx, func(ctx context.Context, q *store.Queries, owner pgtype.UUID) error {
		rows, err := q.DashboardLeagues(ctx, store.DashboardLeaguesParams{UserID: owner, ID: after, Limit: size + 1})
		if err != nil {
			return err
		}
		if len(rows) > int(size) {
			rows = rows[:size]
			response.NextPageToken = rows[len(rows)-1].ID.String()
		}
		for _, row := range rows {
			response.Leagues = append(response.Leagues, leagueMessage(store.DashboardLeagueRow(row)))
		}
		return nil
	})
	// Before the first import the configured local owner may not exist yet.
	if connect.CodeOf(err) == connect.CodeNotFound {
		return response, nil
	}
	return response, err
}

// GetLeague returns an accessible league, its teams, and imported matchup rounds.
// Team selection is specific to the configured owner.
func (s *Service) GetLeague(ctx context.Context, request *v1.GetLeagueRequest) (*v1.GetLeagueResponse, error) {
	id, err := parseID(request.LeagueSeasonId)
	if err != nil {
		return nil, err
	}
	response := &v1.GetLeagueResponse{}
	err = s.read(ctx, func(ctx context.Context, q *store.Queries, owner pgtype.UUID) error {
		league, err := q.DashboardLeague(ctx, store.DashboardLeagueParams{UserID: owner, ID: id})
		if err != nil {
			return err
		}
		response.League = leagueMessage(league)
		teams, err := q.ListTeamsForUser(ctx, store.ListTeamsForUserParams{UserID: owner, LeagueSeasonID: id})
		if err != nil {
			return err
		}
		for _, team := range teams {
			item, err := teamMessage(team, response.League.SelectedTeamIds)
			if err != nil {
				return err
			}
			response.Teams = append(response.Teams, item)
		}
		response.AvailableMatchupPeriods, err = q.DashboardMatchupPeriods(ctx, store.DashboardMatchupPeriodsParams{UserID: owner, LeagueSeasonID: id})
		return err
	})
	return response, err
}
