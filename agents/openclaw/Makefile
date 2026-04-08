PWD := $(shell pwd)
CONFIG_FILE := .openclaw/openclaw.json
IMAGE := ghcr.io/openclaw/openclaw:2026.4.5

.PHONY: workspace init run start configure add-agents url devices approve clean install-plugin build-plugin deploy-plugin test

workspace:
	# Create the folder and the nested workspace
	mkdir -p .openclaw/workspace
	# Force ownership to UID 1000 so the container user 'node' can write to it
	sudo chown -R 1000:1000 .openclaw
	# Ensure the directory is actually writable
	sudo chmod -R 777 .openclaw

init: 
	docker run -it --rm \
		-v "$(PWD)/.openclaw":/home/node/.openclaw \
		-v "$(PWD)/.openclaw/workspace":/home/node/.openclaw/workspace \
		-e HOME=/home/node \
		$(IMAGE) \
		node dist/index.js onboard

run:
	docker run -d \
		--name openclaw \
		-p 18789:18789 \
		-v "$(PWD)/.openclaw":/home/node/.openclaw \
		-v "$(PWD)/.openclaw/workspace":/home/node/.openclaw/workspace \
		-v "$(PWD)/agentshield-plugin":/home/node/.openclaw/agentshield-plugin \
		--restart unless-stopped \
		$(IMAGE) \
		node dist/index.js gateway run


define CONFIGURE_SCRIPT
import json, sys
path = "$(CONFIG_FILE)"
with open(path, "r") as f:
    data = json.load(f)

# 1. Ensure gateway binds to all interfaces (required for Docker port mapping)
data.setdefault("gateway", {})["bind"] = "lan"

# 2. Enable diagnostics + OTel exporter (sends traces/metrics to Timeplus)
data["diagnostics"] = {
    "enabled": True,
    "otel": {
        "enabled": True,
        "endpoint": "http://host.docker.internal:4318",
        "traces": True,
        "metrics": True,
        "logs": True,
        "serviceName": "openclaw",
        "sampleRate": 1,
        "flushIntervalMs": 3000
    }
}

# 3. Enable the diagnostics-otel plugin
data.setdefault("plugins", {}).setdefault("entries", {})["diagnostics-otel"] = {"enabled": True}

# 4. Register the agentshield plugin load path
data["plugins"].setdefault("load", {}).setdefault("paths", [])
if "/home/node/.openclaw/agentshield-plugin" not in data["plugins"]["load"]["paths"]:
    data["plugins"]["load"]["paths"].append("/home/node/.openclaw/agentshield-plugin")

# 5. Enable the OpenAI-compatible HTTP endpoint (needed for automated testing)
data.setdefault("gateway", {}).setdefault("http", {}).setdefault("endpoints", {})["chatCompletions"] = {"enabled": True}

# 6. Enable the agentshield plugin with Timeplus config
data["plugins"]["entries"]["agentshield"] = {
    "enabled": True,
    "config": {
        "timeplusUrl": "http://host.docker.internal:3218",
        "stream": "agentshield_hook_events",
        "flushMs": 2000,
        "batchSize": 50,
        "username": "proton",
        "password": "timeplus@t+",
        "deploymentId": "local",
        "deploymentName": "Local Dev"
    }
}

with open(path, "w") as f:
    json.dump(data, f, indent=2)
print("Config updated:", path)
endef
export CONFIGURE_SCRIPT

configure:
	@python3 -c "$$CONFIGURE_SCRIPT"

add-agents:
	# Create an 'analyst' agent with its own isolated workspace.
	# Idempotent: safe to re-run — openclaw skips creation if already present.
	mkdir -p .openclaw/workspace-analyst
	docker exec openclaw node dist/index.js agents add analyst \
		--workspace /home/node/.openclaw/workspace-analyst \
		--non-interactive 2>&1 | grep -v "already exists" || true
	@echo "Agents:"
	@docker exec openclaw node dist/index.js agents list

start:
	docker stop openclaw || true
	docker rm openclaw || true
	docker run -d \
		--name openclaw \
		-p 18789:18789 \
		-v "$(PWD)/.openclaw":/home/node/.openclaw \
		-v "$(PWD)/.openclaw/workspace":/home/node/.openclaw/workspace \
		-v "$(PWD)/agentshield-plugin":/home/node/.openclaw/agentshield-plugin \
		-e HOME=/home/node \
		--restart unless-stopped \
		$(IMAGE) \
		node dist/index.js gateway run

url:
	@TOKEN=$$(grep -o '"token": *"[^"]*"' $(CONFIG_FILE) | head -1 | cut -d'"' -f4); \
	echo "\033[1;32m🚀 Access Dashboard:\033[0m http://localhost:18789/?token=$$TOKEN"

devices:
	docker exec openclaw node dist/index.js devices list

approve:
	@PENDING=$$(docker exec openclaw node dist/index.js devices list 2>&1 | grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}'); \
	if [ -z "$$PENDING" ]; then \
		echo "No pending device requests."; \
	else \
		for id in $$PENDING; do \
			echo "Approving $$id ..."; \
			docker exec openclaw node dist/index.js devices approve $$id; \
		done; \
	fi

install-plugin:
	cd agentshield-plugin && npm install

build-plugin: install-plugin
	cd agentshield-plugin && npm run build

deploy-plugin: build-plugin start

test:
	python3 tests/run_scenarios.py

clean:
	docker stop openclaw || true
	docker rm openclaw || true