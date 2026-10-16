package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestOpenAICompatibleListModelsAndComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" ||
			r.Header.Get("content-type") != "application/json" {
			t.Errorf("unexpected headers: %v", r.Header)
		}
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"fake-model"}]}`))
		case "/v1/chat/completions":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["model"] != "fake-model" || body["stream"] != true || body["max_tokens"] != float64(200) {
				t.Errorf("chat body = %#v", body)
			}
			messages := body["messages"].([]any)
			if messages[0].(map[string]any)["role"] != "system" {
				t.Errorf("missing system message: %#v", messages)
			}
			assistant := messages[2].(map[string]any)
			calls := assistant["tool_calls"].([]any)
			args := calls[0].(map[string]any)["function"].(map[string]any)["arguments"]
			if args != `{"name":"Jo"}` {
				t.Errorf("tool arguments = %#v", args)
			}
			tool := messages[3].(map[string]any)
			if tool["role"] != "tool" || tool["tool_call_id"] != "call_1" {
				t.Errorf("tool result = %#v", tool)
			}
			tools := body["tools"].([]any)
			if tools[0].(map[string]any)["function"].(map[string]any)["parameters"] == nil {
				t.Errorf("tool schema missing: %#v", tools)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write(fixture(t, "openai_tool.sse"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	provider, err := New(Config{Provider: ProviderOpenAICompatible, BaseURL: server.URL + "/v1", APIKey: "sk-test"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	models, err := provider.ListModels(context.Background())
	if err != nil || !reflect.DeepEqual(models, []string{"fake-model"}) {
		t.Fatalf("models = %v, %v", models, err)
	}
	var deltas []string
	response, err := provider.Complete(context.Background(), Request{
		Model: "fake-model", System: "Be helpful", MaxTokens: 200,
		Messages: []Message{
			{Role: RoleUser, Text: "Look up a player"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_1", Name: "lookup", Input: json.RawMessage(`{"name":"Jo"}`)}}},
			{Role: RoleUser, ToolResults: []ToolResult{{CallID: "call_1", Content: "Found Joe"}}},
		},
		Tools: []Tool{{Name: "lookup", Description: "Look up", InputSchema: json.RawMessage(`{"type":"object"}`)}},
	}, func(delta string) { deltas = append(deltas, delta) })
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "Found Joe." || response.StopReason != "tool_calls" ||
		len(response.ToolCalls) != 1 || string(response.ToolCalls[0].Input) != `{"name":"Joe"}` {
		t.Fatalf("response = %+v", response)
	}
	if strings.Join(deltas, "") != response.Text {
		t.Fatalf("text deltas = %q", deltas)
	}
}

func TestOpenAICompatibleKeylessRequestOmitsAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if value := r.Header.Get("Authorization"); value != "" {
			t.Errorf("Authorization = %q", value)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"local-model"}]}`))
	}))
	defer server.Close()
	provider, err := New(Config{Provider: ProviderOpenAICompatible, BaseURL: server.URL}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	models, err := provider.ListModels(context.Background())
	if err != nil || !reflect.DeepEqual(models, []string{"local-model"}) {
		t.Fatalf("models = %v, %v", models, err)
	}
}

func TestOpenAICompatibleTextOnlyStreamOmitsTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if _, exists := body["tools"]; exists {
			t.Error("empty tools list was sent")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(fixture(t, "openai_text.sse"))
	}))
	defer server.Close()
	provider, err := New(Config{Provider: ProviderOpenAICompatible, BaseURL: server.URL}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	response, err := provider.Complete(context.Background(), Request{Model: "local-model"}, nil)
	if err != nil || response.Text != "Hello from OpenAI." || response.StopReason != "stop" {
		t.Fatalf("response = %+v, %v", response, err)
	}
}

func TestOpenAICompatibleUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("secret body must not leak"))
	}))
	defer server.Close()
	provider, err := New(Config{Provider: ProviderOpenAICompatible, BaseURL: server.URL, APIKey: "sk-test"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.ListModels(context.Background())
	if !errors.Is(err, ErrUnauthorized) || strings.Contains(err.Error(), "secret body") {
		t.Fatalf("error = %v", err)
	}
}
