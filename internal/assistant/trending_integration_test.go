//go:build integration

package assistant

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/dashboard"
	"github.com/adityachoudhari26/league-lens/internal/database"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/llm"
	"github.com/adityachoudhari26/league-lens/internal/playerids"
	"github.com/adityachoudhari26/league-lens/internal/sleeper"
)

func TestTrendingAvailabilityUsesLatestOwnerScopedRosterSnapshots(t *testing.T) {
	ctx := context.Background()
	db := openSettingsTestDatabase(t)
	now := time.Now().UTC()
	ref := fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "trending-sleeper", Season: 2026}
	players := []fantasy.Player{
		{ExternalID: "s-own", Name: "Own Player", Kind: fantasy.PlayerPerson},
		{ExternalID: "s-away", Name: "Away Player", Kind: fantasy.PlayerPerson},
		{ExternalID: "s-dropped", Name: "Dropped Player", Kind: fantasy.PlayerPerson},
		{ExternalID: "s-free", Name: "Free Player", Kind: fantasy.PlayerPerson},
	}
	importTrendingFixture(t, ctx, db, "trending-sleeper@example.test",
		trendingSleeperData(ref, players, now, true))
	importTrendingFixture(t, ctx, db, "trending-sleeper@example.test",
		trendingSleeperData(ref, players, now.Add(time.Minute), false))

	reader := dashboard.New(db, "trending-sleeper@example.test", nil, nil, nil)
	leagueID := trendingLeagueID(t, ctx, reader)
	service := &Service{
		DB: db, OwnerEmail: "trending-sleeper@example.test", Dashboard: reader,
		Trending: &trendingSourceStub{rows: []sleeper.TrendingPlayer{
			{PlayerID: "s-own", Count: 100},
			{PlayerID: "s-away", Count: 90},
			{PlayerID: "s-free", Count: 80},
			{PlayerID: "s-dropped", Count: 70},
			{PlayerID: "not-mapped", Count: 60},
		}},
		PlayerCatalog: playerids.Catalog{
			"s-own":     {Name: "Own Player", Position: "WR", Team: "BUF"},
			"s-away":    {Name: "Away Player", Position: "RB", Team: "KC"},
			"s-free":    {Name: "Free Player", Position: "TE", Team: "FA"},
			"s-dropped": {Name: "Dropped Player", Position: "LB", Team: "FA"},
		},
	}
	result := runAssistantTool(ctx, service.chatTools(), llm.ToolCall{
		ID: "trending", Name: "get_trending_players",
		Input: json.RawMessage(`{"league_season_id":"` + leagueID + `"}`),
	})
	if result.IsError {
		t.Fatalf("get_trending_players: %s", result.Content)
	}
	var output trendingToolResult
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Players) != 4 ||
		output.Players[0].Name != "Own Player" || output.Players[0].Availability != "on your team" ||
		output.Players[1].Name != "Away Player" || output.Players[1].Availability != "rostered by Away Team" ||
		output.Players[2].Name != "Free Player" || output.Players[2].Availability != "available" ||
		output.Players[3].Name != "Dropped Player" || output.Players[3].Availability != "available" {
		t.Fatalf("Sleeper availability = %+v", output.Players)
	}
	if denied := runAssistantTool(ctx, service.chatTools(), llm.ToolCall{
		ID: "private-league", Name: "get_trending_players",
		Input: json.RawMessage(`{"league_season_id":"00000000-0000-0000-0000-000000000001"}`),
	}); !denied.IsError || denied.Content != "requested data not found" {
		t.Fatalf("unowned league response = %+v", denied)
	}
}

