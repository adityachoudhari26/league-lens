package assistant

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"connectrpc.com/connect/v2"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/llm"
)

const chatTestConversationID = "00000000-0000-0000-0000-000000000004"

type fakeConversationStore struct {
	conversationID       string
	createCalls          int
	createErr            error
	loadedConversationID string
	loadCalls            int
	history              []llm.Message
	loadErr              error
	savedConversationID  string
	saveCalls            int
	savedTurns           [][]llm.Message
	saveErr              error
}

func (s *fakeConversationStore) createConversation(context.Context) (string, error) {
	s.createCalls++
	if s.createErr != nil {
		return "", s.createErr
	}
	if s.conversationID != "" {
		return s.conversationID, nil
	}
	return chatTestConversationID, nil
}

func (s *fakeConversationStore) loadConversation(_ context.Context, conversationID string) ([]llm.Message, error) {
	s.loadCalls++
	s.loadedConversationID = conversationID
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	return cloneChatTestMessages(s.history), nil
}

func (s *fakeConversationStore) saveConversationTurn(
	_ context.Context,
	conversationID string,
	turn []llm.Message,
) error {
	s.saveCalls++
	s.savedConversationID = conversationID
	if s.saveErr != nil {
		return s.saveErr
	}
	s.savedTurns = append(s.savedTurns, cloneChatTestMessages(turn))
	return nil
}

type chatTestProvider struct {
	response        llm.Response
	completionErr   error
	request         llm.Request
	completionCalls int
	hasDeadline     bool
}

func (p *chatTestProvider) ListModels(context.Context) ([]string, error) {
	return nil, nil
}

func (p *chatTestProvider) Complete(
	ctx context.Context,
	request llm.Request,
	onText func(string),
) (llm.Response, error) {
	p.request = request
	p.completionCalls++
	_, p.hasDeadline = ctx.Deadline()
	if p.completionErr != nil {
		return llm.Response{}, p.completionErr
	}
	if p.response.Text != "" {
		onText(p.response.Text)
	}
	return p.response, nil
}

func TestChatStartsConversationAndSavesTurn(t *testing.T) {
	conversationID := "00000000-0000-0000-0000-000000000001"
	store := &fakeConversationStore{conversationID: conversationID}
	provider := &chatTestProvider{response: llm.Response{Text: "Hello there."}}
	var events []*v1.ChatResponse
	service := &Service{conversations: store}

	err := runChatTest(service, &v1.ChatRequest{Message: "  Hello  "}, provider, nil, func(event *v1.ChatResponse) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 1 || events[0].GetConversation().GetId() != conversationID {
		t.Fatalf("first event = %+v, want ConversationStarted", events)
	}
	if !provider.hasDeadline || provider.completionCalls != 1 {
		t.Fatalf("provider deadline=%v calls=%d", provider.hasDeadline, provider.completionCalls)
	}
	if store.createCalls != 1 {
		t.Fatalf("conversation create calls = %d, want one", store.createCalls)
	}
	assertChatTestSavedTurn(t, store, conversationID, []llm.Message{
		{Role: llm.RoleUser, Text: "Hello"},
		{Role: llm.RoleAssistant, Text: "Hello there."},
	})
}

func TestChatLoadsExistingConversationHistory(t *testing.T) {
	conversationID := "00000000-0000-0000-0000-000000000002"
	history := []llm.Message{{Role: llm.RoleUser, Text: "earlier question"}}
	store := &fakeConversationStore{history: history}
	provider := &chatTestProvider{response: llm.Response{Text: "A later answer."}}
	var events []*v1.ChatResponse
	service := &Service{conversations: store}

	err := runChatTest(service, &v1.ChatRequest{
		ConversationId: conversationID, Message: "follow up",
	}, provider, nil, func(event *v1.ChatResponse) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.createCalls != 0 || store.loadCalls != 1 || store.loadedConversationID != conversationID {
		t.Fatalf("store calls = create:%d load:%d id:%q", store.createCalls, store.loadCalls, store.loadedConversationID)
	}
	if len(events) != 1 || events[0].GetConversation() != nil {
		t.Fatalf("events = %+v, want no ConversationStarted", events)
	}
	assertChatTestMessages(t, provider.request.Messages, []llm.Message{
		{Role: llm.RoleUser, Text: "earlier question"},
		{Role: llm.RoleUser, Text: "follow up"},
	})
	assertChatTestSavedTurn(t, store, conversationID, []llm.Message{
		{Role: llm.RoleUser, Text: "follow up"},
		{Role: llm.RoleAssistant, Text: "A later answer."},
	})
}

func TestChatDoesNotSaveProviderFailures(t *testing.T) {
	store := &fakeConversationStore{}
	provider := &chatTestProvider{completionErr: llm.ErrUnavailable}

	err := runChatTest(&Service{conversations: store}, chatTestRequest("", "question"), provider, nil, ignoreChatEvent)
	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("chat error = %v, want Unavailable", err)
	}
	if store.saveCalls != 0 {
		t.Fatalf("save calls = %d, want none", store.saveCalls)
	}
}

func TestChatDoesNotCreateConversationWhenAssistantIsNotSetUp(t *testing.T) {
	store := &fakeConversationStore{}
	want := connect.NewError(connect.CodeFailedPrecondition, "assistant not set up")

	err := runChatTest(&Service{conversations: store}, chatTestRequest("", "question"), nil, want, ignoreChatEvent)
	if !errors.Is(err, want) || !connectErrorCodeIs(err, connect.CodeFailedPrecondition) || store.createCalls != 0 {
		t.Fatalf("chat error=%v create calls=%d", err, store.createCalls)
	}
}

func TestChatValidatesMessageText(t *testing.T) {
	for _, text := range []string{"", " \t\n ", strings.Repeat("a", maxChatMessageRunes+1)} {
		err := runChatTest(
			&Service{conversations: &fakeConversationStore{}},
			chatTestRequest("", text),
			&chatTestProvider{},
			nil,
			ignoreChatEvent,
		)
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("text length %d error = %v, want InvalidArgument", utf8.RuneCountInString(text), err)
		}
	}

	text := strings.Repeat("界", maxChatMessageRunes)
	store := &fakeConversationStore{}
	err := runChatTest(&Service{conversations: store}, chatTestRequest("", text), &chatTestProvider{}, nil, ignoreChatEvent)
	if err != nil || len(store.savedTurns) != 1 || utf8.RuneCountInString(store.savedTurns[0][0].Text) != maxChatMessageRunes {
		t.Fatalf("4000-rune message: error=%v saved=%+v", err, store.savedTurns)
	}
}

