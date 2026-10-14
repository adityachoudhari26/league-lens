package dashboard

import (
	"context"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
)

func (s *Service) GetOverviewPlayerUpdates(context.Context, *v1.GetOverviewPlayerUpdatesRequest) (*v1.GetOverviewPlayerUpdatesResponse, error) {
	return &v1.GetOverviewPlayerUpdatesResponse{Coverage: coverage("unavailable", "Player news is not connected yet. No news checks have been performed.")}, nil
}
