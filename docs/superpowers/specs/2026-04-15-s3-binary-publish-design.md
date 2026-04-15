# Design: Publish AgentGuard Binaries to S3 via GitHub Actions

**Date:** 2026-04-15  
**Status:** Approved

## Overview

Add an `upload-s3` job to the existing `.github/workflows/release.yml` that publishes cross-platform AgentGuard binaries to AWS S3 on every version tag push (`v*`). Binaries are written to both a versioned path and a rolling `latest` path, enabling an installation script to fetch either a specific version or the most recent release.

## Trigger

- **Event:** `push` to tags matching `v*`
- **Same trigger as:** `publish-binaries` (already in `release.yml`)

## Job Graph

```
create-release
      ↓
publish-binaries (matrix: 4 platforms)   ← already exists
      ↓  needs: [publish-binaries]
upload-s3                                 ← new job
```

The `upload-s3` job runs after all matrix legs of `publish-binaries` complete. It downloads the GitHub Actions artifacts produced by `publish-binaries` rather than rebuilding the binaries.

## Platforms

| GOOS    | GOARCH | Artifact name            |
|---------|--------|--------------------------|
| linux   | amd64  | agentguard-linux-amd64   |
| linux   | arm64  | agentguard-linux-arm64   |
| darwin  | amd64  | agentguard-darwin-amd64  |
| darwin  | arm64  | agentguard-darwin-arm64  |

## S3 Path Structure

```
s3://<AWS_S3_BUCKET>/agentguard/<version>/agentguard-<os>-<arch>   (versioned)
s3://<AWS_S3_BUCKET>/agentguard/latest/agentguard-<os>-<arch>      (rolling latest)
```

Example for tag `v1.2.3`:
```
s3://agentguard/agentguard/v1.2.3/agentguard-linux-amd64
s3://agentguard/agentguard/v1.2.3/agentguard-linux-arm64
s3://agentguard/agentguard/v1.2.3/agentguard-darwin-amd64
s3://agentguard/agentguard/v1.2.3/agentguard-darwin-arm64
s3://agentguard/agentguard/latest/agentguard-linux-amd64
s3://agentguard/agentguard/latest/agentguard-linux-arm64
s3://agentguard/agentguard/latest/agentguard-darwin-amd64
s3://agentguard/agentguard/latest/agentguard-darwin-arm64
```

Public URL pattern used by the install script:
```
https://<bucket>.s3.<region>.amazonaws.com/agentguard/latest/agentguard-linux-amd64
https://<bucket>.s3.<region>.amazonaws.com/agentguard/v1.2.3/agentguard-linux-amd64
```

## Authentication

AWS credentials stored as GitHub Actions secrets (no OIDC):

| Secret              | Purpose                          |
|---------------------|----------------------------------|
| `AWS_ACCESS_KEY_ID` | IAM user access key              |
| `AWS_SECRET_ACCESS_KEY` | IAM user secret              |
| `AWS_REGION`        | Bucket region (e.g. `us-west-1`) |
| `AWS_S3_BUCKET`     | Bucket name (e.g. `agentguard`)  |

Action used: `aws-actions/configure-aws-credentials@v4`

## Bucket Access

The S3 bucket has a public-read bucket policy applied directly — no per-object ACL flags needed on upload. Objects are public by virtue of the bucket policy.

## `upload-s3` Job Steps

1. `actions/download-artifact@v4` — download all 4 artifacts in one call using `pattern: agentguard-*`
2. `aws-actions/configure-aws-credentials@v4` — configure AWS credentials from secrets
3. Upload versioned copies: `aws s3 cp` each binary to `agentguard/<version>/`
4. Upload `latest` copies: `aws s3 cp` each binary to `agentguard/latest/`

## Permissions

The `upload-s3` job requires `contents: read` only (no GitHub Release write access needed).

## What Is Not Changed

- `ci.yml` — no changes
- `publish-binaries` job — no changes; it continues attaching binaries to GitHub Releases
- `publish-binaries-main` job — no changes; it continues storing artifacts for main branch builds
- Docker publishing — no changes
