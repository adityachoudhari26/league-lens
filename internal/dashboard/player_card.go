package dashboard

import (
	"context"

	"connectrpc.com/connect/v2"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
)

// GetPlayerCard is not built yet; the profile cache and card assembly land in
// the player card service PR.
func (s *Service) GetPlayerCard(context.Context, *v1.GetPlayerCardRequest) (*v1.GetPlayerCardResponse, error) {
	return nil, connect.NewError(connect.CodeUnimplemented, "player cards are not built yet")
}
