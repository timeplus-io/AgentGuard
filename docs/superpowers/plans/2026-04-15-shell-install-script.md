# Shell Install Script Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create `install/install.sh` — a POSIX shell installer that downloads the AgentGuard binary from S3, configures Timeplus Enterprise connection settings, optionally sets up a background service, and is uploaded to S3 on every release.

**Architecture:** Single self-contained shell script. No external dependencies beyond `curl` or `wget` (standard on all target platforms). Structured as a set of small functions called in sequence by `main()`. CI uploads the script to S3 as part of the existing `upload-s3` job.

**Tech Stack:** POSIX sh, `shellcheck` for linting, AWS CLI (in CI), GitHub Actions.

---

### Task 1: Script skeleton — helpers, arg parsing, platform detection

**Files:**
- Create: `install/install.sh`

- [ ] **Step 1: Create `install/install.sh` with shebang, constants, helper functions, and arg parsing**

```sh
#!/bin/sh
# AgentGuard installer
# Usage: curl -fsSL https://agentguard.s3.us-west-1.amazonaws.com/install.sh | sh
#        curl -fsSL https://agentguard.s3.us-west-1.amazonaws.com/install.sh | sh -s -- --version v1.2.3
set -e

# ── Constants ─────────────────────────────────────────────────────────────────
S3_BASE="https://agentguard.s3.us-west-1.amazonaws.com"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="${HOME}/.agentguard"
CONFIG_FILE="${CONFIG_DIR}/config.yaml"
AGENTGUARD_VERSION="latest"

# ── Helpers ───────────────────────────────────────────────────────────────────
info()  { printf '\033[0;32m[agentguard]\033[0m %s\n' "$*"; }
warn()  { printf '\033[0;33m[agentguard]\033[0m %s\n' "$*" >&2; }
error() { printf '\033[0;31m[agentguard]\033[0m %s\n' "$*" >&2; exit 1; }

is_interactive() { [ -t 0 ]; }

prompt() {
  # $1 = label, $2 = default value — prints default in brackets, returns input or default
  printf '%s [%s]: ' "$1" "$2"
  read -r _val
  if [ -z "$_val" ]; then _val="$2"; fi
  printf '%s' "$_val"
}

prompt_secret() {
  # $1 = label — hides input, returns value
  printf '%s []: ' "$1"
  stty -echo 2>/dev/null || true
  read -r _val
  stty echo 2>/dev/null || true
  printf '\n'
  printf '%s' "$_val"
}

# ── Arg parsing ───────────────────────────────────────────────────────────────
parse_args() {
  while [ $# -gt 0 ]; do
    case "$1" in
      --version)
        shift
        [ $# -gt 0 ] || error "--version requires a value (e.g. --version v1.2.3)"
        AGENTGUARD_VERSION="$1"
        ;;
      *)
        error "Unknown argument: $1. Supported: --version <tag>"
        ;;
    esac
    shift
  done
}

# ── Platform detection ────────────────────────────────────────────────────────
detect_platform() {
  _os=$(uname -s)
  _arch=$(uname -m)

  case "$_os" in
    Darwin) OS="darwin" ;;
    Linux)  OS="linux"  ;;
    *) error "Unsupported OS: ${_os}. AgentGuard supports macOS and Linux only." ;;
  esac

  case "$_arch" in
    x86_64)          ARCH="amd64" ;;
    arm64|aarch64)   ARCH="arm64" ;;
    *) error "Unsupported architecture: ${_arch}. AgentGuard supports amd64 and arm64 only." ;;
  esac

  BINARY_NAME="agentguard-${OS}-${ARCH}"
  DOWNLOAD_URL="${S3_BASE}/agentguard/${AGENTGUARD_VERSION}/${BINARY_NAME}"
}

main() {
  parse_args "$@"
  detect_platform
  info "Platform: ${OS}/${ARCH}"
  info "Version:  ${AGENTGUARD_VERSION}"
  info "URL:      ${DOWNLOAD_URL}"
}

main "$@"
```

- [ ] **Step 2: Make executable**

```bash
chmod +x install/install.sh
```

- [ ] **Step 3: Lint with shellcheck**

```bash
shellcheck install/install.sh
```

Expected: no output (no warnings or errors). If `shellcheck` is not installed: `brew install shellcheck` (macOS) or `apt-get install shellcheck` (Linux).

- [ ] **Step 4: Smoke-test arg parsing and platform detection**

