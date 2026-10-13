package playerids

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMappingFallback(t *testing.T) {
	c, err := Parse(strings.NewReader("name,position,sleeper_id,espn_id,gsis_id,mfl_id\nExample,QB,10,100,00-1,200\nAmbiguous,RB,20,101,,\nOther,RB,20,102,,\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c["10"].IDs["espn"] != "100" || c["10"].Name != "Example" {
		t.Fatal("mapping missing")
	}
	if _, ok := c["20"]; ok {
		t.Fatal("ambiguous ID guessed")
	}
	if _, err := Parse(strings.NewReader("bad,header\na,b\n")); err == nil {
		t.Fatal("bad columns accepted")
	}
}

func TestFreshCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ids.csv")
	if err := os.WriteFile(path, []byte("name,position,sleeper_id,espn_id\nCached,QB,10,100\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	catalog, version, err := Load(ctx, path)
	if err != nil || catalog["10"].Name != "Cached" || !strings.HasPrefix(version, "sha256:") {
		t.Fatalf("cache not used: %v %s", err, version)
	}
}
