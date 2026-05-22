package xkeencontrolpanel

import (
	"embed"
	"io/fs"
)

// WebFS embeds the static frontend assets from frontend/dist directory.
//go:embed frontend/dist
var WebFS embed.FS

// GetWebFS returns the embedded static frontend assets as fs.FS filesystem.
func GetWebFS() (fs.FS, error) {
	return fs.Sub(WebFS, "frontend/dist")
}
