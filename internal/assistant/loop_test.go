package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/llm"
)

type scriptedChatProvider struct {
	responses []llm.Response
	requests  []llm.Request
}

type countingLeaguesDashboard struct {
	fakeAssistantDashboard
	listLeaguesCalls int
}

func (d *countingLeaguesDashboard) ListLeagues(
	ctx context.Context,
	request *v1.ListLeaguesRequest,
) (*v1.ListLeaguesResponse, error) {
	d.listLeaguesCalls++
	return d.fakeAssistantDashboard.ListLeagues(ctx, request)
}

func (p *scriptedChatProvider) ListModels(context.Context) ([]string, error) { return nil, nil }

func (p *scriptedChatProvider) Complete(
	_ context.Context,
	request llm.Request,
	onText func(string),
) (llm.Response, error) {
	p.requests = append(p.requests, request)
	response := p.responses[len(p.requests)-1]
	if response.Text != "" {
		onText(response.Text)
	}
	return response, nil
}

func TestChatLoopReturnsToolRoundMessages(t *testing.T) {
	reader := &fakeAssistantDashboard{}
	call := llm.ToolCall{ID: "call-1", Name: "list_leagues", Input: json.RawMessage(`{}`)}
	provider := &scriptedChatProvider{responses: []llm.Response{
		{ToolCalls: []llm.ToolCall{call}},
		{Text: "I found your matchup."},
	}}
	var events []*v1.ChatResponse
	turn := runChatLoopTest(t, provider, buildReadOnlyTools(reader), nil,
		llm.Message{Role: llm.RoleUser, Text: "matchup"}, &events)

	if len(provider.requests) != 2 || len(provider.requests[0].Tools) != 10 ||
		provider.requests[0].System == "" || len(provider.requests[1].Messages) != 3 {
		t.Fatalf("provider requests = %+v", provider.requests)
	}
	if len(events) != 2 || events[0].GetTool().GetLabel() != "Looking up your leagues" ||
		events[1].GetText().GetText() != "I found your matchup." {
		t.Fatalf("streamed events = %+v", events)
	}
	assertLoopTestTurn(t, turn, []llm.Message{
		{Role: llm.RoleUser, Text: "matchup"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}},
		{Role: llm.RoleUser, ToolResults: provider.requests[1].Messages[2].ToolResults},
		{Role: llm.RoleAssistant, Text: "I found your matchup."},
	})
	if len(turn[2].ToolResults) != 1 || turn[2].ToolResults[0].CallID != call.ID || turn[2].ToolResults[0].IsError {
		t.Fatalf("tool results = %+v", turn[2].ToolResults)
	}
}

func TestChatLoopDoesNotSaveUnrunToolAtLimit(t *testing.T) {
	reader := &countingLeaguesDashboard{}
	responses := make([]llm.Response, 0, maxToolRounds+1)
	for round := 0; round < maxToolRounds; round++ {
		responses = append(responses, llm.Response{ToolCalls: []llm.ToolCall{{
			ID: fmt.Sprintf("call-%d", round), Name: "list_leagues", Input: json.RawMessage(`{}`),
		}}})
	}
	responses = append(responses, llm.Response{ToolCalls: []llm.ToolCall{{
		ID: "unrun-call", Name: "list_leagues", Input: json.RawMessage(`{}`),
	}}})
	provider := &scriptedChatProvider{responses: responses}
	tools := buildAssistantTools(reader)
	var events []*v1.ChatResponse
	turn := runChatLoopTest(t, provider, tools, nil, llm.Message{Role: llm.RoleUser, Text: "look it up"}, &events)

	if len(provider.requests) != maxToolRounds+1 || reader.listLeaguesCalls != maxToolRounds {
		t.Fatalf("rounds=%d list_leagues calls=%d", len(provider.requests), reader.listLeaguesCalls)
	}
	if len(turn) != 2*maxToolRounds+2 {
		t.Fatalf("tool-limit turn messages = %d, want %d", len(turn), 2*maxToolRounds+2)
	}
	finalRequest := provider.requests[maxToolRounds]
	if len(finalRequest.Tools) != len(tools) {
		t.Fatalf("final request tools = %d, want %d", len(finalRequest.Tools), len(tools))
	}
	assertLoopTestNoToolCall(t, turn, "unrun-call")
	if last := turn[len(turn)-1]; last.Role != llm.RoleAssistant || last.Text != toolLimitFallback ||
		len(last.ToolCalls) != 0 {
		t.Fatalf("tool-limit message = %+v", last)
	}
	if events[len(events)-1].GetText().GetText() != toolLimitFallback {
		t.Fatalf("final streamed event = %+v", events[len(events)-1])
	}
}

