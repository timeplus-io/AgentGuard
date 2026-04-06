//go:build !embed

package web

import (
	"io/fs"
	"os"
)

// FS returns the frontend assets from the local ./public directory.
// Used during local development — Vite dev server is the primary frontend
// server, so this path is only hit if you need the built assets locally.
func FS() (fs.FS, error) {
	return os.DirFS("./public"), nil
}
