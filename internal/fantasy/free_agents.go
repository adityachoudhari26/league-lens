package fantasy

import "sort"

// SortFreeAgents sorts projected points descending, nil last, and applies the result limit.
func SortFreeAgents(agents []FreeAgent, limit int) []FreeAgent {
	sort.SliceStable(agents, func(i, j int) bool {
		left, right := agents[i].ProjectedPoints, agents[j].ProjectedPoints
		if left == nil || right == nil {
			return left != nil
		}
		return *left > *right
	})
	if limit <= 0 {
		limit = 15
	}
	if limit > 25 {
		limit = 25
	}
	if limit > len(agents) {
		limit = len(agents)
	}
	return agents[:limit]
}
