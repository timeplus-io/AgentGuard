#!/bin/sh
# AgentGuard installer
# Usage: curl -fsSL https://agentguard.s3.us-west-1.amazonaws.com/install.sh | sh
#        curl -fsSL https://agentguard.s3.us-west-1.amazonaws.com/install.sh | sh -s -- --version v1.2.3
set -e

# ── Constants ─────────────────────────────────────────────────────────────────
S3_BASE="https://agentguard.s3.us-west-1.amazonaws.com"
# shellcheck disable=SC2034
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="${HOME}/.agentguard"
# shellcheck disable=SC2034
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