func TestTrendingAvailabilityUsesESPNIDsAndOmitsUnavailableMappings(t *testing.T) {
	ctx := context.Background()
	db := openSettingsTestDatabase(t)
	now := time.Now().UTC()
	ref := fantasy.LeagueRef{Provider: fantasy.ProviderESPN, ExternalID: "trending-espn", Season: 2026}
	players := []fantasy.Player{
		{
			ExternalID: "e-own", Name: "ESPN Player", Kind: fantasy.PlayerPerson,
			KnownIDs: []fantasy.PlayerID{{Namespace: fantasy.PlayerIDSleeper, ExternalID: "s-espn-own"}},
		},
		{
			ExternalID: "e-no-catalog-id", Name: "No ESPN Mapping", Kind: fantasy.PlayerPerson,
			KnownIDs: []fantasy.PlayerID{{Namespace: fantasy.PlayerIDSleeper, ExternalID: "s-no-espn"}},
		},
	}
	data := trendingRosterData(ref, players, now, []string{"e-own"}, nil)
	importTrendingFixture(t, ctx, db, "trending-espn@example.test", data)

	reader := dashboard.New(db, "trending-espn@example.test", nil, nil, nil)
	leagueID := trendingLeagueID(t, ctx, reader)
	service := &Service{
		DB: db, OwnerEmail: "trending-espn@example.test", Dashboard: reader,
		Trending: &trendingSourceStub{rows: []sleeper.TrendingPlayer{
			{PlayerID: "s-espn-own", Count: 20},
			{PlayerID: "s-no-espn", Count: 10},
		}},
		PlayerCatalog: playerids.Catalog{
			"s-espn-own": {Name: "ESPN Player", Position: "WR", IDs: map[string]string{
				"sleeper": "s-espn-own", "espn": "e-own",
			}},
			"s-no-espn": {Name: "No ESPN Mapping", Position: "RB", IDs: map[string]string{
				"sleeper": "s-no-espn",
			}},
		},
	}
	result := runAssistantTool(ctx, service.chatTools(), llm.ToolCall{
		ID: "trending", Name: "get_trending_players",
		Input: json.RawMessage(`{"league_season_id":"` + leagueID + `"}`),
	})
	if result.IsError {
		t.Fatalf("get_trending_players: %s", result.Content)
	}
	var output map[string]any
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
		t.Fatal(err)
	}
	rows := output["players"].([]any)
	if len(rows) != 2 {
		t.Fatalf("ESPN trending players = %v", rows)
	}
	first := rows[0].(map[string]any)
	second := rows[1].(map[string]any)
	if first["name"] != "ESPN Player" || first["availability"] != "on your team" {
		t.Fatalf("ESPN mapped player = %v", first)
	}
	if second["name"] != "No ESPN Mapping" {
		t.Fatalf("ESPN unmapped player = %v", second)
	}
	if _, exists := second["availability"]; exists {
		t.Fatalf("availability should be omitted without an ESPN catalog ID: %v", second)
	}
}

func importTrendingFixture(
	t *testing.T,
	ctx context.Context,
	db *database.DB,
	owner string,
	data fantasy.LeagueData,
) {
	t.Helper()
	service := importer.Service{DB: db, Source: assistantLeagueSource{data: data}}
	_, err := service.Import(ctx, importer.Request{
		OwnerEmail: owner, ExternalAccountID: string(data.League.Ref.Provider),
		Selection: fantasy.ConnectionSelection{
			League: data.League.Ref, SelectedTeamIDs: []string{"home"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func trendingSleeperData(
	ref fantasy.LeagueRef,
	players []fantasy.Player,
	now time.Time,
	includeDropped bool,
) fantasy.LeagueData {
	awayIDs := []string{"s-away"}
	if includeDropped {
		awayIDs = append(awayIDs, "s-dropped")
	}
	return trendingRosterData(ref, players, now, []string{"s-own"}, awayIDs)
}

func trendingRosterData(
	ref fantasy.LeagueRef,
	players []fantasy.Player,
	now time.Time,
	homeIDs, awayIDs []string,
) fantasy.LeagueData {
	league := fantasy.League{Ref: ref, Name: "Trending Test", TeamCount: 2}
	return fantasy.LeagueData{
		League: league, FetchedAt: now,
		Teams: []fantasy.Team{
			{ExternalID: "home", Name: "Home Team"},
			{ExternalID: "away", Name: "Away Team"},
		},
		Players: players,
		Rosters: []fantasy.RosterSnapshot{
			trendingRosterSnapshot("home", homeIDs, now),
			trendingRosterSnapshot("away", awayIDs, now),
		},
	}
}

func trendingRosterSnapshot(teamID string, playerIDs []string, observedAt time.Time) fantasy.RosterSnapshot {
	entries := make([]fantasy.RosterEntry, 0, len(playerIDs))
	for _, playerID := range playerIDs {
		entries = append(entries, fantasy.RosterEntry{
			PlayerID: playerID, Slot: fantasy.SlotBench,
		})
	}
	return fantasy.RosterSnapshot{
		TeamID: teamID, ScoringPeriod: 1, Origin: fantasy.SnapshotObserved,
		Coverage: fantasy.CoverageFull, ObservedAt: observedAt, Entries: entries,
	}
}

func trendingLeagueID(t *testing.T, ctx context.Context, reader assistantDashboard) string {
	t.Helper()
	listed := runAssistantTool(ctx, buildReadOnlyTools(reader), llm.ToolCall{
		ID: "leagues", Name: "list_leagues", Input: json.RawMessage(`{}`),
	})
	if listed.IsError {
		t.Fatalf("list_leagues: %s", listed.Content)
	}
	return findJSONField(t, listed.Content, "id")
}