```bash
# Test default (latest)
./install/install.sh 2>&1 | grep "Version:  latest"

# Test version flag
./install/install.sh --version v1.2.3 2>&1 | grep "Version:  v1.2.3"

# Test unknown arg exits non-zero
./install/install.sh --foo 2>&1 | grep "Unknown argument" && echo "exit-check: ok"
```

Expected: each grep matches its pattern.

- [ ] **Step 5: Commit**

```bash
git add install/install.sh
git commit -m "feat(install): add shell installer skeleton with arg parsing and platform detection"
```

---

### Task 2: Download, install, and verify the binary

**Files:**
- Modify: `install/install.sh`

- [ ] **Step 1: Replace the `main()` body with the full download + install + verify flow**

Replace the temporary `main()` at the bottom of the file with the three new functions and an updated `main()`:

```sh
# ── Download ──────────────────────────────────────────────────────────────────
download_binary() {
  TMP_FILE=$(mktemp)
  info "Downloading AgentGuard ${AGENTGUARD_VERSION} (${OS}/${ARCH})..."

  if command -v curl > /dev/null 2>&1; then
    curl -fsSL "$DOWNLOAD_URL" -o "$TMP_FILE" \
      || { rm -f "$TMP_FILE"; error "Download failed. Verify that version '${AGENTGUARD_VERSION}' exists: ${DOWNLOAD_URL}"; }
  elif command -v wget > /dev/null 2>&1; then
    wget -qO "$TMP_FILE" "$DOWNLOAD_URL" \
      || { rm -f "$TMP_FILE"; error "Download failed. Verify that version '${AGENTGUARD_VERSION}' exists: ${DOWNLOAD_URL}"; }
  else
    error "Neither curl nor wget found. Please install one and retry."
  fi

  chmod +x "$TMP_FILE"
}

# ── Install ───────────────────────────────────────────────────────────────────
install_binary() {
  if [ -w "$INSTALL_DIR" ]; then
    mv "$TMP_FILE" "${INSTALL_DIR}/agentguard"
  else
    info "Writing to ${INSTALL_DIR} requires sudo..."
    sudo mv "$TMP_FILE" "${INSTALL_DIR}/agentguard" \
      || { rm -f "$TMP_FILE"; error "Failed to install to ${INSTALL_DIR}. Check sudo permissions."; }
  fi
  info "Installed: ${INSTALL_DIR}/agentguard"
}

# ── Verify ────────────────────────────────────────────────────────────────────
verify_binary() {
  if ! "${INSTALL_DIR}/agentguard" --help > /dev/null 2>&1; then
    rm -f "${INSTALL_DIR}/agentguard"
    error "Installed binary failed to run. The download may be corrupt or incompatible with this OS/arch."
  fi
  info "Binary verified."
}

main() {
  parse_args "$@"
  detect_platform
  download_binary
  install_binary
  verify_binary
}

main "$@"
```

- [ ] **Step 2: Lint**

```bash
shellcheck install/install.sh
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add install/install.sh
git commit -m "feat(install): add download, install, and verify steps"
```

---

### Task 3: Timeplus Enterprise configuration prompts

**Files:**
- Modify: `install/install.sh`

- [ ] **Step 1: Add `configure_timeplus()` function before `main()`**

Insert this function before the `main()` definition:

