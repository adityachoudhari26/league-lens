package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect/v2"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/llm"
)

const (
	maxActionSummaryRunes     = 200
	maxActionFingerprintRunes = 200
)

type assistantSync interface {
	StartSync(context.Context, *v1.StartSyncRequest) (*v1.StartSyncResponse, error)
}

type syncLeaguesActionInput struct {
	OnlyStale bool `json:"only_stale"`
}

type alertActionInput struct {
	Fingerprint string `json:"fingerprint"`
	Summary     string `json:"summary"`
}

// RunAssistantAction executes an action only after the user confirms its proposal.
func (s *Service) RunAssistantAction(
	ctx context.Context,
	request *v1.RunAssistantActionRequest,
) (*v1.RunAssistantActionResponse, error) {
	if request == nil || request.GetAction() == nil {
		return nil, invalidArgument("an assistant action is required")
	}

	switch action := request.GetAction().GetAction().(type) {
	case *v1.AssistantAction_SyncLeagues:
		return s.startLeagueSync(ctx, action.SyncLeagues.GetOnlyStale())
	case *v1.AssistantAction_DismissAlert:
		return s.dismissAlert(ctx, action.DismissAlert.GetFingerprint())
	case *v1.AssistantAction_RestoreAlert:
		return s.restoreAlert(ctx, action.RestoreAlert.GetFingerprint())
	default:
		return nil, invalidArgument("an assistant action is required")
	}
}

func (s *Service) startLeagueSync(ctx context.Context, onlyStale bool) (*v1.RunAssistantActionResponse, error) {
	if s.Sync == nil {
		return nil, internalError("assistant sync is unavailable")
	}
	response, err := s.Sync.StartSync(ctx, &v1.StartSyncRequest{OnlyStale: onlyStale})
	if err != nil {
		return nil, assistantActionError(err)
	}
	if response == nil {
		return nil, internalError("assistant sync returned no status")
	}

	started := 0
	for _, league := range response.GetLeagues() {
		if league.GetState() == "syncing" {
			started++
		}
	}
	return &v1.RunAssistantActionResponse{Summary: syncActionSummary(started)}, nil
}

func (s *Service) dismissAlert(ctx context.Context, fingerprint string) (*v1.RunAssistantActionResponse, error) {
	if s.Dashboard == nil {
		return nil, internalError("assistant dashboard is unavailable")
	}
	_, err := s.Dashboard.DismissLineupAlert(ctx, &v1.DismissLineupAlertRequest{Fingerprint: fingerprint})
	if err != nil {
		return nil, assistantActionError(err)
	}
	return &v1.RunAssistantActionResponse{Summary: "Dismissed the alert."}, nil
}

func (s *Service) restoreAlert(ctx context.Context, fingerprint string) (*v1.RunAssistantActionResponse, error) {
	if s.Dashboard == nil {
		return nil, internalError("assistant dashboard is unavailable")
	}
	_, err := s.Dashboard.RestoreLineupAlert(ctx, &v1.RestoreLineupAlertRequest{Fingerprint: fingerprint})
	if err != nil {
		return nil, assistantActionError(err)
	}
	return &v1.RunAssistantActionResponse{Summary: "Restored the alert."}, nil
}

func syncActionSummary(started int) string {
	if started == 0 {
		return "No leagues needed refreshing."
	}
	unit := "leagues"
	if started == 1 {
		unit = "league"
	}
	return fmt.Sprintf("Started a refresh of %d %s.", started, unit)
}

func assistantActionError(err error) error {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return err
	}
	return internalError("assistant action could not be completed")
}

