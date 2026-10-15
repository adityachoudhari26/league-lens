package fantasy

import (
	"testing"
	"time"
)

func TestCurrentSeason(t *testing.T) {
	for _, test := range []struct {
		date string
		want int
	}{
		{date: "2026-02-28T12:00:00Z", want: 2025},
		{date: "2026-03-01T00:00:00Z", want: 2026},
		{date: "2026-12-31T23:59:59Z", want: 2026},
	} {
		t.Run(test.date, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, test.date)
			if err != nil {
				t.Fatal(err)
			}
			if got := CurrentSeason(now); got != test.want {
				t.Fatalf("CurrentSeason(%s) = %d, want %d", test.date, got, test.want)
			}
		})
	}
}
