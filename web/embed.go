// Package web embeds the built single-page application so it can be served by
// the aeman binary without any external assets.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// DistFS returns the embedded frontend file system rooted at the build output.
func DistFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
