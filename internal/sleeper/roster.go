package sleeper

import (
	"fmt"
	"sort"
	"strconv"
)

type entry struct{ ID, Slot, Label string }

func rosterEntries(players, starters, reserve, taxi, positions []string) ([]entry, string, error) {
	slots := starterSlots(positions)
	if len(starters) > len(slots) {
		return nil, "", fmt.Errorf("more starters than configured slots")
	}
	byID := map[string]entry{}
	coverage := "full"
	if players == nil {
		coverage = "lineup"
	}
	for _, id := range players {
		if id == "" || id == "0" {
			return nil, "", fmt.Errorf("invalid roster player")
		}
		if _, ok := byID[id]; ok {
			return nil, "", fmt.Errorf("duplicate roster player")
		}
		byID[id] = entry{ID: id, Label: "BN"}
	}
	starterSeen := map[string]bool{}
	for i, id := range starters {
		if id == "0" || id == "" {
			continue
		}
		if starterSeen[id] {
			return nil, "", fmt.Errorf("duplicate starter")
		}
		starterSeen[id] = true
		if _, ok := byID[id]; !ok && players != nil {
			return nil, "", fmt.Errorf("starter missing from roster")
		}
		byID[id] = entry{ID: id, Slot: strconv.Itoa(i), Label: slots[i]}
	}
	for label, ids := range map[string][]string{"IR": reserve, "TAXI": taxi} {
		for _, id := range ids {
			e, ok := byID[id]
			if !ok || starterSeen[id] {
				return nil, "", fmt.Errorf("invalid reserve/taxi player")
			}
			e.Label = label
			byID[id] = e
		}
	}
	return sortedRosterEntries(byID), coverage, nil
}

func starterSlots(positions []string) []string {
	slots := []string{}
	for _, position := range positions {
		if position != "BN" && position != "IR" && position != "TAXI" {
			slots = append(slots, position)
		}
	}
	return slots
}

func sortedRosterEntries(byID map[string]entry) []entry {
	entries := make([]entry, 0, len(byID))
	for _, item := range byID {
		entries = append(entries, item)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries
}
