package main

import (
	"bytes"
	"testing"
)

func TestArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"espn"}, {"sleeper"}, {"sleeper", "--username", "test"}, {"sleeper", "--username", "test", "--league-id", "1", "extra"}} {
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
