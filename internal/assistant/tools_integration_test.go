//go:build integration

package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/dashboard"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/leaguesync"
	"github.com/adityachoudhari26/league-lens/internal/llm"
)

type assistantLeagueSource struct{ data fantasy.LeagueData }

func (s assistantLeagueSource) DescribeLeague(context.Context, string) (fantasy.League, error) {
	return s.data.League, nil
}

func (s assistantLeagueSource) FetchLeague(
	context.Context,
	fantasy.League,
	fantasy.FetchRequest,
) (fantasy.LeagueData, error) {
	return s.data, nil
}

type assistantFreeAgentSource struct {
	assistantLeagueSource
	query fantasy.FreeAgentQuery
}

func (s *assistantFreeAgentSource) ListFreeAgents(_ context.Context, _ fantasy.League, query fantasy.FreeAgentQuery) ([]fantasy.FreeAgent, error) {
	s.query = query
	points := 12.5
	restOfSeasonPoints, restOfSeasonWeeks := 99.9, 11
	return []fantasy.FreeAgent{
		{
			ExternalPlayerID: "free-agent-1", Name: "Available Player", Position: "RB",
			Status: "available", ProjectedPoints: &points,
			RestOfSeasonPoints: &restOfSeasonPoints, RestOfSeasonWeeks: &restOfSeasonWeeks,
		},
		{
			ExternalPlayerID: "free-agent-2", Name: "Unknown Player", Position: "WR",
			Status: "available",
		},
	}, nil
}

type assistantFreeAgentSourceFactory struct{ source fantasy.Provider }

func (f assistantFreeAgentSourceFactory) SourceFor(leaguesync.Target) (fantasy.Provider, error) {
	return f.source, nil
}

