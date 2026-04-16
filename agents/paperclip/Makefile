PWD              := $(shell pwd)
DATA_DIR         := $(PWD)/.paperclip
OPENCLAW_WS_URL  ?= ws://localhost:18789
COMPANY_ID       ?= $(shell PAPERCLIP_DATA_DIR="$(DATA_DIR)" npx paperclipai env --json 2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('PAPERCLIP_COMPANY_ID',''))" 2>/dev/null)

.PHONY: help init start stop logs url openclaw-agent clean

## Print available targets
help:
	@echo ""
	@echo "  make init              First-time setup: create config and embedded Postgres in .paperclip/"
	@echo "  make start             Start the Paperclip server (http://127.0.0.1:3100)"
	@echo "  make stop              Stop any running Paperclip process"
	@echo "  make logs              Tail the Paperclip log file"
	@echo "  make url               Print the dashboard URL"
	@echo "  make openclaw-agent    Print the JSON to create an OpenClaw-backed agent"
	@echo "  make clean             Remove all local state (.paperclip/ directory)"
	@echo ""
	@echo "  See SHOWCASE.md for the full Paperclip + OpenClaw + AgentGuard demo."
	@echo ""

## First-time setup — creates .paperclip/ with embedded Postgres, config, and JWT secret.
## Safe to re-run; onboard skips steps that are already done.
init:
	npx paperclipai onboard --yes --data-dir "$(DATA_DIR)"

## Start the Paperclip server in the background.
## On a fresh install, run 'make init' first.
start:
	@echo "Starting Paperclip at http://127.0.0.1:3100 ..."
	PAPERCLIP_DATA_DIR="$(DATA_DIR)" npx paperclipai run --data-dir "$(DATA_DIR)" &
	@echo "Run 'make logs' to follow output, 'make url' for the dashboard link."

## Stop any running Paperclip process
stop:
	@pkill -f "paperclipai" 2>/dev/null && echo "Paperclip stopped." || echo "No running Paperclip process found."

## Tail the Paperclip log file
logs:
	@LOG=$$(ls -t "$(DATA_DIR)"/instances/default/logs/*.log 2>/dev/null | head -1); \
	if [ -n "$$LOG" ]; then tail -f "$$LOG"; \
	else echo "No log file found — has Paperclip been started yet?"; fi

## Print the dashboard URL
url:
	@echo "http://127.0.0.1:3100"

## Print a curl command to create an OpenClaw-backed agent via the Paperclip API.
## Edit the name/role/reportsTo fields before running.
openclaw-agent:
	@echo ""
	@echo "Create an OpenClaw-backed agent (paste into a terminal or script):"
	@echo ""
	@echo "curl -X POST http://127.0.0.1:3100/api/companies/<COMPANY_ID>/agent-hires \\"
	@echo "  -H 'Authorization: Bearer <PAPERCLIP_API_KEY>' \\"
	@echo "  -H 'Content-Type: application/json' \\"
	@echo "  -d '{"
	@echo "    \"name\": \"Engineer\","
	@echo "    \"role\": \"engineer\","
	@echo "    \"title\": \"Software Engineer\","
	@echo "    \"icon\": \"code\","
	@echo "    \"reportsTo\": \"<cto-agent-id>\","
	@echo "    \"capabilities\": \"Implements features, fixes bugs, writes tests. Routes through OpenClaw.\","
	@echo "    \"adapterType\": \"openclaw_gateway\","
	@echo "    \"adapterConfig\": {"
	@echo "      \"url\": \"$(OPENCLAW_WS_URL)\","
	@echo "      \"autoPairOnFirstConnect\": true,"
	@echo "      \"sessionKeyStrategy\": \"issue\""
	@echo "    },"
	@echo "    \"runtimeConfig\": {"
	@echo "      \"heartbeat\": {"
	@echo "        \"enabled\": true,"
	@echo "        \"intervalSec\": 3600,"
	@echo "        \"wakeOnDemand\": true"
	@echo "      }"
	@echo "    }"
	@echo "  }'"
	@echo ""
	@echo "See SHOWCASE.md for the full setup guide."
	@echo ""

## Remove all local Paperclip state (requires re-init)
clean: stop
	rm -rf "$(DATA_DIR)"
	@echo ".paperclip/ removed. Run 'make init' to start fresh."
