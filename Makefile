.PHONY: all build test clean package-plugins \
        claudecode-build claudecode-test \
        openclaw-build openclaw-test \
        hermes-build hermes-test help

CLAUDECODE := agents/claudecode/agentguard-plugin
OPENCLAW   := agents/openclaw/agentguard-plugin
HERMES     := agents/hermes/agentguard-plugin
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
