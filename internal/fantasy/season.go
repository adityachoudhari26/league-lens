package fantasy

import "time"

// CurrentSeason returns the NFL season that began the previous September.
func CurrentSeason(now time.Time) int {
	season := now.Year()
	if now.Month() < time.March {
		season--
	}
	return season
}
