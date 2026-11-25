PYTHON ?= python3.12

.PHONY: install test build clean

## install: install the plugin and the local AgentGuard SDK in editable mode
install:
	$(PYTHON) -m pip install -e ../../../sdk/python -e . --break-system-packages --quiet

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
