package assistant

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/llm"
)

type chatTestProvider struct {
	deltas        []string
	completionErr error
	request       llm.Request
	hasDeadline   bool
}

func (p *chatTestProvider) ListModels(context.Context) ([]string, error) {
	return nil, nil
}

func (p *chatTestProvider) Complete(ctx context.Context, request llm.Request, onText func(string)) (llm.Response, error) {
	p.request = request
	_, p.hasDeadline = ctx.Deadline()
	for _, delta := range p.deltas {
		onText(delta)
	}
	return llm.Response{}, p.completionErr
}

func TestChatForwardsDeltasAndTrimsHistory(t *testing.T) {
	provider := &chatTestProvider{deltas: []string{"One", " two", "!"}}
	request := &v1.ChatRequest{}
	for index := 1; index <= 23; index++ {
		request.Messages = append(request.Messages, &v1.ChatMessage{
			Role: v1.ChatRole_CHAT_ROLE_USER, Text: messageNumber(index),
		})
	}
	var events []*v1.ChatResponse
	service := &Service{}
	err := service.chat(context.Background(), request, func(event *v1.ChatResponse) error {
		events = append(events, event)
		return nil
	}, func(context.Context) (llm.Provider, string, error) {
		return provider, "fake-model", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != len(provider.deltas) {
		t.Fatalf("events = %d, want %d", len(events), len(provider.deltas))
	}
	for index, event := range events {
		if got := event.GetText().GetText(); got != provider.deltas[index] {
			t.Errorf("delta %d = %q, want %q", index, got, provider.deltas[index])
		}
	}
	if !provider.hasDeadline {
		t.Fatal("provider context has no deadline")
	}
	if provider.request.Model != "fake-model" || provider.request.MaxTokens != 1500 {
		t.Fatalf("completion settings = %+v", provider.request)
	}
	if len(provider.request.Messages) != retainedChatMessages ||
		provider.request.Messages[0].Text != messageNumber(4) ||
		provider.request.Messages[len(provider.request.Messages)-1].Text != messageNumber(23) {
		t.Fatalf("messages were not trimmed to the last 20: %+v", provider.request.Messages)
	}
	for index, message := range provider.request.Messages {
		if message.Role != llm.RoleUser {
			t.Errorf("message %d role = %q", index, message.Role)
		}
	}
}

func TestChatPreservesNotSetUpError(t *testing.T) {
	want := connect.NewError(connect.CodeFailedPrecondition, "assistant not set up")
	err := (&Service{}).chat(
		context.Background(),
		&v1.ChatRequest{Messages: []*v1.ChatMessage{{Role: v1.ChatRole_CHAT_ROLE_USER, Text: "Hello"}}},
		func(*v1.ChatResponse) error { return nil },
		func(context.Context) (llm.Provider, string, error) { return nil, "", want },
	)
	if !connectErrorCodeIs(err, connect.CodeFailedPrecondition) {
		t.Fatalf("chat error = %v, want FailedPrecondition", err)
	}
}

func TestChatMapsProviderErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		code connect.Code
	}{
		{name: "rejected key", err: llm.ErrUnauthorized, code: connect.CodePermissionDenied},
		{name: "unavailable", err: llm.ErrUnavailable, code: connect.CodeUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := &chatTestProvider{completionErr: test.err}
			err := (&Service{}).chat(
				context.Background(),
				&v1.ChatRequest{Messages: []*v1.ChatMessage{{Role: v1.ChatRole_CHAT_ROLE_USER, Text: "Hello"}}},
				func(*v1.ChatResponse) error { return nil },
				func(context.Context) (llm.Provider, string, error) { return provider, "fake-model", nil },
			)
			if !connectErrorCodeIs(err, test.code) {
				t.Fatalf("chat error = %v, want %v", err, test.code)
			}
		})
	}
}

