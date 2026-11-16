// Package web is the built UI (Vite -> web/dist) embedded in the binary.
package web

import (
	"embed"
	"io/fs"
	"os"
)

//go:embed all:dist
var dist embed.FS

// dir is a folder on disk served in place of the embedded copy (the -web flag,
// for development): a rebuilt page shows on reload, without rebuilding the exe.
// Set once in main before the routes, never changed after.
var dir string

// UseDir serves the UI from a folder on disk instead of the embedded copy.
func UseDir(d string) { dir = d }

// FromDisk reports whether the UI comes from a folder on disk.
func FromDisk() bool { return dir != "" }

// Dist is the contents of web/dist: the folder given to UseDir, or the embedded copy.
func Dist() fs.FS {
	if dir != "" {
		return os.DirFS(dir)
	}
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
