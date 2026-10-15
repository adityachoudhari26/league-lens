//go:build embedweb

package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embeddedFiles embed.FS

// Dist returns the built frontend filesystem.
func Dist() fs.FS {
	dist, err := fs.Sub(embeddedFiles, "dist")
	if err != nil {
		panic(err)
	}
	return dist
}
