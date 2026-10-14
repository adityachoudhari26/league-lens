package sleeper

import (
	"context"
	"log/slog"
	"net/url"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

// playerRecord keeps only the /players/nfl/{id} fields League Lens uses.
type playerRecord struct {
	Team         *string `json:"team"`
	InjuryStatus *string `json:"injury_status"`
}

// annotateAvailability adds injury status and NFL team to the observed rosters
// of AvailabilityTeamIDs, looking up each player individually so statuses are
// as current as the sync. History is left alone because today's status says
// nothing about past weeks. A failed lookup leaves that player unknown.
func (c *Client) annotateAvailability(ctx context.Context, data *fantasy.LeagueData) {
	if len(c.AvailabilityTeamIDs) == 0 {
		return
	}
	entries := c.availabilityEntries(data)
	records := map[string]*playerRecord{}
	for _, entry := range entries {
		record, looked := records[entry.PlayerID]
		if !looked {
			record = c.lookupPlayer(ctx, entry.PlayerID)
			records[entry.PlayerID] = record
		}
		applyAvailability(entry, record)
	}
}

// availabilityEntries returns the observed roster entries of the teams that need availability.
func (c *Client) availabilityEntries(data *fantasy.LeagueData) []*fantasy.RosterEntry {
	selected := map[string]bool{}
	for _, id := range c.AvailabilityTeamIDs {
		selected[id] = true
	}
	var entries []*fantasy.RosterEntry
	for i := range data.Rosters {
		snapshot := &data.Rosters[i]
		if snapshot.Origin != fantasy.SnapshotObserved || !selected[snapshot.TeamID] {
			continue
		}
		for j := range snapshot.Entries {
			entries = append(entries, &snapshot.Entries[j])
		}
	}
	return entries
}

// lookupPlayer reads one player from Sleeper, returning nil when the lookup fails.
func (c *Client) lookupPlayer(ctx context.Context, playerID string) *playerRecord {
	var record playerRecord
	if err := c.get(ctx, "/players/nfl/"+url.PathEscape(playerID), &record); err != nil {
		slog.Warn("sleeper player lookup failed; availability unknown", "player_id", playerID)
		return nil
	}
	return &record
}

func applyAvailability(entry *fantasy.RosterEntry, record *playerRecord) {
	if record == nil {
		entry.InjuryStatus = fantasy.InjuryUnknown
		return
	}
	entry.InjuryStatus = injuryStatus(valueOf(record.InjuryStatus))
	entry.ProTeam = valueOf(record.Team)
}

func valueOf(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// injuryStatus maps Sleeper designations. No designation means the player is
// healthy; unfamiliar designations are unknown rather than guessed.
func injuryStatus(raw string) fantasy.InjuryStatus {
	switch raw {
	case "":
		return fantasy.InjuryActive
	case "Questionable":
		return fantasy.InjuryQuestionable
	case "Doubtful":
		return fantasy.InjuryDoubtful
	case "Out", "PUP", "Sus":
		return fantasy.InjuryOut
	case "IR":
		return fantasy.InjuryReserve
	default:
		return fantasy.InjuryUnknown
	}
}
