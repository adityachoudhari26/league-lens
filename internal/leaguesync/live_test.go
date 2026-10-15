package leaguesync

import (
	"context"
	"testing"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/news"
)

type scoreboardTestSources struct {
	board       fantasy.NFLScoreboard
	scoreboard  int
	providerUse int
}

func (s *scoreboardTestSources) SourceFor(Target) (fantasy.Provider, error) {
	s.providerUse++
	return nil, nil
}

func (*scoreboardTestSources) NFLSchedule(context.Context, int) ([]fantasy.NFLGame, error) {
	return nil, nil
}

func (s *scoreboardTestSources) NFLScoreboard(context.Context) (fantasy.NFLScoreboard, error) {
	s.scoreboard++
	return s.board, nil
}

func (s *scoreboardTestSources) NFLWeekScoreboard(context.Context, int, int) (fantasy.NFLScoreboard, error) {
	return s.board, nil
}

func (*scoreboardTestSources) NFLGamePlays(context.Context, string) ([]fantasy.NFLPlay, error) {
	return nil, nil
}

func (*scoreboardTestSources) NewsSource() news.Source { return nil }

func TestActiveNFLTeamsUsesFifteenMinuteWindow(t *testing.T) {
	now := time.Date(2026, time.October, 16, 1, 0, 0, 0, time.UTC)
	lastLive := map[string]time.Time{}
	recordLiveTeams([]fantasy.NFLGameStatus{
		{HomeTeam: "KC", AwayTeam: "DEN", State: fantasy.NFLGameLive},
		{HomeTeam: "BUF", AwayTeam: "MIA", State: fantasy.NFLGameScheduled},
	}, now, lastLive)

	if active := activeNFLTeams(now, lastLive); len(active) != 2 {
		t.Fatalf("active teams now = %v, want KC and DEN", active)
	}
	if active := activeNFLTeams(now.Add(10*time.Minute), lastLive); len(active) != 2 {
		t.Fatalf("active teams after 10 minutes = %v, want KC and DEN", active)
	}
	if active := activeNFLTeams(now.Add(16*time.Minute), lastLive); len(active) != 0 {
		t.Fatalf("active teams after 16 minutes = %v, want none", active)
	}
	if _, found := lastLive["BUF"]; found {
		t.Fatal("scheduled team was marked live")
	}
}

func TestLiveCandidateRelevance(t *testing.T) {
	active := map[string]struct{}{"KC": {}}
	for _, test := range []struct {
		name  string
		teams []string
		want  bool
	}{
		{name: "overlap", teams: []string{"KC", "BUF"}, want: true},
		{name: "no overlap", teams: []string{"BUF", "MIA"}},
		{name: "unknown team", teams: []string{"", "BUF"}, want: true},
		{name: "empty teams", teams: []string{}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := liveCandidateRelevant(test.teams, active); got != test.want {
				t.Fatalf("relevant = %t, want %t", got, test.want)
			}
		})
	}
}

func TestLiveScoreTickSkipsNonRegularSeason(t *testing.T) {
	sources := &scoreboardTestSources{board: fantasy.NFLScoreboard{SeasonType: 1}}
	runner := &Runner{sources: sources}
	runner.runLiveScoreTick(context.Background(), map[string]time.Time{})
	if sources.scoreboard != 1 || sources.providerUse != 0 {
		t.Fatalf("scoreboard reads=%d provider calls=%d, want 1 and 0", sources.scoreboard, sources.providerUse)
	}
}

func TestLiveScoreTickSkipsDatabaseWhenNoGamesAreLive(t *testing.T) {
	sources := &scoreboardTestSources{board: fantasy.NFLScoreboard{SeasonType: 2}}
	runner := &Runner{sources: sources, now: time.Now}
	runner.runLiveScoreTick(context.Background(), map[string]time.Time{})
	if sources.scoreboard != 1 || sources.providerUse != 0 {
		t.Fatalf("scoreboard reads=%d provider calls=%d, want 1 and 0", sources.scoreboard, sources.providerUse)
	}
}

func TestStarterRemaining(t *testing.T) {
	liveGame := fantasy.NFLGameStatus{
		HomeTeam: "KC", AwayTeam: "SF", State: fantasy.NFLGameLive,
		Period: 2, Clock: 7*time.Minute + 30*time.Second,
	}
	for _, test := range []struct {
		name    string
		proTeam string
		games   []fantasy.NFLGameStatus
		want    float64
	}{
		{name: "bye", proTeam: "BUF", games: []fantasy.NFLGameStatus{liveGame}, want: 0},
		{name: "unknown team with games left", games: []fantasy.NFLGameStatus{liveGame}, want: 1},
		{name: "unknown team when all games are final", games: []fantasy.NFLGameStatus{{State: fantasy.NFLGameFinal}}, want: 0},
		{name: "known team mid-game", proTeam: "KC", games: []fantasy.NFLGameStatus{liveGame}, want: 0.625},
	} {
		t.Run(test.name, func(t *testing.T) {
			clock := newLiveGameClock(test.games)
			if got := clock.starterRemaining(test.proTeam); got != test.want {
				t.Fatalf("starterRemaining() = %v, want %v", got, test.want)
			}
		})
	}
}
