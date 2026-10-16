package dashboard

import (
	"context"
	"time"

	"connectrpc.com/connect/v2"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
)

const (
	liveGamesCacheTTL     = 10 * time.Second
	liveGamesFetchTimeout = 10 * time.Second
)

// LiveScoreboard reads the public NFL scoreboard.
type LiveScoreboard interface {
	NFLScoreboard(context.Context) (fantasy.NFLScoreboard, error)
}

// GetLiveGames returns in-progress NFL games from the public scoreboard.
func (s *Service) GetLiveGames(ctx context.Context, _ *v1.GetLiveGamesRequest) (*v1.GetLiveGamesResponse, error) {
	games, err := s.liveGamesSnapshot(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, "live scoreboard is unavailable")
	}
	return &v1.GetLiveGamesResponse{Games: liveGameMessages(games)}, nil
}

// liveGamesSnapshot returns a cached live-only view or fetches a fresh scoreboard.
func (s *Service) liveGamesSnapshot(ctx context.Context) ([]fantasy.NFLGameStatus, error) {
	if s.scoreboard == nil {
		return nil, nil
	}

	s.liveGamesMu.Lock()
	defer s.liveGamesMu.Unlock()

	now := s.now()
	if s.liveGamesCached && now.Sub(s.liveGamesFetchedAt) < liveGamesCacheTTL {
		return s.liveGamesCache, nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, liveGamesFetchTimeout)
	defer cancel()
	scoreboard, err := s.scoreboard.NFLScoreboard(fetchCtx)
	if err != nil {
		return nil, err
	}

	liveGames := make([]fantasy.NFLGameStatus, 0, len(scoreboard.Games))
	for _, game := range scoreboard.Games {
		if game.State == fantasy.NFLGameLive {
			liveGames = append(liveGames, game)
		}
	}
	s.liveGamesCache = liveGames
	s.liveGamesFetchedAt = s.now()
	s.liveGamesCached = true
	return s.liveGamesCache, nil
}

// liveGameMessages converts scoreboard games to their public RPC shape.
func liveGameMessages(games []fantasy.NFLGameStatus) []*v1.LiveGame {
	messages := make([]*v1.LiveGame, 0, len(games))
	for _, game := range games {
		messages = append(messages, &v1.LiveGame{
			HomeTeam:       game.HomeTeam,
			AwayTeam:       game.AwayTeam,
			PossessionTeam: game.Possession,
			RedZone:        game.RedZone,
		})
	}
	return messages
}
