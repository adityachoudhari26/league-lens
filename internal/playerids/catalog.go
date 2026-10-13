// Package playerids reads identity mappings, never matches names.
package playerids

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const URL = "https://raw.githubusercontent.com/dynastyprocess/data/master/files/db_playerids.csv"

type Entry struct {
	Name     string
	Position string
	IDs      map[string]string
}
type Catalog map[string]Entry

func Parse(r io.Reader) (Catalog, error) {
	reader := csv.NewReader(r)
	header, err := reader.Read()
	if err != nil {
		return nil, err
	}
	cols := map[string]int{}
	for i, h := range header {
		cols[h] = i
	}
	for _, h := range []string{"name", "position", "sleeper_id", "espn_id"} {
		if _, ok := cols[h]; !ok {
			return nil, fmt.Errorf("missing mapping column %s", h)
		}
	}
	c := Catalog{}
	seen := map[string]bool{}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		get := func(k string) string {
			n, ok := cols[k]
			if !ok {
				return ""
			}
			v := strings.TrimSpace(row[n])
			if v == "NA" {
				return ""
			}
			return v
		}
		id := get("sleeper_id")
		if id == "" {
			continue
		}
		if seen[id] {
			delete(c, id)
			continue
		}
		seen[id] = true
		e := Entry{Name: get("name"), Position: get("position"), IDs: map[string]string{"sleeper": id}}
		for _, p := range []string{"espn", "gsis", "mfl"} {
			if v := get(p + "_id"); v != "" {
				e.IDs[p] = v
			}
		}
		c[id] = e
	}
	return c, nil
}

// Load uses a daily disk cache; callers may fall back to provider-only identities
// when this optional community dataset is unavailable. No player catalog is inserted.
func Load(ctx context.Context, path string) (Catalog, string, error) {
	var b []byte
	if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) < 24*time.Hour {
		b, err = os.ReadFile(path)
		if err != nil {
			return nil, "", err
		}
	}
	if len(b) == 0 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, URL, nil)
		if err != nil {
			return nil, "", err
		}
		resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
		if err != nil {
			return nil, "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, "", fmt.Errorf("mapping HTTP %d", resp.StatusCode)
		}
		b, err = io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if err != nil {
			return nil, "", err
		}
		if _, err = Parse(strings.NewReader(string(b))); err != nil {
			return nil, "", err
		}
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, "", err
		}
		f, err := os.CreateTemp(filepath.Dir(path), "ids-*")
		if err != nil {
			return nil, "", err
		}
		defer os.Remove(f.Name())
		if _, err = f.Write(b); err != nil {
			f.Close()
			return nil, "", err
		}
		if err = f.Close(); err != nil {
			return nil, "", err
		}
		if err = os.Rename(f.Name(), path); err != nil {
			return nil, "", err
		}
	}
	c, err := Parse(strings.NewReader(string(b)))
	return c, fmt.Sprintf("sha256:%x", sha256.Sum256(b)), err
}
