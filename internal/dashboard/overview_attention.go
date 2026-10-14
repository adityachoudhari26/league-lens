package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	v1 "github.com/adityachoudhari26/league-lens/internal/gen/leaguelens/v1"
	"github.com/adityachoudhari26/league-lens/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// GetOverviewAttention lists lineup issues for the owner's starters in each
// team's latest full or lineup snapshot. Every rule reads saved data only:
// empty starting slots, starters ruled out or on bye (action), and questionable
// starters (monitor). A player's alert disappears once his game kicks off,
// because the slot is locked by then.
func (s *Service) GetOverviewAttention(ctx context.Context, _ *v1.GetOverviewAttentionRequest) (*v1.GetOverviewAttentionResponse, error) {
	response := &v1.GetOverviewAttentionResponse{Coverage: coverage("empty", "Import a league to check your lineup.")}
	now := time.Now()
	err := s.readOverview(ctx, func(ctx context.Context, q *store.Queries, owner pgtype.UUID, teams []store.OverviewTeamsRow) error {
		if len(teams) == 0 {
			return nil
		}

		schedules := weekSchedules{}
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
			schedule, err := schedules.load(ctx, q, team.Season, team.Period)
			if err != nil {
				return err
			}

			response.Alerts = append(response.Alerts, emptySlotAlerts(team, settings, entries)...)
			response.Alerts = append(response.Alerts, starterAlerts(team, entries, schedule, now)...)
		}

		visibleAlerts, err := dropDismissedAlerts(ctx, q, owner, response.Alerts)
		if err != nil {
			return err
		}
		response.Alerts = visibleAlerts
		sortAlerts(response.Alerts)
		response.Coverage = attentionCoverage(schedules.allLoaded())
		return nil
	})
	return response, err
}

// dropDismissedAlerts removes alerts the owner dismissed in their current week and status.
func dropDismissedAlerts(ctx context.Context, q *store.Queries, owner pgtype.UUID, alerts []*v1.LineupAlert) ([]*v1.LineupAlert, error) {
	if len(alerts) == 0 {
		return alerts, nil
	}

	fingerprints := make([]string, 0, len(alerts))
	for _, alert := range alerts {
		fingerprints = append(fingerprints, alert.Fingerprint)
	}
	dismissedRows, err := q.ListDismissedAlerts(ctx, store.ListDismissedAlertsParams{UserID: owner, Fingerprints: fingerprints})
	if err != nil {
		return nil, err
	}
	dismissed := make(map[string]bool, len(dismissedRows))
	for _, fingerprint := range dismissedRows {
		dismissed[fingerprint] = true
	}
	return withoutDismissed(alerts, dismissed), nil
}

func attentionCoverage(scheduleLoaded bool) *v1.OverviewCoverage {
	if scheduleLoaded {
		return coverage("partial", "Checks your starters for empty slots, injury designations, and byes as of each league's last sync. Late inactives can arrive after a sync.")
	}
	return coverage("partial", "Checks your starters for empty slots and injury designations as of each league's last sync. Bye checks are unavailable until the NFL schedule loads.")
}

func emptySlotAlerts(team store.OverviewTeamsRow, settings fantasy.LeagueSettings, entries []store.ListSnapshotRosterForUserRow) []*v1.LineupAlert {
	var alerts []*v1.LineupAlert
	for _, gap := range emptyStarterSlots(settings, entries) {
		alert := &v1.LineupAlert{
			Id:         team.ID.String() + ":" + gap.slot,
			Team:       overviewTeam(team),
			Kind:       v1.AlertKind_ALERT_KIND_EMPTY_SLOT,
			Title:      fmt.Sprintf("%d empty %s starting slot(s)", gap.count, gap.slot),
			Detail:     "This slot was empty at the last sync. Fill it in your fantasy provider before kickoff.",
			Severity:   v1.AlertSeverity_ALERT_SEVERITY_ACTION,
			Slot:       gap.slot,
			EmptySlots: int32(gap.count),
		}
		alert.Fingerprint = alertFingerprint(alert, team.Period)
		alerts = append(alerts, alert)
	}
	return alerts
}

