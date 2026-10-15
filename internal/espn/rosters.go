package espn

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func appendRosters(data *fantasy.LeagueData, players map[string]fantasy.Player, raw leagueResponse, origin fantasy.SnapshotOrigin) error {
	for _, team := range raw.Teams {
		if team.Roster == nil || team.Roster.Entries == nil {
			continue
		}
		snapshot := fantasy.RosterSnapshot{
			TeamID:        strconv.Itoa(team.ID),
			ScoringPeriod: fantasy.ScoringPeriod(raw.Week),
			Origin:        origin,
			Coverage:      fantasy.CoverageFull,
			ObservedAt:    data.FetchedAt,
		}
		if origin == fantasy.SnapshotObserved {
			snapshot.Record = normalizeRecord(team)
		}
		for _, entry := range *team.Roster.Entries {
			player, err := normalizePlayer(entry)
			if err != nil {
				return err
			}
			// Current metadata takes precedence over older snapshots.
			if _, exists := players[player.ExternalID]; !exists {
				players[player.ExternalID] = player
			}
			slot, _ := normalizeSlot(entry.Slot)
			snapshot.Entries = append(snapshot.Entries, fantasy.RosterEntry{
				PlayerID:          player.ExternalID,
				ProviderSlotID:    strconv.Itoa(entry.Slot),
				Slot:              slot,
				EligiblePositions: eligiblePositions(entry.Pool.Player.EligibleSlots),
				InjuryStatus:      injuryStatus(entry.Pool.Player.InjuryStatus),
				ProTeam:           proTeamAbbreviation(entry.Pool.Player.ProTeam),
			})
		}
		data.Rosters = append(data.Rosters, snapshot)
	}
	return nil
}

func eligiblePositions(slots []int) []fantasy.Position {
	var positions []fantasy.Position
	seen := map[fantasy.Position]bool{}
	for _, id := range slots {
		slot, eligible := normalizeSlot(id)
		if slot == fantasy.SlotFlex || slot == fantasy.SlotSuperflex {
			continue
		}
		for _, position := range eligible {
			if !seen[position] {
				positions = append(positions, position)
				seen[position] = true
			}
		}
	}
	sort.Slice(positions, func(i, j int) bool { return positions[i] < positions[j] })
	return positions
}

func normalizePlayer(entry entryResponse) (fantasy.Player, error) {
	raw := entry.Pool.Player
	if entry.PlayerID == 0 || raw.ID != entry.PlayerID {
		return fantasy.Player{}, fmt.Errorf("missing or inconsistent ESPN player identity")
	}
	player := fantasy.Player{ExternalID: strconv.Itoa(raw.ID), Name: raw.Name, Kind: fantasy.PlayerPerson}
	if player.Name == "" {
		player.Name = "ESPN " + player.ExternalID
	}
	if raw.Position == 16 {
		player.Kind = fantasy.PlayerTeamDefense
		if alias := proTeamAbbreviation(raw.ProTeam); alias != "" {
			player.KnownIDs = []fantasy.PlayerID{{Namespace: fantasy.PlayerIDSleeper, ExternalID: alias}}
		}
	} else if raw.ID < 0 {
		return player, fmt.Errorf("unknown ESPN team player kind")
	}
	return player, nil
}

// injuryStatus maps ESPN designations. DAY_TO_DAY is not an official NFL game
// status, so it is treated as questionable (worth monitoring). A missing or
// unfamiliar status is unknown rather than guessed.
func injuryStatus(raw string) fantasy.InjuryStatus {
	switch raw {
	case "ACTIVE":
		return fantasy.InjuryActive
	case "QUESTIONABLE", "DAY_TO_DAY":
		return fantasy.InjuryQuestionable
	case "DOUBTFUL":
		return fantasy.InjuryDoubtful
	case "OUT", "SUSPENSION":
		return fantasy.InjuryOut
	case "INJURY_RESERVE":
		return fantasy.InjuryReserve
	default:
		return fantasy.InjuryUnknown
	}
}

// proTeamAbbreviation maps an ESPN proTeamId to the abbreviation Sleeper also
// uses. Free agents (0) and unfamiliar IDs return "".
func proTeamAbbreviation(id int) string {
	// ESPN proTeamId identifies the franchise; never match a defense by name.
	abbreviations := map[int]string{
		1:  "ATL",
		2:  "BUF",
		3:  "CHI",
		4:  "CIN",
		5:  "CLE",
		6:  "DAL",
		7:  "DEN",
		8:  "DET",
		9:  "GB",
		10: "TEN",
		11: "IND",
		12: "KC",
		13: "LV",
		14: "LAR",
		15: "MIA",
		16: "MIN",
		17: "NE",
		18: "NO",
		19: "NYG",
		20: "NYJ",
		21: "PHI",
		22: "ARI",
		23: "PIT",
		24: "LAC",
		25: "SF",
		26: "SEA",
		27: "TB",
		28: "WAS",
		29: "CAR",
		30: "JAX",
		33: "BAL",
		34: "HOU",
	}
	return abbreviations[id]
}
