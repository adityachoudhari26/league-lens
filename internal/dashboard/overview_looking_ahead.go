package dashboard

import (
	"context"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
)

func (s *Service) GetOverviewLookingAhead(context.Context, *v1.GetOverviewLookingAheadRequest) (*v1.GetOverviewLookingAheadResponse, error) {
	return &v1.GetOverviewLookingAheadResponse{Coverage: coverage("unavailable", "NFL bye schedules are not connected yet. Upcoming roster conflicts have not been checked.")}, nil
}
