# S3 Binary Publish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an `upload-s3` job to the existing `release.yml` workflow that uploads all 4 cross-platform AgentGuard binaries to S3 (versioned + `latest` paths) on every `v*` tag push.

**Architecture:** The `upload-s3` job runs after all `publish-binaries` matrix legs complete. It downloads the GitHub Actions artifacts those legs already produce, then runs `aws s3 cp` to upload each binary to both a versioned path and a rolling `latest` path. No new build steps — reuses existing artifacts.

**Tech Stack:** GitHub Actions, `aws-actions/configure-aws-credentials@v4`, `actions/download-artifact@v4`, AWS CLI (pre-installed on `ubuntu-latest`).

---

### Task 1: Add `upload-s3` job to `release.yml`

**Files:**
- Modify: `.github/workflows/release.yml`

The new job goes at the end of the file, after `publish-binaries-main`.

- [ ] **Step 1: Open `.github/workflows/release.yml` and append the `upload-s3` job**

Add the following at the end of the file (after the `publish-binaries-main` job, at the same indentation level as the other top-level jobs):

```yaml
  upload-s3:
    name: Upload binaries to S3
    runs-on: ubuntu-latest
    needs: [publish-binaries]
    if: startsWith(github.ref, 'refs/tags/v') && needs.publish-binaries.result == 'success'
    permissions:
      contents: read
    steps:
      - name: Download all binaries
        uses: actions/download-artifact@v4
        with:
          pattern: agentguard-*
          merge-multiple: true
          path: dist/

      - name: Configure AWS credentials
        uses: aws-actions/configure-aws-credentials@v4
        with:
          aws-access-key-id: ${{ secrets.AWS_ACCESS_KEY_ID }}
          aws-secret-access-key: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          aws-region: ${{ secrets.AWS_REGION }}

      - name: Upload versioned binaries to S3
        run: |
          VERSION="${{ github.ref_name }}"
          for binary in dist/agentguard-*; do
            name=$(basename "$binary")
            aws s3 cp "$binary" "s3://${{ secrets.AWS_S3_BUCKET }}/agentguard/${VERSION}/${name}"
            echo "Uploaded: s3://${{ secrets.AWS_S3_BUCKET }}/agentguard/${VERSION}/${name}"
          done

      - name: Upload latest binaries to S3
        run: |
          for binary in dist/agentguard-*; do
            name=$(basename "$binary")
            aws s3 cp "$binary" "s3://${{ secrets.AWS_S3_BUCKET }}/agentguard/latest/${name}"
            echo "Uploaded: s3://${{ secrets.AWS_S3_BUCKET }}/agentguard/latest/${name}"
          done
```

- [ ] **Step 2: Validate YAML syntax**

```bash
# From repo root — python3 is available on macOS
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/release.yml'))" && echo "YAML OK"
```

Expected output: `YAML OK`

If you have `actionlint` installed, also run:
```bash
actionlint .github/workflows/release.yml
```

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci: publish binaries to S3 on version tag push"
```

---

### Task 2: Add required GitHub Actions secrets (manual step)

**Files:** None (GitHub UI configuration)

These four secrets must exist in the repository's GitHub Actions secrets before the workflow can run. Navigate to **GitHub → repo → Settings → Secrets and variables → Actions → New repository secret** and add each one.

- [ ] **Step 1: Add `AWS_ACCESS_KEY_ID`**

  Value: the access key ID for an IAM user with `s3:PutObject` permission on the target bucket.

- [ ] **Step 2: Add `AWS_SECRET_ACCESS_KEY`**

  Value: the corresponding secret access key.

- [ ] **Step 3: Add `AWS_REGION`**

  Value: `us-west-1` (matches the `agentguard` bucket region inferred from `agentguard.s3.us-west-1.amazonaws.com`).

- [ ] **Step 4: Add `AWS_S3_BUCKET`**

  Value: `agentguard` (the bucket name).

---

### Task 3: Verify end-to-end by pushing a test tag

**Files:** None

- [ ] **Step 1: Push a test tag**

```bash
git tag v0.0.2-test
git push origin v0.0.2-test
```

- [ ] **Step 2: Watch the workflow in GitHub Actions**

Navigate to **GitHub → Actions → Release** workflow. Confirm the job graph runs in this order:
1. `create-release` completes
2. Four `publish-binaries` matrix legs run in parallel
3. `upload-s3` starts only after all four matrix legs succeed

- [ ] **Step 3: Verify versioned files in S3**

```bash
aws s3 ls s3://agentguard/agentguard/v0.0.2-test/ --region us-west-1
```

Expected — four objects listed:
```
agentguard-darwin-amd64
agentguard-darwin-arm64
agentguard-linux-amd64
agentguard-linux-arm64
```

- [ ] **Step 4: Verify `latest` files in S3**

```bash
aws s3 ls s3://agentguard/agentguard/latest/ --region us-west-1
```

Expected — same four objects.

- [ ] **Step 5: Verify public HTTP access**

```bash
curl -sI https://agentguard.s3.us-west-1.amazonaws.com/agentguard/latest/agentguard-linux-amd64 | head -1
```

Expected: `HTTP/1.1 200 OK`

- [ ] **Step 6: Delete the test tag (cleanup)**

```bash
git tag -d v0.0.2-test
git push origin --delete v0.0.2-test
```

Also delete the test GitHub Release created by `create-release` via:
```bash
gh release delete v0.0.2-test --yes
```
