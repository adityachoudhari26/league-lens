package dashboard

import "testing"

func TestProviderLeagueURL(t *testing.T) {
	tests := []struct {
		name             string
		provider         string
		externalLeagueID string
		want             string
	}{
		{"ESPN", "espn", "12345", "https://fantasy.espn.com/football/league?leagueId=12345&seasonId=2026"},
		{"ESPN escapes league ID in query", "espn", "12 34&x=1", "https://fantasy.espn.com/football/league?leagueId=12+34%26x%3D1&seasonId=2026"},
		{"Sleeper", "sleeper", "12345", "https://sleeper.com/leagues/12345"},
		{"Sleeper escapes league ID as one path segment", "sleeper", "a/b?c", "https://sleeper.com/leagues/a%2Fb%3Fc"},
		{"unknown provider", "unknown", "12345", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := providerLeagueURL(tt.provider, tt.externalLeagueID, 2026)
			if got != tt.want {
				t.Fatalf("providerLeagueURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
