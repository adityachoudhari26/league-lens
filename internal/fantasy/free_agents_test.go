package fantasy

import "testing"

func TestSortFreeAgentsOrdersPointsNilLastAndLimits(t *testing.T) {
	agents := []FreeAgent{
		{Name: "No projection"},
		{Name: "Low", ProjectedPoints: floatPointer(4)},
		{Name: "High", ProjectedPoints: floatPointer(12)},
		{Name: "Middle", ProjectedPoints: floatPointer(8)},
	}
	sorted := SortFreeAgents(agents, 2)
	if len(sorted) != 2 || sorted[0].Name != "High" || sorted[1].Name != "Middle" {
		t.Fatalf("sorted agents = %+v", sorted)
	}
}

func TestSortFreeAgentsCapsProviderResultLimit(t *testing.T) {
	agents := make([]FreeAgent, 30)
	if sorted := SortFreeAgents(agents, 100); len(sorted) != 25 {
		t.Fatalf("result count = %d, want max 25", len(sorted))
	}
}

func floatPointer(value float64) *float64 {
	return &value
}
