package dashboard

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect/v2"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
)

type liveScoreboardStub struct {
	scoreboard fantasy.NFLScoreboard
	err        error
	calls      int
}

func (s *liveScoreboardStub) NFLScoreboard(context.Context) (fantasy.NFLScoreboard, error) {
	s.calls++
	return s.scoreboard, s.err
}

func TestGetLiveGamesFiltersAndCachesScoreboard(t *testing.T) {
	now := time.Date(2026, time.October, 16, 0, 30, 0, 0, time.UTC)
	source := &liveScoreboardStub{scoreboard: fantasy.NFLScoreboard{Games: []fantasy.NFLGameStatus{
		{HomeTeam: "DEN", AwayTeam: "SEA", State: fantasy.NFLGameLive, Possession: "DEN", RedZone: true},
		{HomeTeam: "JAX", AwayTeam: "HOU", State: fantasy.NFLGameScheduled},
		{HomeTeam: "KC", AwayTeam: "LAC", State: fantasy.NFLGameFinal},
	}}}
	service := New(nil, "", nil, nil, source)
	service.now = func() time.Time { return now }

	response, err := service.GetLiveGames(context.Background(), &v1.GetLiveGamesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Games) != 1 {
		t.Fatalf("got %d games, want 1 live game", len(response.Games))
	}
	if game := response.Games[0]; game.HomeTeam != "DEN" || game.AwayTeam != "SEA" || game.PossessionTeam != "DEN" || !game.RedZone {
		t.Fatalf("live game = %+v", game)
	}

	if _, err = service.GetLiveGames(context.Background(), &v1.GetLiveGamesRequest{}); err != nil {
		t.Fatal(err)
	}
	if source.calls != 1 {
		t.Fatalf("scoreboard calls within cache TTL = %d, want 1", source.calls)
	}

	now = now.Add(liveGamesCacheTTL)
	if _, err = service.GetLiveGames(context.Background(), &v1.GetLiveGamesRequest{}); err != nil {
		t.Fatal(err)
	}
	if source.calls != 2 {
		t.Fatalf("scoreboard calls after cache TTL = %d, want 2", source.calls)
	}
}

func TestGetLiveGamesMapsSourceErrorToUnavailable(t *testing.T) {
	source := &liveScoreboardStub{err: errors.New("provider detail")}
	service := New(nil, "", nil, nil, source)
	if _, err := service.GetLiveGames(context.Background(), &v1.GetLiveGamesRequest{}); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("GetLiveGames error = %v, want unavailable", err)
	}
}

func TestGetLiveGamesWithoutScoreboard(t *testing.T) {
	service := New(nil, "", nil, nil, nil)
	response, err := service.GetLiveGames(context.Background(), &v1.GetLiveGamesRequest{})
	if err != nil || len(response.Games) != 0 {
		t.Fatalf("GetLiveGames = %+v, %v; want empty response", response, err)
	}
}
