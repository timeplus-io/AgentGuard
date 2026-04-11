# CI Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add GitHub Actions CI/CD pipelines that lint, test, and build on every PR and main push, publish a Docker image to `ghcr.io` on every main merge (tagged `latest` + short SHA, plus semver on `v*` tags), and publish multi-platform Go binaries as artifacts on every main merge and as a GitHub Release on `v*` tags.

**Architecture:** Two workflow files — `ci.yml` (quality gates, three jobs: backend checks, frontend checks, full embed build) and `release.yml` (Docker publish + binary matrix). New tooling files scaffolded first so the workflow jobs have something to call: golangci-lint config for Go, ESLint flat config + Vitest for the frontend.

**Tech Stack:** GitHub Actions, Go 1.23, Node 20, golangci-lint, ESLint 9 (flat config), Vitest 2, Docker Buildx, `ghcr.io`

---

## Files

| Action | Path |
|--------|------|
| Create | `.golangci.yml` |
| Create | `frontend/eslint.config.js` |
| Modify | `frontend/package.json` |
| Create | `.github/workflows/ci.yml` |
| Create | `.github/workflows/release.yml` |

---

### Task 1: Frontend linting and test scaffolding

**Files:**
- Modify: `frontend/package.json`
- Create: `frontend/eslint.config.js`

- [ ] **Step 1: Install ESLint and Vitest devDependencies**

Run from the repo root:
```bash
cd frontend && npm install --save-dev \
  eslint@^9 \
  @eslint/js@^9 \
  eslint-plugin-react@^7 \
  globals@^15 \
  vitest@^2
```

Expected: `package-lock.json` updated, no errors.

- [ ] **Step 2: Add `lint` and `test` scripts to `frontend/package.json`**

The `scripts` block currently is:
```json
"scripts": {
  "dev": "vite",
  "build": "vite build",
  "preview": "vite preview"
},
```

Replace with:
```json
"scripts": {
  "dev": "vite",
  "build": "vite build",
  "preview": "vite preview",
  "lint": "eslint src/",
  "test": "vitest run"
},
```

- [ ] **Step 3: Create `frontend/eslint.config.js`**

```js
import js from '@eslint/js'
import reactPlugin from 'eslint-plugin-react'
import globals from 'globals'

export default [
  js.configs.recommended,
  {
    files: ['src/**/*.{js,jsx}'],
    plugins: { react: reactPlugin },
    languageOptions: {
      globals: { ...globals.browser },
      parserOptions: { ecmaFeatures: { jsx: true } },
    },
    rules: {
      'react/jsx-uses-react': 'error',
      'react/jsx-uses-vars': 'error',
      'no-unused-vars': ['warn', { varsIgnorePattern: '^_', argsIgnorePattern: '^_' }],
      'no-console': 'warn',
    },
  },
]
```

- [ ] **Step 4: Verify lint runs**

```bash
cd frontend && npm run lint
```

Expected: exits 0 (warnings are fine, errors fail the run). Fix any errors before continuing.

- [ ] **Step 5: Verify test scaffold runs**

```bash
cd frontend && npm run test
```

Expected output:
```
RUN  v2.x.x

Test Files  0 passed (0)
     Tests  0 passed (0)
```
Exits 0. No test files needed — this is intentional scaffolding.

- [ ] **Step 6: Commit**

```bash
git add frontend/package.json frontend/package-lock.json frontend/eslint.config.js
git commit -m "chore(frontend): add ESLint and Vitest scaffolding"
```

---

### Task 2: Backend lint configuration

**Files:**
- Create: `.golangci.yml`

- [ ] **Step 1: Create `.golangci.yml` at the repo root**

```yaml
linters:
  disable-all: true
  enable:
    - errcheck
    - govet
    - staticcheck
    - ineffassign
    - misspell

run:
  timeout: 3m

issues:
  exclude-rules:
    - path: _test\.go
      linters:
        - errcheck
  exclude-dirs:
    - vendor
```

- [ ] **Step 2: Verify golangci-lint works locally (if installed)**

If `golangci-lint` is installed locally:
```bash
cd backend && golangci-lint run ../
```

If not installed, skip — the CI job will catch issues. Move to Step 3.

Expected (if installed): exits 0 or lists lint warnings only. Address any errors.

- [ ] **Step 3: Commit**

```bash
git add .golangci.yml
git commit -m "chore(backend): add golangci-lint config"
```

---

### Task 3: CI workflow

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: Create `.github/workflows/` directory**

```bash
mkdir -p .github/workflows
```

- [ ] **Step 2: Create `.github/workflows/ci.yml`**

```yaml
name: CI

on:
  pull_request:
  push:
    branches: [main]

jobs:
  backend-checks:
    name: Backend — lint & test
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: backend
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
          cache-dependency-path: backend/go.sum

      - name: Download modules
        run: go mod download

      - name: Lint
        uses: golangci/golangci-lint-action@v6
        with:
          working-directory: backend
          version: latest

      - name: Test
        run: go test ./...

  frontend-checks:
    name: Frontend — lint & test
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: frontend
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-node@v4
        with:
          node-version: '20'
          cache: npm
          cache-dependency-path: frontend/package-lock.json

      - name: Install dependencies
        run: npm ci

      - name: Lint
        run: npm run lint

      - name: Test
        run: npm run test

  build:
    name: Full embed build
    runs-on: ubuntu-latest
    needs: [backend-checks, frontend-checks]
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-node@v4
        with:
          node-version: '20'
          cache: npm
          cache-dependency-path: frontend/package-lock.json

      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
          cache-dependency-path: backend/go.sum

      - name: Install frontend dependencies
        working-directory: frontend
        run: npm ci

      - name: Build frontend
        working-directory: frontend
        run: npm run build

      - name: Sync frontend into backend embed dir
        run: |
          mkdir -p backend/internal/web/dist
          cp -r frontend/dist/. backend/internal/web/dist/

      - name: Build backend (embedded)
        working-directory: backend
        run: CGO_ENABLED=0 go build -tags embed ./...
```

