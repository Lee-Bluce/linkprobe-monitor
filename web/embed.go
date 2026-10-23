package web

import (
	"embed"
	"io/fs"
)

// files holds the Vite build output. scripts/build.ps1 (or build.sh) rebuilds
// it before the Go binary is compiled, so the shipped binary is self-contained.
//
//go:embed all:dist
var files embed.FS

// Dist returns the web root (index.html at "/").
func Dist() fs.FS {
	sub, err := fs.Sub(files, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
