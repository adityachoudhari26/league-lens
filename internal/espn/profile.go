package espn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
	"github.com/adityachoudhari26/league-lens/internal/playerprofile"
)

const (
	coreAthleteURL     = "https://sports.core.api.espn.com/v2/sports/football/leagues/nfl/athletes/%d"
	athleteURL         = "https://site.web.api.espn.com/apis/common/v3/sports/football/nfl/athletes/%d"
	athleteOverviewURL = athleteURL + "/overview"
	fantasyPlayersURL  = "https://lm-api-reads.fantasy.espn.com/apis/v3/games/ffl/seasons/%d/players?view=kona_player_info"
	teamLogoURL        = "https://a.espncdn.com/i/teamlogos/nfl/500/%s.png"
	profileMaxBody     = 4 << 20
)

// ProfileSource reads public ESPN player bios, season stats, and fantasy
// outlooks for one fantasy season. It needs no league cookies.
type ProfileSource struct {
	HTTP   *http.Client
	Season int
}

// coreAthlete is ESPN's core athlete record, the only one with numeric bio
// fields.
type coreAthlete struct {
	Headshot    struct{ Href string } `json:"headshot"`
	Jersey      string                `json:"jersey"`
	DateOfBirth string                `json:"dateOfBirth"`
	Height      float64               `json:"height"`
	Weight      float64               `json:"weight"`
	Draft       *struct {
		Year      int `json:"year"`
		Round     int `json:"round"`
		Selection int `json:"selection"`
	} `json:"draft"`
}

type athleteSummaryResponse struct {
	Athlete struct {
		College      struct{ Name string } `json:"college"`
		StatsSummary struct {
			Statistics []athleteStat `json:"statistics"`
		} `json:"statsSummary"`
	} `json:"athlete"`
}

type athleteStat struct {
	DisplayName      string  `json:"displayName"`
	ShortDisplayName string  `json:"shortDisplayName"`
	Value            float64 `json:"value"`
	Rank             int     `json:"rank"`
}

type athleteOverviewResponse struct {
	Fantasy struct {
		PositionRank string `json:"positionRank"`
	} `json:"fantasy"`
}

type fantasyPlayer struct {
	ID                int64 `json:"id"`
	ProTeamID         int   `json:"proTeamId"`
	DefaultPositionID int   `json:"defaultPositionId"`
	Ownership         *struct {
		PercentOwned   float64 `json:"percentOwned"`
		PercentStarted float64 `json:"percentStarted"`
	} `json:"ownership"`
	SeasonOutlook string `json:"seasonOutlook"`
	Outlooks      struct {
		OutlooksByWeek map[string]string `json:"outlooksByWeek"`
	} `json:"outlooks"`
}

// athleteResponses holds the ESPN responses behind one athlete's profile.
// Summary, overview, and fantasy are optional; a missing one leaves its fields
// unreported.
type athleteResponses struct {
	bio           coreAthlete
	summary       athleteSummaryResponse
	overview      athleteOverviewResponse
	fantasy       fantasyPlayer
	fantasyListed bool
}

// Name is the stable storage key for ESPN profiles.
func (ProfileSource) Name() string { return "espn" }

// Profile fetches a player's card data by ESPN ID. Team defenses (negative ESPN
// IDs) have no athlete record, so they get fantasy data and their team logo.
func (s ProfileSource) Profile(ctx context.Context, player playerprofile.Player) (playerprofile.Profile, error) {
	espnID, err := profileESPNID(player)
	if err != nil {
		return playerprofile.Profile{}, err
	}
	if espnID < 0 {
		return s.defenseProfile(ctx, espnID)
	}
	return s.athleteProfile(ctx, espnID)
}

// profileESPNID reads the player's ESPN ID; players without a usable one are
// unknown to this source.
func profileESPNID(player playerprofile.Player) (int64, error) {
	espnID, err := strconv.ParseInt(player.ExternalIDs["espn"], 10, 64)
	if err != nil || espnID == 0 {
		return 0, playerprofile.ErrUnknownPlayer
	}
	return espnID, nil
}

func (s ProfileSource) athleteProfile(ctx context.Context, espnID int64) (playerprofile.Profile, error) {
	responses, err := s.fetchAthleteResponses(ctx, espnID)
	if err != nil {
		return playerprofile.Profile{}, err
	}
	return assembleAthleteProfile(responses), nil
}

