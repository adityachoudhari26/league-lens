//go:build integration

package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/dashboard"
	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/llm"
	"github.com/adityachoudhari26/league-lens/internal/sleeper"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

type integrationUsageSource struct {
	mu    sync.Mutex
	rows  []sleeper.WeeklyStat
	err   error
	calls []int
}

func (source *integrationUsageSource) WeeklyStats(_ context.Context, _, week int) ([]sleeper.WeeklyStat, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.calls = append(source.calls, week)
	return source.rows, source.err
}

func (source *integrationUsageSource) weeksCalled() []int {
	source.mu.Lock()
	defer source.mu.Unlock()
	return slices.Clone(source.calls)
}

func TestGetPlayerUsageReadsCompletedWeeksAndOwnerScopedRoster(t *testing.T) {
	ctx := context.Background()
	db := openSettingsTestDatabase(t)
	now := time.Now().UTC()
	ref := fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "usage-fixture", Season: 2026}
	data := usageIntegrationFixture(now, ref)
	importAssistantUsageLeague(t, ctx, db, data, "usage@example.test", "usage-owner")
	otherRef := fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "usage-other-fixture", Season: 2026}
	importAssistantUsageLeague(t, ctx, db, usageIntegrationFixture(now, otherRef), "other-usage@example.test", "usage-other-owner")
	seedUsageSchedule(t, ctx, db, now)

	reader := dashboard.New(db, "usage@example.test", nil, nil, nil)
	source := &integrationUsageSource{rows: usageIntegrationStats(t)}
	service := &Service{DB: db, OwnerEmail: "usage@example.test", Dashboard: reader, Usage: source}
	tools := service.chatTools()
	leagueID, awayTeamID := usageIntegrationLeagueIDs(t, ctx, reader, tools)
	otherReader := dashboard.New(db, "other-usage@example.test", nil, nil, nil)
	otherTools := (&Service{DB: db, OwnerEmail: "other-usage@example.test", Dashboard: otherReader}).chatTools()
	otherLeagues := runAssistantTool(ctx, otherTools, llm.ToolCall{
		ID: "other-leagues", Name: "list_leagues", Input: json.RawMessage(`{}`),
	})
	if otherLeagues.IsError {
		t.Fatalf("list_leagues for other owner: %s", otherLeagues.Content)
	}
	otherLeagueID := findJSONField(t, otherLeagues.Content, "id")

	roster := runUsageTool(t, tools, fmt.Sprintf(`{"league_season_id":%q}`, leagueID))
	expectedDefault := usageIntegrationResult(2026, []int32{3, 4, 5}, true)
	assertUsageToolResult(t, roster, expectedDefault)
	assertUsageWeeksCalled(t, source.weeksCalled(), []int{3, 4, 5})

	sixWeeks := runUsageTool(t, tools, fmt.Sprintf(`{"league_season_id":%q,"weeks":6}`, leagueID))
	expectedSixWeeks := usageIntegrationResult(2026, []int32{1, 2, 3, 4, 5}, true)
	assertUsageToolResult(t, sixWeeks, expectedSixWeeks)

	awayRoster := rosterContent(t, ctx, tools, awayTeamID)
	awayPlayerID := findPlayerIDByName(t, awayRoster, "Away Player")
	away := runUsageTool(t, tools, fmt.Sprintf(`{"player_ids":[%q]}`, awayPlayerID))
	expectedAway := usageIntegrationResult(2026, []int32{3, 4, 5}, false)
	expectedAway.Players = []usagePlayerResult{{
		Name: "Away Player", Position: "WR", NFLTeam: "BUF",
		Games: []usageGame{
			{Week: 3, Snaps: 60, TeamSnaps: 100, SnapPct: 60, Targets: 4, TargetShare: 100, AirYards: 20},
			{Week: 4, Snaps: 60, TeamSnaps: 100, SnapPct: 60, Targets: 4, TargetShare: 100, AirYards: 20},
			{Week: 5, Snaps: 60, TeamSnaps: 100, SnapPct: 60, Targets: 4, TargetShare: 100, AirYards: 20},
		},
		Average: usageAverage{SnapPct: 60, Targets: 4, TargetShare: 100},
	}}
	assertUsageToolResult(t, away, expectedAway)

	assertUsageToolError(t, tools,
		`{"league_season_id":"00000000-0000-0000-0000-000000000098"}`, "requested data not found")
	assertUsageToolError(t, tools,
		`{"player_ids":["00000000-0000-0000-0000-000000000099"]}`,
		"unknown player_id 00000000-0000-0000-0000-000000000099")
	assertUsageToolError(t, tools,
		fmt.Sprintf(`{"league_season_id":%q}`, otherLeagueID), "requested data not found")

	failingService := &Service{
		DB: db, OwnerEmail: "usage@example.test", Dashboard: reader,
		Usage: &integrationUsageSource{err: errors.New("Sleeper stats unavailable")},
	}
	assertUsageToolError(t, failingService.chatTools(), fmt.Sprintf(`{"league_season_id":%q}`, leagueID),
		"couldn't load the requested data")
}