func buildActionTools(reader assistantDashboard) []assistantTool {
	return []assistantTool{
		newAssistantActionTool(
			"sync_leagues",
			"Proposes refreshing the owner's imported leagues. only_stale is true to skip leagues with fresh saved data.",
			`{"type":"object","properties":{
				"only_stale":{"type":"boolean","description":"Whether to skip leagues with fresh saved data"}
			},"required":["only_stale"],"additionalProperties":false}`,
			syncLeaguesProposal,
		),
		newAssistantActionTool(
			"dismiss_alert",
			"Proposes dismissing a current alert. fingerprint comes from get_alerts; summary is a short description for the confirmation card.",
			`{"type":"object","properties":{
				"fingerprint":{"type":"string","description":"Fingerprint returned by get_alerts"},
				"summary":{"type":"string","description":"Short human description, at most 200 characters"}
			},"required":["fingerprint","summary"],"additionalProperties":false}`,
			func(ctx context.Context, raw json.RawMessage) (*v1.ActionProposal, error) {
				return dismissAlertProposal(ctx, reader, raw)
			},
		),
		newAssistantActionTool(
			"restore_alert",
			"Proposes restoring a dismissed alert. fingerprint identifies the alert; summary is a short description for the confirmation card.",
			`{"type":"object","properties":{
				"fingerprint":{"type":"string","description":"Fingerprint of the dismissed alert"},
				"summary":{"type":"string","description":"Short human description, at most 200 characters"}
			},"required":["fingerprint","summary"],"additionalProperties":false}`,
			restoreAlertProposal,
		),
	}
}

func newAssistantActionTool(
	name, description, schema string,
	propose func(context.Context, json.RawMessage) (*v1.ActionProposal, error),
) assistantTool {
	return assistantTool{
		definition: llm.Tool{
			Name: name, Description: description, InputSchema: []byte(schema),
		},
		propose: propose,
	}
}

func syncLeaguesProposal(_ context.Context, raw json.RawMessage) (*v1.ActionProposal, error) {
	input, err := decodeToolInput[syncLeaguesActionInput](raw)
	if err != nil {
		return nil, err
	}
	summary := "Refresh all leagues"
	if input.OnlyStale {
		summary = "Refresh stale leagues"
	}
	return &v1.ActionProposal{
		Summary: summary,
		Action: &v1.AssistantAction{
			Action: &v1.AssistantAction_SyncLeagues{
				SyncLeagues: &v1.SyncLeaguesAction{OnlyStale: input.OnlyStale},
			},
		},
	}, nil
}

func dismissAlertProposal(
	ctx context.Context,
	reader assistantDashboard,
	raw json.RawMessage,
) (*v1.ActionProposal, error) {
	input, summary, err := decodeAlertAction(raw)
	if err != nil {
		return nil, err
	}
	alerts, err := reader.GetOverviewAttention(ctx, &v1.GetOverviewAttentionRequest{})
	if err != nil {
		return nil, err
	}
	if !hasAlertFingerprint(alerts, input.Fingerprint) {
		return nil, connect.NewError(connect.CodeNotFound, "alert not found")
	}
	return &v1.ActionProposal{
		Summary: summary,
		Action: &v1.AssistantAction{
			Action: &v1.AssistantAction_DismissAlert{
				DismissAlert: &v1.DismissAlertAction{Fingerprint: input.Fingerprint, Summary: summary},
			},
		},
	}, nil
}

func restoreAlertProposal(_ context.Context, raw json.RawMessage) (*v1.ActionProposal, error) {
	input, summary, err := decodeAlertAction(raw)
	if err != nil {
		return nil, err
	}
	return &v1.ActionProposal{
		Summary: summary,
		Action: &v1.AssistantAction{
			Action: &v1.AssistantAction_RestoreAlert{
				RestoreAlert: &v1.RestoreAlertAction{Fingerprint: input.Fingerprint, Summary: summary},
			},
		},
	}, nil
}

func decodeAlertAction(raw json.RawMessage) (alertActionInput, string, error) {
	input, err := decodeToolInput[alertActionInput](raw)
	if err != nil {
		return input, "", err
	}
	if err := validateActionFingerprint(input.Fingerprint); err != nil {
		return input, "", err
	}
	summary, err := boundedActionSummary(input.Summary)
	if err != nil {
		return input, "", err
	}
	return input, summary, nil
}

func validateActionFingerprint(fingerprint string) error {
	if strings.TrimSpace(fingerprint) == "" || len([]rune(fingerprint)) > maxActionFingerprintRunes {
		return errInvalidToolInput
	}
	return nil
}

func boundedActionSummary(summary string) (string, error) {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return "", errInvalidToolInput
	}
	runes := []rune(summary)
	if len(runes) > maxActionSummaryRunes {
		summary = string(runes[:maxActionSummaryRunes])
	}
	return summary, nil
}

func hasAlertFingerprint(response *v1.GetOverviewAttentionResponse, fingerprint string) bool {
	if response == nil {
		return false
	}
	for _, alert := range response.GetAlerts() {
		if alert.GetFingerprint() == fingerprint {
			return true
		}
	}
	return false
}
