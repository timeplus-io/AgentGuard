.PHONY: all dev build clean test \
        backend-build backend-run backend-clean backend-fmt backend-lint \
        frontend-install frontend-dev frontend-build frontend-preview frontend-lint \
        lint \
        docker-build docker-run docker-stop docker-push docker-release \
        compose-up compose-down compose-restart \
        scripts-sync package-plugins \
        docs-api

BINARY      := backend/server
DOCKER_REPO := timeplus
IMAGE       := agentguard
TAG         ?= 0.8.0
FULL_IMAGE  := $(DOCKER_REPO)/$(IMAGE):$(TAG)
PORT        := 8080

# ── Top-level ────────────────────────────────────────────────────────────────

all: build

## build: full production build — frontend → embed → single binary
build: frontend-build web-sync scripts-sync backend-build

## dev: start backend and frontend dev servers in parallel
dev:
	@trap 'kill 0' SIGINT; \
	$(MAKE) backend-run & \
	$(MAKE) frontend-dev & \
	wait

# ── Backend ──────────────────────────────────────────────────────────────────

## backend-build: compile Go binary with embedded frontend (CGO_ENABLED=0 for macOS 26+)
backend-build:
	cd backend && CGO_ENABLED=0 go build -tags embed -o server cmd/server/main.go

## backend-run: build and start the backend server on :8080
backend-run: backend-build
	./$(BINARY)

## web-sync: copy frontend build output into the backend embed directory
web-sync:
	mkdir -p backend/internal/web/dist
	cp -r frontend/dist/. backend/internal/web/dist/

## scripts-sync: copy agents/install.sh into the backend embed directory
scripts-sync:
	cp agents/install.sh backend/internal/scripts/install.sh

## package-plugins: build all agent plugins and pack them into packages/ for offline distribution
## Output: packages/timeplus-agentguard-claudecode-plugin-*.tgz
##         packages/timeplus-agentguard-openclaw-plugin-*.tgz
##         packages/agentguard_hermes_plugin-*.whl
package-plugins:
	@mkdir -p packages
	@echo "── Building Claude Code plugin ─────────────────────────────────────────────"
	cd agents/claudecode/agentguard-plugin && npm install && npm run build
	cd agents/claudecode/agentguard-plugin && npm pack --pack-destination ../../../packages
	@echo "── Building OpenClaw plugin ────────────────────────────────────────────────"
	cd agents/openclaw/agentguard-plugin && npm install && npm run build
	cd agents/openclaw/agentguard-plugin && npm pack --pack-destination ../../../packages
	@echo "── Building Hermes plugin ──────────────────────────────────────────────────"
	cd agents/hermes/agentguard-plugin && python3 -m pip install build hatchling --quiet && python3 -m build --wheel --outdir ../../../packages
	@echo ""
	@echo "  ✓ Plugin packages written to packages/"
	@ls -1 packages/

## backend-tidy: sync Go module dependencies
backend-tidy:
	cd backend && go mod tidy

## backend-check: compile check without producing a binary
backend-check:
	cd backend && CGO_ENABLED=0 go build -tags embed ./...

## backend-fmt: format Go source files
backend-fmt:
	cd backend && gofmt -w .

## backend-lint: run golangci-lint on the backend (requires golangci-lint in PATH)
backend-lint:
	cd backend && golangci-lint run

## backend-clean: remove compiled binary
backend-clean:
	rm -f $(BINARY)

# ── Frontend ─────────────────────────────────────────────────────────────────

## frontend-install: install npm dependencies
frontend-install:
	cd frontend && npm install

## frontend-dev: start Vite dev server on :5173 (proxies /api → :8080)
frontend-dev:
	cd frontend && npm run dev

## frontend-build: production build to frontend/dist/
frontend-build:
	cd frontend && npm run build

## frontend-lint: run ESLint on the frontend source
frontend-lint:
	cd frontend && npm run lint

## frontend-preview: preview the production build locally
frontend-preview: frontend-build
	cd frontend && npm run preview

# ── Lint ─────────────────────────────────────────────────────────────────────

## lint: run all linters (backend golangci-lint + frontend ESLint)
lint: backend-lint frontend-lint

# ── Docker ───────────────────────────────────────────────────────────────────

## docker-build: build the Docker image (TAG=x.y.z for versioned builds)
docker-build:
	docker build -t $(FULL_IMAGE) .

## docker-run: run the Docker image on :8080
docker-run:
	docker run --rm -p $(PORT):8080 --name $(IMAGE) $(FULL_IMAGE)

## docker-stop: stop the running container
docker-stop:
	docker stop $(IMAGE)

## docker-push: push image to Docker Hub (docker login required)
docker-push: docker-build
	docker push $(FULL_IMAGE)

## docker-release: build and push both versioned tag and latest
docker-release:
	@if [ "$(TAG)" = "latest" ]; then echo "Usage: make docker-release TAG=x.y.z"; exit 1; fi
	docker build -t $(DOCKER_REPO)/$(IMAGE):$(TAG) -t $(DOCKER_REPO)/$(IMAGE):latest .
	docker push $(DOCKER_REPO)/$(IMAGE):$(TAG)
	docker push $(DOCKER_REPO)/$(IMAGE):latest

# ── Docker Compose ───────────────────────────────────────────────────────────

## compose-up: start the full stack with Docker Compose
compose-up:
	docker compose up --build

## compose-down: stop the full stack
compose-down:
	docker compose down

## compose-restart: stop stack, drop Timeplus data volume, and restart fresh
compose-restart:
	docker compose down -v --remove-orphans
	docker compose up --build

# ── Docs ─────────────────────────────────────────────────────────────────────

## docs-api: export OpenAPI spec to docs/spec/ as JSON, YAML, and Markdown (server must be running on :8080)
docs-api:
	@command -v widdershins >/dev/null 2>&1 || { echo "widdershins not found — run: npm install -g widdershins"; exit 1; }
	@command -v python3 >/dev/null 2>&1 || { echo "python3 not found (needed for YAML conversion)"; exit 1; }
	@mkdir -p docs/spec
	curl -sf http://localhost:8080/docs/openapi.json -o docs/spec/openapi.json
	python3 -c "import json,yaml,sys; yaml.dump(json.load(open('docs/spec/openapi.json')),open('docs/spec/openapi.yaml','w'),allow_unicode=True,sort_keys=False)" 2>/dev/null || \
	  python3 -c "import json,sys; d=json.load(open('docs/spec/openapi.json')); print('YAML conversion skipped — install PyYAML: pip install pyyaml')"
	widdershins --search false --language_tabs 'shell:curl' 'javascript:JS' 'python:Python' \
	            --summary docs/spec/openapi.json -o docs/spec/api.md
	@echo "API docs written to docs/spec/ (openapi.json, openapi.yaml, api.md)"

# ── Helpers ──────────────────────────────────────────────────────────────────

## clean: remove all build artifacts
clean: backend-clean
	rm -rf frontend/dist

## help: list available targets
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
