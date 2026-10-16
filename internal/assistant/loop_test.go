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
	reader := &countingLeaguesDashboard{
		fakeAssistantDashboard: fakeAssistantDashboard{listLeaguesErr: errors.New("database detail")},
	}
	calls := make([]llm.Response, 0, maxToolRounds+1)
	for index := 0; index < maxToolRounds; index++ {
		calls = append(calls, llm.Response{ToolCalls: []llm.ToolCall{{
			ID: "call", Name: "list_leagues", Input: json.RawMessage(`{}`),
		}}})
	}
	calls = append(calls, llm.Response{ToolCalls: []llm.ToolCall{{
		ID: "final-call", Name: "list_leagues", Input: json.RawMessage(`{}`),
	}}})
	provider := &scriptedChatProvider{responses: calls}
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
	err := loop.run(ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != maxToolRounds+1 {
		t.Fatalf("completion rounds = %d", len(provider.requests))
	}
	if len(provider.requests[maxToolRounds].Tools) != 10 ||
		!strings.Contains(provider.requests[maxToolRounds].System, "Tool limit reached. Answer with the data you already have.") {
		t.Fatalf("final request did not enforce the tool limit: %+v", provider.requests[maxToolRounds])
	}
	if reader.listLeaguesCalls != maxToolRounds {
		t.Fatalf("list_leagues calls = %d, want %d", reader.listLeaguesCalls, maxToolRounds)
	}
	if len(events) != maxToolRounds+1 {
		t.Fatalf("tool-limit events = %+v", events)
	}
	if events[len(events)-1].GetText().GetText() != "I couldn't finish looking that up. Try a narrower question." {
		t.Fatalf("final tool-limit event = %+v", events[len(events)-1])
	}
	result := provider.requests[1].Messages[len(provider.requests[1].Messages)-1].ToolResults
	if len(result) != 1 || !result[0].IsError || result[0].Content != "couldn't load the requested data" {
		t.Fatalf("safe error result = %+v", result)
	}
}
