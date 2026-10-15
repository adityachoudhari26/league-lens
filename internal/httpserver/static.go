package httpserver

import (
	"bytes"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

func staticHandler(static fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Cache-Control", "no-cache")
		requestedFile := strings.TrimPrefix(r.URL.Path, "/")
		isAsset := strings.HasPrefix(r.URL.Path, "/assets/")
		if isAsset {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		if !fs.ValidPath(requestedFile) {
			if isAsset {
				http.NotFound(w, r)
				return
			}
			requestedFile = "index.html"
		}
		content, err := fs.ReadFile(static, requestedFile)
		if err != nil {
			if isAsset {
				http.NotFound(w, r)
				return
			}
			requestedFile = "index.html"
			content, err = fs.ReadFile(static, requestedFile)
		}
		if err != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, requestedFile, time.Time{}, bytes.NewReader(content))
	})
}
