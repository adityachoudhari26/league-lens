package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/llm"
)

type fakeAssistantDashboard struct {
	listLeaguesRequest      *v1.ListLeaguesRequest
	getLeagueRequest        *v1.GetLeagueRequest
	getTeamRosterRequest    *v1.GetTeamRosterRequest
	listMatchupsRequest     *v1.ListMatchupsRequest
	getMatchupDetailRequest *v1.GetMatchupDetailRequest
	getWinChanceRequest     *v1.GetMatchupWinChanceRequest
	getAlertsRequest        *v1.GetOverviewAttentionRequest
	getPlayerNewsRequest    *v1.GetOverviewPlayerUpdatesRequest
	getPlayerCardRequest    *v1.GetPlayerCardRequest
	getLiveGamesRequest     *v1.GetLiveGamesRequest
	dismissAlertRequest     *v1.DismissLineupAlertRequest
	restoreAlertRequest     *v1.RestoreLineupAlertRequest
	alerts                  []*v1.LineupAlert
	getAlertsErr            error
	dismissAlertErr         error
	restoreAlertErr         error
	listLeaguesErr          error
	availableMatchupPeriods []int32
}

func (d *fakeAssistantDashboard) ListLeagues(_ context.Context, request *v1.ListLeaguesRequest) (*v1.ListLeaguesResponse, error) {
	d.listLeaguesRequest = request
	return &v1.ListLeaguesResponse{}, d.listLeaguesErr
}

func (d *fakeAssistantDashboard) GetLeague(_ context.Context, request *v1.GetLeagueRequest) (*v1.GetLeagueResponse, error) {
	d.getLeagueRequest = request
	return &v1.GetLeagueResponse{AvailableMatchupPeriods: d.availableMatchupPeriods}, nil
}

func (d *fakeAssistantDashboard) GetTeamRoster(_ context.Context, request *v1.GetTeamRosterRequest) (*v1.GetTeamRosterResponse, error) {
	d.getTeamRosterRequest = request
	return &v1.GetTeamRosterResponse{}, nil
}

func (d *fakeAssistantDashboard) ListMatchups(_ context.Context, request *v1.ListMatchupsRequest) (*v1.ListMatchupsResponse, error) {
	d.listMatchupsRequest = request
	return &v1.ListMatchupsResponse{}, nil
}

func (d *fakeAssistantDashboard) GetMatchupDetail(_ context.Context, request *v1.GetMatchupDetailRequest) (*v1.GetMatchupDetailResponse, error) {
	d.getMatchupDetailRequest = request
	return &v1.GetMatchupDetailResponse{}, nil
}

func (d *fakeAssistantDashboard) GetMatchupWinChance(_ context.Context, request *v1.GetMatchupWinChanceRequest) (*v1.GetMatchupWinChanceResponse, error) {
	d.getWinChanceRequest = request
	return &v1.GetMatchupWinChanceResponse{}, nil
}

func (d *fakeAssistantDashboard) GetOverviewAttention(_ context.Context, request *v1.GetOverviewAttentionRequest) (*v1.GetOverviewAttentionResponse, error) {
	d.getAlertsRequest = request
	return &v1.GetOverviewAttentionResponse{Alerts: d.alerts}, d.getAlertsErr
}

func (d *fakeAssistantDashboard) DismissLineupAlert(_ context.Context, request *v1.DismissLineupAlertRequest) (*v1.DismissLineupAlertResponse, error) {
	d.dismissAlertRequest = request
	return &v1.DismissLineupAlertResponse{}, d.dismissAlertErr
}

func (d *fakeAssistantDashboard) RestoreLineupAlert(_ context.Context, request *v1.RestoreLineupAlertRequest) (*v1.RestoreLineupAlertResponse, error) {
	d.restoreAlertRequest = request
	return &v1.RestoreLineupAlertResponse{}, d.restoreAlertErr
}

func (d *fakeAssistantDashboard) GetOverviewPlayerUpdates(
	_ context.Context,
	request *v1.GetOverviewPlayerUpdatesRequest,
) (*v1.GetOverviewPlayerUpdatesResponse, error) {
	d.getPlayerNewsRequest = request
	return &v1.GetOverviewPlayerUpdatesResponse{}, nil
}

func (d *fakeAssistantDashboard) GetPlayerCard(_ context.Context, request *v1.GetPlayerCardRequest) (*v1.GetPlayerCardResponse, error) {
	d.getPlayerCardRequest = request
	return &v1.GetPlayerCardResponse{}, nil
}

