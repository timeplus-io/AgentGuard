# Docker Hub Publish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the existing `publish-docker` CI job to push the AgentGuard image to Docker Hub (`timeplus/agentguard`) alongside the existing GHCR push, using a single build.

**Architecture:** Add a Docker Hub login step to the existing `publish-docker` job and add `timeplus/agentguard` as a second image in `docker/metadata-action`. The `docker/build-push-action` call is unchanged — it automatically pushes to both registries via the combined tag list.

**Tech Stack:** GitHub Actions, `docker/login-action@v3`, `docker/metadata-action@v5`, `docker/build-push-action@v6`.

---

### Task 1: Add Docker Hub login and image to `publish-docker` job

**Files:**
- Modify: `.github/workflows/release.yml`

The current `publish-docker` job steps are (in order):
1. `actions/checkout@v4`
2. `docker/setup-buildx-action@v3`
3. Log in to GitHub Container Registry (`docker/login-action@v3` with `registry: ghcr.io`)
4. Extract Docker metadata (`docker/metadata-action@v5` — images: `ghcr.io/<owner>/agentguard`)
5. Build and push (`docker/build-push-action@v6`)
6. Update release notes

Make two changes:

**Change 1** — Add a Docker Hub login step between steps 3 and 4:

```yaml
      - name: Log in to Docker Hub
        uses: docker/login-action@v3
        with:
          username: ${{ secrets.DOCKERHUB_USERNAME }}
          password: ${{ secrets.DOCKERHUB_TOKEN }}
```

**Change 2** — Extend the `images:` field in the `Extract Docker metadata` step to include both registries:

```yaml
      - name: Extract Docker metadata
        id: meta
        uses: docker/metadata-action@v5
        with:
          images: |
            ghcr.io/${{ github.repository_owner }}/agentguard
            timeplus/agentguard
          tags: |
            type=sha,prefix=,format=short
            type=raw,value=latest,enable={{is_default_branch}}
            type=semver,pattern={{version}}
```

- [ ] **Step 1: Read the current `publish-docker` job in `.github/workflows/release.yml`**

Locate the step named `Log in to GitHub Container Registry` and the step named `Extract Docker metadata`.

- [ ] **Step 2: Insert the Docker Hub login step after the GHCR login step**

The file currently has:

```yaml
      - name: Log in to GitHub Container Registry
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Extract Docker metadata
```

Replace with:

```yaml
      - name: Log in to GitHub Container Registry
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Log in to Docker Hub
        uses: docker/login-action@v3
        with:
          username: ${{ secrets.DOCKERHUB_USERNAME }}
          password: ${{ secrets.DOCKERHUB_TOKEN }}

      - name: Extract Docker metadata
```

- [ ] **Step 3: Add `timeplus/agentguard` to the `images:` list in the metadata step**

Find the `Extract Docker metadata` step. Its `images:` currently is a single value:

```yaml
          images: ghcr.io/${{ github.repository_owner }}/agentguard
```

Replace with:

```yaml
          images: |
            ghcr.io/${{ github.repository_owner }}/agentguard
            timeplus/agentguard
```

- [ ] **Step 4: Validate YAML**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/release.yml'))" && echo "YAML OK"
```

Expected: `YAML OK`

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci: publish agentguard image to Docker Hub on release"
```

---

### Task 2: Add GitHub Actions secrets (manual)

**Files:** None (GitHub UI)

Navigate to **GitHub → repo → Settings → Secrets and variables → Actions → New repository secret** and add:

- [ ] **Step 1: Add `DOCKERHUB_USERNAME`**

  Value: the Docker Hub username for the `timeplus` organisation (e.g. `timeplus` or the bot account used for CI).

- [ ] **Step 2: Add `DOCKERHUB_TOKEN`**

  Value: a Docker Hub access token (not the account password). Generate one at:
  hub.docker.com → Account Settings → Security → New Access Token → set permissions to Read & Write.
