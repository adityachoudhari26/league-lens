package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type anthropicTestTransport struct {
	destination *url.URL
	next        http.RoundTripper
}

func (t anthropicTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	rewritten := request.Clone(request.Context())
	target := *request.URL
	target.Scheme = t.destination.Scheme
	target.Host = t.destination.Host
	rewritten.URL = &target
	rewritten.Host = t.destination.Host
	return t.next.RoundTrip(rewritten)
}

func anthropicTestClient(server *httptest.Server) *http.Client {
	destination, err := url.Parse(server.URL)
	if err != nil {
		panic(err)
	}
	return &http.Client{Transport: anthropicTestTransport{
		destination: destination, next: server.Client().Transport,
	}}
}

func TestAnthropicListModelsAndComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk-test" ||
			r.Header.Get("anthropic-version") != "2023-06-01" ||
			r.Header.Get("content-type") != "application/json" {
			t.Errorf("unexpected headers: %v", r.Header)
		}
		switch r.URL.Path {
		case "/v1/models":
			if r.URL.Query().Get("limit") != "100" {
				t.Errorf("models limit = %q", r.URL.Query().Get("limit"))
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-test"}]}`))
		case "/v1/messages":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["model"] != "claude-test" || body["stream"] != true ||
				body["system"] != "Be helpful" || body["max_tokens"] != float64(300) {
				t.Errorf("message body = %#v", body)
			}
			messages := body["messages"].([]any)
			last := messages[1].(map[string]any)
			content := last["content"].([]any)
			if content[0].(map[string]any)["type"] != "tool_use" {
				t.Errorf("assistant tool call was not encoded: %#v", content)
			}
			tools := body["tools"].([]any)
			schema := tools[0].(map[string]any)["input_schema"].(map[string]any)
			if schema["type"] != "object" {
				t.Errorf("tool schema = %#v", schema)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write(fixture(t, "anthropic_tool.sse"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider, err := New(Config{Provider: ProviderAnthropic, APIKey: "sk-test"}, anthropicTestClient(server))
	if err != nil {
		t.Fatal(err)
	}
	models, err := provider.ListModels(context.Background())
	if err != nil || !reflect.DeepEqual(models, []string{"claude-test"}) {
		t.Fatalf("models = %v, %v", models, err)
	}
	var deltas []string
	response, err := provider.Complete(context.Background(), Request{
		Model: "claude-test", System: "Be helpful", MaxTokens: 300,
		Messages: []Message{{Role: RoleUser, Text: "Look up a player"}, {
			Role: RoleAssistant, ToolCalls: []ToolCall{{
				ID: "call_1", Name: "lookup", Input: json.RawMessage(`{"name":"Jo"}`),
			}},
		}},
		Tools: []Tool{{Name: "lookup", Description: "Look up", InputSchema: json.RawMessage(`{"type":"object"}`)}},
	}, func(delta string) { deltas = append(deltas, delta) })
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "I found it." || response.StopReason != "tool_use" ||
		len(response.ToolCalls) != 1 || string(response.ToolCalls[0].Input) != `{"name":"Joe"}` {
		t.Fatalf("response = %+v", response)
	}
	if strings.Join(deltas, "") != response.Text {
		t.Fatalf("text deltas = %q", deltas)
	}
}

func TestAnthropicUnauthorizedAndErrorEvent(t *testing.T) {
	for _, tc := range []struct {
		name       string
		statusCode int
		fixture    string
		want       error
	}{
		{name: "unauthorized", statusCode: http.StatusUnauthorized, want: ErrUnauthorized},
		{name: "error event", statusCode: http.StatusOK, fixture: "anthropic_error.sse", want: ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.statusCode != http.StatusOK {
					w.WriteHeader(tc.statusCode)
					_, _ = w.Write([]byte("secret body must not leak"))
					return
				}
				_, _ = w.Write(fixture(t, tc.fixture))
			}))
			defer server.Close()
			provider, err := New(Config{Provider: ProviderAnthropic, APIKey: "sk-test"}, anthropicTestClient(server))
			if err != nil {
				t.Fatal(err)
			}
			_, err = provider.Complete(context.Background(), Request{Model: "claude-test"}, nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if strings.Contains(err.Error(), "secret body") {
				t.Fatal("provider response body leaked")
			}
		})
	}
}

func TestAnthropicTextOnlyStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(fixture(t, "anthropic_text.sse"))
	}))
	defer server.Close()
	provider, err := New(Config{Provider: ProviderAnthropic, APIKey: "sk-test"}, anthropicTestClient(server))
	if err != nil {
		t.Fatal(err)
	}
	response, err := provider.Complete(context.Background(), Request{Model: "claude-test"}, nil)
	if err != nil || response.Text != "Hello from Anthropic." || response.StopReason != "end_turn" {
		t.Fatalf("response = %+v, %v", response, err)
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}
