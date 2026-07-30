package catalog

import (
	"embed"
	"io/fs"
	"log"
)

//go:embed packages
var rawPackagesFS embed.FS

// PackagesFS is the embedded package-manifest catalog, rooted at the YAML files
// directly (the "packages/" prefix is stripped so loadPackagesFS sees
// "core.yaml" etc.). It is kept separate from the rule SeedFS on purpose: the
// rule loader (loadFS) unmarshals every .yaml it walks into a Definition keyed
// by id, so a package manifest sharing that tree would be mis-parsed as a rule.
var PackagesFS fs.FS

func init() {
	sub, err := fs.Sub(rawPackagesFS, "packages")
	if err != nil {
		log.Fatalf("catalog: failed to sub embedded packages FS: %v", err)
	}
	PackagesFS = sub
}