func usageIntegrationFixture(now time.Time, ref fantasy.LeagueRef) fantasy.LeagueData {
	player := func(id, name string, ids ...fantasy.PlayerID) fantasy.Player {
		return fantasy.Player{ExternalID: id, Name: name, Kind: fantasy.PlayerPerson, KnownIDs: ids}
	}
	entry := func(id, proTeam string) fantasy.RosterEntry {
		return fantasy.RosterEntry{PlayerID: id, Slot: fantasy.SlotBench, ProTeam: proTeam}
	}
	return fantasy.LeagueData{
		League: fantasy.League{Ref: ref, Name: "Usage Fixture", TeamCount: 2}, FetchedAt: now,
		Teams: []fantasy.Team{{ExternalID: "home", Name: "Home"}, {ExternalID: "away", Name: "Away"}},
		Players: []fantasy.Player{
			player("1001", "Usage RB"),
			player("1002", "Usage WR"),
			player("1003", "Usage K"),
			player("unmapped", "Unmapped Player"),
			player("1004", "Away Player"),
		},
		Rosters: []fantasy.RosterSnapshot{
			{
				TeamID: "home", ScoringPeriod: 6, Origin: fantasy.SnapshotObserved,
				Coverage: fantasy.CoverageFull, ObservedAt: now,
				Entries: []fantasy.RosterEntry{
					entry("1001", "KC"), entry("1002", "KC"),
					entry("1003", "SF"), entry("unmapped", "SEA"),
				},
			},
			{
				TeamID: "away", ScoringPeriod: 6, Origin: fantasy.SnapshotObserved,
				Coverage: fantasy.CoverageFull, ObservedAt: now,
				Entries: []fantasy.RosterEntry{entry("1004", "BUF")},
			},
		},
	}
}

