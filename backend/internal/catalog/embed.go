package catalog

import (
	"embed"
	"io/fs"
	"log"
)

//go:embed initial
var rawInitialFS embed.FS

// InitialFS is the embedded initial catalog, rooted at the YAML files directly
// (the "initial/" prefix is stripped so loadFS sees "rp-001.yaml" etc.).
var InitialFS fs.FS

func init() {
	sub, err := fs.Sub(rawInitialFS, "initial")
	if err != nil {
		log.Fatalf("catalog: failed to sub embedded FS: %v", err)
	}
	InitialFS = sub
}