func TestChatValidation(t *testing.T) {
	validMessage := func(role v1.ChatRole, text string) *v1.ChatMessage {
		return &v1.ChatMessage{Role: role, Text: text}
	}
	tests := []struct {
		name     string
		messages []*v1.ChatMessage
	}{
		{name: "empty", messages: nil},
		{name: "too many", messages: repeatedMessages(maxChatMessages + 1)},
		{name: "last assistant", messages: []*v1.ChatMessage{validMessage(v1.ChatRole_CHAT_ROLE_ASSISTANT, "Hello")}},
		{name: "unspecified role", messages: []*v1.ChatMessage{validMessage(v1.ChatRole_CHAT_ROLE_UNSPECIFIED, "Hello")}},
		{name: "blank text", messages: []*v1.ChatMessage{validMessage(v1.ChatRole_CHAT_ROLE_USER, " \t\n ")}},
		{name: "text too long", messages: []*v1.ChatMessage{validMessage(v1.ChatRole_CHAT_ROLE_USER, strings.Repeat("a", maxChatMessageRunes+1))}},
		{name: "nil message", messages: []*v1.ChatMessage{nil}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validatedChatMessages(test.messages)
			if !connectErrorCodeIs(err, connect.CodeInvalidArgument) {
				t.Fatalf("validation error = %v, want InvalidArgument", err)
			}
		})
	}
}

func TestChatTrimsMessageTextAndAllowsNonAlternatingRoles(t *testing.T) {
	messages, err := validatedChatMessages([]*v1.ChatMessage{
		{Role: v1.ChatRole_CHAT_ROLE_USER, Text: "  first  "},
		{Role: v1.ChatRole_CHAT_ROLE_USER, Text: "  last  "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].Text != "first" || messages[1].Text != "last" {
		t.Fatalf("validated messages = %+v", messages)
	}
}

func TestChatSkipsEmptyAssistantProposalPlaceholder(t *testing.T) {
	messages, err := validatedChatMessages([]*v1.ChatMessage{
		{Role: v1.ChatRole_CHAT_ROLE_USER, Text: "refresh my leagues"},
		{Role: v1.ChatRole_CHAT_ROLE_ASSISTANT},
		{Role: v1.ChatRole_CHAT_ROLE_ASSISTANT, Text: "Started a refresh of 1 league."},
		{Role: v1.ChatRole_CHAT_ROLE_USER, Text: "dismiss an alert"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 {
		t.Fatalf("validated messages = %+v, want empty assistant proposal placeholder omitted", messages)
	}
	if messages[0].Text != "refresh my leagues" ||
		messages[1].Text != "Started a refresh of 1 league." ||
		messages[2].Text != "dismiss an alert" {
		t.Fatalf("validated messages = %+v", messages)
	}
}

func TestChatSendFailureStopsProvider(t *testing.T) {
	provider := &chatTestProvider{deltas: []string{"first", "second"}}
	sendErr := errors.New("client disconnected")
	sent := 0
	err := (&Service{}).chat(
		context.Background(),
		&v1.ChatRequest{Messages: []*v1.ChatMessage{{Role: v1.ChatRole_CHAT_ROLE_USER, Text: "Hello"}}},
		func(*v1.ChatResponse) error {
			sent++
			return sendErr
		},
		func(context.Context) (llm.Provider, string, error) { return provider, "fake-model", nil },
	)
	if !errors.Is(err, sendErr) || sent != 1 {
		t.Fatalf("chat error = %v, sent %d events", err, sent)
	}
}

func connectErrorCodeIs(err error, want connect.Code) bool {
	var connectErr *connect.Error
	return errors.As(err, &connectErr) && connectErr.Code() == want
}

func repeatedMessages(count int) []*v1.ChatMessage {
	messages := make([]*v1.ChatMessage, count)
	for index := range messages {
		messages[index] = &v1.ChatMessage{Role: v1.ChatRole_CHAT_ROLE_USER, Text: "message"}
	}
	return messages
}

func messageNumber(index int) string {
	return fmt.Sprintf("message %02d", index)
}
