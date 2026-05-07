#!/bin/sh
# AgentGuard installer
# Usage: curl -fsSL https://agentguard.s3.us-west-1.amazonaws.com/install.sh | sh
#        curl -fsSL https://agentguard.s3.us-west-1.amazonaws.com/install.sh | sh -s -- --version v1.2.3
set -eu

# ── Constants ─────────────────────────────────────────────────────────────────
S3_BASE="https://agentguard.s3.us-west-1.amazonaws.com"
INSTALL_DIR="/usr/local/bin"
AGENTGUARD_VERSION="latest"
CONFIG_DIR="${HOME}/.agentguard"
CONFIG_FILE="${CONFIG_DIR}/config.yaml"

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
  # $1 = label — hides input; result stored in _prompt_secret_val (not stdout, to keep stty in parent shell)
  printf '%s []: ' "$1"
  stty -echo 2>/dev/null || true
  read -r _prompt_secret_val
  stty echo 2>/dev/null || true
  printf '\n'
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
  # Restore SELinux file context so systemd can exec the binary on SELinux-enabled
  # systems (openEuler, RHEL, CentOS). `mv` from /tmp preserves the tmp_t context,
  # which systemd's init_t domain cannot execute — causing exit code 203/EXEC.
  if command -v restorecon > /dev/null 2>&1; then
    restorecon "${INSTALL_DIR}/agentguard" 2>/dev/null || true
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
  prompt_secret '  Password'
  TP_PASSWORD="$_prompt_secret_val"

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
  SYSTEM_CONFIG_DIR="/etc/agentguard"
  SYSTEM_CONFIG_FILE="${SYSTEM_CONFIG_DIR}/config.yaml"

  # Copy config to a system-wide path. The service runs as root whose $HOME is
  # /root, so the user-scoped ~/.agentguard/config.yaml written during interactive
  # setup would not be found at runtime.
  sudo mkdir -p "$SYSTEM_CONFIG_DIR"
  if [ -f "$CONFIG_FILE" ]; then
    sudo cp "$CONFIG_FILE" "$SYSTEM_CONFIG_FILE"
    sudo chmod 640 "$SYSTEM_CONFIG_FILE"
    info "Config copied to ${SYSTEM_CONFIG_FILE}"
  else
    warn "No config file found at ${CONFIG_FILE}; service will use defaults."
    warn "Edit ${SYSTEM_CONFIG_FILE} to configure your Timeplus Enterprise connection."
  fi

  sudo tee "$UNIT_PATH" > /dev/null << UNIT
[Unit]
Description=AgentGuard security monitoring
After=network.target

[Service]
ExecStart=${INSTALL_DIR}/agentguard --config ${SYSTEM_CONFIG_FILE}
WorkingDirectory=${SYSTEM_CONFIG_DIR}
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
        printf '  Config:  /etc/agentguard/config.yaml\n'
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
