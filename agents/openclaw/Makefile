PWD := $(shell pwd)
CONFIG_FILE := .openclaw/openclaw.json
IMAGE := ghcr.io/openclaw/openclaw:2026.4.5

LITELLM_API_KEY := sk-openclaw-proxy

# AgentGuard plugin defaults — OpenClaw runs in Docker, so it needs to reach
# AgentGuard on the host via host.docker.internal. Override via env var when
# running outside Docker.
AGENTGUARD_URL ?= http://host.docker.internal:8080
AGENTGUARD_HOLDS_ENABLED ?= true
AGENTGUARD_HOLD_FAIL_POLICY ?= deny

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
		-v "$(PWD)/agentguard-plugin":/home/node/.openclaw/agentguard-plugin \
		-e HOME=/home/node \
		-e LITELLM_API_KEY=$(LITELLM_API_KEY) \
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

# 4. Register the agentguard plugin load path
data["plugins"].setdefault("load", {}).setdefault("paths", [])
if "/home/node/.openclaw/agentguard-plugin" not in data["plugins"]["load"]["paths"]:
    data["plugins"]["load"]["paths"].append("/home/node/.openclaw/agentguard-plugin")

# 5. Enable the OpenAI-compatible HTTP endpoint (needed for automated testing)
data.setdefault("gateway", {}).setdefault("http", {}).setdefault("endpoints", {})["chatCompletions"] = {"enabled": True}

# 6. Enable the agentguard plugin with Timeplus config
data["plugins"]["entries"]["agentguard"] = {
    "enabled": True,
    "config": {
        "timeplusUrl": "http://host.docker.internal:3218",
        "stream": "agentguard_hook_events",
        "flushMs": 2000,
        "batchSize": 50,
        "username": "proton",
        "password": "timeplus@t+",
        "deploymentId": "local",
        "deploymentName": "Local Dev",
        "agentguardUrl": "$(AGENTGUARD_URL)",
        "holdFailPolicy": "$(AGENTGUARD_HOLD_FAIL_POLICY)",
        "holdsEnabled": "$(AGENTGUARD_HOLDS_ENABLED)".lower() != "false",
    }
}

# 7. Configure LiteLLM as the default model provider (workaround for openclaw#54073:
#    anthropic-vertex provider fails to register its stream function because pi-ai
#    pre-registers the anthropic-messages API, causing requests to hit the wrong URL)
data.setdefault("agents", {}).setdefault("defaults", {})["model"] = "litellm/claude-sonnet-4-6"
data.setdefault("models", {}).setdefault("providers", {})["litellm"] = {
    "api": "openai-completions",
    "baseUrl": "http://host.docker.internal:4000",
    "models": [
        {
            "id": "claude-sonnet-4-6",
            "name": "Claude Sonnet 4.6",
            "reasoning": True,
            "input": ["text", "image"],
            "cost": {"input": 3, "output": 15, "cacheRead": 0.3, "cacheWrite": 3.75},
            "contextWindow": 1000000,
            "maxTokens": 128000
        },
        {
            "id": "claude-opus-4-6",
            "name": "Claude Opus 4.6",
            "reasoning": True,
            "input": ["text", "image"],
            "cost": {"input": 5, "output": 25, "cacheRead": 0.5, "cacheWrite": 6.25},
            "contextWindow": 1000000,
            "maxTokens": 128000
        }
    ]
}

with open(path, "w") as f:
    json.dump(data, f, indent=2)
print("Config updated:", path)
endef
export CONFIGURE_SCRIPT

configure:
	@python3 -c "$$CONFIGURE_SCRIPT"

start:
	docker stop openclaw || true
	docker rm openclaw || true
	docker run -d \
		--name openclaw \
		-p 18789:18789 \
		-v "$(PWD)/.openclaw":/home/node/.openclaw \
		-v "$(PWD)/.openclaw/workspace":/home/node/.openclaw/workspace \
		-v "$(PWD)/agentguard-plugin":/home/node/.openclaw/agentguard-plugin \
		-e HOME=/home/node \
		-e LITELLM_API_KEY=$(LITELLM_API_KEY) \
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

add-agents:
	# Create an 'analyst' agent with its own isolated workspace.
	# Idempotent: safe to re-run — openclaw skips creation if already present.
	mkdir -p .openclaw/workspace-analyst
	docker exec openclaw node dist/index.js agents add analyst \
		--workspace /home/node/.openclaw/workspace-analyst \
		--non-interactive 2>&1 | grep -v "already exists" || true
	@echo "Agents:"
	@docker exec openclaw node dist/index.js agents list

install-plugin:
	cd agentguard-plugin && npm install

build-plugin: install-plugin
	cd agentguard-plugin && npm run build

deploy-plugin: build-plugin start

test:
	python3 tests/run_scenarios.py

clean:
	docker stop openclaw || true
	docker rm openclaw || true
