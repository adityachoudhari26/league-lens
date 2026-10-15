package main

import (
	"bytes"
	"testing"
)

func TestArguments(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"espn"},
		{"sleeper"},
		{"sleeper", "--username", "test"},
		{"sleeper", "--username", "test", "--league-id", "1", "extra"},
	} {
		var out bytes.Buffer
		if err := run(args, &out, &out); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	var out bytes.Buffer
	if err := run([]string{"sleeper", "--help"}, &out, &out); err != nil {
		t.Fatal(err)
	}
}

func TestESPNArguments(t *testing.T) {
	var out bytes.Buffer
	options, err := parseImportOptions([]string{"espn", "--league-id", "123", "--season", "2026", "--team-id", "2"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if options.leagueID != "123" {
		t.Fatal("lost league selection")
	}
	for _, args := range [][]string{
		{"espn", "--league-id", "123", "--team-id", "2"},
		{"espn", "--league-id", "123", "--season", "2026"},
		{"espn", "--league-id", "123", "--season", "2026", "--team-id", "0"},
	} {
		if _, err := parseImportOptions(args, &out); err == nil {
			t.Fatalf("accepted invalid selection %v", args)
		}
	}
}
