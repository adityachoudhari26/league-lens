package dashboard

import (
	"context"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// ListMatchups reads an accessible league's requested matchup round.
// A matchup round may span multiple scoring weeks.
func (s *Service) ListMatchups(ctx context.Context, request *v1.ListMatchupsRequest) (*v1.ListMatchupsResponse, error) {
	id, err := parseID(request.LeagueSeasonId)
	if err != nil {
		return nil, err
	}
	if err = validatePeriod(request.MatchupPeriod); err != nil {
		return nil, err
	}
	response := &v1.ListMatchupsResponse{}
	err = s.read(ctx, func(ctx context.Context, q *store.Queries, owner pgtype.UUID) error {
		if _, err := q.DashboardLeague(ctx, store.DashboardLeagueParams{UserID: owner, ID: id}); err != nil {
			return err
		}
		matches, err := q.ListMatchupsForUser(ctx, store.ListMatchupsForUserParams{UserID: owner, LeagueSeasonID: id, Period: request.MatchupPeriod})
		if err != nil {
			return err
		}
		for _, match := range matches {
			item, err := matchupMessage(match)
			if err != nil {
				return err
			}
			response.Matchups = append(response.Matchups, item)
		}
		return nil
	})
	return response, err
}
