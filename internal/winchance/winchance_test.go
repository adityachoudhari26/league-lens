package winchance

import (
	"math"
	"testing"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func TestEstimateWinChance(t *testing.T) {
	projected := func(points float64) *float64 { return &points }
	tests := []struct {
		name string
		a, b Side
		want float64
	}{
		{
			name: "equal pregame sides",
			a:    Side{Starters: []Starter{{Projected: projected(15), Remaining: 1}}},
			b:    Side{Starters: []Starter{{Projected: projected(15), Remaining: 1}}},
			want: 0.5,
		},
		{
			name: "done and leading",
			a:    Side{Score: 10, Starters: []Starter{{Projected: projected(15), Remaining: 0}}},
			b:    Side{Score: 9, Starters: []Starter{{Projected: projected(15), Remaining: 0}}},
			want: 1,
		},
		{
			name: "done and trailing",
			a:    Side{Score: 9, Starters: []Starter{{Projected: projected(15), Remaining: 0}}},
			b:    Side{Score: 10, Starters: []Starter{{Projected: projected(15), Remaining: 0}}},
			want: 0,
		},
		{
			name: "done and tied",
			a:    Side{Score: 10, Starters: []Starter{{Projected: projected(15), Remaining: 0}}},
			b:    Side{Score: 10, Starters: []Starter{{Projected: projected(15), Remaining: 0}}},
			want: 0.5,
		},
		{
			name: "opponent has a projected starter left",
			a:    Side{Score: 10},
			b:    Side{Starters: []Starter{{Projected: projected(15), Remaining: 1}}},
			want: 0.5 * (1 + math.Erf(((10-15)/(SpreadPerProjectedPoint*15))/math.Sqrt2)),
		},
		{
			name: "nil projection adds nothing",
			a:    Side{Score: 5, Starters: []Starter{{Projected: nil, Remaining: 1}}},
			b:    Side{Score: 5},
			want: 0.5,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Estimate(test.a, test.b)
			if math.Abs(got.WinChanceA-test.want) > 1e-9 {
				t.Fatalf("WinChanceA = %.12f, want %.12f", got.WinChanceA, test.want)
			}
		})
	}
}

func TestEstimateClampsRemaining(t *testing.T) {
	got := Estimate(
		Side{Starters: []Starter{{Projected: floatPointer(10), Remaining: -1}}},
		Side{Starters: []Starter{{Projected: floatPointer(10), Remaining: 2}}},
	)
	wantChance := 0.5 * (1 + math.Erf((-10/(SpreadPerProjectedPoint*10))/math.Sqrt2))
	if got.ProjectedA != 0 || got.ProjectedB != 10 {
		t.Fatalf("projected scores = %v/%v, want 0/10", got.ProjectedA, got.ProjectedB)
	}
	if math.Abs(got.WinChanceA-wantChance) > 1e-9 {
		t.Fatalf("WinChanceA = %.12f, want %.12f", got.WinChanceA, wantChance)
	}
}

func TestEstimateIsSymmetric(t *testing.T) {
	a := Side{Score: 31, Starters: []Starter{{Projected: floatPointer(18), Remaining: 0.7}}}
	b := Side{Score: 27, Starters: []Starter{{Projected: floatPointer(12), Remaining: 0.4}}}
	chanceA := Estimate(a, b).WinChanceA
	chanceB := Estimate(b, a).WinChanceA
	if math.Abs(chanceA+chanceB-1) > 1e-9 {
		t.Fatalf("swapped win chances = %.12f + %.12f, want 1", chanceA, chanceB)
	}
}

func TestEstimateProjectedScore(t *testing.T) {
	got := Estimate(Side{
		Score: 4,
		Starters: []Starter{
			{Projected: floatPointer(10), Remaining: 0.5},
			{Projected: floatPointer(20), Remaining: 1},
			{Projected: nil, Remaining: 1},
			{Projected: floatPointer(-5), Remaining: 1},
		},
	}, Side{})
	if got.ProjectedA != 29 {
		t.Fatalf("ProjectedA = %v, want 29", got.ProjectedA)
	}
}

func TestGameRemaining(t *testing.T) {
	for _, test := range []struct {
		name string
		game fantasy.NFLGameStatus
		want float64
	}{
		{name: "scheduled", game: fantasy.NFLGameStatus{State: fantasy.NFLGameScheduled}, want: 1},
		{name: "live before first period", game: fantasy.NFLGameStatus{State: fantasy.NFLGameLive, Period: 0}, want: 1},
		{name: "live Q1 15:00", game: fantasy.NFLGameStatus{State: fantasy.NFLGameLive, Period: 1, Clock: 15 * time.Minute}, want: 1},
		{name: "live Q2 7:30", game: fantasy.NFLGameStatus{State: fantasy.NFLGameLive, Period: 2, Clock: 7*time.Minute + 30*time.Second}, want: 0.625},
		{name: "halftime", game: fantasy.NFLGameStatus{State: fantasy.NFLGameLive, Period: 2}, want: 0.5},
		{name: "live Q4 0:00", game: fantasy.NFLGameStatus{State: fantasy.NFLGameLive, Period: 4}, want: 0},
		{name: "overtime", game: fantasy.NFLGameStatus{State: fantasy.NFLGameLive, Period: 5, Clock: 10 * time.Minute}, want: 0},
		{name: "final", game: fantasy.NFLGameStatus{State: fantasy.NFLGameFinal, Period: 4}, want: 0},
		{name: "postponed", game: fantasy.NFLGameStatus{State: fantasy.NFLGamePostponed, Period: 2}, want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := GameRemaining(test.game); got != test.want {
				t.Fatalf("GameRemaining() = %v, want %v", got, test.want)
			}
		})
	}
}

func floatPointer(value float64) *float64 { return &value }
