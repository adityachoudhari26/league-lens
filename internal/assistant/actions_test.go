package assistant

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect/v2"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/llm"
)

type fakeAssistantSync struct {
	request  *v1.StartSyncRequest
	response *v1.StartSyncResponse
	err      error
}

func (s *fakeAssistantSync) StartSync(_ context.Context, request *v1.StartSyncRequest) (*v1.StartSyncResponse, error) {
	s.request = request
	return s.response, s.err
}

func TestRunAssistantActionExecutesEachConfirmedAction(t *testing.T) {
	dashboard := &fakeAssistantDashboard{}
	syncHandler := &fakeAssistantSync{response: &v1.StartSyncResponse{Leagues: []*v1.LeagueSyncStatus{
		{State: "syncing"}, {State: "succeeded"}, {State: "syncing"},
	}}}
	service := &Service{Dashboard: dashboard, Sync: syncHandler}

	synced, err := service.RunAssistantAction(context.Background(), &v1.RunAssistantActionRequest{
		Action: &v1.AssistantAction{Action: &v1.AssistantAction_SyncLeagues{
			SyncLeagues: &v1.SyncLeaguesAction{OnlyStale: true},
		}},
	})
	if err != nil || syncHandler.request.GetOnlyStale() != true ||
		synced.GetSummary() != "Started a refresh of 2 leagues." {
		t.Fatalf("sync result = %v, request = %v, error = %v", synced, syncHandler.request, err)
	}

	dismissed, err := service.RunAssistantAction(context.Background(), &v1.RunAssistantActionRequest{
		Action: &v1.AssistantAction{Action: &v1.AssistantAction_DismissAlert{
			DismissAlert: &v1.DismissAlertAction{Fingerprint: "alert:week1"},
		}},
	})
	if err != nil || dashboard.dismissAlertRequest.GetFingerprint() != "alert:week1" ||
		dismissed.GetSummary() != "Dismissed the alert." {
		t.Fatalf("dismiss result = %v, request = %v, error = %v", dismissed, dashboard.dismissAlertRequest, err)
	}

	restored, err := service.RunAssistantAction(context.Background(), &v1.RunAssistantActionRequest{
		Action: &v1.AssistantAction{Action: &v1.AssistantAction_RestoreAlert{
			RestoreAlert: &v1.RestoreAlertAction{Fingerprint: "alert:week2"},
		}},
	})
	if err != nil || dashboard.restoreAlertRequest.GetFingerprint() != "alert:week2" ||
		restored.GetSummary() != "Restored the alert." {
		t.Fatalf("restore result = %v, request = %v, error = %v", restored, dashboard.restoreAlertRequest, err)
	}
}

func TestRunAssistantActionValidatesAndSanitizesFailures(t *testing.T) {
	service := &Service{Sync: &fakeAssistantSync{err: errors.New("private provider detail")}}
	if _, err := service.RunAssistantAction(context.Background(), nil); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("nil request error = %v", err)
	}
	_, err := service.RunAssistantAction(context.Background(), &v1.RunAssistantActionRequest{
		Action: &v1.AssistantAction{Action: &v1.AssistantAction_SyncLeagues{
			SyncLeagues: &v1.SyncLeaguesAction{},
		}},
	})
	if connect.CodeOf(err) != connect.CodeInternal || err.Error() == "private provider detail" {
		t.Fatalf("sync failure error = %v", err)
	}
}

func TestRunAssistantActionRecordsSuccessfulOutcome(t *testing.T) {
	store := &fakeConversationStore{}
	service := &Service{Dashboard: &fakeAssistantDashboard{}, conversations: store}

	response, err := service.RunAssistantAction(context.Background(), dismissActionRequest(actionTestConversationID))
	if err != nil || response.GetSummary() != "Dismissed the alert." {
		t.Fatalf("action response = %v, error = %v", response, err)
	}
	assertChatTestSavedTurn(t, store, actionTestConversationID, []llm.Message{
		{Role: llm.RoleUser, Text: "I confirmed the proposed action."},
		{Role: llm.RoleAssistant, Text: "Dismissed the alert."},
	})
}

func TestRunAssistantActionRecordsFailureAndReturnsActionError(t *testing.T) {
	want := connect.NewError(connect.CodeUnavailable, "action unavailable")
	store := &fakeConversationStore{}
	service := &Service{
		Dashboard:     &fakeAssistantDashboard{dismissAlertErr: want},
		conversations: store,
	}

	response, err := service.RunAssistantAction(context.Background(), dismissActionRequest(actionTestConversationID))
	if response != nil || !errors.Is(err, want) {
		t.Fatalf("action response = %v, error = %v; want original error %v", response, err, want)
	}
	assertChatTestSavedTurn(t, store, actionTestConversationID, []llm.Message{
		{Role: llm.RoleUser, Text: "I confirmed the proposed action."},
		{Role: llm.RoleAssistant, Text: "The action failed."},
	})
}

func TestRunAssistantActionIgnoresOutcomeSaveFailure(t *testing.T) {
	store := &fakeConversationStore{saveErr: errors.New("storage unavailable")}
	service := &Service{Dashboard: &fakeAssistantDashboard{}, conversations: store}

	response, err := service.RunAssistantAction(context.Background(), dismissActionRequest(actionTestConversationID))
	if err != nil || response.GetSummary() != "Dismissed the alert." {
		t.Fatalf("action response = %v, error = %v", response, err)
	}
	if store.saveCalls != 1 {
		t.Fatalf("outcome save calls = %d, want one", store.saveCalls)
	}
}

