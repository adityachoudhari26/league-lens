package playerids

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/adityachoudhari26/league-lens/internal/diskcache"
)

// DefaultCachePath is the shared on-disk location of the daily catalog cache.
func DefaultCachePath() (string, error) {
	return diskcache.Path("playerids.csv")
}

// Load uses a daily disk cache; callers may fall back to provider-only identities
// when this optional community dataset is unavailable. No player catalog is inserted.
func Load(ctx context.Context, path string) (Catalog, string, error) {
	contents, err := diskcache.ReadFresh(path, 24*time.Hour)
	if err != nil {
		return nil, "", err
	}
	if len(contents) == 0 {
		contents, err = downloadCatalog(ctx)
		if err != nil {
			return nil, "", err
		}
		if _, err = Parse(strings.NewReader(string(contents))); err != nil {
			return nil, "", err
		}
		if err = diskcache.Replace(path, contents); err != nil {
			return nil, "", err
		}
	}
	catalog, err := Parse(strings.NewReader(string(contents)))
	version := fmt.Sprintf("sha256:%x", sha256.Sum256(contents))
	return catalog, version, err
}

func downloadCatalog(ctx context.Context) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, URL, nil)
	if err != nil {
		return nil, err
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("mapping HTTP %d", response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, 8<<20))
}
