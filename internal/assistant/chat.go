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
	maxChatMessages         = 40
	maxChatMessageRunes     = 4000
	retainedChatMessages    = 20
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
		return invalidArgument("chat messages are required")
	}
	messages, err := validatedChatMessages(request.GetMessages())
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
		cancel: cancel, provider: provider, model: model, send: send, tools: buildAssistantTools(s.Dashboard, s),
	}
	return loop.run(chatCtx, request.GetPage(), messages)
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

// validatedChatMessages drops empty assistant proposal placeholders from client history.
func validatedChatMessages(messages []*v1.ChatMessage) ([]llm.Message, error) {
	if len(messages) < 1 || len(messages) > maxChatMessages {
		return nil, invalidArgument("chat requires between 1 and 40 messages")
	}
	if messages[len(messages)-1] == nil ||
		messages[len(messages)-1].GetRole() != v1.ChatRole_CHAT_ROLE_USER {
		return nil, invalidArgument("the last chat message must be from the user")
	}
	validated := make([]llm.Message, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			return nil, invalidArgument("chat messages cannot be empty")
		}
		role, err := chatRole(message.GetRole())
		if err != nil {
			return nil, err
		}
		text := strings.TrimSpace(message.GetText())
		textLength := utf8.RuneCountInString(text)
		if role == llm.RoleAssistant && textLength == 0 {
			continue
		}
		if textLength < 1 || textLength > maxChatMessageRunes {
			return nil, invalidArgument("chat message text must be 1 to 4000 characters")
		}
		validated = append(validated, llm.Message{Role: role, Text: text})
	}
	if len(validated) > retainedChatMessages {
		validated = validated[len(validated)-retainedChatMessages:]
	}
	return validated, nil
}

func chatRole(role v1.ChatRole) (llm.Role, error) {
	switch role {
	case v1.ChatRole_CHAT_ROLE_USER:
		return llm.RoleUser, nil
	case v1.ChatRole_CHAT_ROLE_ASSISTANT:
		return llm.RoleAssistant, nil
	default:
		return "", invalidArgument("chat message role must be user or assistant")
	}
}
