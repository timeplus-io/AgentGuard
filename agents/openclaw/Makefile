PWD := $(shell pwd)
CONFIG_FILE := .openclaw/openclaw.json

.PHONY: workspace init run start fix url devices approve clean

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
		ghcr.io/openclaw/openclaw:latest \
		node dist/index.js onboard

run:
	docker run -d \
		--name openclaw \
		-p 18789:18789 \
		-v "$(PWD)/.openclaw":/home/node/.openclaw \
		-v "$(PWD)/.openclaw/workspace":/home/node/.openclaw/workspace \
		--restart unless-stopped \
		ghcr.io/openclaw/openclaw:latest \
		node dist/index.js gateway run


define FIX_SCRIPT
import json
path = "$(CONFIG_FILE)"
with open(path, "r") as f:
    data = json.load(f)

# Use bind mode string (replaces legacy IP alias e.g. "0.0.0.0")
data["gateway"]["bind"] = "lan"

# Remove unrecognized root-level keys from older config versions
data.pop("remote", None)

with open(path, "w") as f:
    json.dump(data, f, indent=2)
endef
export FIX_SCRIPT

fix:
	@echo "🔧 Syncing Gateway and Remote tokens (Array Mode)..."
	@python3 -c "$$FIX_SCRIPT"
	@echo "✅ Config patched. Remote is now an array."


start: fix
	docker stop openclaw || true
	docker rm openclaw || true
	docker run -d \
		--name openclaw \
		-p 18789:18789 \
		-v "$(PWD)/.openclaw":/home/node/.openclaw \
		-v "$(PWD)/.openclaw/workspace":/home/node/.openclaw/workspace \
		-e HOME=/home/node \
		--restart unless-stopped \
		ghcr.io/openclaw/openclaw:latest \
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