func TestChatLoopStoresResultsForEveryProposedStepCall(t *testing.T) {
	calls := []llm.ToolCall{
		{
			ID: "invalid-action", Name: "dismiss_alert",
			Input: json.RawMessage(`{"fingerprint":"missing-alert","summary":"Dismiss alert"}`),
		},
		{ID: "not-run", Name: "get_alerts", Input: json.RawMessage(`{}`)},
		{ID: "proposed-action", Name: "sync_leagues", Input: json.RawMessage(`{"only_stale":false}`)},
	}
	provider := &scriptedChatProvider{responses: []llm.Response{{ToolCalls: calls}}}
	var events []*v1.ChatResponse
	turn := runChatLoopTest(t, provider, buildAssistantTools(&fakeAssistantDashboard{}), nil,
		llm.Message{Role: llm.RoleUser, Text: "refresh leagues"}, &events)

	if len(events) != 1 || events[0].GetProposal().GetSummary() != "Refresh all leagues" {
		t.Fatalf("proposal events = %+v", events)
	}
	if len(turn) != 3 || !reflect.DeepEqual(turn[1].ToolCalls, calls) || len(turn[2].ToolResults) != len(calls) {
		t.Fatalf("proposal turn = %+v", turn)
	}
	results := turn[2].ToolResults
	if results[0].CallID != "invalid-action" || !results[0].IsError || results[0].Content == "" {
		t.Fatalf("invalid action result = %+v", results[0])
	}
	if results[1].CallID != "not-run" || !results[1].IsError ||
		results[1].Content != "Not run because an action was proposed in the same step." {
		t.Fatalf("not-run result = %+v", results[1])
	}
	if results[2].CallID != "proposed-action" || results[2].IsError ||
		results[2].Content != "Shown to the user for confirmation. It runs only if they confirm." {
		t.Fatalf("proposed result = %+v", results[2])
	}
}

func TestChatLoopOmitsEmptyFinalAssistantMessage(t *testing.T) {
	provider := &scriptedChatProvider{responses: []llm.Response{{}}}
	var events []*v1.ChatResponse
	turn := runChatLoopTest(t, provider, nil, nil, llm.Message{Role: llm.RoleUser, Text: "hello"}, &events)

	assertLoopTestTurn(t, turn, []llm.Message{{Role: llm.RoleUser, Text: "hello"}})
	if len(events) != 0 {
		t.Fatalf("empty final response events = %+v", events)
	}
}

func TestChatLoopReturnsInvalidActionResultToModel(t *testing.T) {
	call := llm.ToolCall{
		ID: "dismiss", Name: "dismiss_alert",
		Input: json.RawMessage(`{"fingerprint":"stale-alert","summary":"Dismiss alert"}`),
	}
	provider := &scriptedChatProvider{responses: []llm.Response{
		{ToolCalls: []llm.ToolCall{call}},
		{Text: "I couldn't find that alert."},
	}}
	var events []*v1.ChatResponse
	turn := runChatLoopTest(t, provider, buildAssistantTools(&fakeAssistantDashboard{}), nil,
		llm.Message{Role: llm.RoleUser, Text: "dismiss that alert"}, &events)

	if len(provider.requests) != 2 || len(events) != 1 || len(turn) != 4 ||
		events[0].GetProposal() != nil || events[0].GetText().GetText() != "I couldn't find that alert." {
		t.Fatalf("invalid action flow: requests=%+v events=%+v", provider.requests, events)
	}
	result := turn[2].ToolResults
	if len(result) != 1 || !result[0].IsError || result[0].Content != "requested data not found" {
		t.Fatalf("invalid action tool result = %+v", result)
	}
}

func runChatLoopTest(
	t *testing.T,
	provider *scriptedChatProvider,
	tools []assistantTool,
	history []llm.Message,
	userMessage llm.Message,
	events *[]*v1.ChatResponse,
) []llm.Message {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	loop := chatLoop{
		cancel: cancel, provider: provider, model: "fake", tools: tools,
		send: func(event *v1.ChatResponse) error {
			*events = append(*events, event)
			return nil
		},
	}
	turn, err := loop.run(ctx, nil, history, userMessage)
	if err != nil {
		t.Fatal(err)
	}
	return turn
}

func assertLoopTestTurn(t *testing.T, got, want []llm.Message) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("turn = %+v, want %+v", got, want)
	}
}

func assertLoopTestNoToolCall(t *testing.T, turn []llm.Message, callID string) {
	t.Helper()
	for _, message := range turn {
		if loopTestHasToolCall(message.ToolCalls, callID) {
			t.Fatalf("unrun tool call %q was saved in turn: %+v", callID, turn)
		}
	}
}

func loopTestHasToolCall(calls []llm.ToolCall, callID string) bool {
	for _, call := range calls {
		if call.ID == callID {
			return true
		}
	}
	return false
}
