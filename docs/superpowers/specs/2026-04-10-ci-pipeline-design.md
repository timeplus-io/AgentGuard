# CI Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add GitHub Actions CI/CD pipelines that lint, test, and build the project on every PR and main push, publish a Docker image to GHCR on main merges, and publish multi-platform Go binaries as artifacts (every main merge) and GitHub Releases (on `v*` tags).

**Architecture:** Two workflow files — `ci.yml` for quality gates and `release.yml` for publishing. CI runs three parallel/sequential jobs (backend checks, frontend checks, full embed build). Release runs Docker publish and a binary matrix in parallel. New tooling files (golangci-lint config, ESLint flat config, vitest) are added alongside the workflows.

**Tech Stack:** GitHub Actions, Go 1.23, Node 20, Docker Buildx (multi-platform), `ghcr.io`, golangci-lint, ESLint, Vitest

---

## Decisions

| Topic | Decision |
|-------|----------|
| CI trigger | Every PR + every push to `main` |
| Release trigger | Push to `main` (Docker + artifacts) + `v*` tag push (GitHub Release) |
| Container registry | `ghcr.io/timeplus/agentshield` (uses `GITHUB_TOKEN`, no extra secrets) |
| Docker image tags | `latest` + short git SHA on main; `vX.Y.Z` + `latest` on tag push |
| Binary platforms | `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64` |
| Go linter | `golangci-lint` via official action |
| Frontend linter | ESLint with flat config (`eslint.config.js`) |
| Test scaffolding | `go test ./...` (vacuous today) + `vitest run` (exits 0 with no test files) |

---

## Workflow Structure

```
.github/
  workflows/
    ci.yml       — lint + test + build checks (PR + main push)
    release.yml  — Docker publish + binary publish (main push + v* tags)
```

---

## CI Workflow (`ci.yml`)

**Triggers:** `pull_request` (all branches) + `push` to `main`

### Job 1: `backend-checks` (independent, runs immediately)

Runner: `ubuntu-latest`

1. Checkout (`actions/checkout@v4`)
2. Setup Go 1.23 (`actions/setup-go@v5`) — module cache enabled
3. `go mod download` (inside `backend/`)
4. `golangci-lint-action@v6` — reads `.golangci.yml`, timeout 3m
5. `go test ./...` (inside `backend/`) — vacuous today, no workflow change needed when tests are added

### Job 2: `frontend-checks` (independent, runs in parallel with Job 1)

Runner: `ubuntu-latest`

1. Checkout
2. Setup Node 20 (`actions/setup-node@v4`) — npm cache keyed on `frontend/package-lock.json`
3. `npm ci` (inside `frontend/`)
4. `npm run lint` — runs `eslint src/`
5. `npm run test` — runs `vitest run` (exits 0 with no test files)

### Job 3: `build` (sequential, needs Jobs 1 + 2)

Runner: `ubuntu-latest`

1. Checkout
2. Setup Node 20 (same cache config as Job 2)
3. Setup Go 1.23
4. `npm ci` (inside `frontend/`)
5. `npm run build` — produces `frontend/dist/`
6. web-sync: `mkdir -p backend/internal/web/dist && cp -r frontend/dist/. backend/internal/web/dist/`
7. `CGO_ENABLED=0 go build -tags embed ./...` (inside `backend/`) — compile check matching `make backend-check`

---

## Release Workflow (`release.yml`)

**Triggers:** `push` to `main` + `push` of tags matching `v*`

### Job 1: `publish-docker`

Runner: `ubuntu-latest`

1. Checkout
2. Login to `ghcr.io` using `GITHUB_TOKEN`
3. Set up Docker Buildx
4. Extract metadata with `docker/metadata-action@v5`:
   - On main push: tags `latest` + short SHA
   - On `v*` tag push: tags `vX.Y.Z` + `latest` + short SHA
5. Build and push with `docker/build-push-action`:
   - Tags: from metadata step above
   - Platforms: `linux/amd64,linux/arm64` (Docker multi-arch via Buildx)

### Job 2: `publish-binaries` (matrix, runs on both main push and v* tags)

Runner: `ubuntu-latest` (cross-compilation — no macOS runner needed)

Matrix:
```yaml
matrix:
  include:
    - goos: linux  goarch: amd64  name: agentshield-linux-amd64
    - goos: linux  goarch: arm64  name: agentshield-linux-arm64
    - goos: darwin goarch: amd64  name: agentshield-darwin-amd64
    - goos: darwin goarch: arm64  name: agentshield-darwin-arm64
```

Steps per leg:
1. Checkout
2. Setup Node 20
3. Setup Go 1.23
4. `npm ci` + `npm run build` (inside `frontend/`)
5. web-sync: `mkdir -p backend/internal/web/dist && cp -r frontend/dist/. backend/internal/web/dist/`
6. Build binary:
   ```bash
   cd backend
   CGO_ENABLED=0 GOOS=${{ matrix.goos }} GOARCH=${{ matrix.goarch }} \
     go build -tags embed -o ../${{ matrix.name }} cmd/server/main.go
   ```
7. Upload artifact: `actions/upload-artifact@v4` — name `${{ matrix.name }}`, path `${{ matrix.name }}`
8. Create/update GitHub Release and attach binary — only when `startsWith(github.ref, 'refs/tags/v')`, using `softprops/action-gh-release@v2`

---

## New Files Required

### `.golangci.yml` (repo root)

Minimal config enabling a focused set of linters:
- `errcheck` — unhandled errors
- `govet` — Go vet checks
- `staticcheck` — static analysis
- `unused` — unused code detection
- Timeout: 3m
- Exclude generated files and test helpers

### `frontend/eslint.config.js` (flat config format)

- Parser: default JS
- Plugin: `eslint-plugin-react` (JSX rules)
- Rules: `no-unused-vars` (warn), `react/prop-types` (off — no TypeScript), `no-console` (warn)
- Targets: `src/**/*.{js,jsx}`

### `frontend/package.json` changes

New devDependencies:
- `eslint` `^9`
- `@eslint/js` `^9`
- `eslint-plugin-react` `^7`
- `vitest` `^2`

New scripts:
```json
"lint": "eslint src/",
"test": "vitest run"
```

---

## Secrets Required

| Secret | Where set | Used by |
|--------|-----------|---------|
| `GITHUB_TOKEN` | Automatic (built-in) | Docker push to ghcr.io, GitHub Release creation |

No additional secrets needed.

---

## Extending Later

- **Go tests:** Add `*_test.go` files — `go test ./...` picks them up with no workflow changes
- **Frontend tests:** Add `*.test.jsx` files under `src/` — `vitest run` picks them up with no workflow changes
- **golangci-lint rules:** Add linters to `.golangci.yml` incrementally
- **CD to staging:** Add a deploy job to `release.yml` after `publish-docker`, triggered on main push
