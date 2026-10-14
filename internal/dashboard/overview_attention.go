package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// GetOverviewAttention checks only complete observed lineups for empty starter slots.
// Coverage remains partial because injuries, byes, and kickoff times are not available.
func (s *Service) GetOverviewAttention(ctx context.Context, _ *v1.GetOverviewAttentionRequest) (*v1.GetOverviewAttentionResponse, error) {
	response := &v1.GetOverviewAttentionResponse{Coverage: coverage("empty", "Import a league to check your lineup.")}
	err := s.readOverview(ctx, func(ctx context.Context, q *store.Queries, owner pgtype.UUID, teams []store.OverviewTeamsRow) error {
		if len(teams) == 0 {
			return nil
		}
		response.Coverage = coverage("partial", "Checks empty starter slots in the last imported lineup only. Injury, bye-week, and kickoff data are not connected.")
		for _, team := range teams {
			if !team.SnapshotID.Valid || (team.Coverage != "full" && team.Coverage != "lineup") {
				continue
			}
			entries, err := q.ListSnapshotRosterForUser(ctx, store.ListSnapshotRosterForUserParams{UserID: owner, SnapshotID: team.SnapshotID})
			if err != nil {
				return err
			}
			var settings fantasy.LeagueSettings
			if err = json.Unmarshal(team.SettingsJson, &settings); err != nil {
				return err
			}
			for _, gap := range emptyStarterSlots(settings, entries) {
				response.Alerts = append(response.Alerts, &v1.LineupAlert{Id: team.ID.String() + ":" + gap.slot, Team: overviewTeam(team), Title: fmt.Sprintf("%d empty %s starting slot(s)", gap.count, gap.slot), Detail: "This slot was empty at the last import. Review your lineup in your fantasy provider before kickoff.", Severity: "action", Slot: gap.slot, EmptySlots: int32(gap.count)})
			}
		}
		return nil
	})
	return response, err
}

type slotGap struct {
	slot  string
	count int
}

func isStarter(slot fantasy.RosterSlot) bool {
	switch slot {
	case fantasy.SlotQB, fantasy.SlotRB, fantasy.SlotWR, fantasy.SlotTE, fantasy.SlotK, fantasy.SlotDefense, fantasy.SlotFlex, fantasy.SlotSuperflex:
		return true
	}
	return false
}

// emptyStarterSlots compares configured starter capacity with occupied slots.
// Repeated slot definitions are combined; bench and reserve capacity is ignored.
// Unknown entry slots suppress inference rather than producing false alerts.
func emptyStarterSlots(settings fantasy.LeagueSettings, entries []store.ListSnapshotRosterForUserRow) []slotGap {
	counts := map[string]int{}
	for _, slot := range settings.RosterSlots {
		if isStarter(slot.Slot) {
			counts[string(slot.Slot)] += slot.Count
		}
	}
	for _, entry := range entries {
		// Unknown positions prevent a reliable completeness check; never invent gaps.
		if !entry.SlotLabel.Valid || entry.SlotLabel.String == string(fantasy.SlotUnknown) {
			return nil
		}
		counts[entry.SlotLabel.String]--
	}
	var gaps []slotGap
	for slot, count := range counts {
		if count > 0 {
			gaps = append(gaps, slotGap{slot, count})
		}
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].slot < gaps[j].slot })
	return gaps
}
