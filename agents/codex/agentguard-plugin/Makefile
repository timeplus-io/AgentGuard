PYTHON ?= python3.12

.PHONY: test build check clean

## test: run the plugin unit tests (stdlib only, no SDK needed)
test:
	$(PYTHON) -m unittest discover -s tests -v

## build: build sdist + wheel into dist/ from a clean dist/ (needs hatch)
build: clean
	$(PYTHON) -m hatch build

## check: validate the built distributions' metadata and README rendering (needs twine)
check: build
	$(PYTHON) -m twine check --strict dist/*

## clean: remove build artifacts
clean:
	rm -rf dist/ *.egg-info agentguard_codex.egg-info .hatch .ruff_cache .pytest_cache
