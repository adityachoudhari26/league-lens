package espn

import "encoding/json"

type leagueResponse struct {
	ID     int64 `json:"id"`
	Season int   `json:"seasonId"`
	Week   int   `json:"scoringPeriodId"`
	Status struct {
		LatestWeek int `json:"latestScoringPeriod"`
		FinalWeek  int `json:"finalScoringPeriod"`
	} `json:"status"`
	Settings settingsResponse  `json:"settings"`
	Teams    []teamResponse    `json:"teams"`
	Schedule []matchupResponse `json:"schedule"`
}
type settingsResponse struct {
	Name     string          `json:"name"`
	Size     int             `json:"size"`
	Roster   json.RawMessage `json:"rosterSettings"`
	Scoring  json.RawMessage `json:"scoringSettings"`
	Schedule struct {
		Periods map[string][]int `json:"matchupPeriods"`
	} `json:"scheduleSettings"`
}
type teamResponse struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Record *struct {
		Overall *recordResponse `json:"overall"`
	} `json:"record"`
	Roster *struct {
		Entries *[]entryResponse `json:"entries"`
	} `json:"roster"`
}
type recordResponse struct {
	Wins          *int     `json:"wins"`
	Losses        *int     `json:"losses"`
	Ties          *int     `json:"ties"`
	PointsFor     *float64 `json:"pointsFor"`
	PointsAgainst *float64 `json:"pointsAgainst"`
}
type entryResponse struct {
	PlayerID int `json:"playerId"`
	Slot     int `json:"lineupSlotId"`
	Pool     struct {
		Player           playerResponse `json:"player"`
		AppliedStatTotal *float64       `json:"appliedStatTotal"`
	} `json:"playerPoolEntry"`
}
type playerResponse struct {
	ID            int            `json:"id"`
	Name          string         `json:"fullName"`
	Position      int            `json:"defaultPositionId"`
	ProTeam       int            `json:"proTeamId"`
	InjuryStatus  string         `json:"injuryStatus"`
	EligibleSlots []int          `json:"eligibleSlots"`
	Stats         []statResponse `json:"stats"`
}
type statResponse struct {
	SourceID        int      `json:"statSourceId"`
	SplitTypeID     int      `json:"statSplitTypeId"`
	ScoringPeriodID int      `json:"scoringPeriodId"`
	AppliedTotal    *float64 `json:"appliedTotal"` // League-scored total for this stat line.
}
type matchupResponse struct {
	ID     int           `json:"id"`
	Period int           `json:"matchupPeriodId"`
	Winner string        `json:"winner"`
	Home   *sideResponse `json:"home"`
	Away   *sideResponse `json:"away"`
}
type sideResponse struct {
	TeamID int             `json:"teamId"`
	Points *float64        `json:"totalPoints"`
	Lineup *lineupResponse `json:"rosterForCurrentScoringPeriod"`
}
type lineupResponse struct {
	Entries []entryResponse `json:"entries"`
}
