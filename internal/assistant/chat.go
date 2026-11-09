package assistant

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect/v2"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	leaguelensv1connect "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1/leaguelensv1connect"
	"github.com/adityachoudhari26/league-lens/internal/llm"
)

const (
	chatTimeout             = 2 * time.Minute
	maxChatMessageRunes     = 4000
	chatCompletionMaxTokens = 1500
)

type chatProviderLoader func(context.Context) (llm.Provider, string, error)
type chatEventSender func(*v1.ChatResponse) error

// Chat streams a completion using the owner's saved assistant settings.
func (s *Service) Chat(
	ctx context.Context,
	request *v1.ChatRequest,
	stream leaguelensv1connect.AssistantServiceChatServerStream,
) error {
	return s.chat(ctx, request, stream.Send, s.providerForChat)
}

func (s *Service) chat(
	ctx context.Context,
	request *v1.ChatRequest,
	send chatEventSender,
	loadProvider chatProviderLoader,
) error {
	if request == nil {
		return invalidArgument("a chat message is required")
	}
	text, err := validatedChatText(request.GetMessage())
	if err != nil {
		return err
	}
	chatCtx, cancel := context.WithTimeout(ctx, chatTimeout)
	defer cancel()
	provider, model, err := loadProvider(chatCtx)
	if err != nil {
		return err
	}
	loop := chatLoop{
		cancel: cancel, provider: provider, model: model, send: send, tools: s.chatTools(),
	}
	conversationID, history, err := s.openConversation(chatCtx, request.GetConversationId(), loop.sendEvent)
	if err != nil {
		return err
	}
	userMessage := llm.Message{Role: llm.RoleUser, Text: text}
	turn, err := loop.run(chatCtx, request.GetPage(), history, userMessage)
	if err != nil {
		return err
	}
	return s.conversationStore().saveConversationTurn(chatCtx, conversationID, turn)
}

// openConversation starts a new conversation or loads its saved history.
func (s *Service) openConversation(
	ctx context.Context,
	conversationID string,
	send chatEventSender,
) (string, []llm.Message, error) {
	if conversationID != "" {
		messages, err := s.conversationStore().loadConversation(ctx, conversationID)
		return conversationID, messages, err
	}
	conversationID, err := s.conversationStore().createConversation(ctx)
	if err != nil {
		return "", nil, err
	}
	if err := send(&v1.ChatResponse{
		Event: &v1.ChatResponse_Conversation{
			Conversation: &v1.ConversationStarted{Id: conversationID},
		},
	}); err != nil {
		return "", nil, err
	}
	return conversationID, nil, nil
}

func (s *Service) providerForChat(ctx context.Context) (llm.Provider, string, error) {
	settings, found, err := s.loadSettings(ctx)
	if err != nil {
		return nil, "", internalError("assistant settings could not be loaded")
	}
	if !found {
		return nil, "", connect.NewError(connect.CodeFailedPrecondition, "assistant not set up")
	}
	key, err := s.resolveKey("", settings.Provider, settings.BaseUrl, settings, found)
	if err != nil {
		return nil, "", err
	}
	provider, err := s.newProvider(llm.Config{
		Provider: settings.Provider, BaseURL: settings.BaseUrl, APIKey: key.Plaintext,
	})
	if err != nil {
		return nil, "", internalError("saved assistant provider settings are invalid")
	}
	return provider, settings.Model, nil
}

// validatedChatText trims the new user message and enforces its rune limit.
func validatedChatText(text string) (string, error) {
	text = strings.TrimSpace(text)
	textLength := utf8.RuneCountInString(text)
	if textLength < 1 || textLength > maxChatMessageRunes {
		return "", invalidArgument("chat message text must be 1 to 4000 characters")
	}
	return text, nil
}
