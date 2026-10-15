package httpserver

import (
	"bytes"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

// staticHandler falls back to index.html for SPA routes; missing hashed assets
// return 404 so stale HTML is never cached as immutable.
func staticHandler(static fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}

		name, content, ok := resolveStaticFile(static, r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}

		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(content))
	})
}

func resolveStaticFile(static fs.FS, urlPath string) (name string, content []byte, ok bool) {
	requestedFile := strings.TrimPrefix(urlPath, "/")
	if fs.ValidPath(requestedFile) {
		content, err := fs.ReadFile(static, requestedFile)
		if err == nil {
			return requestedFile, content, true
		}
	}

	if strings.HasPrefix(urlPath, "/assets/") {
		return "", nil, false
	}
	content, err := fs.ReadFile(static, "index.html")
	if err != nil {
		return "", nil, false
	}
	return "index.html", content, true
}
