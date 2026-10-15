package espn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

const (
	nflGamePlaysURL = "https://sports.core.api.espn.com/v2/sports/football/leagues/nfl/events/%s/competitions/%s/plays"
	playPageLimit   = 400
	playBodyLimit   = 16 << 20
)

type playPage struct {
	PageCount int          `json:"pageCount"`
	Items     []rawNFLPlay `json:"items"`
}

type rawNFLPlay struct {
	Wallclock string `json:"wallclock"`
	Period    struct {
		Number int `json:"number"`
	} `json:"period"`
	Clock struct {
		Value float64 `json:"value"`
	} `json:"clock"`
	Type struct {
		ID string `json:"id"`
	} `json:"type"`
	Yards             int `json:"statYardage"`
	PointAfterAttempt struct {
		ID json.RawMessage `json:"id"`
	} `json:"pointAfterAttempt"`
	Participants []struct {
		Athlete struct {
			Ref string `json:"$ref"`
		} `json:"athlete"`
		Type string `json:"type"`
	} `json:"participants"`
}

// FetchNFLGamePlays reads a game's public ESPN play-by-play without league cookies.
func FetchNFLGamePlays(ctx context.Context, client *http.Client, eventID string) ([]fantasy.NFLPlay, error) {
	if _, err := strconv.ParseUint(eventID, 10, 64); err != nil {
		return nil, fmt.Errorf("valid ESPN event ID required")
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}

	endpoint := fmt.Sprintf(nflGamePlaysURL, eventID, eventID)
	plays := make([]fantasy.NFLPlay, 0)
	pageCount := 1
	for pageNumber := 1; pageNumber <= pageCount; pageNumber++ {
		pageURL, err := playPageURL(endpoint, pageNumber)
		if err != nil {
			return nil, err
		}
		body, err := readPublicPlayPage(ctx, client, pageURL)
		if err != nil {
			return nil, err
		}
		pagePlays, pageTotal, err := parseNFLPlayPage(body)
		if err != nil {
			return nil, err
		}
		if pageTotal > pageCount {
			pageCount = pageTotal
		}
		plays = append(plays, pagePlays...)
	}
	sort.SliceStable(plays, func(i, j int) bool { return plays[i].At.Before(plays[j].At) })
	return plays, nil
}

func playPageURL(endpoint string, page int) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("cannot construct ESPN play request")
	}
	query := parsed.Query()
	query.Set("limit", strconv.Itoa(playPageLimit))
	if page > 1 {
		query.Set("page", strconv.Itoa(page))
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func readPublicPlayPage(ctx context.Context, client *http.Client, endpoint string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot construct ESPN play request")
	}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("ESPN play request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ESPN plays returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, playBodyLimit+1))
	if err != nil || len(body) > playBodyLimit {
		return nil, fmt.Errorf("cannot read ESPN play response")
	}
	return body, nil
}

func parseNFLPlayPage(body []byte) ([]fantasy.NFLPlay, int, error) {
	var page playPage
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, 0, fmt.Errorf("invalid ESPN play page: %w", err)
	}
	if page.PageCount < 1 {
		page.PageCount = 1
	}
	plays := make([]fantasy.NFLPlay, 0, len(page.Items))
	for _, raw := range page.Items {
		parsed, ok := parseNFLPlay(raw)
		if ok {
			plays = append(plays, parsed)
		}
	}
	sort.SliceStable(plays, func(i, j int) bool { return plays[i].At.Before(plays[j].At) })
	return plays, page.PageCount, nil
}

func parseNFLPlay(raw rawNFLPlay) (fantasy.NFLPlay, bool) {
	at, err := time.Parse(time.RFC3339, raw.Wallclock)
	if err != nil {
		return fantasy.NFLPlay{}, false
	}
	participants := make([]fantasy.NFLPlayParticipant, 0, len(raw.Participants))
	for _, participant := range raw.Participants {
		athleteID := athleteIDFromRef(participant.Athlete.Ref)
		if athleteID != "" {
			participants = append(participants, fantasy.NFLPlayParticipant{
				AthleteID: athleteID,
				Role:      participant.Type,
			})
		}
	}
	return fantasy.NFLPlay{
		At:             at,
		Period:         raw.Period.Number,
		Clock:          time.Duration(raw.Clock.Value * float64(time.Second)),
		Kind:           nflPlayKind(raw.Type.ID),
		Yards:          raw.Yards,
		ExtraPointGood: extraPointGood(raw.PointAfterAttempt.ID),
		Participants:   participants,
	}, true
}

func nflPlayKind(typeID string) fantasy.NFLPlayKind {
	switch typeID {
	case "5":
		return fantasy.NFLPlayRush
	case "68":
		return fantasy.NFLPlayRushingTouchdown
	case "24":
		return fantasy.NFLPlayPassReception
	case "67":
		return fantasy.NFLPlayPassingTouchdown
	case "3":
		return fantasy.NFLPlayPassIncompletion
	case "26":
		return fantasy.NFLPlayInterception
	case "59":
		return fantasy.NFLPlayFieldGoalGood
	case "66":
		return fantasy.NFLPlayEndOfGame
	default:
		return fantasy.NFLPlayOther
	}
}

func extraPointGood(id json.RawMessage) bool {
	var value string
	if json.Unmarshal(id, &value) == nil {
		return value == "61"
	}
	var numeric int
	return json.Unmarshal(id, &numeric) == nil && numeric == 61
}

func athleteIDFromRef(value string) string {
	ref, err := url.Parse(value)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(ref.Path, "/"), "/")
	if len(parts) < 2 || parts[len(parts)-2] != "athletes" {
		return ""
	}
	id := parts[len(parts)-1]
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		return ""
	}
	return id
}
