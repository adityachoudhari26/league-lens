package assistant

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect/v2"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
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

func repeatActionSummary(value string, count int) string {
	result := ""
	for range count {
		result += value
	}
	return result
}
