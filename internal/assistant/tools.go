package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/llm"
)

const (
	maxToolResultBytes  = 12_000
	truncatedToolSuffix = "…(truncated)"
)

var errInvalidToolInput = errors.New("invalid tool input")

type assistantDashboard interface {
	ListLeagues(context.Context, *v1.ListLeaguesRequest) (*v1.ListLeaguesResponse, error)
	GetLeague(context.Context, *v1.GetLeagueRequest) (*v1.GetLeagueResponse, error)
	GetTeamRoster(context.Context, *v1.GetTeamRosterRequest) (*v1.GetTeamRosterResponse, error)
	ListMatchups(context.Context, *v1.ListMatchupsRequest) (*v1.ListMatchupsResponse, error)
	GetMatchupDetail(context.Context, *v1.GetMatchupDetailRequest) (*v1.GetMatchupDetailResponse, error)
	GetMatchupWinChance(context.Context, *v1.GetMatchupWinChanceRequest) (*v1.GetMatchupWinChanceResponse, error)
	GetOverviewAttention(context.Context, *v1.GetOverviewAttentionRequest) (*v1.GetOverviewAttentionResponse, error)
	DismissLineupAlert(context.Context, *v1.DismissLineupAlertRequest) (*v1.DismissLineupAlertResponse, error)
	RestoreLineupAlert(context.Context, *v1.RestoreLineupAlertRequest) (*v1.RestoreLineupAlertResponse, error)
	GetOverviewPlayerUpdates(context.Context, *v1.GetOverviewPlayerUpdatesRequest) (*v1.GetOverviewPlayerUpdatesResponse, error)
	GetPlayerCard(context.Context, *v1.GetPlayerCardRequest) (*v1.GetPlayerCardResponse, error)
	GetLiveGames(context.Context, *v1.GetLiveGamesRequest) (*v1.GetLiveGamesResponse, error)
}

type assistantTool struct {
	definition llm.Tool
	label      func(json.RawMessage) string
	run        func(context.Context, json.RawMessage) (string, error)
	propose    func(context.Context, json.RawMessage) (*v1.ActionProposal, error)
}

func buildReadOnlyTools(reader assistantDashboard) []assistantTool {
	tools := append([]assistantTool{}, leagueTools(reader)...)
	tools = append(tools, matchupTools(reader)...)
	tools = append(tools, overviewTools(reader)...)
	tools = append(tools, playerTools(reader)...)
	return tools
}

func buildAssistantTools(reader assistantDashboard) []assistantTool {
	tools := buildReadOnlyTools(reader)
	return append(tools, buildActionTools(reader)...)
}

func toolDefinitions(tools []assistantTool) []llm.Tool {
	definitions := make([]llm.Tool, len(tools))
	for index, tool := range tools {
		definitions[index] = tool.definition
	}
	return definitions
}

func toolLabel(tools []assistantTool, name string, input json.RawMessage) string {
	for _, tool := range tools {
		if tool.definition.Name == name {
			return tool.label(input)
		}
	}
	return "Checking requested data"
}

func findAssistantToolByName(tools []assistantTool, name string) *assistantTool {
	for index := range tools {
		if tools[index].definition.Name == name {
			return &tools[index]
		}
	}
	return nil
}

func runAssistantTool(ctx context.Context, tools []assistantTool, call llm.ToolCall) llm.ToolResult {
	for _, tool := range tools {
		if tool.definition.Name != call.Name {
			continue
		}
		if tool.run == nil {
			return llm.ToolResult{
				CallID: call.ID, Content: "couldn't prepare the requested action", IsError: true,
			}
		}
		content, err := tool.run(ctx, call.Input)
		if err != nil {
			return llm.ToolResult{CallID: call.ID, Content: toolErrorMessage(err), IsError: true}
		}
		return llm.ToolResult{CallID: call.ID, Content: content}
	}
	return llm.ToolResult{CallID: call.ID, Content: "unknown tool", IsError: true}
}

func newAssistantTool(
	name, description, schema string,
	label func(json.RawMessage) string,
	run func(context.Context, json.RawMessage) (string, error),
) assistantTool {
	return assistantTool{
		definition: llm.Tool{
			Name: name, Description: description, InputSchema: json.RawMessage(schema),
		},
		label: label,
		run:   run,
	}
}

func readTool[In any, Out proto.Message](
	name, description, schema string,
	label func(json.RawMessage) string,
	call func(context.Context, In) (Out, error),
) assistantTool {
	return newAssistantTool(name, description, schema, label, func(ctx context.Context, raw json.RawMessage) (string, error) {
		input, err := decodeToolInput[In](raw)
		if err != nil {
			return "", err
		}
		response, err := call(ctx, input)
		if err != nil {
			return "", err
		}
		return marshalToolResponse(response)
	})
}

func decodeToolInput[T any](raw json.RawMessage) (T, error) {
	var input T
	if !json.Valid(raw) {
		return input, errInvalidToolInput
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, errInvalidToolInput
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return input, errInvalidToolInput
	}
	return input, nil
}

func marshalToolResponse(response proto.Message) (string, error) {
	content, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(response)
	if err != nil {
		return "", err
	}
	return truncateToolResult(string(content)), nil
}

func truncateToolResult(content string) string {
	if len(content) <= maxToolResultBytes {
		return content
	}
	cut := maxToolResultBytes - len(truncatedToolSuffix)
	for cut > 0 && !utf8.ValidString(content[:cut]) {
		cut--
	}
	return content[:cut] + truncatedToolSuffix
}

func toolErrorMessage(err error) string {
	if errors.Is(err, errInvalidToolInput) || connect.CodeOf(err) == connect.CodeInvalidArgument {
		return "invalid tool input"
	}
	if connect.CodeOf(err) == connect.CodeNotFound {
		return "requested data not found"
	}
	return "couldn't load the requested data"
}