func (d *fakeAssistantDashboard) GetLiveGames(_ context.Context, request *v1.GetLiveGamesRequest) (*v1.GetLiveGamesResponse, error) {
	d.getLiveGamesRequest = request
	return &v1.GetLiveGamesResponse{}, nil
}

func TestAssistantToolsMapInputsAndLabels(t *testing.T) {
	reader := &fakeAssistantDashboard{availableMatchupPeriods: []int32{2, 7, 4}}
	tools := buildReadOnlyTools(reader)
	for _, name := range []string{
		"list_leagues", "get_league", "get_team_roster", "list_matchups", "get_my_matchup",
		"get_win_chance", "get_alerts", "get_player_news", "get_player", "get_live_games",
	} {
		if findAssistantTool(tools, name) == nil {
			t.Fatalf("missing tool %q", name)
		}
	}
	calls := []llm.ToolCall{
		{ID: "leagues", Name: "list_leagues", Input: json.RawMessage(`{}`)},
		{ID: "league", Name: "get_league", Input: json.RawMessage(`{"league_season_id":"league-id"}`)},
		{ID: "roster", Name: "get_team_roster", Input: json.RawMessage(`{"team_id":"team-id","week":3}`)},
		{ID: "matches", Name: "list_matchups", Input: json.RawMessage(`{"league_season_id":"league-id","week":7}`)},
		{ID: "mine", Name: "get_my_matchup", Input: json.RawMessage(`{"league_season_id":"league-id","week":5}`)},
		{ID: "chance", Name: "get_win_chance", Input: json.RawMessage(`{"matchup_id":"match-id"}`)},
		{ID: "alerts", Name: "get_alerts", Input: json.RawMessage(`{}`)},
		{ID: "news", Name: "get_player_news", Input: json.RawMessage(`{}`)},
		{ID: "player", Name: "get_player", Input: json.RawMessage(`{"player_id":"player-id"}`)},
		{ID: "games", Name: "get_live_games", Input: json.RawMessage(`{}`)},
	}
	for _, call := range calls {
		if result := runAssistantTool(context.Background(), tools, call); result.IsError {
			t.Fatalf("%s result = %+v", call.Name, result)
		}
	}
	if reader.listLeaguesRequest.PageSize != 100 ||
		reader.getLeagueRequest.LeagueSeasonId != "league-id" ||
		reader.getTeamRosterRequest.TeamId != "team-id" || reader.getTeamRosterRequest.GetScoringPeriod() != 3 ||
		reader.listMatchupsRequest.MatchupPeriod != 7 || reader.getMatchupDetailRequest.GetMatchupPeriod() != 5 ||
		reader.getWinChanceRequest.MatchupId != "match-id" || reader.getAlertsRequest == nil ||
		reader.getPlayerNewsRequest == nil || reader.getPlayerCardRequest.PlayerId != "player-id" ||
		reader.getLiveGamesRequest == nil {
		t.Fatalf("tool requests were not mapped: %+v", reader)
	}
	label := toolLabel(tools, "get_my_matchup", json.RawMessage(`{"league_season_id":"league-id","week":5}`))
	if label != "Reading your matchup (week 5)" {
		t.Fatalf("matchup label = %q", label)
	}
}

func TestListMatchupsDefaultsToLatestAvailablePeriod(t *testing.T) {
	reader := &fakeAssistantDashboard{availableMatchupPeriods: []int32{2, 7, 4}}
	result := runAssistantTool(context.Background(), buildReadOnlyTools(reader), llm.ToolCall{
		ID: "matches", Name: "list_matchups", Input: json.RawMessage(`{"league_season_id":"league-id"}`),
	})
	if result.IsError || reader.getLeagueRequest.LeagueSeasonId != "league-id" || reader.listMatchupsRequest.MatchupPeriod != 7 {
		t.Fatalf("default period result = %+v, reader = %+v", result, reader)
	}
}

func TestAssistantToolErrorsAreSafe(t *testing.T) {
	reader := &fakeAssistantDashboard{listLeaguesErr: connect.NewError(connect.CodeNotFound, "database internals")}
	tools := buildReadOnlyTools(reader)
	for _, call := range []llm.ToolCall{
		{ID: "bad-json", Name: "get_league", Input: json.RawMessage(`{"league_season_id":1}`)},
		{ID: "missing", Name: "list_leagues", Input: json.RawMessage(`{}`)},
		{ID: "unknown", Name: "private_tool", Input: json.RawMessage(`{}`)},
	} {
		result := runAssistantTool(context.Background(), tools, call)
		if !result.IsError || strings.Contains(result.Content, "internals") {
			t.Fatalf("%s error result = %+v", call.Name, result)
		}
	}
	result := runAssistantTool(context.Background(), tools, llm.ToolCall{
		ID: "extra", Name: "get_league", Input: json.RawMessage(`{"league_season_id":"id","extra":true}`),
	})
	if !result.IsError || result.Content != "invalid tool input" {
		t.Fatalf("unknown input property result = %+v", result)
	}
}

