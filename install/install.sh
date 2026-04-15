#!/bin/sh
# AgentGuard installer
# Usage: curl -fsSL https://agentguard.s3.us-west-1.amazonaws.com/install.sh | sh
#        curl -fsSL https://agentguard.s3.us-west-1.amazonaws.com/install.sh | sh -s -- --version v1.2.3
set -eu

# ── Constants ─────────────────────────────────────────────────────────────────
S3_BASE="https://agentguard.s3.us-west-1.amazonaws.com"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="${HOME}/.agentguard"
# shellcheck disable=SC2034  # used in forthcoming install/config steps
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
