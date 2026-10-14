package espn

import (
	"testing"
	"time"
)

func TestParseNFLScheduleEmitsBothSides(t *testing.T) {
	body := []byte(`{"settings":{"proTeams":[
		{"id":12,"proGamesByScoringPeriod":{"1":[{"date":1789431300000,"homeProTeamId":12,"awayProTeamId":7}]}},
		{"id":7,"proGamesByScoringPeriod":{"1":[{"date":1789431300000,"homeProTeamId":12,"awayProTeamId":7}]}},
		{"id":0,"proGamesByScoringPeriod":{}}
	]}}`)
	games, err := parseNFLSchedule(body, 2026)
	if err != nil || len(games) != 2 {
		t.Fatalf("games = %+v, %v", games, err)
	}
	byTeam := map[string]string{}
	for _, game := range games {
		byTeam[game.Team] = game.Opponent
		if game.Week != 1 || !game.KickoffAt.Equal(time.UnixMilli(1789431300000)) {
			t.Fatalf("game = %+v", game)
		}
	}
	if byTeam["KC"] != "DEN" || byTeam["DEN"] != "KC" {
		t.Fatalf("opponents = %v", byTeam)
	}
	if _, err := parseNFLSchedule([]byte(`{"settings":{"proTeams":[]}}`), 2026); err == nil {
		t.Fatal("empty schedule accepted")
	}
}
