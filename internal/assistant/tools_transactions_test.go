package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/leaguesync"
	"github.com/adityachoudhari26/league-lens/internal/llm"
)

type transactionToolSourceFactory struct{}

func (transactionToolSourceFactory) SourceFor(leaguesync.Target) (fantasy.Provider, error) {
	return nil, nil
}

func TestGetTransactionsToolRegistrationSchemaAndValidation(t *testing.T) {
	service := &Service{
		Dashboard:       &fakeAssistantDashboard{},
		ProviderSources: transactionToolSourceFactory{},
	}
	tools := service.chatTools()
	freeAgentIndex, transactionIndex := -1, -1
	for index, tool := range tools {
		switch tool.definition.Name {
		case "get_free_agents":
			freeAgentIndex = index
		case "get_transactions":
			transactionIndex = index
		}
	}
	if freeAgentIndex < 0 || transactionIndex != freeAgentIndex+1 {
		t.Fatalf("get_transactions was not registered after free agents: %v, %v", freeAgentIndex, transactionIndex)
	}
	if label := toolLabel(tools, "get_transactions", json.RawMessage(`{}`)); label != "Checking league activity" {
		t.Fatalf("transaction label = %q", label)
	}
	tool := findAssistantToolByName(tools, "get_transactions")
	var schema map[string]any
	if err := json.Unmarshal(tool.definition.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	if schema["additionalProperties"] != false ||
		schema["required"].([]any)[0] != "league_season_id" ||
		properties["league_season_id"].(map[string]any)["format"] != "uuid" ||
		properties["weeks_back"].(map[string]any)["default"] != float64(transactionDefaultWeeksBack) ||
		properties["limit"].(map[string]any)["default"] != float64(transactionDefaultLimit) {
		t.Fatalf("transaction schema = %+v", schema)
	}

	validID := "01234567-89ab-cdef-0123-456789abcdef"
	defaults, err := validateTransactionInput(transactionToolInput{LeagueSeasonID: validID})
	if err != nil || defaults.weeksBack != transactionDefaultWeeksBack || defaults.limit != transactionDefaultLimit {
		t.Fatalf("transaction defaults = %+v, err=%v", defaults, err)
	}
	for _, input := range []string{
		`{}`,
		`{"league_season_id":"bad"}`,
		`{"league_season_id":"` + validID + `","weeks_back":0}`,
		`{"league_season_id":"` + validID + `","weeks_back":5}`,
		`{"league_season_id":"` + validID + `","limit":0}`,
		`{"league_season_id":"` + validID + `","limit":51}`,
		`{"league_season_id":"` + validID + `","extra":true}`,
		`{"league_season_id":"` + validID + `"} {}`,
	} {
		result := runAssistantTool(context.Background(), tools, llm.ToolCall{
			ID: "invalid", Name: "get_transactions", Input: json.RawMessage(input),
		})
		if !result.IsError || result.Content != "invalid tool input" {
			t.Errorf("input %s result = %+v", input, result)
		}
	}
}

func TestTransactionProviderErrorsHaveSafeMessages(t *testing.T) {
	for _, testCase := range []struct {
		err  error
		want string
	}{
		{err: errTransactionsESPN, want: "couldn't load league activity from ESPN"},
		{err: errTransactionsSleeper, want: "couldn't load league activity from Sleeper"},
		{err: errTransactionsUnavailable, want: "couldn't load league activity"},
	} {
		if got := toolErrorMessage(testCase.err); got != testCase.want {
			t.Errorf("toolErrorMessage(%v) = %q, want %q", testCase.err, got, testCase.want)
		}
	}
	if got := errTransactionsError(fantasy.ProviderESPN); !errors.Is(got, errTransactionsESPN) {
		t.Fatalf("ESPN error mapping = %v", got)
	}
	if got := errTransactionsError(fantasy.ProviderSleeper); !errors.Is(got, errTransactionsSleeper) {
		t.Fatalf("Sleeper error mapping = %v", got)
	}
}

func TestTransactionWeeksUseTrailingRange(t *testing.T) {
	for _, testCase := range []struct {
		current, back int
		want          []int
	}{
		{current: 6, back: 3, want: []int{4, 5, 6}},
		{current: 2, back: 4, want: []int{1, 2}},
	} {
		got := transactionWeeks(testCase.current, testCase.back)
		if len(got) != len(testCase.want) {
			t.Fatalf("transaction weeks(%d,%d) = %v", testCase.current, testCase.back, got)
		}
		for index := range got {
			if got[index] != testCase.want[index] {
				t.Fatalf("transaction weeks(%d,%d) = %v, want %v", testCase.current, testCase.back, got, testCase.want)
			}
		}
	}
}
