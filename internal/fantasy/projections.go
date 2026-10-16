package fantasy

import "context"

// RestOfSeasonFinalWeek is the last week counted; leagues do not store their regular-season end week yet.
const RestOfSeasonFinalWeek = 17

// RestOfSeasonQuery selects a projection window and provider player IDs.
type RestOfSeasonQuery struct {
	Week              int
	ExternalPlayerIDs []string
}

// RestOfSeasonProjection contains league-scored projections and covered games.
type RestOfSeasonProjection struct {
	Points float64
	Weeks  int
}

// RestOfSeasonProjector returns league-scored projected points from each player's next
// unplayed week through RestOfSeasonFinalWeek. Players without a projection are absent from the map.
type RestOfSeasonProjector interface {
	ProjectRestOfSeason(ctx context.Context, league League, query RestOfSeasonQuery) (map[string]RestOfSeasonProjection, error)
}
