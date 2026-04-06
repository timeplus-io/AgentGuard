//go:build embed

package web

import (
	"embed"
	"io/fs"
)

//go:embed dist
var embeddedFS embed.FS

// FS returns the embedded frontend dist as an fs.FS rooted at dist/.
func FS() (fs.FS, error) {
	return fs.Sub(embeddedFS, "dist")
}
