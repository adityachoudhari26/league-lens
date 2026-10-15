package leaguesync

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
)

// Handler exposes a Runner as the Connect SyncService.
type Handler struct {
	runner *Runner
}

func NewHandler(runner *Runner) *Handler {
	return &Handler{runner: runner}
}

// StartSync begins background provider syncs and returns immediately.
func (h *Handler) StartSync(ctx context.Context, request *v1.StartSyncRequest) (*v1.StartSyncResponse, error) {
	statuses, err := h.runner.Start(ctx, request.GetOnlyStale())
	if err != nil {
		return nil, syncError(err)
	}
	return &v1.StartSyncResponse{Leagues: statusMessages(statuses)}, nil
}

func (h *Handler) GetSyncStatus(ctx context.Context, _ *v1.GetSyncStatusRequest) (*v1.GetSyncStatusResponse, error) {
	statuses, err := h.runner.Status(ctx)
	if err != nil {
		return nil, syncError(err)
	}
	return &v1.GetSyncStatusResponse{Leagues: statusMessages(statuses)}, nil
}

func statusMessages(statuses []Status) []*v1.LeagueSyncStatus {
	messages := make([]*v1.LeagueSyncStatus, 0, len(statuses))
	for _, status := range statuses {
		league := status.Target.Selection.League
		messages = append(messages, &v1.LeagueSyncStatus{
			LeagueSeasonId:       status.Target.ID,
			LeagueName:           status.Target.Name,
			Provider:             string(league.Provider),
			Season:               int32(league.Season),
			State:                status.State,
			LastSuccessfulSyncAt: timestamp(status.Target.LastSuccessfulSyncAt),
			FailedAt:             timestamp(status.FailedAt),
			ErrorCode:            status.ErrorCode,
		})
	}
	return messages
}

func timestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}

func syncError(err error) error {
	if errors.Is(err, context.Canceled) {
		return connect.NewError(connect.CodeCanceled, "request canceled")
	}
	return connect.NewError(connect.CodeUnavailable, "could not read sync status")
}
