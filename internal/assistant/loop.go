package assistant

import (
	"context"
	"time"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/llm"
)

const maxToolRounds = 8

type chatLoop struct {
	cancel   context.CancelFunc
	provider llm.Provider
	model    string
	send     chatEventSender
	tools    []assistantTool
}

// run completes a chat with sequential, bounded read-only tool rounds.
func (loop *chatLoop) run(ctx context.Context, page *v1.PageContext, messages []llm.Message) error {
	system := buildSystemPrompt(time.Now(), page)
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
			return loop.finishAtToolLimit(response)
		}
		messages = append(messages, llm.Message{
			Role: llm.RoleAssistant, Text: response.Text, ToolCalls: response.ToolCalls,
		})
		proposal, actionErrors := loop.proposeAction(ctx, response.ToolCalls)
		if proposal != nil {
			return loop.sendEvent(&v1.ChatResponse{
				Event: &v1.ChatResponse_Proposal{Proposal: proposal},
			})
		}
		results, err := loop.runToolCalls(ctx, response.ToolCalls, actionErrors)
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
		sendErr = loop.sendEvent(&v1.ChatResponse{
			Event: &v1.ChatResponse_Text{Text: &v1.TextDelta{Text: delta}},
		})
	})
	if sendErr != nil {
		return llm.Response{}, sendErr
	}
	if completionErr != nil {
		return llm.Response{}, mapProviderError(completionErr)
	}
	return response, nil
}

func (loop *chatLoop) finishAtToolLimit(response llm.Response) error {
	if response.Text != "" {
		return nil
	}
	return loop.sendEvent(&v1.ChatResponse{
		Event: &v1.ChatResponse_Text{Text: &v1.TextDelta{
			Text: "I couldn't finish looking that up. Try a narrower question.",
		}},
	})
}

func (loop *chatLoop) sendEvent(event *v1.ChatResponse) error {
	err := loop.send(event)
	if err != nil {
		loop.cancel()
	}
	return err
}

func (loop *chatLoop) proposeAction(
	ctx context.Context,
	calls []llm.ToolCall,
) (*v1.ActionProposal, map[string]llm.ToolResult) {
	actionErrors := make(map[string]llm.ToolResult)
	for _, call := range calls {
		tool := findAssistantToolByName(loop.tools, call.Name)
		if tool == nil || tool.propose == nil {
			continue
		}
		proposal, err := tool.propose(ctx, call.Input)
		if err != nil {
			actionErrors[call.ID] = llm.ToolResult{
				CallID: call.ID, Content: toolErrorMessage(err), IsError: true,
			}
			continue
		}
		return proposal, nil
	}
	return nil, actionErrors
}

func (loop *chatLoop) runToolCalls(
	ctx context.Context,
	calls []llm.ToolCall,
	actionErrors map[string]llm.ToolResult,
) ([]llm.ToolResult, error) {
	results := make([]llm.ToolResult, 0, len(calls))
	for _, call := range calls {
		if result, invalidAction := actionErrors[call.ID]; invalidAction {
			results = append(results, result)
			continue
		}
		if err := loop.sendEvent(&v1.ChatResponse{
			Event: &v1.ChatResponse_Tool{Tool: &v1.ToolActivity{Label: toolLabel(loop.tools, call.Name, call.Input)}},
		}); err != nil {
			return nil, err
		}
		results = append(results, runAssistantTool(ctx, loop.tools, call))
	}
	return results, nil
}
