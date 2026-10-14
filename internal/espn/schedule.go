package espn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

type scheduleResponse struct {
	Settings struct {
		ProTeams []struct {
			ID    int                              `json:"id"`
			Games map[string][]scheduledGameRecord `json:"proGamesByScoringPeriod"`
		} `json:"proTeams"`
	} `json:"settings"`
}

type scheduledGameRecord struct {
	Date int64 `json:"date"` // Unix milliseconds.
	Home int   `json:"homeProTeamId"`
	Away int   `json:"awayProTeamId"`
}

// FetchNFLSchedule reads ESPN's public NFL schedule for a season. It needs no
// league cookies and sends none.
func FetchNFLSchedule(ctx context.Context, client *http.Client, season int) ([]fantasy.NFLGame, error) {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	url := fmt.Sprintf("https://lm-api-reads.fantasy.espn.com/apis/v3/games/ffl/seasons/%d?view=proTeamSchedules_wl", season)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("ESPN schedule request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ESPN schedule returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	return parseNFLSchedule(body, season)
}

// parseNFLSchedule emits one game per team per week from that team's side.
// Unfamiliar team IDs are skipped rather than guessed.
func parseNFLSchedule(body []byte, season int) ([]fantasy.NFLGame, error) {
	var raw scheduleResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("invalid ESPN schedule: %w", err)
	}

	var games []fantasy.NFLGame
	for _, team := range raw.Settings.ProTeams {
		abbreviation := proTeamAbbreviation(team.ID)
		if abbreviation == "" {
			continue
		}
		for period, records := range team.Games {
			week, err := strconv.Atoi(period)
			if err != nil || week < 1 || len(records) == 0 {
				continue
			}
			record := records[0]
			opponentID := record.Home
			if opponentID == team.ID {
				opponentID = record.Away
			}
			opponent := proTeamAbbreviation(opponentID)
			if opponent == "" || record.Date <= 0 {
				continue
			}
			games = append(games, fantasy.NFLGame{Season: season, Week: fantasy.ScoringPeriod(week), Team: abbreviation, Opponent: opponent, KickoffAt: time.UnixMilli(record.Date).UTC()})
		}
	}
	if len(games) == 0 {
		return nil, fmt.Errorf("ESPN schedule has no games for %d", season)
	}
	return games, nil
}
