package dashboard

import (
	"context"

	"connectrpc.com/connect/v2"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
)

// GetMatchupDetail returns the owner's matchup for one round with both
// lineups. Not implemented until the lineup import lands.
func (s *Service) GetMatchupDetail(context.Context, *v1.GetMatchupDetailRequest) (*v1.GetMatchupDetailResponse, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, "matchup detail is not implemented yet")
}
