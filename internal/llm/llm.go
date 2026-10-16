package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

const (
	ProviderAnthropic        = "anthropic"
	ProviderOpenAICompatible = "openai_compatible"
)

// Role identifies the speaker for a message in a conversation.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// ToolCall is a provider-requested operation with JSON input.
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResult contains the result of a previously requested operation.
type ToolResult struct {
	CallID  string
	Content string
	IsError bool
}

// Message is one user, assistant, or tool result in a conversation.
type Message struct {
	Role        Role
	Text        string
	ToolCalls   []ToolCall
	ToolResults []ToolResult
}

// Tool describes an operation the provider may request.
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// Request contains the conversation, tools, and limits for a completion.
type Request struct {
	Model     string
	System    string
	Messages  []Message
	Tools     []Tool
	MaxTokens int
}

// Response contains streamed text, tool calls, and the provider stop reason.
type Response struct {
	Text       string
	ToolCalls  []ToolCall
	StopReason string
}

// Provider calls one AI vendor API and emits text deltas as they arrive.
type Provider interface {
	ListModels(context.Context) ([]string, error)
	Complete(context.Context, Request, func(string)) (Response, error)
}

var (
	ErrUnauthorized = errors.New("ai provider rejected the api key")
	ErrUnavailable  = errors.New("ai provider unavailable")
)

// Config selects a provider and supplies its endpoint and optional key.
type Config struct {
	Provider string
	BaseURL  string
	APIKey   string
}

// New constructs a provider client without imposing a timeout on long streams.
func New(cfg Config, client *http.Client) (Provider, error) {
	if client == nil {
		client = http.DefaultClient
	}
	switch cfg.Provider {
	case ProviderAnthropic:
		return &anthropicProvider{client: client, apiKey: cfg.APIKey}, nil
	case ProviderOpenAICompatible:
		if err := ValidateBaseURL(cfg.BaseURL); err != nil {
			return nil, err
		}
		return &openAIProvider{client: client, baseURL: cfg.BaseURL, apiKey: cfg.APIKey}, nil
	default:
		return nil, errors.New("unsupported ai provider")
	}
}

// ValidateBaseURL checks an OpenAI-compatible HTTP(S) endpoint without credentials, query, fragment, or trailing slash.
func ValidateBaseURL(baseURL string) error {
	if baseURL == "" || baseURL[len(baseURL)-1] == '/' {
		return errors.New("openai-compatible base URL is required without a trailing slash")
	}
	request, err := http.NewRequest(http.MethodGet, baseURL, nil)
	if err != nil || request.URL.Host == "" || request.URL.User != nil ||
		(request.URL.Scheme != "http" && request.URL.Scheme != "https") ||
		request.URL.RawQuery != "" || request.URL.Fragment != "" {
		return errors.New("openai-compatible base URL must be an http(s) URL")
	}
	return nil
}

func sendRequest(ctx context.Context, client *http.Client, method, endpoint string, body []byte, headers http.Header) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, ErrUnavailable
	}
	request.Header = headers.Clone()
	response, err := client.Do(request)
	if err != nil {
		return nil, ErrUnavailable
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		_ = response.Body.Close()
		return nil, fmt.Errorf("%w: HTTP %d", ErrUnauthorized, response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_ = response.Body.Close()
		return nil, fmt.Errorf("%w: HTTP %d", ErrUnavailable, response.StatusCode)
	}
	return response, nil
}

func decodeJSON(response *http.Response, target any) error {
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return ErrUnavailable
	}
	return nil
}

func readSSE(response *http.Response, onEvent func(string, []byte) error) error {
	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	var event string
	var data []byte
	dispatch := func() error {
		if len(data) == 0 {
			event = ""
			return nil
		}
		err := onEvent(event, data)
		event, data = "", nil
		return err
	}
	for scanner.Scan() {
		line := scanner.Bytes()
		line = bytes.TrimSuffix(line, []byte("\r"))
		switch {
		case len(line) == 0:
			if err := dispatch(); err != nil {
				return err
			}
		case bytes.HasPrefix(line, []byte("event:")):
			event = string(bytes.TrimSpace(line[len("event:"):]))
		case bytes.HasPrefix(line, []byte("data:")):
			if len(data) > 0 {
				data = append(data, '\n')
			}
			data = append(data, bytes.TrimSpace(line[len("data:"):])...)
		}
	}
	if err := scanner.Err(); err != nil {
		return ErrUnavailable
	}
	return dispatch()
}

func streamError(event string, data []byte) error {
	if event == "error" {
		return ErrUnavailable
	}
	var message struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &message) == nil && message.Type == "error" {
		return ErrUnavailable
	}
	return nil
}

func decodeObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return json.RawMessage(`{}`)
	}
	return raw
}
