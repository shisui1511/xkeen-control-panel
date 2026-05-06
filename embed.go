package xkeencontrolpanel

import (
	"embed"
	"io/fs"
)

//go:embed frontend/dist
var WebFS embed.FS

func GetWebFS() (fs.FS, error) {
	return fs.Sub(WebFS, "frontend/dist")
}
