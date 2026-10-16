//go:build integration

package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adityachoudhari26/league-lens/internal/dashboard"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/llm"
	"github.com/adityachoudhari26/league-lens/internal/store"
)

func TestEvaluateTradeUsesOwnerScopedRosterAndSyncedMatchups(t *testing.T) {
	ctx := context.Background()
	db := openSettingsTestDatabase(t)
	now := time.Now().UTC()
	ref := fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "trade-fixture", Season: 2026}
	league := fantasy.League{Ref: ref, Name: "Trade Fixture", TeamCount: 2}
	data := tradeFixtureData(now, league)
	importService := importer.Service{DB: db, Source: assistantLeagueSource{data: data}}
	_, err := importService.Import(ctx, importer.Request{
		OwnerEmail: "trade@example.test", ExternalAccountID: "sleeper",
		Selection: fantasy.ConnectionSelection{League: ref, SelectedTeamIDs: []string{"home"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := seedTradeSchedule(ctx, db, now); err != nil {
		t.Fatal(err)
	}

	reader := dashboard.New(db, "trade@example.test", nil, nil, nil)
	leagues := runAssistantTool(ctx, buildReadOnlyTools(reader), llm.ToolCall{
		ID: "leagues", Name: "list_leagues", Input: json.RawMessage(`{}`),
	})
	if leagues.IsError {
		t.Fatalf("list_leagues: %s", leagues.Content)
	}
	leagueID := findJSONField(t, leagues.Content, "id")
	leaguePage, err := reader.GetLeague(ctx, &v1.GetLeagueRequest{LeagueSeasonId: leagueID})
	if err != nil {
		t.Fatal(err)
	}
	var ownTeamID, otherTeamID string
	for _, team := range leaguePage.GetTeams() {
		if team.GetSelected() {
			ownTeamID = team.GetId()
		} else {
			otherTeamID = team.GetId()
		}
	}
	tradeService := &Service{DB: db, OwnerEmail: "trade@example.test", Dashboard: reader}
	tools := tradeService.chatTools()
	ownRoster := runAssistantTool(ctx, tools, llm.ToolCall{
		ID: "own-roster", Name: "get_team_roster",
		Input: json.RawMessage(`{"team_id":"` + ownTeamID + `"}`),
	})
	otherRoster := runAssistantTool(ctx, tools, llm.ToolCall{
		ID: "other-roster", Name: "get_team_roster",
		Input: json.RawMessage(`{"team_id":"` + otherTeamID + `"}`),
	})
	if ownRoster.IsError || otherRoster.IsError {
		t.Fatalf("roster results: own=%s other=%s", ownRoster.Content, otherRoster.Content)
	}
	giveID := findPlayerIDByName(t, ownRoster.Content, "Give QB")
	getID := findPlayerIDByName(t, otherRoster.Content, "Target WR")
	evaluated := runAssistantTool(ctx, tools, llm.ToolCall{
		ID: "trade", Name: "evaluate_trade",
		Input: json.RawMessage(fmt.Sprintf(
			`{"league_season_id":%q,"give_player_ids":[%q],"get_player_ids":[%q]}`,
			leagueID, giveID, getID,
		)),
	})
	if evaluated.IsError {
		t.Fatalf("evaluate_trade: %s", evaluated.Content)
	}
	var result tradeToolResult
	if err := json.Unmarshal([]byte(evaluated.Content), &result); err != nil {
		t.Fatal(err)
	}
	if result.Basis != tradePointsBasis || result.Limitation != tradeLineupLimitation {
		t.Fatalf("trade basis or coverage limitation: %+v", result)
	}
	if len(result.Players) != 2 || result.Players[0].Name != "Give QB" ||
		result.Players[1].Name != "Target WR" {
		t.Fatalf("players: %+v", result.Players)
	}
	if result.Players[0].AveragePoints == nil || *result.Players[0].AveragePoints < 26.66 ||
		*result.Players[0].AveragePoints > 26.68 || len(result.Players[0].RecentPoints) != 4 ||
		result.Players[0].RecentPoints[2].Points != nil {
		t.Fatalf("give player points: %+v", result.Players[0])
	}
	target := result.Players[1]
	if target.TeamName != "Away Team" || target.NFLTeam != "KC" ||
		target.InjuryStatus != string(fantasy.InjuryQuestionable) || target.ByeWeek == nil ||
		*target.ByeWeek != 6 || target.ProjectedPoints == nil || *target.ProjectedPoints != 18.5 ||
		target.RecentPoints[2].Points != nil {
		t.Fatalf("get player metadata: %+v", target)
	}
	if result.OwnerRosterCounts.Before["QB"] != 1 || result.OwnerRosterCounts.Before["RB"] != 1 ||
		result.OwnerRosterCounts.After["QB"] != 0 || result.OwnerRosterCounts.After["WR"] != 1 {
		t.Fatalf("owner position counts: %+v", result.OwnerRosterCounts)
	}

	rejected := runAssistantTool(ctx, tools, llm.ToolCall{
		ID: "invalid-trade", Name: "evaluate_trade",
		Input: json.RawMessage(fmt.Sprintf(
			`{"league_season_id":%q,"give_player_ids":[%q],"get_player_ids":[%q]}`,
			leagueID, getID, giveID,
		)),
	})
	if !rejected.IsError || rejected.Content != "Target WR is not on your team to give" {
		t.Fatalf("invalid owner player result: %+v", rejected)
	}
}

func tradeFixtureData(now time.Time, league fantasy.League) fantasy.LeagueData {
	players := []fantasy.Player{
		{ExternalID: "give-qb", Name: "Give QB", Kind: fantasy.PlayerPerson},
		{ExternalID: "keep-rb", Name: "Keep RB", Kind: fantasy.PlayerPerson},
		{ExternalID: "target-wr", Name: "Target WR", Kind: fantasy.PlayerPerson},
		{ExternalID: "away-te", Name: "Away TE", Kind: fantasy.PlayerPerson},
	}
	data := fantasy.LeagueData{
		League: league, FetchedAt: now,
		Teams:   []fantasy.Team{{ExternalID: "home", Name: "Home Team"}, {ExternalID: "away", Name: "Away Team"}},
		Players: players,
		Rosters: []fantasy.RosterSnapshot{
			{
				TeamID: "home", ScoringPeriod: 5, Origin: fantasy.SnapshotObserved,
				Coverage: fantasy.CoverageFull, ObservedAt: now,
				Entries: []fantasy.RosterEntry{
					{PlayerID: "give-qb", Slot: fantasy.SlotQB, EligiblePositions: []fantasy.Position{fantasy.PositionQB}, ProTeam: "BUF"},
					{PlayerID: "keep-rb", Slot: fantasy.SlotRB, EligiblePositions: []fantasy.Position{fantasy.PositionRB}},
				},
			},
			{
				TeamID: "away", ScoringPeriod: 5, Origin: fantasy.SnapshotObserved,
				Coverage: fantasy.CoverageFull, ObservedAt: now,
				Entries: []fantasy.RosterEntry{
					{PlayerID: "target-wr", Slot: fantasy.SlotWR, EligiblePositions: []fantasy.Position{fantasy.PositionWR}, InjuryStatus: fantasy.InjuryQuestionable, ProTeam: "KC"},
					{PlayerID: "away-te", Slot: fantasy.SlotTE, EligiblePositions: []fantasy.Position{fantasy.PositionTE}},
				},
			},
		},
	}
	for week := 1; week <= 5; week++ {
		status := fantasy.MatchupFinal
		if week == 5 {
			status = fantasy.MatchupInProgress
		}
		data.Matchups = append(data.Matchups, fantasy.Matchup{
			ExternalID: fmt.Sprintf("week-%d", week), Period: fantasy.MatchupPeriod(week),
			ScoringPeriods: []fantasy.ScoringPeriod{fantasy.ScoringPeriod(week)},
			TeamA:          fantasy.MatchupSide{TeamID: "home"}, TeamB: &fantasy.MatchupSide{TeamID: "away"},
			Status: status,
		})
		homePoints := []float64{10, 0, 30, 40, 0}
		awayPoints := []float64{8, 0, 10, 14, 0}
		homePoint := homePoints[week-1]
		awayPoint := awayPoints[week-1]
		homeEntry := fantasy.LineupEntry{PlayerID: "give-qb", Slot: fantasy.SlotQB, Starter: true}
		if week != 2 {
			homeEntry.Points = &homePoint
		}
		awayEntry := fantasy.LineupEntry{PlayerID: "target-wr", Slot: fantasy.SlotWR, Starter: true}
		if week == 1 || week == 3 || week == 4 {
			awayEntry.Points = &awayPoint
		}
		if week == 5 {
			projection := 18.5
			awayEntry.ProjectedPoints = &projection
		}
		data.Lineups = append(data.Lineups, fantasy.MatchupLineup{
			Period: fantasy.MatchupPeriod(week), MatchupID: fmt.Sprintf("week-%d", week),
			TeamID: "home", Entries: []fantasy.LineupEntry{homeEntry},
		})
		if week != 2 {
			data.Lineups = append(data.Lineups, fantasy.MatchupLineup{
				Period: fantasy.MatchupPeriod(week), MatchupID: fmt.Sprintf("week-%d", week),
				TeamID: "away", Entries: []fantasy.LineupEntry{awayEntry},
			})
		}
		data.CompleteMatchupPeriods = append(data.CompleteMatchupPeriods, fantasy.MatchupPeriod(week))
	}
	return data
}

func seedTradeSchedule(ctx context.Context, db interface {
	InTx(context.Context, func(*store.Queries) error) error
}, now time.Time) error {
	fetchedAt := pgtype.Timestamptz{Time: now, Valid: true}
	return db.InTx(ctx, func(q *store.Queries) error {
		for week := int32(1); week <= 18; week++ {
			for _, team := range []string{"BUF", "KC"} {
				if team == "KC" && week == 6 {
					continue
				}
				if err := q.AddNFLGame(ctx, store.AddNFLGameParams{
					Season: 2026, Week: week, Team: team, Opponent: "OTHER",
					KickoffAt: fetchedAt, FetchedAt: fetchedAt,
				}); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func findPlayerIDByName(t *testing.T, content, name string) string {
	t.Helper()
	var value any
	if err := json.Unmarshal([]byte(content), &value); err != nil {
		t.Fatal(err)
	}
	if id := findNamedPlayerID(value, name); id != "" {
		return id
	}
	t.Fatalf("%q missing from %s", name, content)
	return ""
}

func findNamedPlayerID(value any, name string) string {
	switch value := value.(type) {
	case map[string]any:
		if value["player_name"] == name {
			if id, ok := value["player_id"].(string); ok {
				return id
			}
		}
		for _, child := range value {
			if id := findNamedPlayerID(child, name); id != "" {
				return id
			}
		}
	case []any:
		for _, child := range value {
			if id := findNamedPlayerID(child, name); id != "" {
				return id
			}
		}
	}
	return ""
}
