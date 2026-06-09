package xkeencontrolpanel

import (
	"embed"
	"io/fs"
)

// WebFS embeds the static frontend assets from frontend/dist directory.
//
//go:embed frontend/dist
var WebFS embed.FS

// GetWebFS returns the embedded static frontend assets as fs.FS filesystem.
func GetWebFS() (fs.FS, error) {
	return fs.Sub(WebFS, "frontend/dist")
}

// TemplatesFS embeds the config templates from internal/templates directory.
//
//go:embed internal/templates
var TemplatesFS embed.FS

// GetTemplatesFS returns the embedded config templates as fs.FS filesystem.
func GetTemplatesFS() (fs.FS, error) {
	return fs.Sub(TemplatesFS, "internal/templates")
}
