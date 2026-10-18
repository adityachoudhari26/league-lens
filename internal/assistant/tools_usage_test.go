package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/llm"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
	"github.com/adityachoudhari26/league-lens/internal/sleeper"
)

type usageSourceStub struct {
	mu       sync.Mutex
	rows     []sleeper.WeeklyStat
	failures map[int]int
	calls    map[int]int
	started  chan struct{}
	release  chan struct{}
	inFlight atomic.Int32
	maximum  atomic.Int32
}

func (source *usageSourceStub) WeeklyStats(_ context.Context, _, week int) ([]sleeper.WeeklyStat, error) {
	current := source.inFlight.Add(1)
	defer source.inFlight.Add(-1)
	updateUsageMaximum(&source.maximum, current)
	source.mu.Lock()
	if source.calls == nil {
		source.calls = make(map[int]int)
	}
	source.calls[week]++
	call := source.calls[week]
	failure := call <= source.failures[week]
	started, release := source.started, source.release
	rows := source.rows
	source.mu.Unlock()
	if started != nil {
		started <- struct{}{}
	}
	if release != nil {
		<-release
	}
	if failure {
		return nil, errors.New("weekly stats unavailable")
	}
	return rows, nil
}

func updateUsageMaximum(maximum *atomic.Int32, value int32) {
	for previous := maximum.Load(); value > previous; previous = maximum.Load() {
		if maximum.CompareAndSwap(previous, value) {
			return
		}
	}
}

func TestUsageToolRegistrationAndValidation(t *testing.T) {
	source := &usageSourceStub{}
	tools := (&Service{Usage: source}).chatTools()
	tool := findAssistantToolByName(tools, "get_player_usage")
	if tool == nil || tool.label(json.RawMessage(`{}`)) != "Checking player usage" {
		t.Fatal("usage tool is missing or has the wrong label")
	}
	if tool.definition.Description != getPlayerUsageDescription ||
		strings.Contains(strings.ToLower(tool.definition.Description), "limitation") {
		t.Fatalf("usage description = %q", tool.definition.Description)
	}
	if findAssistantToolByName((&Service{}).chatTools(), "get_player_usage") != nil {
		t.Fatal("usage tool registered without a usage source")
	}

	elevenIDs := `"` + strings.Repeat(`00000000-0000-0000-0000-000000000001","`, 10) + `00000000-0000-0000-0000-000000000001"`
	for input, want := range map[string]string{
		`{}`:                "pass league_season_id or player_ids",
		`{"player_ids":[]}`: "pass league_season_id or player_ids",
		`{"league_season_id":"00000000-0000-0000-0000-000000000001","unknown":true}`: "invalid tool input",
		`{"league_season_id":"not-a-uuid"}`:                                          "invalid tool input",
		`{"player_ids":["not-a-uuid"]}`:                                              "invalid tool input",
		`{"player_ids":[` + elevenIDs + `]}`:                                         "pass at most 10 player_ids",
		`{"player_ids":["00000000-0000-0000-0000-000000000001"],"weeks":0}`:          "weeks must be between 1 and 6",
		`{"player_ids":["00000000-0000-0000-0000-000000000001"],"weeks":7}`:          "weeks must be between 1 and 6",
		`{"player_ids":["00000000-0000-0000-0000-000000000001"],"weeks":"3"}`:        "invalid tool input",
	} {
		result := runAssistantTool(context.Background(), tools, llm.ToolCall{
			ID: "usage-invalid", Name: "get_player_usage", Input: json.RawMessage(input),
		})
		if !result.IsError || result.Content != want {
			t.Errorf("input %s: result = %+v, want %q", input, result, want)
		}
	}
	if len(source.calls) != 0 {
		t.Fatalf("source called for invalid input: %v", source.calls)
	}
}

func TestIndexUsageWeekSeparatesNumericPlayerTargets(t *testing.T) {
	rows := append(usageFixtureRows(t), usageTestStat(t, "5555", "LAR", "DEF", map[string]float64{"rec_tgt": 99}))
	indexed := indexUsageWeek(rows)
	if indexed.teamTargets["LAR"] != 54 {
		t.Fatalf("LAR targets = %v, want 54", indexed.teamTargets["LAR"])
	}
	if _, found := indexed.players["TEAM_LAR"]; found {
		t.Fatal("team total was indexed as a player")
	}
	if _, found := indexed.players["LAR"]; found {
		t.Fatal("defense was indexed as a player")
	}
	if _, found := indexed.players["5555"]; found {
		t.Fatal("numeric defense was indexed as a player")
	}
}

