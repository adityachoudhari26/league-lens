package fantasy

import "testing"

func TestFetchRequestSkipsWeek(t *testing.T) {
	request := FetchRequest{SkipWeeks: []ScoringPeriod{1, 2}}
	for _, test := range []struct {
		name          string
		request       FetchRequest
		week, current ScoringPeriod
		want          bool
	}{
		{"listed", request, 1, 4, true},
		{"unlisted", request, 3, 4, false},
		{"listed but current", request, 1, 1, false},
		{"zero request", FetchRequest{}, 1, 4, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.request.SkipsWeek(test.week, test.current); got != test.want {
				t.Fatalf("SkipsWeek(%d, %d) = %v, want %v", test.week, test.current, got, test.want)
			}
		})
	}
}
