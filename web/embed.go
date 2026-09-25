// Package web exposes the compiled frontend (web/dist) to the Go binary.
package web

import (
	"embed"
	"io/fs"
)

// dist holds the Vite build output. The directory always contains a
// committed .gitkeep so the embed pattern matches even before the first
// frontend build; in that case the server serves a "not built" notice.
//
//go:embed all:dist
var dist embed.FS

// Dist returns the build output rooted at web/dist.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // "dist" is a literal embedded directory; cannot fail
	}
	return sub
}
