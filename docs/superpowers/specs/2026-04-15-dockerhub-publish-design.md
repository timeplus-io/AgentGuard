# Design: Publish AgentGuard to Docker Hub

**Date:** 2026-04-15  
**Status:** Approved

## Overview

Extend the existing `publish-docker` job in `.github/workflows/release.yml` to push the AgentGuard image to Docker Hub (`timeplus/agentguard`) in addition to the existing GitHub Container Registry (`ghcr.io/<owner>/agentguard`). A single build pushes to both registries.

## Changes to `publish-docker` job

### 1. Add Docker Hub login step

Insert after the existing GHCR login step:

```yaml
- name: Log in to Docker Hub
  uses: docker/login-action@v3
  with:
    username: ${{ secrets.DOCKERHUB_USERNAME }}
    password: ${{ secrets.DOCKERHUB_TOKEN }}
```

### 2. Extend `docker/metadata-action` images list

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

### 3. `docker/build-push-action` — unchanged

No changes needed. It automatically picks up tags for both registries from `steps.meta.outputs.tags`.

## Tag Strategy

Both registries receive identical tags:

| Trigger | Tag |
|---------|-----|
| Any push | `sha-<short>` |
| Push to `main` | `latest` |
| Tag `v1.2.3` | `v1.2.3` |

## Required GitHub Secrets

| Secret | Value |
|--------|-------|
| `DOCKERHUB_USERNAME` | Docker Hub username (e.g. `timeplus`) |
| `DOCKERHUB_TOKEN` | Docker Hub access token (not password) — generate at hub.docker.com → Account Settings → Security → New Access Token |

## What Is Not Changed

- Build matrix (still `linux/amd64,linux/arm64`)
- Tag logic
- `create-release`, `publish-binaries`, `upload-s3`, `publish-binaries-main` jobs