// starterAlerts flags starters who are on bye or carry an injury designation.
// Starters whose game has kicked off are skipped, as are byes once the week's
// last game has kicked off. Missing team, status, or
// schedule data never produces an alert.
func starterAlerts(team store.OverviewTeamsRow, entries []store.ListSnapshotRosterForUserRow, schedule weekSchedule, now time.Time) []*v1.LineupAlert {
	var alerts []*v1.LineupAlert
	for _, entry := range entries {
		slot := fantasy.RosterSlot(entry.SlotLabel.String)
		if !entry.SlotLabel.Valid || !isStarter(slot) {
			continue
		}
		game, hasGame := schedule.games[entry.ProTeam.String]
		if hasGame && !game.KickoffAt.Time.After(now) {
			continue
		}

		if schedule.loaded() && entry.ProTeam.Valid && !hasGame {
			if !schedule.finished(now) {
				alerts = append(alerts, byeAlert(team, entry, slot))
			}
			continue
		}
		if alert := injuryAlert(team, entry, slot, game, hasGame); alert != nil {
			alerts = append(alerts, alert)
		}
	}
	return alerts
}

func byeAlert(team store.OverviewTeamsRow, entry store.ListSnapshotRosterForUserRow, slot fantasy.RosterSlot) *v1.LineupAlert {
	alert := &v1.LineupAlert{
		Id:         playerAlertID(team, entry, "bye"),
		Team:       overviewTeam(team),
		Kind:       v1.AlertKind_ALERT_KIND_BYE,
		Severity:   v1.AlertSeverity_ALERT_SEVERITY_ACTION,
		Slot:       string(slot),
		PlayerName: entry.DisplayName,
		ProTeam:    entry.ProTeam.String,
		Title:      fmt.Sprintf("Your starting %s is on bye", slotName(slot)),
		Detail:     fmt.Sprintf("%s (%s) has no game in week %d. Start someone else before your first kickoff.", entry.DisplayName, entry.ProTeam.String, team.Period),
	}
	alert.Fingerprint = alertFingerprint(alert, team.Period)
	return alert
}

// injuryAlert maps designations to severity: out, IR, and doubtful need action;
// questionable is worth monitoring. Active and unknown statuses are silent.
func injuryAlert(team store.OverviewTeamsRow, entry store.ListSnapshotRosterForUserRow, slot fantasy.RosterSlot, game store.ListNFLGamesForWeekRow, hasGame bool) *v1.LineupAlert {
	status := fantasy.InjuryStatus(entry.InjuryStatus.String)
	label, severity := map[fantasy.InjuryStatus]string{
		fantasy.InjuryOut:          "out",
		fantasy.InjuryReserve:      "on IR",
		fantasy.InjuryDoubtful:     "doubtful",
		fantasy.InjuryQuestionable: "questionable",
	}[status], v1.AlertSeverity_ALERT_SEVERITY_ACTION
	if label == "" {
		return nil
	}

	title := fmt.Sprintf("Your starting %s is %s", slotName(slot), label)
	if status == fantasy.InjuryQuestionable {
		severity = v1.AlertSeverity_ALERT_SEVERITY_MONITOR
		title = fmt.Sprintf("Keep an eye on your starting %s", slotName(slot))
	}
	alert := &v1.LineupAlert{
		Id:           playerAlertID(team, entry, "injury"),
		Team:         overviewTeam(team),
		Kind:         v1.AlertKind_ALERT_KIND_INJURY,
		Severity:     severity,
		Slot:         string(slot),
		PlayerName:   entry.DisplayName,
		ProTeam:      entry.ProTeam.String,
		InjuryStatus: injuryStatusMessage(status),
		Title:        title,
		Detail:       fmt.Sprintf("%s was listed %s by %s at the last sync.", entry.DisplayName, label, providerName(team.Provider)),
	}
	alert.Fingerprint = alertFingerprint(alert, team.Period)
	if hasGame {
		alert.KickoffAt = timestamp(game.KickoffAt)
	}
	return alert
}

// alertFingerprint changes when an alert's week or relevant condition changes.
func alertFingerprint(alert *v1.LineupAlert, period int32) string {
	fingerprint := fmt.Sprintf("%s:week%d", alert.Id, period)
	var state string
	switch alert.Kind {
	case v1.AlertKind_ALERT_KIND_INJURY:
		state = strings.ToLower(strings.TrimPrefix(alert.InjuryStatus.String(), "INJURY_STATUS_"))
	case v1.AlertKind_ALERT_KIND_EMPTY_SLOT:
		state = strconv.FormatInt(int64(alert.EmptySlots), 10)
	}
	if state != "" {
		fingerprint += ":" + state
	}
	return fingerprint
}

