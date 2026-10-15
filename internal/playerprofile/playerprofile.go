// Package playerprofile defines source-independent player profile contracts
// for the player info card.
package playerprofile

import (
	"context"
	"errors"
	"time"
)

// ErrUnknownPlayer reports that a source has no profile for the player, for
// example because it lacks that source's player ID.
var ErrUnknownPlayer = errors.New("player not known to profile source")

// Player is the player to look up; ExternalIDs maps provider names to that
// provider's player identifier.
type Player struct {
	ID          string
	ExternalIDs map[string]string
}

// Profile is a player's descriptive and fantasy context at fetch time. Empty
// strings, nil pointers, and empty slices mean the source didn't report it.
type Profile struct {
	HeadshotURL     string
	Jersey          string
	BirthDate       *time.Time
	HeightInches    *int
	WeightLbs       *int
	College         string
	Draft           *Draft
	PositionRank    *int
	PercentRostered *float64
	PercentStarted  *float64
	WeeklyOutlook   *Outlook
	SeasonOutlook   string
	SeasonStats     []Stat
}

// Draft is the player's NFL draft selection; undrafted players have no Draft.
type Draft struct {
	Year  int
	Round int
	Pick  int
}

// Outlook is a fantasy analyst outlook written for one NFL week.
type Outlook struct {
	Week int
	Text string
}

// Stat is one season-to-date stat in source display order, such as "Pass
// Yards" 1037 ranked 17th. Rank is nil when the source gives none.
type Stat struct {
	Label string
	Value float64
	Rank  *int
}

// Source fetches a player's current profile. It returns ErrUnknownPlayer when
// it cannot identify the player.
type Source interface {
	Name() string
	Profile(ctx context.Context, player Player) (Profile, error)
}
