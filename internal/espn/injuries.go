package espn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	nflCoreBaseURL         = "https://sports.core.api.espn.com/v2/sports/football/leagues/nfl"
	athleteInjuriesPath    = "/seasons/%d/athletes/%d/injuries?limit=%d"
	athleteInjuryPageSize  = 3
	injuryRequestsInFlight = 4
	injuryCacheTTL         = 30 * time.Minute
	injuryBodyLimit        = 1 << 20
)

// AthleteInjury is the newest entry from an athlete's ESPN injury list.
type AthleteInjury struct {
	Status     string // ESPN's status, e.g. "Questionable", "Out", "Injured Reserve".
	Injury     string // Body part or reason, e.g. "Hamstring".
	ReturnDate string // ESPN's estimated return date, YYYY-MM-DD.
	Note       string // ESPN's short news comment.
	Updated    time.Time
}

type athleteInjuryPage struct {
	Items []athleteInjuryEntry `json:"items"`
}

type athleteInjuryEntry struct {
	Status       string `json:"status"`
	ShortComment string `json:"shortComment"`
	Date         string `json:"date"`
	Details      struct {
		Type       string `json:"type"`
		ReturnDate string `json:"returnDate"`
	} `json:"details"`
}

type cachedAthleteInjury struct {
	injury  *AthleteInjury
	expires time.Time
}

// InjuryClient reads ESPN's public per-athlete injury lists. It sends no league
// cookies, keeps at most four requests in flight, and caches each athlete for 30 minutes.
type InjuryClient struct {
	http    *http.Client
	baseURL string
	now     func() time.Time

	mu    sync.Mutex
	cache map[int64]cachedAthleteInjury
}

// NewInjuryClient uses client, or a client with a 10-second timeout when nil.
func NewInjuryClient(client *http.Client) *InjuryClient {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &InjuryClient{http: client, baseURL: nflCoreBaseURL, now: time.Now}
}

// errNoAthleteInjuries marks ESPN's 404 for an athlete without an injury list this season.
var errNoAthleteInjuries = errors.New("ESPN has no injury list for the athlete")

// AthleteInjuries returns each athlete's newest ESPN injury entry, or nil when ESPN
// lists none. Any athlete still failing after one retry fails the whole call, so a
// missing lookup is never mistaken for a healthy player.
func (c *InjuryClient) AthleteInjuries(ctx context.Context, season int, athleteIDs []int64) (map[int64]*AthleteInjury, error) {
	injuries := make(map[int64]*AthleteInjury, len(athleteIDs))
	var mu sync.Mutex
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(injuryRequestsInFlight)
	for _, athleteID := range athleteIDs {
		group.Go(func() error {
			injury, err := c.athleteInjury(groupCtx, season, athleteID)
			if err != nil {
				return err
			}
			mu.Lock()
			defer mu.Unlock()
			injuries[athleteID] = injury
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return injuries, nil
}

// athleteInjury serves a fresh cache entry or reads and caches the athlete's list.
func (c *InjuryClient) athleteInjury(ctx context.Context, season int, athleteID int64) (*AthleteInjury, error) {
	if injury, found := c.cachedInjury(athleteID); found {
		return injury, nil
	}
	injury, err := c.readAthleteInjury(ctx, season, athleteID)
	if shouldRetryInjuryRead(ctx, err) {
		injury, err = c.readAthleteInjury(ctx, season, athleteID)
	}
	if errors.Is(err, errNoAthleteInjuries) {
		injury, err = nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.storeInjury(athleteID, injury)
	return injury, nil
}

// shouldRetryInjuryRead retries transient failures once, but not a 404 (no injury
// list) or a cancelled or expired request.
func shouldRetryInjuryRead(ctx context.Context, err error) bool {
	return err != nil && ctx.Err() == nil && !errors.Is(err, errNoAthleteInjuries) &&
		!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

func (c *InjuryClient) readAthleteInjury(ctx context.Context, season int, athleteID int64) (*AthleteInjury, error) {
	body, err := c.fetchAthleteInjuries(ctx, season, athleteID)
	if err != nil {
		return nil, err
	}
	return parseAthleteInjuries(body)
}

func (c *InjuryClient) cachedInjury(athleteID int64) (*AthleteInjury, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, found := c.cache[athleteID]
	if !found || !c.now().Before(entry.expires) {
		return nil, false
	}
	return entry.injury, true
}

func (c *InjuryClient) storeInjury(athleteID int64, injury *AthleteInjury) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		c.cache = make(map[int64]cachedAthleteInjury)
	}
	c.cache[athleteID] = cachedAthleteInjury{injury: injury, expires: c.now().Add(injuryCacheTTL)}
}

func (c *InjuryClient) fetchAthleteInjuries(ctx context.Context, season int, athleteID int64) ([]byte, error) {
	endpoint := c.baseURL + fmt.Sprintf(athleteInjuriesPath, season, athleteID, athleteInjuryPageSize)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("cannot construct ESPN injury request")
	}
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("ESPN injury request failed")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, errNoAthleteInjuries
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ESPN injuries returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, injuryBodyLimit+1))
	if err != nil || len(body) > injuryBodyLimit {
		return nil, errors.New("cannot read ESPN injury response")
	}
	return body, nil
}

// parseAthleteInjuries keeps only the newest dated entry. ESPN inlines entries in the
// per-athlete list and returns an empty list once a player is healthy again.
func parseAthleteInjuries(body []byte) (*AthleteInjury, error) {
	var page athleteInjuryPage
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, errors.New("invalid ESPN injury response")
	}
	injuries := datedInjuries(page.Items)
	if len(injuries) == 0 {
		return nil, nil
	}
	sort.SliceStable(injuries, func(i, j int) bool { return injuries[i].Updated.After(injuries[j].Updated) })
	return &injuries[0], nil
}

// datedInjuries drops entries without a status or a parseable date.
func datedInjuries(entries []athleteInjuryEntry) []AthleteInjury {
	injuries := make([]AthleteInjury, 0, len(entries))
	for _, entry := range entries {
		updated, err := parseScoreboardDate(entry.Date)
		if entry.Status == "" || err != nil {
			continue
		}
		injuries = append(injuries, AthleteInjury{
			Status: entry.Status, Injury: entry.Details.Type, ReturnDate: entry.Details.ReturnDate,
			Note: entry.ShortComment, Updated: updated.UTC(),
		})
	}
	return injuries
}