func TestBuildUsageResultUsesWeeklyTeamTotalsAndSkipsNonSkillPlayers(t *testing.T) {
	rows := usageFixtureRows(t)
	indexed := indexUsageWeek(rows)
	players := make([]usagePlayer, 0, len(indexed.players))
	for playerID := range indexed.players {
		players = append(players, usagePlayer{name: playerID, statsID: playerID})
	}
	players = append(players, usagePlayer{name: "No Game", statsID: "no-game"})
	result := buildUsageResult(2026, []int32{5}, players, map[int32]usageWeek{5: indexed})
	puka := findUsagePlayer(t, result.Players, "9493")
	wantGame := usageGame{
		Week: 5, Snaps: 70, TeamSnaps: 82, SnapPct: 85.4, Targets: 24,
		TargetShare: 44.4, RedZoneTargets: 3, Carries: 2, RedZoneCarries: 1, AirYards: 57,
	}
	if len(puka.Games) != 1 || puka.Games[0] != wantGame ||
		puka.Position != "WR" || puka.NFLTeam != "LAR" {
		t.Fatalf("Puka usage = %+v, want game %+v", puka, wantGame)
	}
	encoded, err := json.Marshal(puka.Games[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "pass_attempts") || strings.Contains(string(encoded), "red_zone_pass_attempts") {
		t.Fatalf("game JSON contains empty pass keys: %s", encoded)
	}
	if findOptionalUsagePlayer(result.Players, "12015") != nil {
		t.Fatal("kicker was included")
	}
	if findOptionalUsagePlayer(result.Players, "11514") != nil {
		t.Fatal("inactive player was included")
	}
	if findOptionalUsagePlayer(result.Players, "No Game") != nil {
		t.Fatal("player without a game was included")
	}
	olPlayer := findUsagePlayer(t, result.Players, "11782")
	if len(olPlayer.Games) != 1 || olPlayer.Games[0].Snaps != 0 {
		t.Fatalf("player with a game but no offensive snaps = %+v, want zero snaps", olPlayer)
	}
}

func TestBuildUsageResultAveragesUnroundedPercentagesAndUsesNewestTeam(t *testing.T) {
	weekFour := usageTestStat(t, "1", "A", "RB", map[string]float64{
		"gp": 1, "off_snp": 1, "tm_off_snp": 3, "rec_tgt": 1, "rush_att": 1,
	})
	weekFive := usageTestStat(t, "1", "B", "WR", map[string]float64{
		"gp": 1, "off_snp": 2, "tm_off_snp": 3, "rec_tgt": 2, "rush_att": 2,
	})
	weekData := map[int32]usageWeek{
		4: indexUsageWeek([]sleeper.WeeklyStat{
			weekFour, usageTestStat(t, "2", "A", "WR", map[string]float64{"rec_tgt": 2}),
		}),
		5: indexUsageWeek([]sleeper.WeeklyStat{
			weekFive, usageTestStat(t, "3", "B", "WR", map[string]float64{"rec_tgt": 2}),
		}),
	}
	result := buildUsageResult(2026, []int32{4, 5}, []usagePlayer{{name: "Player", statsID: "1"}}, weekData)
	if len(result.Players) != 1 {
		t.Fatalf("players = %+v, want one", result.Players)
	}
	player := result.Players[0]
	if player.NFLTeam != "B" || player.Position != "WR" || len(player.Games) != 2 ||
		player.Games[0].TargetShare != 33.3 || player.Games[1].TargetShare != 50 {
		t.Fatalf("traded player usage = %+v", player)
	}
	wantAverage := usageAverage{SnapPct: 50, Targets: 1.5, TargetShare: 41.7, Carries: 1.5}
	if player.Average != wantAverage {
		t.Fatalf("average = %+v, want %+v", player.Average, wantAverage)
	}
}

func TestMapUsageSleeperIDsUsesCatalogAndDropsUnmappedIDs(t *testing.T) {
	service := &Service{PlayerCatalog: playerids.Catalog{
		"9493": {IDs: map[string]string{"espn": "101", "sleeper": "9493"}},
	}}
	players := service.mapUsageSleeperIDs(context.Background(), []usagePlayer{
		{name: "Sleeper", sleeperID: "1234"},
		{name: "ESPN", sleeperID: "legacy-id", espnID: "101"},
		{name: "Unmapped", sleeperID: "legacy-id"},
		{name: "Non-numeric catalog result", sleeperID: "legacy-id", espnID: "missing"},
	})
	if len(players) != 2 || players[0].statsID != "1234" || players[1].statsID != "9493" {
		t.Fatalf("mapped players = %+v", players)
	}
	if got := (&Service{}).mapUsageSleeperIDs(context.Background(), []usagePlayer{
		{name: "Catalog error", sleeperID: "legacy-id", espnID: "101"},
	}); len(got) != 0 {
		t.Fatalf("catalog error mapped players = %+v, want none", got)
	}
}

func TestLoadUsageWeeksRetriesFailsLimitsConcurrencyAndCaches(t *testing.T) {
	t.Run("retry once then succeed", func(t *testing.T) {
		source := &usageSourceStub{failures: map[int]int{5: 1}}
		service := &Service{Usage: source}
		if _, err := service.loadUsageWeeks(context.Background(), 2026, []int32{5}); err != nil {
			t.Fatal(err)
		}
		if source.calls[5] != 2 {
			t.Fatalf("week 5 calls = %d, want 2", source.calls[5])
		}
	})
	t.Run("failure after retry", func(t *testing.T) {
		source := &usageSourceStub{failures: map[int]int{5: 2}}
		if _, err := (&Service{Usage: source}).loadUsageWeeks(context.Background(), 2026, []int32{5}); err == nil {
			t.Fatal("two failed requests succeeded")
		}
		if source.calls[5] != 2 {
			t.Fatalf("week 5 calls = %d, want 2", source.calls[5])
		}
	})
	t.Run("no more than three in flight", func(t *testing.T) {
		source := &usageSourceStub{started: make(chan struct{}, 6), release: make(chan struct{})}
		service := &Service{Usage: source}
		done := make(chan error, 1)
		go func() {
			_, err := service.loadUsageWeeks(context.Background(), 2026, []int32{1, 2, 3, 4, 5, 6})
			done <- err
		}()
		for range maxUsageFetchesInFlight {
			select {
			case <-source.started:
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for concurrent stats requests")
			}
		}
		if source.inFlight.Load() != maxUsageFetchesInFlight {
			t.Fatalf("requests in flight = %d, want %d", source.inFlight.Load(), maxUsageFetchesInFlight)
		}
		close(source.release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if source.maximum.Load() > maxUsageFetchesInFlight {
			t.Fatalf("maximum in flight = %d, want at most %d", source.maximum.Load(), maxUsageFetchesInFlight)
		}
	})
	t.Run("cache hit and expiration", func(t *testing.T) {
		source := &usageSourceStub{}
		service := &Service{Usage: source}
		now := time.Date(2026, time.October, 17, 12, 0, 0, 0, time.UTC)
		service.usage.now = func() time.Time { return now }
		for range 2 {
			if _, err := service.loadUsageWeeks(context.Background(), 2026, []int32{5}); err != nil {
				t.Fatal(err)
			}
		}
		if source.calls[5] != 1 {
			t.Fatalf("calls after cache hit = %d, want 1", source.calls[5])
		}
		now = now.Add(usageWeekCacheTTL + time.Second)
		if _, err := service.loadUsageWeeks(context.Background(), 2026, []int32{5}); err != nil {
			t.Fatal(err)
		}
		if source.calls[5] != 2 {
			t.Fatalf("calls after expiration = %d, want 2", source.calls[5])
		}
	})
}

func usageFixtureRows(t *testing.T) []sleeper.WeeklyStat {
	t.Helper()
	content, err := os.ReadFile("../sleeper/testdata/weekly_stats_week5.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []sleeper.WeeklyStat
	if err := json.Unmarshal(content, &rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

func usageTestStat(t *testing.T, playerID, team, position string, stats map[string]float64) sleeper.WeeklyStat {
	t.Helper()
	content, err := json.Marshal(struct {
		PlayerID string             `json:"player_id"`
		Team     string             `json:"team"`
		Stats    map[string]float64 `json:"stats"`
		Player   struct {
			Position string `json:"position"`
		} `json:"player"`
	}{
		PlayerID: playerID, Team: team, Stats: stats,
		Player: struct {
			Position string `json:"position"`
		}{Position: position},
	})
	if err != nil {
		t.Fatal(err)
	}
	var row sleeper.WeeklyStat
	if err := json.Unmarshal(content, &row); err != nil {
		t.Fatal(err)
	}
	return row
}

func findUsagePlayer(t *testing.T, players []usagePlayerResult, name string) usagePlayerResult {
	t.Helper()
	player := findOptionalUsagePlayer(players, name)
	if player == nil {
		t.Fatalf("player %s not found in %+v", name, players)
	}
	return *player
}

func findOptionalUsagePlayer(players []usagePlayerResult, name string) *usagePlayerResult {
	for index := range players {
		if players[index].Name == name {
			return &players[index]
		}
	}
	return nil
}