func TestAssistantToolSchemaAndResultLimits(t *testing.T) {
	tools := buildReadOnlyTools(&fakeAssistantDashboard{})
	for _, tool := range tools {
		var schema map[string]any
		if err := json.Unmarshal(tool.definition.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		if schema["additionalProperties"] != false {
			t.Errorf("%s allows additional properties", tool.definition.Name)
		}
	}
	content := truncateToolResult(strings.Repeat("a", maxToolResultBytes+1))
	if len(content) != maxToolResultBytes || !strings.HasSuffix(content, truncatedToolSuffix) {
		t.Fatalf("truncated content length/suffix = %d / %q", len(content), content[len(content)-len(truncatedToolSuffix):])
	}
	content = truncateToolResult(strings.Repeat("é", maxToolResultBytes))
	if !json.Valid([]byte(fmt.Sprintf("%q", content))) || len(content) > maxToolResultBytes {
		t.Fatalf("truncated unicode content is invalid: %d bytes", len(content))
	}
}

func TestFreeAgentToolSchemaLabelAndInputValidation(t *testing.T) {
	tool := freeAgentTools(&fakeAssistantDashboard{}, &Service{})[0]
	if tool.definition.Name != "get_free_agents" ||
		toolLabel([]assistantTool{tool}, "get_free_agents", json.RawMessage(`{}`)) != "Checking free agents" {
		t.Fatalf("free-agent tool = %+v", tool)
	}
	var schema map[string]any
	if err := json.Unmarshal(tool.definition.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	if schema["required"].([]any)[0] != "league_season_id" ||
		properties["position"].(map[string]any)["type"] != "string" ||
		properties["limit"].(map[string]any)["default"] != float64(15) {
		t.Fatalf("free-agent schema = %+v", schema)
	}
	for _, input := range []string{
		`{"league_season_id":"id","position":"QB","limit":26}`,
		`{"league_season_id":"id","position":"FLEX"}`,
		`{"position":"QB"}`,
	} {
		result := runAssistantTool(context.Background(), []assistantTool{tool}, llm.ToolCall{
			ID: "invalid", Name: "get_free_agents", Input: json.RawMessage(input),
		})
		if !result.IsError || result.Content != "invalid tool input" {
			t.Fatalf("input %s result = %+v", input, result)
		}
	}
}

func TestFreeAgentProviderErrorsDoNotExposeDetails(t *testing.T) {
	tests := []struct {
		provider fantasy.ProviderID
		want     string
	}{
		{provider: fantasy.ProviderESPN, want: "couldn't load free agents from ESPN"},
		{provider: fantasy.ProviderSleeper, want: "couldn't load free agents from Sleeper"},
	}
	for _, test := range tests {
		got := toolErrorMessage(providerFreeAgentError(test.provider, errors.New("secret provider response")))
		if got != test.want {
			t.Errorf("%s error = %q, want %q", test.provider, got, test.want)
		}
	}
}

func TestTrimWinChanceResponseKeepsStartAndLatestSamples(t *testing.T) {
	response := &v1.GetMatchupWinChanceResponse{WinChance: &v1.MatchupWinChance{}}
	for index := 0; index < 20; index++ {
		response.WinChance.Samples = append(response.WinChance.Samples, &v1.WinChanceSample{TeamScore: float64(index)})
	}
	trimmed := trimWinChanceResponse(response)
	if len(trimmed.WinChance.Samples) != 12 || trimmed.WinChance.Samples[0].TeamScore != 0 ||
		trimmed.WinChance.Samples[1].TeamScore != 9 || trimmed.WinChance.Samples[11].TeamScore != 19 {
		t.Fatalf("trimmed samples = %+v", trimmed.WinChance.Samples)
	}
}

func findAssistantTool(tools []assistantTool, name string) *assistantTool {
	for index := range tools {
		if tools[index].definition.Name == name {
			return &tools[index]
		}
	}
	return nil
}

var _ assistantDashboard = (*fakeAssistantDashboard)(nil)
