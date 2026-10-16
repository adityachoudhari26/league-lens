package assistant

import (
	"context"
	"time"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/llm"
)

const maxToolRounds = 5

type chatLoop struct {
	cancel   context.CancelFunc
	provider llm.Provider
	model    string
	send     chatEventSender
	tools    []assistantTool
}

// run completes a chat with sequential, bounded read-only tool rounds.
func (loop *chatLoop) run(ctx context.Context, page *v1.PageContext, messages []llm.Message) error {
	system := buildSystemPrompt(time.Now(), page, true)
	toolRounds := 0
	for {
		availableTools := toolDefinitions(loop.tools)
		if toolRounds >= maxToolRounds {
			system += "\nTool limit reached. Answer with the data you already have."
		}
		response, err := loop.completeRound(ctx, system, messages, availableTools)
		if err != nil {
			return err
		}
		if len(response.ToolCalls) == 0 {
			return nil
		}
		if toolRounds >= maxToolRounds {
			if response.Text == "" {
				if err := loop.send(&v1.ChatResponse{
					Event: &v1.ChatResponse_Text{Text: &v1.TextDelta{
						Text: "I couldn't finish looking that up. Try a narrower question.",
					}},
				}); err != nil {
					loop.cancel()
					return err
				}
			}
			return nil
		}
		messages = append(messages, llm.Message{
			Role: llm.RoleAssistant, Text: response.Text, ToolCalls: response.ToolCalls,
		})
		results, err := loop.runToolCalls(ctx, response.ToolCalls)
		if err != nil {
			return err
		}
		messages = append(messages, llm.Message{Role: llm.RoleUser, ToolResults: results})
		toolRounds++
	}
}

func (loop *chatLoop) completeRound(
	ctx context.Context,
	system string,
	messages []llm.Message,
	tools []llm.Tool,
) (llm.Response, error) {
	var sendErr error
	response, completionErr := loop.provider.Complete(ctx, llm.Request{
		Model: loop.model, System: system, Messages: messages, Tools: tools, MaxTokens: chatCompletionMaxTokens,
	}, func(delta string) {
		if sendErr != nil || delta == "" {
			return
		}
		sendErr = loop.send(&v1.ChatResponse{
			Event: &v1.ChatResponse_Text{Text: &v1.TextDelta{Text: delta}},
		})
		if sendErr != nil {
			loop.cancel()
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

func (loop *chatLoop) runToolCalls(
	ctx context.Context,
	calls []llm.ToolCall,
) ([]llm.ToolResult, error) {
	results := make([]llm.ToolResult, 0, len(calls))
	for _, call := range calls {
		if err := loop.send(&v1.ChatResponse{
			Event: &v1.ChatResponse_Tool{Tool: &v1.ToolActivity{Label: toolLabel(loop.tools, call.Name, call.Input)}},
		}); err != nil {
			loop.cancel()
			return nil, err
		}
		results = append(results, runAssistantTool(ctx, loop.tools, call))
	}
	return results, nil
}