func TestReadToolsUseSeededDashboard(t *testing.T) {
	ctx := context.Background()
	db := openSettingsTestDatabase(t)
	now := time.Now().UTC()
	league := fantasy.League{
		Ref:  fantasy.LeagueRef{Provider: fantasy.ProviderSleeper, ExternalID: "assistant-tools", Season: 2026},
		Name: "Assistant Tools", TeamCount: 2,
	}
	zero, one := 7.0, 9.0
	data := fantasy.LeagueData{
		League: league, FetchedAt: now,
		Players: []fantasy.Player{{
			ExternalID: "injured", Name: "Injured Player", Kind: fantasy.PlayerPerson,
		}},
		Teams: []fantasy.Team{{ExternalID: "home", Name: "Home Team"}, {ExternalID: "away", Name: "Away Team"}},
		Rosters: []fantasy.RosterSnapshot{
			{
				TeamID: "home", ScoringPeriod: 1, Origin: fantasy.SnapshotObserved,
				Coverage: fantasy.CoverageFull, ObservedAt: now,
				Entries: []fantasy.RosterEntry{{
					PlayerID: "injured", Slot: fantasy.SlotQB, InjuryStatus: fantasy.InjuryOut, ProTeam: "KC",
				}},
			},
			{TeamID: "away", ScoringPeriod: 1, Origin: fantasy.SnapshotObserved, Coverage: fantasy.CoverageFull, ObservedAt: now},
		},
		Matchups: []fantasy.Matchup{{
			ExternalID: "week-one", Period: 1, ScoringPeriods: []fantasy.ScoringPeriod{1},
			TeamA:  fantasy.MatchupSide{TeamID: "home", Score: &one},
			TeamB:  &fantasy.MatchupSide{TeamID: "away", Score: &zero},
			Status: fantasy.MatchupInProgress,
		}},
		CompleteMatchupPeriods: []fantasy.MatchupPeriod{1},
	}
	importService := importer.Service{DB: db, Source: assistantLeagueSource{data: data}}
	_, err := importService.Import(ctx, importer.Request{
		OwnerEmail: "tools@example.test", ExternalAccountID: "sleeper",
		Selection: fantasy.ConnectionSelection{
			League: league.Ref, SelectedTeamIDs: []string{"home"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	dashboardService := dashboard.New(db, "tools@example.test", nil, nil, nil)
	tools := buildReadOnlyTools(dashboardService)
	listed := runAssistantTool(ctx, tools, llm.ToolCall{
		ID: "leagues", Name: "list_leagues", Input: json.RawMessage(`{}`),
	})
	if listed.IsError {
		t.Fatalf("list_leagues: %s", listed.Content)
	}
	leagueID := findJSONField(t, listed.Content, "id")
	freeAgentSource := &assistantFreeAgentSource{assistantLeagueSource: assistantLeagueSource{data: data}}
	freeAgentService := &Service{
		DB: db, OwnerEmail: "tools@example.test", Dashboard: dashboardService,
		ProviderSources: assistantFreeAgentSourceFactory{source: freeAgentSource},
	}
	freeAgents := runAssistantTool(ctx, freeAgentService.chatTools(), llm.ToolCall{
		ID: "agents", Name: "get_free_agents",
		Input: json.RawMessage(`{"league_season_id":"` + leagueID + `","position":"RB","limit":4}`),
	})
	var freeAgentOutput struct {
		FreeAgents []map[string]json.RawMessage `json:"free_agents"`
	}
	if err := json.Unmarshal([]byte(freeAgents.Content), &freeAgentOutput); err != nil {
		t.Fatal(err)
	}
	if freeAgents.IsError || !strings.Contains(freeAgents.Content, `"source":"Sleeper projections minus rostered players"`) ||
		!strings.Contains(freeAgents.Content, `"week":1`) ||
		strings.Contains(freeAgents.Content, `"season_points"`) ||
		freeAgentSource.query.Week != 1 || freeAgentSource.query.Position != "RB" || freeAgentSource.query.Limit != 4 ||
		len(freeAgentOutput.FreeAgents) != 2 ||
		string(freeAgentOutput.FreeAgents[0]["rest_of_season_points"]) != "99.9" ||
		string(freeAgentOutput.FreeAgents[0]["rest_of_season_weeks"]) != "11" ||
		freeAgentOutput.FreeAgents[1]["rest_of_season_points"] != nil ||
		freeAgentOutput.FreeAgents[1]["rest_of_season_weeks"] != nil {
		t.Fatalf("get_free_agents result=%+v query=%+v", freeAgents, freeAgentSource.query)
	}
	matchup := runAssistantTool(ctx, tools, llm.ToolCall{
		ID: "matchup", Name: "get_my_matchup",
		Input: json.RawMessage(`{"league_season_id":"` + leagueID + `"}`),
	})
	if matchup.IsError || !containsJSONValue(matchup.Content, "Home Team") ||
		!containsJSONValue(matchup.Content, "Away Team") {
		t.Fatalf("get_my_matchup: %s", matchup.Content)
	}

	attention, err := dashboardService.GetOverviewAttention(ctx, &v1.GetOverviewAttentionRequest{})
	if err != nil || len(attention.GetAlerts()) == 0 {
		t.Fatalf("seeded alert: %v %v", attention, err)
	}
	actionService := &Service{Dashboard: dashboardService}
	dismissed, err := actionService.RunAssistantAction(ctx, &v1.RunAssistantActionRequest{
		Action: &v1.AssistantAction{
			Action: &v1.AssistantAction_DismissAlert{
				DismissAlert: &v1.DismissAlertAction{Fingerprint: attention.Alerts[0].Fingerprint},
			},
		},
	})
	if err != nil || dismissed.GetSummary() != "Dismissed the alert." {
		t.Fatalf("dismiss action: %v %v", dismissed, err)
	}
	remaining, err := dashboardService.GetOverviewAttention(ctx, &v1.GetOverviewAttentionRequest{})
	if err != nil || hasAlertFingerprint(remaining, attention.Alerts[0].Fingerprint) {
		t.Fatalf("dismissed alert remained visible: %v %v", remaining, err)
	}
}

func findJSONField(t *testing.T, content, field string) string {
	t.Helper()
	var value any
	if err := json.Unmarshal([]byte(content), &value); err != nil {
		t.Fatal(err)
	}
	if result := findNestedField(value, field); result != "" {
		return result
	}
	t.Fatalf("%s missing from %s", field, content)
	return ""
}

func findNestedField(value any, field string) string {
	switch value := value.(type) {
	case map[string]any:
		if found, ok := value[field].(string); ok {
			return found
		}
		for _, child := range value {
			if found := findNestedField(child, field); found != "" {
				return found
			}
		}
	case []any:
		for _, child := range value {
			if found := findNestedField(child, field); found != "" {
				return found
			}
		}
	}
	return ""
}

func containsJSONValue(content, expected string) bool {
	return strings.Contains(content, `"`+expected+`"`)
}
