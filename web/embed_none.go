//go:build !embedweb

package web

import "io/fs"

// Dist returns nil when the frontend is not embedded.
func Dist() fs.FS {
	return nil
}
