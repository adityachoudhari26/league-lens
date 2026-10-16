package llm

import (
	"context"
	"encoding/json"
	"net/http"
)

type openAIResponsesProvider struct {
	openAIProvider
}

// Complete streams text and tool calls from OpenAI's Responses API.
func (p *openAIResponsesProvider) Complete(
	ctx context.Context,
	request Request,
	onText func(string),
) (Response, error) {
	body, err := json.Marshal(openAIResponsesRequest(request))
	if err != nil {
		return Response{}, ErrUnavailable
	}
	response, err := sendRequest(ctx, p.client, http.MethodPost, p.baseURL+"/responses", body, p.headers())
	if err != nil {
		return Response{}, err
	}

	var result Response
	err = readSSE(response, func(event string, data []byte) error {
		return applyOpenAIResponsesEvent(&result, event, data, onText)
	})
	if err != nil {
		return Response{}, err
	}
	return result, nil
}

type openAIResponsesStreamItem struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIResponsesStreamEvent struct {
	Delta    string                    `json:"delta"`
	Item     openAIResponsesStreamItem `json:"item"`
	Response struct {
		Status            string `json:"status"`
		IncompleteDetails struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
	} `json:"response"`
}

var openAIResponsesEvents = map[string]struct{}{
	"response.output_text.delta": {},
	"response.output_item.done":  {},
	"response.completed":         {},
	"response.incomplete":        {},
}

func applyOpenAIResponsesEvent(
	result *Response,
	event string,
	data []byte,
	onText func(string),
) error {
	if err := streamError(event, data); err != nil {
		return err
	}
	if event == "response.failed" {
		return ErrUnavailable
	}
	if _, ok := openAIResponsesEvents[event]; !ok {
		return nil
	}

	var update openAIResponsesStreamEvent
	if json.Unmarshal(data, &update) != nil {
		return ErrUnavailable
	}
	return applyOpenAIResponsesUpdate(result, event, update, onText)
}

func applyOpenAIResponsesUpdate(
	result *Response,
	event string,
	update openAIResponsesStreamEvent,
	onText func(string),
) error {
	switch event {
	case "response.output_text.delta":
		result.Text += update.Delta
		if onText != nil {
			onText(update.Delta)
		}
	case "response.output_item.done":
		return appendOpenAIResponsesFunctionCall(result, update.Item)
	case "response.completed":
		result.StopReason = update.Response.Status
	case "response.incomplete":
		result.StopReason = update.Response.IncompleteDetails.Reason
	}
	return nil
}

func appendOpenAIResponsesFunctionCall(result *Response, item openAIResponsesStreamItem) error {
	if item.Type != "function_call" {
		return nil
	}
	call, err := decodeOpenAIToolCall(item.CallID, item.Name, item.Arguments)
	if err != nil {
		return err
	}
	result.ToolCalls = append(result.ToolCalls, call)
	return nil
}

func openAIResponsesRequest(request Request) map[string]any {
	payload := map[string]any{
		"model": request.Model, "instructions": request.System, "input": openAIResponsesInput(request.Messages),
		"max_output_tokens": request.MaxTokens, "stream": true, "store": false,
	}
	if len(request.Tools) > 0 {
		payload["tools"] = openAIResponsesTools(request.Tools)
	}
	return payload
}

func openAIResponsesInput(messages []Message) []map[string]any {
	input := make([]map[string]any, 0)
	for _, message := range messages {
		input = append(input, openAIResponsesItems(message)...)
	}
	return input
}

func openAIResponsesItems(message Message) []map[string]any {
	if message.Role == RoleUser && len(message.ToolResults) > 0 {
		items := make([]map[string]any, 0, len(message.ToolResults))
		for _, result := range message.ToolResults {
			items = append(items, map[string]any{
				"type": "function_call_output", "call_id": result.CallID, "output": result.Content,
			})
		}
		return items
	}

	items := make([]map[string]any, 0, 1+len(message.ToolCalls))
	switch message.Role {
	case RoleUser:
		items = append(items, map[string]any{"role": "user", "content": message.Text})
	case RoleAssistant:
		if message.Text != "" {
			items = append(items, map[string]any{"role": "assistant", "content": message.Text})
		}
		for _, call := range message.ToolCalls {
			items = append(items, map[string]any{
				"type": "function_call", "call_id": call.ID, "name": call.Name,
				"arguments": string(decodeObject(call.Input)),
			})
		}
	}
	return items
}

func openAIResponsesTools(tools []Tool) []map[string]any {
	items := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		items = append(items, map[string]any{
			"type": "function", "name": tool.Name, "description": tool.Description,
			"parameters": decodeObject(tool.InputSchema),
		})
	}
	return items
}
