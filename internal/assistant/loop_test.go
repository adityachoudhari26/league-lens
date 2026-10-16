package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/llm"
)

type scriptedChatProvider struct {
	responses []llm.Response
	requests  []llm.Request
}

func (p *scriptedChatProvider) ListModels(context.Context) ([]string, error) { return nil, nil }

func (p *scriptedChatProvider) Complete(_ context.Context, request llm.Request, onText func(string)) (llm.Response, error) {
	p.requests = append(p.requests, request)
	response := p.responses[len(p.requests)-1]
	if response.Text != "" {
		onText(response.Text)
	}
	return response, nil
}

func TestChatToolLoopFeedsResultsAndStreamsActivity(t *testing.T) {
	reader := &fakeAssistantDashboard{}
	provider := &scriptedChatProvider{responses: []llm.Response{
		{ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "list_leagues", Input: json.RawMessage(`{}`)}}},
		{Text: "I found your matchup."},
	}}
	var events []*v1.ChatResponse
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	loop := chatLoop{
		cancel: cancel, provider: provider, model: "fake", tools: buildReadOnlyTools(reader),
		send: func(event *v1.ChatResponse) error {
			events = append(events, event)
			return nil
		},
	}
	err := loop.run(ctx, nil, []llm.Message{{Role: llm.RoleUser, Text: "matchup"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 2 || len(provider.requests[0].Tools) != 10 ||
		provider.requests[0].System == "" || len(provider.requests[1].Messages) != 3 {
		t.Fatalf("provider requests = %+v", provider.requests)
	}
	result := provider.requests[1].Messages[2].ToolResults
	if len(result) != 1 || result[0].CallID != "call-1" || result[0].IsError {
		t.Fatalf("tool results = %+v", result)
	}
	if len(events) != 2 || events[0].GetTool().GetLabel() != "Looking up your leagues" ||
		events[1].GetText().GetText() != "I found your matchup." {
		t.Fatalf("streamed events = %+v", events)
	}
}

func TestChatToolLoopLimitsRoundsAndReturnsErrorResults(t *testing.T) {
	reader := &fakeAssistantDashboard{listLeaguesErr: errors.New("database detail")}
	calls := make([]llm.Response, 0, maxToolRounds+1)
	for index := 0; index < maxToolRounds; index++ {
		calls = append(calls, llm.Response{ToolCalls: []llm.ToolCall{{
			ID: "call", Name: "list_leagues", Input: json.RawMessage(`{}`),
		}}})
	}
	calls = append(calls, llm.Response{Text: "Answer from the available data."})
	provider := &scriptedChatProvider{responses: calls}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	loop := chatLoop{
		cancel: cancel, provider: provider, model: "fake", tools: buildReadOnlyTools(reader),
		send: func(*v1.ChatResponse) error { return nil },
	}
	err := loop.run(ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != maxToolRounds+1 {
		t.Fatalf("completion rounds = %d", len(provider.requests))
	}
	if len(provider.requests[maxToolRounds].Tools) != 0 ||
		!strings.Contains(provider.requests[maxToolRounds].System, "Tool limit reached. Answer with the data you already have.") {
		t.Fatalf("final request did not enforce the tool limit: %+v", provider.requests[maxToolRounds])
	}
	result := provider.requests[1].Messages[len(provider.requests[1].Messages)-1].ToolResults
	if len(result) != 1 || !result[0].IsError || result[0].Content != "couldn't load the requested data" {
		t.Fatalf("safe error result = %+v", result)
	}
}
