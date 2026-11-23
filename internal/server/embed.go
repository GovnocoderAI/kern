package server

import (
	"embed"
	"io/fs"
)

//go:embed web
var webFiles embed.FS

// assets returns the embedded web UI rooted at the web/ directory.
func assets() fs.FS {
	sub, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	return sub
}