func importAssistantUsageLeague(
	t *testing.T,
	ctx context.Context,
	db *database.DB,
	data fantasy.LeagueData,
	ownerEmail, accountID string,
) {
	t.Helper()
	importService := importer.Service{DB: db, Source: assistantLeagueSource{data: data}}
	_, err := importService.Import(ctx, importer.Request{
		OwnerEmail: ownerEmail, ExternalAccountID: accountID,
		Selection: fantasy.ConnectionSelection{League: data.League.Ref, SelectedTeamIDs: []string{"home"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func seedUsageSchedule(t *testing.T, ctx context.Context, db *database.DB, now time.Time) {
	t.Helper()
	err := db.InTx(ctx, func(q *store.Queries) error {
		for week := int32(1); week <= 7; week++ {
			kickoff := now.Add(-time.Duration(week) * 24 * time.Hour)
			if week == 7 {
				kickoff = now.Add(24 * time.Hour)
			}
			if err := q.AddNFLGame(ctx, store.AddNFLGameParams{
				Season: 2026, Week: week, Team: "KC", Opponent: "BUF",
				KickoffAt: pgtype.Timestamptz{Time: kickoff, Valid: true},
				FetchedAt: pgtype.Timestamptz{Time: now, Valid: true},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func usageIntegrationStats(t *testing.T) []sleeper.WeeklyStat {
	t.Helper()
	return []sleeper.WeeklyStat{
		usageTestStat(t, "1001", "KC", "RB", map[string]float64{
			"gp": 1, "off_snp": 54, "tm_off_snp": 100, "rec_tgt": 2,
			"rec_rz_tgt": 1, "rush_att": 10, "rush_rz_att": 1,
		}),
		usageTestStat(t, "1002", "KC", "WR", map[string]float64{
			"gp": 1, "off_snp": 70, "tm_off_snp": 100, "rec_tgt": 3,
			"rec_rz_tgt": 1, "rec_air_yd": 30,
		}),
		usageTestStat(t, "1003", "SF", "K", map[string]float64{
			"gp": 1, "off_snp": 5, "tm_off_snp": 100,
		}),
		usageTestStat(t, "1004", "BUF", "WR", map[string]float64{
			"gp": 1, "off_snp": 60, "tm_off_snp": 100, "rec_tgt": 4, "rec_air_yd": 20,
		}),
	}
}

func usageIntegrationLeagueIDs(
	t *testing.T,
	ctx context.Context,
	reader *dashboard.Service,
	tools []assistantTool,
) (string, string) {
	t.Helper()
	leagues := runAssistantTool(ctx, tools, llm.ToolCall{
		ID: "usage-leagues", Name: "list_leagues", Input: json.RawMessage(`{}`),
	})
	if leagues.IsError {
		t.Fatalf("list_leagues: %s", leagues.Content)
	}
	leagueID := findJSONField(t, leagues.Content, "id")
	page, err := reader.GetLeague(ctx, &v1.GetLeagueRequest{LeagueSeasonId: leagueID})
	if err != nil {
		t.Fatal(err)
	}
	var awayTeamID string
	for _, team := range page.GetTeams() {
		if !team.GetSelected() {
			awayTeamID = team.GetId()
		}
	}
	return leagueID, awayTeamID
}

func runUsageTool(t *testing.T, tools []assistantTool, input string) llm.ToolResult {
	t.Helper()
	result := runAssistantTool(context.Background(), tools, llm.ToolCall{
		ID: "usage", Name: "get_player_usage", Input: json.RawMessage(input),
	})
	if result.IsError {
		t.Fatalf("get_player_usage %s: %s", input, result.Content)
	}
	return result
}

func assertUsageToolResult(t *testing.T, got llm.ToolResult, want usageToolResult) {
	t.Helper()
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if got.IsError || got.Content != string(encoded) {
		t.Fatalf("usage result = %+v\nwant %s", got, encoded)
	}
}

func assertUsageToolError(t *testing.T, tools []assistantTool, input, want string) {
	t.Helper()
	result := runAssistantTool(context.Background(), tools, llm.ToolCall{
		ID: "usage-error", Name: "get_player_usage", Input: json.RawMessage(input),
	})
	if !result.IsError || result.Content != want {
		t.Fatalf("input %s: result = %+v, want %q", input, result, want)
	}
}

func assertUsageWeeksCalled(t *testing.T, got, want []int) {
	t.Helper()
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("usage weeks called = %v, want %v", got, want)
	}
}

func usageIntegrationResult(season int32, weeks []int32, roster bool) usageToolResult {
	result := usageToolResult{Season: season, Weeks: weeks, Players: []usagePlayerResult{}}
	if !roster {
		return result
	}
	for _, name := range []string{"Usage RB", "Usage WR"} {
		result.Players = append(result.Players, usageIntegrationPlayer(name, weeks))
	}
	return result
}

func usageIntegrationPlayer(name string, weeks []int32) usagePlayerResult {
	position, snaps, snapPct, targets, targetShare := "RB", 54, float64(54), 2, float64(40)
	average := usageAverage{
		SnapPct: 54, Targets: 2, TargetShare: 40, RedZoneTargets: 1,
		Carries: 10, RedZoneCarries: 1,
	}
	if name == "Usage WR" {
		position, snaps, snapPct, targets, targetShare = "WR", 70, 70, 3, 60
		average = usageAverage{SnapPct: 70, Targets: 3, TargetShare: 60, RedZoneTargets: 1}
	}
	player := usagePlayerResult{
		Name: name, Position: position, NFLTeam: "KC", Games: make([]usageGame, 0, len(weeks)),
		Average: average,
	}
	for _, week := range weeks {
		game := usageGame{
			Week: week, Snaps: snaps, TeamSnaps: 100, SnapPct: snapPct,
			Targets: targets, TargetShare: targetShare, RedZoneTargets: 1,
		}
		if name == "Usage RB" {
			game.Carries, game.RedZoneCarries = 10, 1
		} else {
			game.AirYards = 30
		}
		player.Games = append(player.Games, game)
	}
	return player
}
