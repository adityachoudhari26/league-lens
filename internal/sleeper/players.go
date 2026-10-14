package sleeper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/diskcache"
	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

// Sleeper asks clients to download /players/nfl at most once a day.
const playersCacheMaxAge = 24 * time.Hour

// directoryPlayer keeps only the /players/nfl fields League Lens uses.
type directoryPlayer struct {
	Team         string `json:"team,omitempty"`
	InjuryStatus string `json:"injury_status,omitempty"`
}

// DefaultPlayersCachePath is the shared on-disk location of the daily player directory.
func DefaultPlayersCachePath() (string, error) {
	return diskcache.Path("sleeper-players.json")
}

// annotateAvailability adds injury status and NFL team to observed rosters.
// History is left alone because today's status says nothing about past weeks.
// When the directory is unavailable the sync continues without availability.
func (c *Client) annotateAvailability(ctx context.Context, data *fantasy.LeagueData) {
	if c.PlayersCachePath == "" {
		return
	}
	directory, err := c.playerDirectory(ctx)
	if err != nil {
		slog.Warn("sleeper player directory unavailable; availability omitted")
		return
	}

	for i := range data.Rosters {
		snapshot := &data.Rosters[i]
		if snapshot.Origin != fantasy.SnapshotObserved {
			continue
		}
		for j := range snapshot.Entries {
			entry := &snapshot.Entries[j]
			player, found := directory[entry.PlayerID]
			if !found {
				entry.InjuryStatus = fantasy.InjuryUnknown
				continue
			}
			entry.InjuryStatus = injuryStatus(player.InjuryStatus)
			entry.ProTeam = player.Team
		}
	}
}

// playerDirectory reads the cached directory, downloading a fresh copy once it is a day old.
func (c *Client) playerDirectory(ctx context.Context) (map[string]directoryPlayer, error) {
	cached, err := diskcache.ReadFresh(c.PlayersCachePath, playersCacheMaxAge)
	if err != nil {
		return nil, err
	}
	if cached != nil {
		var directory map[string]directoryPlayer
		return directory, json.Unmarshal(cached, &directory)
	}

	directory, err := c.downloadPlayerDirectory(ctx)
	if err != nil {
		return nil, err
	}
	compact, err := json.Marshal(directory)
	if err != nil {
		return nil, err
	}
	return directory, diskcache.Replace(c.PlayersCachePath, compact)
}

func (c *Client) downloadPlayerDirectory(ctx context.Context) (map[string]directoryPlayer, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/players/nfl", nil)
	if err != nil {
		return nil, err
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, fmt.Errorf("Sleeper player directory request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Sleeper player directory returned HTTP %d", response.StatusCode)
	}

	var directory map[string]directoryPlayer
	if err = json.NewDecoder(io.LimitReader(response.Body, 64<<20)).Decode(&directory); err != nil {
		return nil, fmt.Errorf("invalid Sleeper player directory: %w", err)
	}
	if len(directory) == 0 {
		return nil, fmt.Errorf("empty Sleeper player directory")
	}
	return directory, nil
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
