PYTHON ?= python3.12

.PHONY: test build clean

## test: run the plugin unit tests (stdlib only, no SDK needed)
test:
	$(PYTHON) -m unittest discover -s tests -v

## build: build sdist + wheel into dist/
build:
	$(PYTHON) -m pip install build hatchling --quiet
	$(PYTHON) -m build

## clean: remove build artifacts
clean:
	rm -rf dist/ *.egg-info agentguard_codex.egg-info .hatch .ruff_cache .pytest_cache
