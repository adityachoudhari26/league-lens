package dashboard

import (
	"context"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
)

func (s *Service) GetOverviewLookingAhead(context.Context, *v1.GetOverviewLookingAheadRequest) (*v1.GetOverviewLookingAheadResponse, error) {
	return &v1.GetOverviewLookingAheadResponse{Coverage: coverage("unavailable", "Upcoming-week checks are not built yet. Byes for the current week appear under Needs attention.")}, nil
}
