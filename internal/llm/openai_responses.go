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

type openAIResponsesStreamEvent struct {
	Delta string `json:"delta"`
	Item  struct {
		Type      string `json:"type"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"item"`
	Response struct {
		Status            string `json:"status"`
		IncompleteDetails struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
	} `json:"response"`
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
	switch event {
	case "response.output_text.delta", "response.output_item.done", "response.completed", "response.incomplete":
	case "response.failed":
		return ErrUnavailable
	default:
		return nil
	}

	var update openAIResponsesStreamEvent
	if json.Unmarshal(data, &update) != nil {
		return ErrUnavailable
	}
	switch event {
	case "response.output_text.delta":
		result.Text += update.Delta
		if onText != nil {
			onText(update.Delta)
		}
	case "response.output_item.done":
		if update.Item.Type != "function_call" {
			return nil
		}
		call, err := decodeOpenAIToolCall(update.Item.CallID, update.Item.Name, update.Item.Arguments)
		if err != nil {
			return err
		}
		result.ToolCalls = append(result.ToolCalls, call)
	case "response.completed":
		result.StopReason = update.Response.Status
	case "response.incomplete":
		result.StopReason = update.Response.IncompleteDetails.Reason
	}
	return nil
}

func openAIResponsesRequest(request Request) map[string]any {
	input := make([]map[string]any, 0)
	for _, message := range request.Messages {
		if message.Role == RoleUser && len(message.ToolResults) > 0 {
			for _, result := range message.ToolResults {
				input = append(input, map[string]any{
					"type": "function_call_output", "call_id": result.CallID, "output": result.Content,
				})
			}
			continue
		}
		switch message.Role {
		case RoleUser:
			input = append(input, map[string]any{"role": "user", "content": message.Text})
		case RoleAssistant:
			if message.Text != "" {
				input = append(input, map[string]any{"role": "assistant", "content": message.Text})
			}
			for _, call := range message.ToolCalls {
				input = append(input, map[string]any{
					"type": "function_call", "call_id": call.ID, "name": call.Name,
					"arguments": string(decodeObject(call.Input)),
				})
			}
		}
	}

	payload := map[string]any{
		"model": request.Model, "instructions": request.System, "input": input,
		"max_output_tokens": request.MaxTokens, "stream": true, "store": false,
	}
	if len(request.Tools) > 0 {
		tools := make([]map[string]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			tools = append(tools, map[string]any{
				"type": "function", "name": tool.Name, "description": tool.Description,
				"parameters": decodeObject(tool.InputSchema),
			})
		}
		payload["tools"] = tools
	}
	return payload
}
