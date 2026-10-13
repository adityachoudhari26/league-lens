package playerids

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Load uses a daily disk cache; callers may fall back to provider-only identities
// when this optional community dataset is unavailable. No player catalog is inserted.
func Load(ctx context.Context, path string) (Catalog, string, error) {
	contents, err := readFreshCache(path)
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
		if err = replaceCache(path, contents); err != nil {
			return nil, "", err
		}
	}
	catalog, err := Parse(strings.NewReader(string(contents)))
	version := fmt.Sprintf("sha256:%x", sha256.Sum256(contents))
	return catalog, version, err
}

func readFreshCache(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) >= 24*time.Hour {
		return nil, nil
	}
	return os.ReadFile(path)
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

func replaceCache(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "ids-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err = temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}
