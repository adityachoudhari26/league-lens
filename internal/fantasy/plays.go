package fantasy

import "time"

// NFLPlayKind is the normalized kind of a play from a public NFL play-by-play feed.
type NFLPlayKind string

const (
	NFLPlayRush             NFLPlayKind = "rush"
	NFLPlayRushingTouchdown NFLPlayKind = "rushing_touchdown"
	NFLPlayPassReception    NFLPlayKind = "pass_reception"
	NFLPlayPassingTouchdown NFLPlayKind = "passing_touchdown"
	NFLPlayPassIncompletion NFLPlayKind = "pass_incompletion"
	NFLPlayInterception     NFLPlayKind = "interception"
	NFLPlayFieldGoalGood    NFLPlayKind = "field_goal_good"
	NFLPlayEndOfGame        NFLPlayKind = "end_of_game"
	NFLPlayOther            NFLPlayKind = "other"
)

// NFLPlayParticipant identifies a person and their role on a play.
type NFLPlayParticipant struct {
	AthleteID string
	Role      string
}

// NFLPlay is one normalized NFL play-by-play event.
type NFLPlay struct {
	At             time.Time
	Period         int
	Clock          time.Duration
	Kind           NFLPlayKind
	Yards          int
	ExtraPointGood bool
	Participants   []NFLPlayParticipant
}
