// Package diskcache keeps small downloaded datasets on disk for a freshness window.
package diskcache

import (
	"os"
	"path/filepath"
	"time"
)

// Path returns a file location in the shared League Lens cache directory.
func Path(name string) (string, error) {
	directory, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "league-lens", name), nil
}

// ReadFresh returns cached contents, or nil when the file is missing or older than maxAge.
func ReadFresh(path string, maxAge time.Duration) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) >= maxAge {
		return nil, nil
	}
	return os.ReadFile(path)
}

// Replace writes contents atomically so readers never see a partial file.
func Replace(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "cache-*")
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
