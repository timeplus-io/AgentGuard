.PHONY: all build test clean package-plugins \
        claudecode-build claudecode-test \
        openclaw-build openclaw-test \
        hermes-build hermes-test \
        sdk-python-build sdk-python-test sdk-js-build \
        publish-sdk-python publish-sdk-js help

CLAUDECODE := agents/claudecode/agentguard-plugin
OPENCLAW   := agents/openclaw/agentguard-plugin
HERMES     := agents/hermes/agentguard-plugin
SDK_PYTHON := sdk/python
SDK_JS     := sdk/javascript
# The Hermes plugin needs Python >= 3.10.
PYTHON     ?= python3.12

all: build

## build: build all agent plugins
build: claudecode-build openclaw-build hermes-build

## test: run all plugin unit tests
test: claudecode-test openclaw-test hermes-test

# ── Claude Code plugin ───────────────────────────────────────────────────────

## claudecode-build: typecheck and bundle the Claude Code hook plugin
claudecode-build:
	cd $(CLAUDECODE) && npm install && npm run typecheck && npm run build

## claudecode-test: run the Claude Code plugin unit tests
claudecode-test:
	cd $(CLAUDECODE) && npm install && npm test

# ── OpenClaw plugin ──────────────────────────────────────────────────────────

## openclaw-build: compile the OpenClaw plugin
openclaw-build:
	cd $(OPENCLAW) && npm install && npm run build

## openclaw-test: run the OpenClaw plugin unit tests
openclaw-test:
	cd $(OPENCLAW) && npm install && npm test

# ── Hermes plugin ────────────────────────────────────────────────────────────

## hermes-build: build the Hermes plugin wheel into its dist/
hermes-build:
	cd $(HERMES) && $(PYTHON) -m pip install build hatchling --quiet && $(PYTHON) -m build --wheel

## hermes-test: run the Hermes plugin unit tests
hermes-test:
	cd $(HERMES) && $(PYTHON) -m unittest discover -s tests -v

# ── SDKs ─────────────────────────────────────────────────────────────────────
# The SDKs are published independently of the plugins and are not part of
# `make build` / `make test`.

## sdk-python-build: build and validate the Python SDK sdist + wheel
sdk-python-build:
	$(MAKE) -C $(SDK_PYTHON) check PYTHON=$(PYTHON)

## sdk-python-test: run the Python SDK unit tests (needs its dev extras installed)
sdk-python-test:
	$(MAKE) -C $(SDK_PYTHON) test PYTHON=$(PYTHON)

## sdk-js-build: build the JavaScript SDK and list what would be published
sdk-js-build:
	$(MAKE) -C $(SDK_JS) pack

## publish-sdk-python: upload the Python SDK to PyPI (needs sdk/python/.pypirc)
publish-sdk-python:
	$(MAKE) -C $(SDK_PYTHON) publish PYTHON=$(PYTHON)

## publish-sdk-js: publish the JavaScript SDK to npm (needs `npm login`)
publish-sdk-js:
	$(MAKE) -C $(SDK_JS) publish

# ── Packaging ────────────────────────────────────────────────────────────────

## package-plugins: build all agent plugins and pack them into packages/ for offline distribution
## Output: packages/timeplus-agentguard-claudecode-plugin-*.tgz
##         packages/timeplus-agentguard-openclaw-plugin-*.tgz
##         packages/agentguard_hermes_plugin-*.whl
package-plugins:
	@mkdir -p packages
	cd $(CLAUDECODE) && npm install && npm run build && npm pack --pack-destination ../../../packages
	cd $(OPENCLAW) && npm install && npm run build && npm pack --pack-destination ../../../packages
	cd $(HERMES) && $(PYTHON) -m pip install build hatchling --quiet && $(PYTHON) -m build --wheel --outdir ../../../packages
	@echo ""
	@echo "  ✓ Plugin packages written to packages/"
	@ls -1 packages/

## clean: remove build outputs
clean:
	rm -rf packages $(CLAUDECODE)/dist $(OPENCLAW)/dist $(HERMES)/dist

## help: list targets
help:
	@grep -E '^## ' Makefile | sed 's/^## //'