```sh
# ── Configure Timeplus Enterprise ─────────────────────────────────────────────
configure_timeplus() {
  if ! is_interactive; then
    warn "Non-interactive mode — skipping configuration."
    warn "Edit ${CONFIG_FILE} to configure your Timeplus Enterprise connection."
    return
  fi

  if [ -f "$CONFIG_FILE" ]; then
    printf 'Config already exists at %s. Overwrite? [y/N]: ' "$CONFIG_FILE"
    read -r _overwrite
    case "$_overwrite" in
      y|Y) ;;
      *) info "Keeping existing config."; return ;;
    esac
  fi

  printf '\n'
  info "Configure Timeplus Enterprise connection"
  printf '\n'

  printf 'Timeplus host\n'
  printf '  The hostname or IP where Timeplus Enterprise is running.\n'
  printf '  AgentGuard streams all agent events into Timeplus Enterprise\n'
  printf '  and queries it for metrics, rules, and security events.\n'
  TP_HOST=$(prompt '  Host' 'localhost')

  printf '\nTimeplus HTTP port\n'
  printf '  HTTP API port. Used for SELECT queries and DDL\n'
  printf '  (creating streams and materialized views).\n'
  TP_PORT=$(prompt '  Port' '3218')

  printf '\nTimeplus native TCP port\n'
  printf '  Native binary protocol port. Used for high-throughput\n'
  printf '  event ingestion from agent plugins.\n'
  TP_NATIVE_PORT=$(prompt '  Native port' '8463')

  printf '\nTimeplus user\n'
  printf '  Username for authenticating with Timeplus Enterprise.\n'
  printf '  Use the default unless your instance has custom access control.\n'
  TP_USER=$(prompt '  User' 'default')

  printf '\nTimeplus password\n'
  printf '  Leave empty if your Timeplus Enterprise instance has no password set.\n'
  TP_PASSWORD=$(prompt_secret '  Password')

  printf '\nAgentGuard server port\n'
  printf '  Port AgentGuard listens on. Change if 8080 is already in use.\n'
  AG_PORT=$(prompt '  Port' '8080')

  printf '\nAgentGuard base URL\n'
  printf '  URL Timeplus Enterprise uses to call back AgentGuard when a\n'
  printf '  security rule fires. Must be reachable from Timeplus Enterprise.\n'
  printf '  Change if AgentGuard and Timeplus run on different hosts.\n'
  AG_URL=$(prompt '  URL' 'http://localhost:8080')

  mkdir -p "$CONFIG_DIR" 2>/dev/null \
    || { warn "Could not create ${CONFIG_DIR}. Skipping config write."; return; }

  cat > "$CONFIG_FILE" << YAML
server:
  port: ${AG_PORT}

agentguard:
  url: ${AG_URL}

timeplus:
  host: ${TP_HOST}
  port: ${TP_PORT}
  native-port: ${TP_NATIVE_PORT}
  user: ${TP_USER}
  password: ${TP_PASSWORD}
YAML

  info "Configuration written to ${CONFIG_FILE}"
}
```

- [ ] **Step 2: Add `configure_timeplus` call to `main()`**

Update `main()` to:

```sh
main() {
  parse_args "$@"
  detect_platform
  download_binary
  install_binary
  verify_binary
  configure_timeplus
}
```

- [ ] **Step 3: Lint**

```bash
shellcheck install/install.sh
```

Expected: no output.

- [ ] **Step 4: Smoke-test non-interactive config skip**

```bash
# Pipe /dev/null to simulate non-interactive (curl | sh) mode
echo "" | sh install/install.sh --version latest 2>&1 | grep "Non-interactive"
```

Expected: line containing `Non-interactive mode — skipping configuration.`

Note: this will attempt a real download. If you want to skip it, comment out the `download_binary` / `install_binary` / `verify_binary` calls temporarily, run the test, then restore them.

- [ ] **Step 5: Commit**

```bash
git add install/install.sh
git commit -m "feat(install): add interactive Timeplus Enterprise config prompts"
```

---

### Task 4: Optional service setup and success summary

**Files:**
- Modify: `install/install.sh`

- [ ] **Step 1: Add `install_service_macos()`, `install_service_linux()`, `setup_service()`, and `print_summary()` functions before `main()`**

