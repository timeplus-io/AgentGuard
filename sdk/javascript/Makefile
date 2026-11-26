.PHONY: install build pack publish-dry-run publish clean help

## install: install dependencies from the lockfile
install:
	npm ci

## build: bundle ESM + CJS + type declarations into dist/
build: install
	npm run build

## pack: build and list exactly what would be published, without uploading
pack: build
	npm pack --dry-run

## publish-dry-run: run the full publish flow against the registry without uploading
publish-dry-run: install
	npm publish --dry-run

## publish: publish to npm (prepublishOnly rebuilds dist/; needs `npm login` first)
publish: install
	npm publish

## clean: remove build artifacts
clean:
	rm -rf dist *.tgz

## help: list targets
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