func TestChatRejectsNilRequest(t *testing.T) {
	err := runChatTest(
		&Service{conversations: &fakeConversationStore{}},
		nil,
		&chatTestProvider{},
		nil,
		ignoreChatEvent,
	)
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("nil request error = %v, want InvalidArgument", err)
	}
}

func TestChatConversationStartSendFailureStopsProvider(t *testing.T) {
	store := &fakeConversationStore{}
	provider := &chatTestProvider{response: llm.Response{Text: "must not run"}}
	sendErr := errors.New("client disconnected")
	sendCalls := 0

	err := runChatTest(&Service{conversations: store}, chatTestRequest("", "question"), provider, nil, func(*v1.ChatResponse) error {
		sendCalls++
		return sendErr
	})
	if !errors.Is(err, sendErr) || sendCalls != 1 || provider.completionCalls != 0 || store.saveCalls != 0 {
		t.Fatalf("error=%v send calls=%d provider calls=%d save calls=%d", err, sendCalls, provider.completionCalls, store.saveCalls)
	}
}

func TestChatReturnsConversationSaveError(t *testing.T) {
	saveErr := connect.NewError(connect.CodeInternal, "save failed")
	store := &fakeConversationStore{saveErr: saveErr}
	provider := &chatTestProvider{response: llm.Response{Text: "answer"}}

	err := runChatTest(&Service{conversations: store}, chatTestRequest("", "question"), provider, nil, ignoreChatEvent)
	if !errors.Is(err, saveErr) {
		t.Fatalf("chat error = %v, want save error %v", err, saveErr)
	}
}

func TestChatReturnsConversationLoadNotFound(t *testing.T) {
	notFound := connect.NewError(connect.CodeNotFound, "conversation not found")
	store := &fakeConversationStore{loadErr: notFound}
	provider := &chatTestProvider{}

	err := runChatTest(&Service{conversations: store}, chatTestRequest("00000000-0000-0000-0000-000000000003", "question"), provider, nil, ignoreChatEvent)
	if !errors.Is(err, notFound) || provider.completionCalls != 0 || store.createCalls != 0 {
		t.Fatalf("chat error=%v provider calls=%d create calls=%d", err, provider.completionCalls, store.createCalls)
	}
}

func runChatTest(
	service *Service,
	request *v1.ChatRequest,
	provider llm.Provider,
	loadErr error,
	send chatEventSender,
) error {
	return service.chat(context.Background(), request, send, func(context.Context) (llm.Provider, string, error) {
		return provider, "fake-model", loadErr
	})
}

func chatTestRequest(conversationID, message string) *v1.ChatRequest {
	return &v1.ChatRequest{ConversationId: conversationID, Message: message}
}

func ignoreChatEvent(*v1.ChatResponse) error {
	return nil
}

func cloneChatTestMessages(messages []llm.Message) []llm.Message {
	cloned := append([]llm.Message(nil), messages...)
	for index := range cloned {
		cloned[index].ToolCalls = append([]llm.ToolCall(nil), messages[index].ToolCalls...)
		cloned[index].ToolResults = append([]llm.ToolResult(nil), messages[index].ToolResults...)
	}
	return cloned
}

func assertChatTestSavedTurn(t *testing.T, store *fakeConversationStore, id string, want []llm.Message) {
	t.Helper()
	if store.savedConversationID != id || len(store.savedTurns) != 1 {
		t.Fatalf("saved conversation=%q turns=%d, want %q and one turn", store.savedConversationID, len(store.savedTurns), id)
	}
	assertChatTestMessages(t, store.savedTurns[0], want)
}

func assertChatTestMessages(t *testing.T, got, want []llm.Message) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("messages = %+v, want %+v", got, want)
	}
	for index := range want {
		if got[index].Role != want[index].Role || got[index].Text != want[index].Text ||
			len(got[index].ToolCalls) != len(want[index].ToolCalls) ||
			len(got[index].ToolResults) != len(want[index].ToolResults) {
			t.Fatalf("message %d = %+v, want %+v", index, got[index], want[index])
		}
	}
}

func connectErrorCodeIs(err error, want connect.Code) bool {
	var connectErr *connect.Error
	return errors.As(err, &connectErr) && connectErr.Code() == want
}
