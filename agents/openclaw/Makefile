PWD := $(shell pwd)
CONFIG_FILE := .openclaw/openclaw.json
IMAGE := ghcr.io/openclaw/openclaw:2026.4.5

.PHONY: workspace init run start configure url devices approve clean

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

clean:
	docker stop openclaw || true
	docker rm openclaw || true