package espn

import (
	"testing"

	"github.com/adityachoudhari26/league-lens/internal/fantasy"
)

func TestInjuryStatusMapsKnownDesignations(t *testing.T) {
	cases := map[string]fantasy.InjuryStatus{
		"ACTIVE":         fantasy.InjuryActive,
		"QUESTIONABLE":   fantasy.InjuryQuestionable,
		"DOUBTFUL":       fantasy.InjuryDoubtful,
		"OUT":            fantasy.InjuryOut,
		"SUSPENSION":     fantasy.InjuryOut,
		"INJURY_RESERVE": fantasy.InjuryReserve,
		"DAY_TO_DAY":     fantasy.InjuryUnknown,
		"":               fantasy.InjuryUnknown,
	}
	for raw, want := range cases {
		if got := injuryStatus(raw); got != want {
			t.Errorf("injuryStatus(%q) = %q, want %q", raw, got, want)
		}
	}
	if got := proTeamAbbreviation(12); got != "KC" {
		t.Errorf("proTeamAbbreviation(12) = %q, want KC", got)
	}
	if got := proTeamAbbreviation(0); got != "" {
		t.Errorf("free agent team = %q, want empty", got)
	}
}
