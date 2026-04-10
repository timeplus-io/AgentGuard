package catalog

import (
	"embed"
	"io/fs"
	"log"
)

//go:embed seed
var rawSeedFS embed.FS

// SeedFS is the embedded seed catalog, rooted at the YAML files directly
// (the "seed/" prefix is stripped so loadFS sees "rp-001.yaml" etc.).
var SeedFS fs.FS

func init() {
	sub, err := fs.Sub(rawSeedFS, "seed")
	if err != nil {
		log.Fatalf("catalog: failed to sub embedded FS: %v", err)
	}
	SeedFS = sub
}