// fetchAthleteResponses reads the four athlete responses in parallel. The core
// record is required: when ESPN has no such athlete, the player is unknown.
func (s ProfileSource) fetchAthleteResponses(ctx context.Context, espnID int64) (athleteResponses, error) {
	var responses athleteResponses
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		return s.getJSON(groupCtx, fmt.Sprintf(coreAthleteURL, espnID), "", &responses.bio)
	})
	group.Go(func() error {
		return ignoreUnknownPlayer(s.getJSON(groupCtx, fmt.Sprintf(athleteURL, espnID), "", &responses.summary))
	})
	group.Go(func() error {
		return ignoreUnknownPlayer(s.getJSON(groupCtx, fmt.Sprintf(athleteOverviewURL, espnID), "", &responses.overview))
	})
	group.Go(func() error {
		var err error
		responses.fantasy, responses.fantasyListed, err = s.fetchFantasyPlayer(groupCtx, espnID)
		return err
	})
	if err := group.Wait(); err != nil {
		return athleteResponses{}, err
	}
	return responses, nil
}

func assembleAthleteProfile(responses athleteResponses) playerprofile.Profile {
	profile := bioProfile(responses.bio)
	profile.College = strings.TrimSpace(responses.summary.Athlete.College.Name)
	profile.SeasonStats = seasonStats(responses.summary.Athlete.StatsSummary.Statistics)
	profile.PositionRank = positionRank(responses.overview.Fantasy.PositionRank)
	if responses.fantasyListed {
		applyFantasyPlayer(&profile, responses.fantasy)
	}
	return profile
}

func (s ProfileSource) defenseProfile(ctx context.Context, espnID int64) (playerprofile.Profile, error) {
	record, listed, err := s.fetchFantasyPlayer(ctx, espnID)
	if err != nil {
		return playerprofile.Profile{}, err
	}
	if !listed {
		return playerprofile.Profile{}, playerprofile.ErrUnknownPlayer
	}
	profile := playerprofile.Profile{HeadshotURL: defenseLogoURL(record.ProTeamID)}
	applyFantasyPlayer(&profile, record)
	return profile, nil
}

// fetchFantasyPlayer reads one player's season fantasy record; listed is false
// when ESPN's fantasy game doesn't carry the player.
func (s ProfileSource) fetchFantasyPlayer(ctx context.Context, espnID int64) (fantasyPlayer, bool, error) {
	if s.Season < 1900 || s.Season > 2200 {
		return fantasyPlayer{}, false, fmt.Errorf("valid ESPN profile season required")
	}
	var records []fantasyPlayer
	endpoint := fmt.Sprintf(fantasyPlayersURL, s.Season)
	if err := s.getJSON(ctx, endpoint, fantasyPlayerFilter(espnID), &records); err != nil {
		return fantasyPlayer{}, false, ignoreUnknownPlayer(err)
	}
	record, listed := findFantasyPlayer(records, espnID)
	return record, listed, nil
}

// fantasyPlayerFilter limits ESPN's season player pool to one player; without
// it ESPN returns every player.
func fantasyPlayerFilter(espnID int64) string {
	return fmt.Sprintf(`{"filterIds":{"value":[%d]}}`, espnID)
}

func findFantasyPlayer(records []fantasyPlayer, espnID int64) (fantasyPlayer, bool) {
	for _, record := range records {
		if record.ID == espnID {
			return record, true
		}
	}
	return fantasyPlayer{}, false
}

// getJSON reads a public ESPN endpoint into result. HTTP 404 means ESPN has no
// such player and is reported as playerprofile.ErrUnknownPlayer.
func (s ProfileSource) getJSON(ctx context.Context, endpoint, fantasyFilter string, result any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("construct ESPN profile request: %w", err)
	}
	if fantasyFilter != "" {
		request.Header.Set("X-Fantasy-Filter", fantasyFilter)
	}
	response, err := s.httpClient().Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ESPN profile request failed: %w", err)
	}
	defer response.Body.Close()
	return decodeProfileResponse(response, result)
}

