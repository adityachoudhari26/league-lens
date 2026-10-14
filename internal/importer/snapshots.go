package importer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"sort"
)

func snapshotHash(snapshot fantasy.RosterSnapshot) (string, error) {
	entries := append([]fantasy.RosterEntry{}, snapshot.Entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].PlayerID < entries[j].PlayerID })
	for index := range entries {
		if entries[index].EligiblePositions != nil {
			entries[index].EligiblePositions = append([]fantasy.Position{}, entries[index].EligiblePositions...)
			sort.Slice(entries[index].EligiblePositions, func(i, j int) bool { return entries[index].EligiblePositions[i] < entries[index].EligiblePositions[j] })
		}
	}
	raw, err := json.Marshal(struct {
		Entries  []fantasy.RosterEntry
		Coverage fantasy.RosterCoverage
		Record   *fantasy.TeamRecord
	}{entries, snapshot.Coverage, snapshot.Record})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}
func (w *leagueWriter) observationUnchanged(ctx context.Context, teamID pgtype.UUID, period fantasy.ScoringPeriod, hash string) (bool, error) {
	previous, err := w.queries.GetLatestObservedSnapshot(ctx, store.GetLatestObservedSnapshotParams{TeamID: teamID, Period: int32(period)})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil && previous.ContentHash == hash, err
}
func (w *leagueWriter) writeSnapshot(ctx context.Context, snapshot fantasy.RosterSnapshot) error {
	hash, err := snapshotHash(snapshot)
	if err != nil {
		return err
	}
	team := w.teams[snapshot.TeamID]
	key := fmt.Sprintf("week-%d:%s", snapshot.ScoringPeriod, snapshot.Coverage)
	if snapshot.Origin == fantasy.SnapshotObserved {
		unchanged, err := w.observationUnchanged(ctx, team.ID, snapshot.ScoringPeriod, hash)
		if err != nil {
			return err
		}
		if unchanged {
			return nil
		}
		key = fmt.Sprintf("%x", w.target.run.ID.Bytes)
	}
	record, err := optionalJSON(snapshot.Record)
	if err != nil {
		return err
	}
	effective := pgtype.Timestamptz{}
	if snapshot.EffectiveAt != nil {
		effective = timestamp(*snapshot.EffectiveAt)
	}
	stored, err := w.queries.UpsertTeamSnapshot(ctx, store.UpsertTeamSnapshotParams{TeamID: team.ID, LeagueSeasonID: w.target.league.ID, SyncRunID: w.target.run.ID, Period: int32(snapshot.ScoringPeriod), Origin: string(snapshot.Origin), Coverage: string(snapshot.Coverage), SourceKey: key, ObservedAt: timestamp(snapshot.ObservedAt), EffectiveAt: effective, RecordJson: record, ContentHash: hash})
	if err != nil {
		return err
	}
	return w.replaceSnapshotEntries(ctx, stored.ID, snapshot.Entries)
}
func (w *leagueWriter) replaceSnapshotEntries(ctx context.Context, snapshotID pgtype.UUID, entries []fantasy.RosterEntry) error {
	if err := w.queries.DeleteSnapshotEntries(ctx, snapshotID); err != nil {
		return err
	}
	for _, entry := range entries {
		slot := pgtype.Text{}
		if entry.ProviderSlotID != "" {
			slot = text(entry.ProviderSlotID)
		}
		eligible, err := optionalJSON(entry.EligiblePositions)
		if err != nil {
			return err
		}
		if err = w.queries.AddRosterEntry(ctx, store.AddRosterEntryParams{
			SnapshotID:            snapshotID,
			PlayerID:              w.players[entry.PlayerID],
			ProviderSlotID:        slot,
			SlotLabel:             text(string(entry.Slot)),
			EligiblePositionsJson: eligible,
			InjuryStatus:          optionalText(string(entry.InjuryStatus)),
			ProTeam:               optionalText(entry.ProTeam),
		}); err != nil {
			return err
		}
	}
	return nil
}
