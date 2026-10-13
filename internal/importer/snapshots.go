package importer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func snapshotHash(entries []entry, coverage string, record []byte) (string, error) {
	raw, err := json.Marshal(struct {
		Entries  []entry
		Coverage string
		Record   json.RawMessage
	}{entries, coverage, record})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

func (w *leagueWriter) observationUnchanged(ctx context.Context, teamID pgtype.UUID, period int, hash string) (bool, error) {
	previous, err := w.queries.GetLatestObservedSnapshot(ctx, store.GetLatestObservedSnapshotParams{TeamID: teamID, Period: int32(period)})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil && previous.ContentHash == hash, err
}

func (w *leagueWriter) writeSnapshot(ctx context.Context, team store.Team, period int, origin string, entries []entry, coverage string, record []byte) error {
	contentHash, err := snapshotHash(entries, coverage, record)
	if err != nil {
		return err
	}
	sourceKey := fmt.Sprintf("week-%d:%s", period, coverage)
	if origin == "observed" {
		unchanged, err := w.observationUnchanged(ctx, team.ID, period, contentHash)
		if err != nil {
			return err
		}
		if unchanged {
			return nil
		}
		sourceKey = fmt.Sprintf("%x", w.target.run.ID.Bytes)
	}
	snapshot, err := w.queries.UpsertTeamSnapshot(ctx, store.UpsertTeamSnapshotParams{
		TeamID: team.ID, LeagueSeasonID: w.target.league.ID, SyncRunID: w.target.run.ID,
		Period: int32(period), Origin: origin, Coverage: coverage, SourceKey: sourceKey,
		ObservedAt: timestamp(w.data.ObservedAt), RecordJson: record, ContentHash: contentHash,
	})
	if err != nil {
		return err
	}
	return w.replaceSnapshotEntries(ctx, snapshot.ID, entries)
}

func (w *leagueWriter) replaceSnapshotEntries(ctx context.Context, snapshotID pgtype.UUID, entries []entry) error {
	if err := w.queries.DeleteSnapshotEntries(ctx, snapshotID); err != nil {
		return err
	}
	for _, entry := range entries {
		playerID, err := w.resolvePlayer(ctx, entry.ID)
		if err != nil {
			return err
		}
		slot := pgtype.Text{}
		if entry.Slot != "" {
			slot = text(entry.Slot)
		}
		if err = w.queries.AddRosterEntry(ctx, store.AddRosterEntryParams{SnapshotID: snapshotID, PlayerID: playerID, ProviderSlotID: slot, SlotLabel: text(entry.Label), EligiblePositionsJson: []byte("[]")}); err != nil {
			return err
		}
	}
	return nil
}
