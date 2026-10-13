package espn

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func normalizeSlot(id int) (fantasy.RosterSlot, []fantasy.Position) {
	positions := map[int]fantasy.Position{0: fantasy.PositionQB, 2: fantasy.PositionRB, 4: fantasy.PositionWR, 6: fantasy.PositionTE, 16: fantasy.PositionDEF, 17: fantasy.PositionK}
	if position, ok := positions[id]; ok {
		return fantasy.RosterSlot(position), []fantasy.Position{position}
	}
	switch id {
	case 3:
		return fantasy.SlotFlex, []fantasy.Position{fantasy.PositionRB, fantasy.PositionWR}
	case 5:
		return fantasy.SlotFlex, []fantasy.Position{fantasy.PositionWR, fantasy.PositionTE}
	case 23:
		return fantasy.SlotFlex, []fantasy.Position{fantasy.PositionRB, fantasy.PositionWR, fantasy.PositionTE}
	case 7:
		return fantasy.SlotSuperflex, []fantasy.Position{fantasy.PositionQB, fantasy.PositionRB, fantasy.PositionWR, fantasy.PositionTE}
	case 20:
		return fantasy.SlotBench, nil
	case 21:
		return fantasy.SlotIR, nil
	default:
		return fantasy.SlotUnknown, nil
	}
}
func normalizeSettings(raw settingsResponse) (fantasy.LeagueSettings, error) {
	var settings fantasy.LeagueSettings
	var roster struct {
		Counts map[string]int `json:"lineupSlotCounts"`
	}
	if len(raw.Roster) > 0 && json.Unmarshal(raw.Roster, &roster) != nil {
		return settings, fmt.Errorf("invalid ESPN roster settings")
	}
	keys := make([]string, 0, len(roster.Counts))
	for key := range roster.Counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		count := roster.Counts[key]
		if count == 0 {
			continue
		}
		if count < 0 {
			return settings, fmt.Errorf("invalid ESPN slot count")
		}
		id, err := strconv.Atoi(key)
		if err != nil {
			return settings, fmt.Errorf("invalid ESPN slot ID")
		}
		slot, eligible := normalizeSlot(id)
		settings.RosterSlots = append(settings.RosterSlots, fantasy.SlotDefinition{Slot: slot, Count: count, EligiblePositions: eligible})
	}
	rules, err := normalizeScoring(raw.Scoring)
	if err != nil {
		return settings, err
	}
	settings.ScoringRules = rules
	// Only settings are retained: no members, owners, or authenticated response.
	settings.ProviderSettings, err = json.Marshal(raw)
	return settings, err
}
func normalizeScoring(raw json.RawMessage) ([]fantasy.ScoringRule, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var scoring struct {
		Items []struct {
			StatID    int                `json:"statId"`
			Points    *float64           `json:"points"`
			Overrides map[string]float64 `json:"pointsOverrides"`
			Reverse   bool               `json:"isReverseItem"`
		} `json:"scoringItems"`
	}
	if json.Unmarshal(raw, &scoring) != nil {
		return nil, fmt.Errorf("invalid ESPN scoring settings")
	}
	stats := map[int]fantasy.Stat{3: fantasy.StatPassingYards, 4: fantasy.StatPassingTouchdowns, 20: fantasy.StatInterceptions, 24: fantasy.StatRushingYards, 25: fantasy.StatRushingTouchdowns, 42: fantasy.StatReceivingYards, 43: fantasy.StatReceivingTouchdowns, 53: fantasy.StatReceptions, 72: fantasy.StatFumblesLost}
	var rules []fantasy.ScoringRule
	for _, item := range scoring.Items {
		stat, known := stats[item.StatID]
		if !known || item.Points == nil || len(item.Overrides) > 0 || item.Reverse {
			continue
		}
		rules = append(rules, fantasy.ScoringRule{Stat: stat, Points: *item.Points, PerUnits: 1})
	}
	return rules, nil
}