func TestRunAssistantActionRejectsInvalidConversationBeforeRunning(t *testing.T) {
	dashboard := &fakeAssistantDashboard{}
	store := &fakeConversationStore{}
	service := &Service{Dashboard: dashboard, conversations: store}

	_, err := service.RunAssistantAction(context.Background(), dismissActionRequest("invalid"))
	if connect.CodeOf(err) != connect.CodeInvalidArgument || dashboard.dismissAlertRequest != nil ||
		store.saveCalls != 0 {
		t.Fatalf("error=%v action request=%v save calls=%d", err, dashboard.dismissAlertRequest, store.saveCalls)
	}
}

func TestRunAssistantActionWithoutConversationDoesNotSave(t *testing.T) {
	store := &fakeConversationStore{}
	service := &Service{Dashboard: &fakeAssistantDashboard{}, conversations: store}

	response, err := service.RunAssistantAction(context.Background(), dismissActionRequest(""))
	if err != nil || response.GetSummary() != "Dismissed the alert." || store.saveCalls != 0 {
		t.Fatalf("action response=%v error=%v save calls=%d", response, err, store.saveCalls)
	}
}

const actionTestConversationID = "00000000-0000-0000-0000-000000000001"

func dismissActionRequest(conversationID string) *v1.RunAssistantActionRequest {
	return &v1.RunAssistantActionRequest{
		ConversationId: conversationID,
		Action: &v1.AssistantAction{Action: &v1.AssistantAction_DismissAlert{
			DismissAlert: &v1.DismissAlertAction{Fingerprint: "alert:week1"},
		}},
	}
}

func TestActionToolsValidateProposals(t *testing.T) {
	reader := &fakeAssistantDashboard{alerts: []*v1.LineupAlert{{Fingerprint: "current-alert"}}}
	tools := buildActionTools(reader)
	syncTool := findAssistantToolByName(tools, "sync_leagues")
	proposal, err := syncTool.propose(context.Background(), []byte(`{"only_stale":false}`))
	if err != nil || proposal.GetSummary() != "Refresh all leagues" ||
		proposal.GetAction().GetSyncLeagues().GetOnlyStale() {
		t.Fatalf("sync proposal = %v, error = %v", proposal, err)
	}

	dismissTool := findAssistantToolByName(tools, "dismiss_alert")
	proposal, err = dismissTool.propose(context.Background(), []byte(`{"fingerprint":"current-alert","summary":"Dismiss alert"}`))
	if err != nil || proposal.GetAction().GetDismissAlert().GetFingerprint() != "current-alert" {
		t.Fatalf("dismiss proposal = %v, error = %v", proposal, err)
	}
	_, err = dismissTool.propose(context.Background(), []byte(`{"fingerprint":"stale-alert","summary":"Dismiss alert"}`))
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("stale fingerprint error = %v", err)
	}

	restoreTool := findAssistantToolByName(tools, "restore_alert")
	proposal, err = restoreTool.propose(context.Background(), []byte(`{"fingerprint":"dismissed-alert","summary":"`+
		repeatActionSummary("é", maxActionSummaryRunes+5)+`"}`))
	if err != nil || len([]rune(proposal.GetSummary())) != maxActionSummaryRunes {
		t.Fatalf("bounded restore summary = %v, error = %v", proposal, err)
	}
}

func TestActionToolLabelsDescribeProposals(t *testing.T) {
	tools := buildActionTools(&fakeAssistantDashboard{})
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "sync_leagues", input: `{"only_stale":true}`,
			want: "Proposed: Refresh stale leagues",
		},
		{
			name: "sync_leagues", input: `{"only_stale":false}`,
			want: "Proposed: Refresh all leagues",
		},
		{
			name: "dismiss_alert", input: `{"fingerprint":"alert","summary":"Dismiss alert"}`,
			want: "Proposed: Dismiss alert",
		},
		{
			name: "restore_alert", input: `{"fingerprint":"alert","summary":"Restore alert"}`,
			want: "Proposed: Restore alert",
		},
		{name: "sync_leagues", input: `{invalid}`, want: "Proposed an action"},
		{name: "dismiss_alert", input: `{invalid}`, want: "Proposed an action"},
		{name: "restore_alert", input: `{invalid}`, want: "Proposed an action"},
	}
	for _, test := range cases {
		t.Run(test.name+"/"+test.want, func(t *testing.T) {
			got := toolLabel(tools, test.name, []byte(test.input))
			if got != test.want {
				t.Fatalf("tool label = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRunAssistantToolWithoutRunnerReturnsActionFallback(t *testing.T) {
	result := runAssistantTool(context.Background(), []assistantTool{{
		definition: llm.Tool{Name: "action"},
	}}, llm.ToolCall{ID: "call", Name: "action"})
	if !result.IsError || result.Content != "couldn't prepare the requested action" {
		t.Fatalf("action tool result = %+v", result)
	}
}

func repeatActionSummary(value string, count int) string {
	result := ""
	for range count {
		result += value
	}
	return result
}
