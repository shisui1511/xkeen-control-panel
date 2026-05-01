package xkeencontrolpanel

import (
	"embed"
	"io/fs"
)

//go:embed web
var WebFS embed.FS

func GetWebFS() (fs.FS, error) {
	return fs.Sub(WebFS, "web")
}
