package sleeper

import (
	"bytes"
	"encoding/json"
	"sort"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func normalizeSlot(raw string) (fantasy.RosterSlot, []fantasy.Position) {
	switch raw {
	case "QB", "RB", "WR", "TE", "K", "DEF":
		return fantasy.RosterSlot(raw), []fantasy.Position{fantasy.Position(raw)}
	case "FLEX":
		return fantasy.SlotFlex, []fantasy.Position{fantasy.PositionRB, fantasy.PositionWR, fantasy.PositionTE}
	case "SUPER_FLEX":
		return fantasy.SlotSuperflex, []fantasy.Position{fantasy.PositionQB, fantasy.PositionRB, fantasy.PositionWR, fantasy.PositionTE}
	case "WRRB_FLEX":
		return fantasy.SlotFlex, []fantasy.Position{fantasy.PositionRB, fantasy.PositionWR}
	case "REC_FLEX":
		return fantasy.SlotFlex, []fantasy.Position{fantasy.PositionWR, fantasy.PositionTE}
	case "BN":
		return fantasy.SlotBench, nil
	case "IR":
		return fantasy.SlotIR, nil
	case "TAXI":
		return fantasy.SlotTaxi, nil
	default:
		return fantasy.SlotUnknown, nil
	}
}

func normalizeSettings(raw League) (fantasy.LeagueSettings, error) {
	var settings fantasy.LeagueSettings
	for _, position := range raw.Positions {
		slot, eligible := normalizeSlot(position)
		settings.RosterSlots = append(settings.RosterSlots, fantasy.SlotDefinition{Slot: slot, Count: 1, EligiblePositions: eligible})
	}
	stats := map[string]fantasy.Stat{
		"pass_yd":  fantasy.StatPassingYards,
		"pass_td":  fantasy.StatPassingTouchdowns,
		"pass_int": fantasy.StatInterceptions,
		"rush_yd":  fantasy.StatRushingYards,
		"rush_td":  fantasy.StatRushingTouchdowns,
		"rec_yd":   fantasy.StatReceivingYards,
		"rec_td":   fantasy.StatReceivingTouchdowns,
		"rec":      fantasy.StatReceptions,
		"fum_lost": fantasy.StatFumblesLost,
	}
	keys := make([]string, 0, len(stats))
	for key := range stats {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value, found := raw.Scoring[key]
		if !found || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			continue
		}
		var points float64
		if err := json.Unmarshal(value, &points); err != nil {
			return settings, err
		}
		settings.ScoringRules = append(settings.ScoringRules, fantasy.ScoringRule{Stat: stats[key], Points: points, PerUnits: 1})
	}
	supplemental, err := json.Marshal(raw)
	settings.ProviderSettings = supplemental
	return settings, err
}
