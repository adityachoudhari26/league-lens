package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestNewRoutesOpenAIResponsesOnlyForOpenAIHost(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{name: "OpenAI", baseURL: "https://api.openai.com/v1", want: "responses"},
		{name: "case insensitive", baseURL: "https://API.OpenAI.com/v1", want: "responses"},
		{name: "compatible server", baseURL: "http://127.0.0.1:1234/v1", want: "chat completions"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider, err := New(Config{Provider: ProviderOpenAICompatible, BaseURL: test.baseURL}, nil)
			if err != nil {
				t.Fatal(err)
			}
			switch test.want {
			case "responses":
				if _, ok := provider.(*openAIResponsesProvider); !ok {
					t.Fatalf("provider = %T, want *openAIResponsesProvider", provider)
				}
			case "chat completions":
				if _, ok := provider.(*openAIProvider); !ok {
					t.Fatalf("provider = %T, want *openAIProvider", provider)
				}
			}
		})
	}
}

func TestOpenAIResponsesRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertOpenAIResponsesRequest(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(fixture(t, "openai_responses_text.sse"))
	}))
	defer server.Close()

	provider := openAIResponsesTestProvider(server, "sk-test")
	_, err := provider.Complete(context.Background(), Request{
		Model: "gpt-6.1-sol", System: "Be brief", MaxTokens: 200,
		Messages: []Message{
			{Role: RoleUser, Text: "Find my league"},
			{
				Role: RoleAssistant, Text: "I'll look that up.",
				ToolCalls: []ToolCall{{
					ID: "call_1", Name: "list_leagues", Input: json.RawMessage(`{"league":"A"}`),
				}},
			},
			{Role: RoleUser, ToolResults: []ToolResult{{CallID: "call_1", Content: "Found the league"}}},
		},
		Tools: []Tool{{
			Name: "list_leagues", Description: "List leagues",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func assertOpenAIResponsesRequest(t *testing.T, r *http.Request) {
	t.Helper()
	if r.URL.Path != "/v1/responses" {
		t.Errorf("path = %q", r.URL.Path)
	}
	if r.Header.Get("Authorization") != "Bearer sk-test" ||
		r.Header.Get("content-type") != "application/json" {
		t.Errorf("unexpected headers: %v", r.Header)
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Error(err)
	}
	if body["model"] != "gpt-6.1-sol" || body["instructions"] != "Be brief" ||
		body["max_output_tokens"] != float64(200) || body["stream"] != true || body["store"] != false {
		t.Errorf("request body = %#v", body)
	}
	if _, exists := body["max_tokens"]; exists {
		t.Error("Responses request included max_tokens")
	}
	assertOpenAIResponsesInput(t, body["input"])
	assertOpenAIResponsesTools(t, body["tools"])
}

func assertOpenAIResponsesInput(t *testing.T, value any) {
	t.Helper()
	input, ok := value.([]any)
	if !ok || len(input) != 4 {
		t.Fatalf("input = %#v", value)
	}
	user := input[0].(map[string]any)
	if user["role"] != "user" || user["content"] != "Find my league" {
		t.Errorf("user input = %#v", user)
	}
	assistant := input[1].(map[string]any)
	if assistant["role"] != "assistant" || assistant["content"] != "I'll look that up." {
		t.Errorf("assistant input = %#v", assistant)
	}
	call := input[2].(map[string]any)
	if call["type"] != "function_call" || call["call_id"] != "call_1" ||
		call["name"] != "list_leagues" || call["arguments"] != `{"league":"A"}` {
		t.Errorf("function call input = %#v", call)
	}
	result := input[3].(map[string]any)
	if result["type"] != "function_call_output" || result["call_id"] != "call_1" ||
		result["output"] != "Found the league" {
		t.Errorf("function output input = %#v", result)
	}
}

func assertOpenAIResponsesTools(t *testing.T, value any) {
	t.Helper()
	tools, ok := value.([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v", value)
	}
	tool := tools[0].(map[string]any)
	if tool["type"] != "function" || tool["name"] != "list_leagues" ||
		tool["description"] != "List leagues" {
		t.Errorf("tool = %#v", tool)
	}
	if _, exists := tool["function"]; exists {
		t.Errorf("tool uses chat-completions nesting: %#v", tool)
	}
	parameters, ok := tool["parameters"].(map[string]any)
	if !ok || parameters["type"] != "object" {
		t.Errorf("tool parameters = %#v", tool["parameters"])
	}
}

func TestOpenAIResponsesToolFixture(t *testing.T) {
	server := openAIResponsesFixtureServer(t, "openai_responses_tool.sse")
	provider := openAIResponsesTestProvider(server, "")
	response, err := provider.Complete(context.Background(), Request{Model: "gpt-6.1-sol"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []ToolCall{{
		ID: "call_es8qC6AtWI9CEi11tJEGb0Or", Name: "list_leagues", Input: json.RawMessage(`{}`),
	}}
	if response.Text != "" || response.StopReason != "completed" || !reflect.DeepEqual(response.ToolCalls, want) {
		t.Fatalf("response = %+v", response)
	}
}

func TestOpenAIResponsesTextFixture(t *testing.T) {
	server := openAIResponsesFixtureServer(t, "openai_responses_text.sse")
	provider := openAIResponsesTestProvider(server, "")
	var deltas []string
	response, err := provider.Complete(context.Background(), Request{Model: "gpt-6.1-sol"}, func(delta string) {
		deltas = append(deltas, delta)
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "You’re" || response.StopReason != "completed" ||
		!reflect.DeepEqual(deltas, []string{"You", "’re"}) {
		t.Fatalf("response = %+v, deltas = %#v", response, deltas)
	}
}

func TestOpenAIResponsesIncomplete(t *testing.T) {
	server := openAIResponsesInlineServer(t, `event: response.output_text.delta
data: {"type":"response.output_text.delta","delta":"Partial"}

event: response.incomplete
data: {"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}

`)
	provider := openAIResponsesTestProvider(server, "")
	response, err := provider.Complete(context.Background(), Request{Model: "gpt-6.1-sol"}, nil)
	if err != nil || response.Text != "Partial" || response.StopReason != "max_output_tokens" {
		t.Fatalf("response = %+v, %v", response, err)
	}
}

func TestOpenAIResponsesFailed(t *testing.T) {
	server := openAIResponsesInlineServer(t, `event: response.failed
data: {"type":"response.failed","response":{"status":"failed"}}

`)
	provider := openAIResponsesTestProvider(server, "")
	_, err := provider.Complete(context.Background(), Request{Model: "gpt-6.1-sol"}, nil)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func TestOpenAIResponsesUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	provider := openAIResponsesTestProvider(server, "sk-test")
	_, err := provider.Complete(context.Background(), Request{Model: "gpt-6.1-sol"}, nil)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("error = %v", err)
	}
}

func TestDecodeOpenAIToolCall(t *testing.T) {
	call, err := decodeOpenAIToolCall("call_1", "list_leagues", "")
	if err != nil || string(call.Input) != `{}` {
		t.Fatalf("call = %+v, error = %v", call, err)
	}
	_, err = decodeOpenAIToolCall("call_1", "list_leagues", "not-json")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func TestOpenAIResponsesRequestOmitsEmptyTools(t *testing.T) {
	if _, exists := openAIResponsesRequest(Request{})["tools"]; exists {
		t.Fatal("empty tools list was sent")
	}
}

func openAIResponsesTestProvider(server *httptest.Server, apiKey string) *openAIResponsesProvider {
	return &openAIResponsesProvider{openAIProvider{
		client: server.Client(), baseURL: server.URL + "/v1", apiKey: apiKey,
	}}
}

func openAIResponsesFixtureServer(t *testing.T, name string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(fixture(t, name))
	}))
	t.Cleanup(server.Close)
	return server
}

func openAIResponsesInlineServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestOpenAIResponsesUnknownEventsAreIgnored(t *testing.T) {
	server := openAIResponsesInlineServer(t, `event: response.unknown
data: not-json

event: response.completed
data: {"type":"response.completed","response":{"status":"completed"}}

`)
	provider := openAIResponsesTestProvider(server, "")
	response, err := provider.Complete(context.Background(), Request{Model: "gpt-6.1-sol"}, nil)
	if err != nil || response.StopReason != "completed" || response.Text != "" {
		t.Fatalf("response = %+v, %v", response, err)
	}
}

func TestOpenAIResponsesErrorEvent(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "event error", body: `event: error
data: {"type":"error","message":"failed"}

`},
		{name: "error type", body: `event: response.created
data: {"type":"error","message":"failed"}

`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := openAIResponsesInlineServer(t, test.body)
			provider := openAIResponsesTestProvider(server, "")
			_, err := provider.Complete(context.Background(), Request{Model: "gpt-6.1-sol"}, nil)
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