func withoutDismissed(alerts []*v1.LineupAlert, dismissed map[string]bool) []*v1.LineupAlert {
	filtered := make([]*v1.LineupAlert, 0, len(alerts))
	for _, alert := range alerts {
		if !dismissed[alert.Fingerprint] {
			filtered = append(filtered, alert)
		}
	}
	return filtered
}

func injuryStatusMessage(status fantasy.InjuryStatus) v1.InjuryStatus {
	statuses := map[fantasy.InjuryStatus]v1.InjuryStatus{
		fantasy.InjuryActive:       v1.InjuryStatus_INJURY_STATUS_ACTIVE,
		fantasy.InjuryQuestionable: v1.InjuryStatus_INJURY_STATUS_QUESTIONABLE,
		fantasy.InjuryDoubtful:     v1.InjuryStatus_INJURY_STATUS_DOUBTFUL,
		fantasy.InjuryOut:          v1.InjuryStatus_INJURY_STATUS_OUT,
		fantasy.InjuryReserve:      v1.InjuryStatus_INJURY_STATUS_INJURED_RESERVE,
		fantasy.InjuryUnknown:      v1.InjuryStatus_INJURY_STATUS_UNKNOWN,
	}
	return statuses[status]
}

func playerAlertID(team store.OverviewTeamsRow, entry store.ListSnapshotRosterForUserRow, kind string) string {
	return team.ID.String() + ":" + entry.PlayerID.String() + ":" + kind
}

func providerName(provider string) string {
	if provider == string(fantasy.ProviderESPN) {
		return "ESPN"
	}
	return "Sleeper"
}

func slotName(slot fantasy.RosterSlot) string {
	names := map[fantasy.RosterSlot]string{
		fantasy.SlotQB: "quarterback", fantasy.SlotRB: "running back", fantasy.SlotWR: "receiver",
		fantasy.SlotTE: "tight end", fantasy.SlotK: "kicker", fantasy.SlotDefense: "defense",
		fantasy.SlotFlex: "flex", fantasy.SlotSuperflex: "superflex",
	}
	return names[slot]
}

// sortAlerts puts definite issues first, then monitor items, then the soonest
// kickoff, so the three alerts shown by default are the most urgent ones.
func sortAlerts(alerts []*v1.LineupAlert) {
	rank := func(alert *v1.LineupAlert) int {
		if alert.Severity == v1.AlertSeverity_ALERT_SEVERITY_ACTION {
			return 0
		}
		return 1
	}
	kickoff := func(alert *v1.LineupAlert) int64 {
		if alert.KickoffAt == nil {
			return math.MaxInt64
		}
		return alert.KickoffAt.AsTime().Unix()
	}
	sort.SliceStable(alerts, func(i, j int) bool {
		if rank(alerts[i]) != rank(alerts[j]) {
			return rank(alerts[i]) < rank(alerts[j])
		}
		return kickoff(alerts[i]) < kickoff(alerts[j])
	})
}

// weekSchedule maps NFL team abbreviations to their game in one scoring week.
// With no games saved for the week, byes and kickoffs are unknown.
type weekSchedule struct {
	games map[string]store.ListNFLGamesForWeekRow
}

func (w weekSchedule) loaded() bool { return len(w.games) > 0 }

// finished reports whether every game of the week has kicked off, after which
// no lineup change can help a starter on bye.
func (w weekSchedule) finished(now time.Time) bool {
	for _, game := range w.games {
		if game.KickoffAt.Time.After(now) {
			return false
		}
	}
	return true
}

// weekSchedules caches weekly schedules across teams that share a season and week.
type weekSchedules map[[2]int32]weekSchedule

func (s weekSchedules) load(ctx context.Context, q *store.Queries, season, week int32) (weekSchedule, error) {
	key := [2]int32{season, week}
	if schedule, ok := s[key]; ok {
		return schedule, nil
	}
	rows, err := q.ListNFLGamesForWeek(ctx, store.ListNFLGamesForWeekParams{Season: season, Week: week})
	if err != nil {
		return weekSchedule{}, err
	}
	schedule := weekSchedule{games: map[string]store.ListNFLGamesForWeekRow{}}
	for _, row := range rows {
		schedule.games[row.Team] = row
	}
	s[key] = schedule
	return schedule, nil
}

func (s weekSchedules) allLoaded() bool {
	for _, schedule := range s {
		if !schedule.loaded() {
			return false
		}
	}
	return true
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
