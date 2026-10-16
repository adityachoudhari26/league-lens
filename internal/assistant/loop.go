package assistant

import (
	"context"
	"time"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/llm"
)

const maxToolRounds = 5

// runChatLoop completes a chat with sequential, bounded read-only tool rounds.
func runChatLoop(
	ctx context.Context,
	cancel context.CancelFunc,
	request *v1.ChatRequest,
	provider llm.Provider,
	model string,
	messages []llm.Message,
	send chatEventSender,
	tools []assistantTool,
) error {
	system := buildSystemPrompt(time.Now(), request.GetPage(), true)
	toolRounds := 0
	for {
		availableTools := toolDefinitions(tools)
		if toolRounds >= maxToolRounds {
			availableTools = nil
			system += "\nTool limit reached. Answer with the data you already have."
		}
		response, err := completeChatRound(ctx, cancel, provider, request, model, system, messages, availableTools, send)
		if err != nil {
			return err
		}
		if len(response.ToolCalls) == 0 || toolRounds >= maxToolRounds {
			return nil
		}
		messages = append(messages, llm.Message{
			Role: llm.RoleAssistant, Text: response.Text, ToolCalls: response.ToolCalls,
		})
		results, err := runToolCalls(ctx, cancel, tools, response.ToolCalls, send)
		if err != nil {
			return err
		}
		messages = append(messages, llm.Message{Role: llm.RoleUser, ToolResults: results})
		toolRounds++
	}
}

func completeChatRound(
	ctx context.Context,
	cancel context.CancelFunc,
	provider llm.Provider,
	request *v1.ChatRequest,
	model, system string,
	messages []llm.Message,
	tools []llm.Tool,
	send chatEventSender,
) (llm.Response, error) {
	var sendErr error
	response, completionErr := provider.Complete(ctx, llm.Request{
		Model: model, System: system, Messages: messages, Tools: tools, MaxTokens: chatCompletionMaxTokens,
	}, func(delta string) {
		if sendErr != nil || delta == "" {
			return
		}
		sendErr = send(&v1.ChatResponse{
			Event: &v1.ChatResponse_Text{Text: &v1.TextDelta{Text: delta}},
		})
		if sendErr != nil {
			cancel()
		}
	})
	if sendErr != nil {
		return llm.Response{}, sendErr
	}
	if completionErr != nil {
		return llm.Response{}, mapProviderError(completionErr)
	}
	return response, nil
}

func runToolCalls(
	ctx context.Context,
	cancel context.CancelFunc,
	tools []assistantTool,
	calls []llm.ToolCall,
	send chatEventSender,
) ([]llm.ToolResult, error) {
	results := make([]llm.ToolResult, 0, len(calls))
	for _, call := range calls {
		if err := send(&v1.ChatResponse{
			Event: &v1.ChatResponse_Tool{Tool: &v1.ToolActivity{Label: toolLabel(tools, call.Name, call.Input)}},
		}); err != nil {
			cancel()
			return nil, err
		}
		results = append(results, runAssistantTool(ctx, tools, call))
	}
	return results, nil
}
