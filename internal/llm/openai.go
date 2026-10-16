package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
)

type openAIProvider struct {
	client  *http.Client
	baseURL string
	apiKey  string
}

// ListModels fetches model IDs from an OpenAI-compatible models endpoint.
func (p *openAIProvider) ListModels(ctx context.Context) ([]string, error) {
	response, err := sendRequest(ctx, p.client, http.MethodGet, p.baseURL+"/models", nil, p.headers())
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := decodeJSON(response, &result); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(result.Data))
	for _, model := range result.Data {
		models = append(models, model.ID)
	}
	return models, nil
}

// Complete streams chat-completion deltas and joins fragmented tool calls.
func (p *openAIProvider) Complete(ctx context.Context, request Request, onText func(string)) (Response, error) {
	body, err := json.Marshal(openAIRequest(request))
	if err != nil {
		return Response{}, ErrUnavailable
	}
	response, err := sendRequest(ctx, p.client, http.MethodPost, p.baseURL+"/chat/completions", body, p.headers())
	if err != nil {
		return Response{}, err
	}

	var result Response
	toolCalls := make(map[int]*openAIToolCall)
	err = readSSE(response, func(event string, data []byte) error {
		if string(data) == "[DONE]" {
			return nil
		}
		if err := streamError(event, data); err != nil {
			return err
		}
		var chunk openAIStreamChunk
		if json.Unmarshal(data, &chunk) != nil {
			return ErrUnavailable
		}
		applyOpenAIChunk(&result, toolCalls, chunk, onText)
		return nil
	})
	if err != nil {
		return Response{}, err
	}
	result.ToolCalls, err = collectOpenAIToolCalls(toolCalls)
	if err != nil {
		return Response{}, err
	}
	return result, nil
}

type openAIStreamChunk struct {
	Choices []openAIStreamChoice `json:"choices"`
}

type openAIStreamChoice struct {
	Delta        openAIStreamDelta `json:"delta"`
	FinishReason string            `json:"finish_reason"`
}

type openAIStreamDelta struct {
	Content   string                 `json:"content"`
	ToolCalls []openAIStreamToolCall `json:"tool_calls"`
}

type openAIStreamToolCall struct {
	Index    int                  `json:"index"`
	ID       string               `json:"id"`
	Function openAIStreamFunction `json:"function"`
}

type openAIStreamFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func applyOpenAIChunk(
	result *Response,
	toolCalls map[int]*openAIToolCall,
	chunk openAIStreamChunk,
	onText func(string),
) {
	if len(chunk.Choices) == 0 {
		return
	}
	choice := chunk.Choices[0]
	if choice.Delta.Content != "" {
		result.Text += choice.Delta.Content
		if onText != nil {
			onText(choice.Delta.Content)
		}
	}
	for _, updateCall := range choice.Delta.ToolCalls {
		call := toolCalls[updateCall.Index]
		if call == nil {
			call = &openAIToolCall{}
			toolCalls[updateCall.Index] = call
		}
		if updateCall.ID != "" {
			call.id = updateCall.ID
		}
		if updateCall.Function.Name != "" {
			call.name = updateCall.Function.Name
		}
		call.arguments.WriteString(updateCall.Function.Arguments)
	}
	if choice.FinishReason != "" {
		result.StopReason = choice.FinishReason
	}
}

func collectOpenAIToolCalls(toolCalls map[int]*openAIToolCall) ([]ToolCall, error) {
	calls := make([]ToolCall, 0, len(toolCalls))
	for _, index := range sortedOpenAIToolIndexes(toolCalls) {
		call := toolCalls[index]
		arguments := call.arguments.String()
		if arguments == "" {
			arguments = "{}"
		}
		if !json.Valid([]byte(arguments)) {
			return nil, ErrUnavailable
		}
		calls = append(calls, ToolCall{
			ID: call.id, Name: call.name, Input: json.RawMessage(arguments),
		})
	}
	return calls, nil
}

type openAIToolCall struct {
	id        string
	name      string
	arguments strings.Builder
}

func (p *openAIProvider) headers() http.Header {
	headers := make(http.Header)
	headers.Set("content-type", "application/json")
	if p.apiKey != "" {
		headers.Set("Authorization", "Bearer "+p.apiKey)
	}
	return headers
}

func openAIRequest(request Request) map[string]any {
	messages := []map[string]any{{"role": "system", "content": request.System}}
	for _, message := range request.Messages {
		if message.Role == RoleUser && len(message.ToolResults) > 0 {
			for _, result := range message.ToolResults {
				messages = append(messages, map[string]any{
					"role": "tool", "tool_call_id": result.CallID, "content": result.Content,
				})
			}
			continue
		}
		encoded := map[string]any{"role": string(message.Role), "content": message.Text}
		if len(message.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				calls = append(calls, map[string]any{
					"id": call.ID, "type": "function",
					"function": map[string]string{
						"name": call.Name, "arguments": string(decodeObject(call.Input)),
					},
				})
			}
			encoded["tool_calls"] = calls
		}
		messages = append(messages, encoded)
	}
	payload := map[string]any{
		"model": request.Model, "stream": true, "messages": messages,
		"max_tokens": request.MaxTokens,
	}
	if len(request.Tools) > 0 {
		tools := make([]map[string]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{
				"name": tool.Name, "description": tool.Description,
				"parameters": decodeObject(tool.InputSchema),
			}})
		}
		payload["tools"] = tools
	}
	return payload
}

func sortedOpenAIToolIndexes(calls map[int]*openAIToolCall) []int {
	indexes := make([]int, 0, len(calls))
	for index := range calls {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	return indexes
}
