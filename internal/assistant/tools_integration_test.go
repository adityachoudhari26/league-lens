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
	"github.com/adityachoudhari26/league-lens/internal/importer"
	"github.com/adityachoudhari26/league-lens/internal/llm"
)

type assistantLeagueSource struct{ data fantasy.LeagueData }

func (s assistantLeagueSource) DescribeLeague(context.Context, string) (fantasy.League, error) {
	return s.data.League, nil
}

func (s assistantLeagueSource) FetchLeague(context.Context, fantasy.League) (fantasy.LeagueData, error) {
	return s.data, nil
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
		Teams: []fantasy.Team{{ExternalID: "home", Name: "Home Team"}, {ExternalID: "away", Name: "Away Team"}},
		Rosters: []fantasy.RosterSnapshot{
			{TeamID: "home", ScoringPeriod: 1, Origin: fantasy.SnapshotObserved, Coverage: fantasy.CoverageFull, ObservedAt: now},
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
	tools := buildReadOnlyTools(dashboard.New(db, "tools@example.test", nil, nil, nil))
	listed := runAssistantTool(ctx, tools, llm.ToolCall{
		ID: "leagues", Name: "list_leagues", Input: json.RawMessage(`{}`),
	})
	if listed.IsError {
		t.Fatalf("list_leagues: %s", listed.Content)
	}
	leagueID := findJSONField(t, listed.Content, "id")
	matchup := runAssistantTool(ctx, tools, llm.ToolCall{
		ID: "matchup", Name: "get_my_matchup",
		Input: json.RawMessage(`{"league_season_id":"` + leagueID + `"}`),
	})
	if matchup.IsError || !containsJSONValue(matchup.Content, "Home Team") ||
		!containsJSONValue(matchup.Content, "Away Team") {
		t.Fatalf("get_my_matchup: %s", matchup.Content)
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
