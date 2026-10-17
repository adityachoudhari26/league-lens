package assistant

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/leaguesync"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

// addTradeRestOfSeason adds optional projections without changing a successful trade result on provider or database failures.
func (s *Service) addTradeRestOfSeason(ctx context.Context, leagueSeasonID string, result *tradeToolResult) {
	if s.ProviderSources == nil || s.Dashboard == nil || result == nil {
		return
	}
	request, ok := s.tradeRestOfSeasonRequest(ctx, leagueSeasonID)
	if !ok {
		return
	}
	externalIDs, externalIDsByPlayer, err := s.tradeRestOfSeasonExternalIDs(
		ctx, request.target, result.Players,
	)
	if err != nil || len(externalIDs) == 0 {
		return
	}
	projectionContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	projections, err := request.projector.ProjectRestOfSeason(
		projectionContext,
		request.league,
		fantasy.RestOfSeasonQuery{Week: request.week, ExternalPlayerIDs: externalIDs},
	)
	if err != nil {
		return
	}
	applyTradeRestOfSeason(result, externalIDsByPlayer, projections)
}

type tradeRestOfSeasonRequest struct {
	target    leaguesync.Target
	projector fantasy.RestOfSeasonProjector
	league    fantasy.League
	week      int
}

func (s *Service) tradeRestOfSeasonRequest(
	ctx context.Context,
	leagueSeasonID string,
) (tradeRestOfSeasonRequest, bool) {
	target, err := s.ownerSyncTarget(ctx, leagueSeasonID, errFreeAgentTargetUnavailable)
	if err != nil {
		return tradeRestOfSeasonRequest{}, false
	}
	source, err := s.ProviderSources.SourceFor(target)
	if err != nil {
		return tradeRestOfSeasonRequest{}, false
	}
	projector, ok := source.(fantasy.RestOfSeasonProjector)
	if !ok {
		return tradeRestOfSeasonRequest{}, false
	}
	leaguePage, err := s.Dashboard.GetLeague(ctx, &v1.GetLeagueRequest{LeagueSeasonId: leagueSeasonID})
	if err != nil {
		return tradeRestOfSeasonRequest{}, false
	}
	week, err := latestSelectedScoringWeek(ctx, s.Dashboard, leaguePage)
	if err != nil {
		return tradeRestOfSeasonRequest{}, false
	}
	return tradeRestOfSeasonRequest{
		target: target, projector: projector,
		league: fantasy.League{Ref: target.Selection.League}, week: week,
	}, true
}

func (s *Service) tradeRestOfSeasonExternalIDs(
	ctx context.Context,
	target leaguesync.Target,
	players []tradePlayer,
) ([]string, map[string]string, error) {
	playerIDs, err := tradePlayerUUIDs(players)
	if err != nil {
		return nil, nil, err
	}
	if len(playerIDs) == 0 {
		return nil, map[string]string{}, nil
	}
	var rows []store.AssistantTradePlayerExternalIDsRow
	err = s.DB.InReadTx(ctx, func(q *store.Queries) error {
		var queryErr error
		rows, queryErr = q.AssistantTradePlayerExternalIDs(ctx, store.AssistantTradePlayerExternalIDsParams{
			Provider:  string(target.Selection.League.Provider),
			PlayerIds: playerIDs,
		})
		return queryErr
	})
	if err != nil {
		return nil, nil, err
	}
	externalIDsByPlayer := make(map[string]string, len(rows))
	externalIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		externalIDsByPlayer[row.PlayerID] = row.ExternalID
		externalIDs = append(externalIDs, row.ExternalID)
	}
	return externalIDs, externalIDsByPlayer, nil
}

func tradePlayerUUIDs(players []tradePlayer) ([]pgtype.UUID, error) {
	ids := make([]pgtype.UUID, 0, len(players))
	seen := make(map[string]bool, len(players))
	for _, player := range players {
		id, err := parseTradeUUID(player.PlayerID)
		if err != nil {
			return nil, err
		}
		if seen[id.String()] {
			continue
		}
		seen[id.String()] = true
		ids = append(ids, id)
	}
	return ids, nil
}

func applyTradeRestOfSeason(
	result *tradeToolResult,
	externalIDsByPlayer map[string]string,
	projections map[string]fantasy.RestOfSeasonProjection,
) {
	for index := range result.Players {
		externalID, found := externalIDsByPlayer[result.Players[index].PlayerID]
		if !found {
			continue
		}
		projection, found := projections[externalID]
		if !found {
			continue
		}
		result.Players[index].RestOfSeasonPoints = &projection.Points
		result.Players[index].RestOfSeasonWeeks = &projection.Weeks
	}
}
