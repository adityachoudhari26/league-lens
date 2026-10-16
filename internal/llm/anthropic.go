package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
)

const anthropicBaseURL = "https://api.anthropic.com/v1"

type anthropicProvider struct {
	client *http.Client
	apiKey string
}

// ListModels fetches model IDs from Anthropic's public models endpoint.
func (p *anthropicProvider) ListModels(ctx context.Context) ([]string, error) {
	headers := p.headers()
	response, err := sendRequest(ctx, p.client, http.MethodGet, anthropicBaseURL+"/models?limit=100", nil, headers)
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

// Complete streams an Anthropic message and reconstructs any tool-use inputs.
func (p *anthropicProvider) Complete(ctx context.Context, request Request, onText func(string)) (Response, error) {
	body, err := json.Marshal(anthropicRequest(request))
	if err != nil {
		return Response{}, ErrUnavailable
	}
	response, err := sendRequest(ctx, p.client, http.MethodPost, anthropicBaseURL+"/messages", body, p.headers())
	if err != nil {
		return Response{}, err
	}

	var result Response
	toolInputs := make(map[int]*anthropicToolInput)
	err = readSSE(response, func(event string, data []byte) error {
		return applyAnthropicEvent(&result, toolInputs, event, data, onText)
	})
	if err != nil {
		return Response{}, err
	}
	for _, index := range sortedToolIndexes(toolInputs) {
		input := toolInputs[index].input.String()
		if input == "" {
			input = "{}"
		}
		if !json.Valid([]byte(input)) {
			return Response{}, ErrUnavailable
		}
		toolInputs[index].call.Input = json.RawMessage(input)
		result.ToolCalls = append(result.ToolCalls, toolInputs[index].call)
	}
	return result, nil
}

type anthropicStreamEvent struct {
	Index        int                   `json:"index"`
	ContentBlock anthropicContentBlock `json:"content_block"`
	Delta        anthropicEventDelta   `json:"delta"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type anthropicEventDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	PartialJSON string `json:"partial_json"`
	StopReason  string `json:"stop_reason"`
}

func applyAnthropicEvent(
	result *Response,
	toolInputs map[int]*anthropicToolInput,
	event string,
	data []byte,
	onText func(string),
) error {
	if err := streamError(event, data); err != nil {
		return err
	}
	switch event {
	case "content_block_start":
		var update anthropicStreamEvent
		if json.Unmarshal(data, &update) != nil {
			return ErrUnavailable
		}
		if update.ContentBlock.Type == "tool_use" {
			toolInputs[update.Index] = &anthropicToolInput{
				call: ToolCall{ID: update.ContentBlock.ID, Name: update.ContentBlock.Name},
			}
		}
	case "content_block_delta":
		var update anthropicStreamEvent
		if json.Unmarshal(data, &update) != nil {
			return ErrUnavailable
		}
		if update.Delta.Type == "text_delta" {
			result.Text += update.Delta.Text
			if onText != nil {
				onText(update.Delta.Text)
			}
		}
		if update.Delta.Type == "input_json_delta" && toolInputs[update.Index] != nil {
			toolInputs[update.Index].input.WriteString(update.Delta.PartialJSON)
		}
	case "message_delta":
		var update anthropicStreamEvent
		if json.Unmarshal(data, &update) != nil {
			return ErrUnavailable
		}
		result.StopReason = update.Delta.StopReason
	}
	return nil
}

type anthropicToolInput struct {
	call  ToolCall
	input strings.Builder
}

func (p *anthropicProvider) headers() http.Header {
	headers := make(http.Header)
	headers.Set("x-api-key", p.apiKey)
	headers.Set("anthropic-version", "2023-06-01")
	headers.Set("content-type", "application/json")
	return headers
}

func anthropicRequest(request Request) map[string]any {
	payload := map[string]any{
		"model": request.Model, "max_tokens": request.MaxTokens,
		"system": request.System, "stream": true,
		"messages": anthropicMessages(request.Messages),
	}
	if len(request.Tools) > 0 {
		tools := make([]map[string]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			tools = append(tools, map[string]any{
				"name": tool.Name, "description": tool.Description,
				"input_schema": decodeObject(tool.InputSchema),
			})
		}
		payload["tools"] = tools
	}
	return payload
}

func anthropicMessages(messages []Message) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		content := make([]map[string]any, 0)
		if message.Text != "" {
			content = append(content, map[string]any{"type": "text", "text": message.Text})
		}
		for _, call := range message.ToolCalls {
			var input any
			_ = json.Unmarshal(decodeObject(call.Input), &input)
			content = append(content, map[string]any{
				"type": "tool_use", "id": call.ID, "name": call.Name, "input": input,
			})
		}
		for _, toolResult := range message.ToolResults {
			content = append(content, map[string]any{
				"type": "tool_result", "tool_use_id": toolResult.CallID,
				"content": toolResult.Content, "is_error": toolResult.IsError,
			})
		}
		result = append(result, map[string]any{"role": string(message.Role), "content": content})
	}
	return result
}

func sortedToolIndexes(inputs map[int]*anthropicToolInput) []int {
	indexes := make([]int, 0, len(inputs))
	for index := range inputs {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	return indexes
}
