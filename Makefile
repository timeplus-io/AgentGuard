.PHONY: all dev build clean test \
        backend-build backend-run backend-clean \
        frontend-install frontend-dev frontend-build frontend-preview \
        docker-build docker-run docker-stop docker-push docker-release

BINARY      := backend/server
DOCKER_REPO := timeplus
IMAGE       := agentshield
TAG         ?= 0.0.1
FULL_IMAGE  := $(DOCKER_REPO)/$(IMAGE):$(TAG)
PORT        := 8080

# ── Top-level ────────────────────────────────────────────────────────────────

all: build

## build: full production build — frontend → embed → single binary
build: frontend-build web-sync backend-build

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

## backend-tidy: sync Go module dependencies
backend-tidy:
	cd backend && go mod tidy

## backend-check: compile check without producing a binary
backend-check:
	cd backend && CGO_ENABLED=0 go build -tags embed ./...

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

## frontend-preview: preview the production build locally
frontend-preview: frontend-build
	cd frontend && npm run preview

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

# ── Helpers ──────────────────────────────────────────────────────────────────

## clean: remove all build artifacts
clean: backend-clean
	rm -rf frontend/dist

## help: list available targets
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