- [ ] **Step 3: Verify the YAML is valid**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))" && echo "YAML OK"
```

Expected: `YAML OK`

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add CI workflow (lint, test, embed build)"
```

---

### Task 4: Release workflow

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Create `.github/workflows/release.yml`**

```yaml
name: Release

on:
  push:
    branches: [main]
    tags: ['v*']

jobs:
  publish-docker:
    name: Publish Docker image
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
    steps:
      - uses: actions/checkout@v4

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Log in to GitHub Container Registry
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Extract Docker metadata
        id: meta
        uses: docker/metadata-action@v5
        with:
          images: ghcr.io/${{ github.repository_owner }}/agentshield
          tags: |
            type=sha,prefix=,format=short
            type=raw,value=latest,enable={{is_default_branch}}
            type=semver,pattern={{version}}

      - name: Build and push
        uses: docker/build-push-action@v6
        with:
          context: .
          push: true
          platforms: linux/amd64,linux/arm64
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
          cache-to: type=gha,mode=max

  publish-binaries:
    name: Build ${{ matrix.name }}
    runs-on: ubuntu-latest
    permissions:
      contents: write
    strategy:
      matrix:
        include:
          - goos: linux
            goarch: amd64
            name: agentshield-linux-amd64
          - goos: linux
            goarch: arm64
            name: agentshield-linux-arm64
          - goos: darwin
            goarch: amd64
            name: agentshield-darwin-amd64
          - goos: darwin
            goarch: arm64
            name: agentshield-darwin-arm64
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-node@v4
        with:
          node-version: '20'
          cache: npm
          cache-dependency-path: frontend/package-lock.json

      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
          cache-dependency-path: backend/go.sum

      - name: Install frontend dependencies
        working-directory: frontend
        run: npm ci

      - name: Build frontend
        working-directory: frontend
        run: npm run build

      - name: Sync frontend into backend embed dir
        run: |
          mkdir -p backend/internal/web/dist
          cp -r frontend/dist/. backend/internal/web/dist/

      - name: Build binary
        env:
          CGO_ENABLED: '0'
          GOOS: ${{ matrix.goos }}
          GOARCH: ${{ matrix.goarch }}
        run: |
          cd backend
          go build -tags embed -o ../${{ matrix.name }} cmd/server/main.go

      - name: Upload artifact
        uses: actions/upload-artifact@v4
        with:
          name: ${{ matrix.name }}
          path: ${{ matrix.name }}
          retention-days: 14

      - name: Attach to GitHub Release
        if: startsWith(github.ref, 'refs/tags/v')
        uses: softprops/action-gh-release@v2
        with:
          files: ${{ matrix.name }}
```

- [ ] **Step 2: Verify the YAML is valid**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/release.yml'))" && echo "YAML OK"
```

Expected: `YAML OK`

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci: add release workflow (Docker publish + multi-platform binaries)"
```

---

### Task 5: Smoke-test via pull request

- [ ] **Step 1: Push the branch and open a PR**

```bash
git push origin HEAD
```

Then open a PR against `main` on GitHub.

- [ ] **Step 2: Verify CI workflow triggers**

In the GitHub Actions tab, confirm the `CI` workflow starts. Check that three jobs appear: `Backend — lint & test`, `Frontend — lint & test`, `Full embed build`.

- [ ] **Step 3: Verify all three jobs pass**

Expected: all three jobs green. If a job fails:
- **Backend lint fails:** run `cd backend && golangci-lint run` locally, fix the reported issues.
- **Frontend lint fails:** run `cd frontend && npm run lint` locally, fix the reported issues.
- **Build fails:** check the web-sync step output and ensure `frontend/dist/` was produced.

- [ ] **Step 4: Merge the PR and verify release workflow triggers**

After merge, open the Actions tab and confirm the `Release` workflow starts. It should run both `publish-docker` and the four `publish-binaries` matrix legs.

- [ ] **Step 5: Verify Docker image appears in GHCR**

Go to `https://github.com/<org>/agentshield/pkgs/container/agentshield`. Confirm two tags exist: `latest` and the short SHA of the merge commit.

- [ ] **Step 6: Verify binary artifacts are uploaded**

In the completed `Release` workflow run, click **Summary** and confirm four artifacts appear: `agentshield-linux-amd64`, `agentshield-linux-arm64`, `agentshield-darwin-amd64`, `agentshield-darwin-arm64`.

- [ ] **Step 7: Smoke-test a v* tag release**

```bash
git tag v0.1.0
git push origin v0.1.0
```

Confirm the `Release` workflow runs again. After it completes, navigate to `https://github.com/<org>/agentshield/releases` and verify a release named `v0.1.0` exists with all four binaries attached and a Docker tag `v0.1.0` visible in GHCR.