func decodeProfileResponse(response *http.Response, result any) error {
	if response.StatusCode == http.StatusNotFound {
		return playerprofile.ErrUnknownPlayer
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("ESPN profile request returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, profileMaxBody+1))
	if err != nil {
		return fmt.Errorf("read ESPN profile response: %w", err)
	}
	if len(body) > profileMaxBody {
		return fmt.Errorf("ESPN profile response exceeds size limit")
	}
	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("invalid ESPN profile response: %w", err)
	}
	return nil
}

func (s ProfileSource) httpClient() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func ignoreUnknownPlayer(err error) error {
	if errors.Is(err, playerprofile.ErrUnknownPlayer) {
		return nil
	}
	return err
}

func bioProfile(bio coreAthlete) playerprofile.Profile {
	return playerprofile.Profile{
		HeadshotURL:  strings.TrimSpace(bio.Headshot.Href),
		Jersey:       strings.TrimSpace(bio.Jersey),
		BirthDate:    birthDate(bio.DateOfBirth),
		HeightInches: positiveInt(bio.Height),
		WeightLbs:    positiveInt(bio.Weight),
		Draft:        draftSelection(bio),
	}
}

// birthDate keeps the calendar date of ESPN's "1995-09-17T07:00Z" timestamps,
// which encode midnight US time rather than a meaningful instant.
func birthDate(raw string) *time.Time {
	if len(raw) < len(time.DateOnly) {
		return nil
	}
	date, err := time.Parse(time.DateOnly, raw[:len(time.DateOnly)])
	if err != nil {
		return nil
	}
	return &date
}

func positiveInt(value float64) *int {
	rounded := int(math.Round(value))
	if rounded <= 0 {
		return nil
	}
	return &rounded
}

// draftSelection reports the draft only when ESPN gives a complete selection;
// undrafted players have no draft record.
func draftSelection(bio coreAthlete) *playerprofile.Draft {
	draft := bio.Draft
	if draft == nil || draft.Year == 0 || draft.Round <= 0 || draft.Selection <= 0 {
		return nil
	}
	return &playerprofile.Draft{Year: draft.Year, Round: draft.Round, Pick: draft.Selection}
}

// seasonStats keeps ESPN's position-specific summary order, labeling each stat
// with its short display name (e.g. "Pass Yards").
func seasonStats(stats []athleteStat) []playerprofile.Stat {
	var result []playerprofile.Stat
	for _, stat := range stats {
		label := statLabel(stat)
		if label == "" {
			continue
		}
		result = append(result, playerprofile.Stat{Label: label, Value: stat.Value, Rank: positiveInt(float64(stat.Rank))})
	}
	return result
}

func statLabel(stat athleteStat) string {
	if label := strings.TrimSpace(stat.ShortDisplayName); label != "" {
		return label
	}
	return strings.TrimSpace(stat.DisplayName)
}

// positionRank parses ESPN's season-to-date fantasy position rank, sent as a
// string such as "14".
func positionRank(raw string) *int {
	rank, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || rank <= 0 {
		return nil
	}
	return &rank
}

func applyFantasyPlayer(profile *playerprofile.Profile, record fantasyPlayer) {
	profile.Position = defaultPosition(record.DefaultPositionID)
	if record.Ownership != nil {
		profile.PercentRostered = &record.Ownership.PercentOwned
		profile.PercentStarted = &record.Ownership.PercentStarted
	}
	profile.WeeklyOutlook = latestOutlook(record.Outlooks.OutlooksByWeek)
	profile.SeasonOutlook = strings.TrimSpace(record.SeasonOutlook)
}

// defaultPosition maps ESPN's defaultPositionId to the player's primary fantasy
// position. Positions League Lens doesn't track, such as IDP ones, are empty.
func defaultPosition(id int) fantasy.Position {
	positions := map[int]fantasy.Position{
		1:  fantasy.PositionQB,
		2:  fantasy.PositionRB,
		3:  fantasy.PositionWR,
		4:  fantasy.PositionTE,
		5:  fantasy.PositionK,
		16: fantasy.PositionDEF,
	}
	return positions[id]
}

// latestOutlook picks the newest NFL week's analyst outlook. ESPN keeps past
// weeks and skips weeks it didn't write one for, such as byes.
func latestOutlook(byWeek map[string]string) *playerprofile.Outlook {
	var latest *playerprofile.Outlook
	for rawWeek, rawText := range byWeek {
		week, err := strconv.Atoi(rawWeek)
		text := strings.TrimSpace(rawText)
		if err != nil || week <= 0 || text == "" || (latest != nil && week <= latest.Week) {
			continue
		}
		latest = &playerprofile.Outlook{Week: week, Text: text}
	}
	return latest
}

func defenseLogoURL(proTeamID int) string {
	abbreviation := proTeamAbbreviation(proTeamID)
	if abbreviation == "" {
		return ""
	}
	return fmt.Sprintf(teamLogoURL, strings.ToLower(abbreviation))
}