```sh
# ── Service setup — macOS ─────────────────────────────────────────────────────
install_service_macos() {
  PLIST_DIR="${HOME}/Library/LaunchAgents"
  PLIST_PATH="${PLIST_DIR}/com.timeplus.agentguard.plist"
  mkdir -p "$PLIST_DIR"
  cat > "$PLIST_PATH" << XML
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.timeplus.agentguard</string>
  <key>ProgramArguments</key>
  <array>
    <string>${INSTALL_DIR}/agentguard</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>/tmp/agentguard.log</string>
  <key>StandardErrorPath</key>
  <string>/tmp/agentguard.log</string>
</dict>
</plist>
XML
  launchctl load "$PLIST_PATH"
  SERVICE_INSTALLED=1
  info "launchd service installed and started."
}

# ── Service setup — Linux ─────────────────────────────────────────────────────
install_service_linux() {
  UNIT_PATH="/etc/systemd/system/agentguard.service"
  sudo tee "$UNIT_PATH" > /dev/null << UNIT
[Unit]
Description=AgentGuard security monitoring
After=network.target

[Service]
ExecStart=${INSTALL_DIR}/agentguard
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT
  sudo systemctl enable --now agentguard
  SERVICE_INSTALLED=1
  info "systemd service installed and started."
}

# ── Service setup — dispatcher ────────────────────────────────────────────────
setup_service() {
  SERVICE_INSTALLED=0

  if ! is_interactive; then
    return
  fi

  printf '\nInstall AgentGuard as a background service? [y/N]: '
  read -r _svc
  case "$_svc" in
    y|Y) ;;
    *) return ;;
  esac

  case "$OS" in
    darwin) install_service_macos ;;
    linux)  install_service_linux ;;
  esac
}

# ── Success summary ───────────────────────────────────────────────────────────
print_summary() {
  printf '\n'
  info "AgentGuard installed successfully!"
  printf '\n'
  printf '  Binary:  %s/agentguard\n' "$INSTALL_DIR"
  printf '  Config:  %s\n' "$CONFIG_FILE"
  printf '\n'

  if [ "${SERVICE_INSTALLED:-0}" = "1" ]; then
    case "$OS" in
      darwin)
        printf '  Service: launchd (com.timeplus.agentguard)\n'
        printf '  Logs:    tail -f /tmp/agentguard.log\n'
        printf '  Stop:    launchctl unload ~/Library/LaunchAgents/com.timeplus.agentguard.plist\n'
        printf '  Start:   launchctl load ~/Library/LaunchAgents/com.timeplus.agentguard.plist\n'
        ;;
      linux)
        printf '  Service: systemd (agentguard)\n'
        printf '  Status:  sudo systemctl status agentguard\n'
        printf '  Logs:    sudo journalctl -u agentguard -f\n'
        printf '  Stop:    sudo systemctl stop agentguard\n'
        printf '  Start:   sudo systemctl start agentguard\n'
        ;;
    esac
  else
    printf '  Start:   agentguard\n'
  fi
  printf '\n'
}
```

- [ ] **Step 2: Update `main()` to call all steps**

```sh
main() {
  parse_args "$@"
  detect_platform
  download_binary
  install_binary
  verify_binary
  configure_timeplus
  setup_service
  print_summary
}

main "$@"
```

- [ ] **Step 3: Lint**

```bash
shellcheck install/install.sh
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add install/install.sh
git commit -m "feat(install): add service setup and success summary"
```

---

### Task 5: Upload `install.sh` to S3 in CI

**Files:**
- Modify: `.github/workflows/release.yml`

The `upload-s3` job currently ends after uploading binaries. Add one more step to also upload the install script.

- [ ] **Step 1: Read the current end of the `upload-s3` job in `.github/workflows/release.yml`**

Locate the last step in `upload-s3` (the `Upload binaries to S3 (versioned + latest)` step) and append a new step after it:

```yaml
      - name: Upload install script to S3
        run: |
          set -euo pipefail
          aws s3 cp install/install.sh "s3://${{ secrets.AWS_S3_BUCKET }}/install.sh"
          echo "Uploaded install.sh → s3://${{ secrets.AWS_S3_BUCKET }}/install.sh"
```

The full `upload-s3` steps section should look like:

```yaml
    steps:
      - uses: actions/checkout@v4

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

      - name: Upload binaries to S3 (versioned + latest)
        run: |
          set -euo pipefail
          VERSION="${{ github.ref_name }}"
          shopt -s nullglob
          binaries=(dist/agentguard-*)
          if [[ ${#binaries[@]} -eq 0 ]]; then
            echo "ERROR: No binaries found in dist/ — aborting S3 upload"
            exit 1
          fi
          for binary in "${binaries[@]}"; do
            name=$(basename "$binary")
            aws s3 cp "$binary" "s3://${{ secrets.AWS_S3_BUCKET }}/agentguard/${VERSION}/${name}"
            aws s3 cp "$binary" "s3://${{ secrets.AWS_S3_BUCKET }}/agentguard/latest/${name}"
            echo "Uploaded ${name} → versioned + latest"
          done

      - name: Upload install script to S3
        run: |
          set -euo pipefail
          aws s3 cp install/install.sh "s3://${{ secrets.AWS_S3_BUCKET }}/install.sh"
          echo "Uploaded install.sh → s3://${{ secrets.AWS_S3_BUCKET }}/install.sh"
```

Note: the `upload-s3` job does NOT have `actions/checkout@v4` currently — it only downloads artifacts. Adding `actions/checkout@v4` as the first step is required so `install/install.sh` is available in the workspace.

- [ ] **Step 2: Validate YAML**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/release.yml'))" && echo "YAML OK"
```

Expected: `YAML OK`

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci: upload install.sh to S3 alongside binaries on release"
```
