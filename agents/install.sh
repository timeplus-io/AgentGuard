#!/usr/bin/env bash
set -euo pipefail

# ── Offline-mode flag ──────────────────────────────────────────────────────
# Usage: install.sh [--offline [PACKAGE_DIR]]
#   --offline            use packages/ dir next to this script
#   --offline /path/dir  use specified dir
OFFLINE_MODE="false"
PACKAGE_DIR=""
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --offline)
      OFFLINE_MODE="true"
      if [[ -n "${2:-}" && "${2:0:1}" != "-" ]]; then
        PACKAGE_DIR="$2"
        shift
      fi
      shift
      ;;
    *) shift ;;
  esac
done

# Auto-detect: if a packages/ dir with plugin files exists next to the script, go offline
if [[ "$OFFLINE_MODE" == "false" && -d "${SCRIPT_DIR}/packages" ]]; then
  if ls "${SCRIPT_DIR}/packages"/*agentguard*.{tgz,whl} 2>/dev/null | grep -q .; then
    echo "  [info] Detected offline package bundle in packages/ — switching to offline mode."
    OFFLINE_MODE="true"
  fi
fi

if [[ "$OFFLINE_MODE" == "true" && -z "$PACKAGE_DIR" ]]; then
  PACKAGE_DIR="${SCRIPT_DIR}/packages"
fi

# ── Banner ─────────────────────────────────────────────────────────────────
echo ""
echo "  ╔═══════════════════════════════════════╗"
echo "  ║   AgentGuard Plugin Installer         ║"
echo "  ╚═══════════════════════════════════════╝"
echo ""
if [[ "$OFFLINE_MODE" == "true" ]]; then
  echo "  Mode: OFFLINE  (packages: ${PACKAGE_DIR})"
  echo ""
fi

# ── Helpers ────────────────────────────────────────────────────────────────
prompt() {
  local prompt_text="$1" default="$2"
  local val
  read -rp "  ${prompt_text} [${default}]: " val
  echo "${val:-$default}"
}

prompt_secret() {
  local prompt_text="$1"
  local val
  read -rsp "  ${prompt_text}: " val
  printf '%s' "$val"
}

require_cmd() {
  if ! command -v "$1" &>/dev/null; then
    echo "Error: $1 not found. $2" >&2
    exit 1
  fi
}

# Resolve the first file matching a glob; exits with an error message if none found.
find_package() {
  local label="$1" glob="$2"
  local found
  found=$(ls $glob 2>/dev/null | sort -V | tail -1)
  if [[ -z "$found" ]]; then
    echo "Error: ${label} package not found matching: ${glob}" >&2
    echo "       Run 'make package-plugins' to prepare offline bundles." >&2
    exit 1
  fi
  echo "$found"
}

# ── Agent selection ────────────────────────────────────────────────────────
echo "Which agent are you installing AgentGuard for?"
echo "  1) Claude Code"
echo "  2) OpenClaw"
echo "  3) Hermes"
echo ""
while true; do
  read -rp "  Select [1-3]: " choice
  case "$choice" in
    1) AGENT_TYPE="claude-code"; break ;;
    2) AGENT_TYPE="openclaw"; break ;;
    3) AGENT_TYPE="hermes"; break ;;
    *) echo "  Please enter 1, 2, or 3." ;;
  esac
done
echo ""

# ── Shared config ──────────────────────────────────────────────────────────
echo "Configuration (press Enter to accept defaults):"
echo ""
AGENT_ID=$(prompt      "Agent ID (this machine's identifier)" "$AGENT_TYPE.$(hostname)")
DEPLOYMENT_ID=$(prompt "Deployment ID"                        "local")
DEPLOYMENT_NAME=$(prompt "Deployment Name"                   "Local Dev")
TIMEPLUS_URL=$(prompt  "Timeplus URL"                         "http://localhost:3218")
TIMEPLUS_USERNAME=$(prompt "Timeplus Username"                "proton")
TIMEPLUS_PASSWORD=$(prompt_secret "Timeplus Password")
echo ""
AGENTGUARD_URL=$(prompt "AgentGuard URL"                     "http://localhost:8080")

echo ""
read -rp "  Enable holds (synchronous tool-call blocking)? [Y/n]: " holds_yn
if [[ "$holds_yn" =~ ^[Nn]$ ]]; then
  HOLDS_ENABLED="false"
else
  HOLDS_ENABLED="true"
fi

if [[ "$HOLDS_ENABLED" == "true" ]]; then
  read -rp "  Hold fail policy (deny/allow) [deny]: " hold_policy
  HOLD_FAIL_POLICY="${hold_policy:-deny}"
else
  HOLD_FAIL_POLICY="deny"
fi

# ── Derive OTel endpoint from Timeplus URL ─────────────────────────────────
_otel_host() {
  TMPURL="$1" python3 -c "import os; from urllib.parse import urlparse; print(urlparse(os.environ['TMPURL']).hostname)"
}
_otel_proto() {
  TMPURL="$1" python3 -c "import os; from urllib.parse import urlparse; print(urlparse(os.environ['TMPURL']).scheme)"
}

# ═══════════════════════════════════════════════════════════════════════════
# CLAUDE CODE
# ═══════════════════════════════════════════════════════════════════════════
if [[ "$AGENT_TYPE" == "claude-code" ]]; then
  echo ""
  echo "── Claude Code ────────────────────────────────────────────────────────────"
  require_cmd python3 "Install Python 3 from https://python.org"

  OTEL_ENDPOINT="$(_otel_proto "$TIMEPLUS_URL")://$(_otel_host "$TIMEPLUS_URL"):4318"

  require_cmd npm "Install Node.js >= 18 from https://nodejs.org"

  if [[ "$OFFLINE_MODE" == "true" ]]; then
    PLUGIN_TGZ=$(find_package "Claude Code" "${PACKAGE_DIR}/*agentguard-claudecode-plugin-*.tgz")
    echo ""
    echo "Installing @timeplus/agentguard-claudecode-plugin globally from ${PLUGIN_TGZ}..."
    npm install -g "$PLUGIN_TGZ"
  else
    echo ""
    echo "Installing @timeplus/agentguard-claudecode-plugin globally..."
    npm install -g @timeplus/agentguard-claudecode-plugin
  fi
  HOOK_COMMAND="agentguard-hook"

  echo ""
  echo "Configuring ~/.claude/settings.json..."
  AGENTGUARD_HOOK_COMMAND="$HOOK_COMMAND" \
  AGENTGUARD_AGENT_ID="$AGENT_ID" \
  AGENTGUARD_DEPLOYMENT_ID="$DEPLOYMENT_ID" \
  AGENTGUARD_DEPLOYMENT_NAME="$DEPLOYMENT_NAME" \
  AGENTGUARD_TIMEPLUS_URL="$TIMEPLUS_URL" \
  AGENTGUARD_USERNAME="$TIMEPLUS_USERNAME" \
  AGENTGUARD_PASSWORD="$TIMEPLUS_PASSWORD" \
  AGENTGUARD_URL="$AGENTGUARD_URL" \
  AGENTGUARD_HOLD_FAIL_POLICY="$HOLD_FAIL_POLICY" \
  AGENTGUARD_HOLDS_ENABLED="$HOLDS_ENABLED" \
  AGENTGUARD_OTEL_ENDPOINT="$OTEL_ENDPOINT" \
  python3 - <<PYEOF
import json, os

settings_path = os.path.expanduser("~/.claude/settings.json")
hook_command  = os.environ["AGENTGUARD_HOOK_COMMAND"]

sync_events  = ["PreToolUse"]
async_events = [
    "SessionStart", "SessionEnd", "UserPromptSubmit",
    "PostToolUse", "PostToolUseFailure",
    "SubagentStart", "SubagentStop", "Stop", "PermissionDenied",
]

sync_entry = {
    "matcher": "*",
    "hooks": [{"type": "command", "command": hook_command, "timeout": 600000}],
}
async_entry = {
    "matcher": "*",
    "hooks": [{"type": "command", "command": hook_command, "async": True}],
}

os.makedirs(os.path.dirname(settings_path), exist_ok=True)
data = {}
if os.path.exists(settings_path):
    with open(settings_path) as f:
        data = json.load(f)

hooks = data.setdefault("hooks", {})

def upsert(event, entry):
    entries = hooks.setdefault(event, [])
    entries[:] = [
        e for e in entries
        if not any(h.get("command") == hook_command for h in e.get("hooks", []))
    ]
    entries.append(entry)

for event in sync_events:
    upsert(event, sync_entry)
for event in async_events:
    upsert(event, async_entry)

import os as _os
_e = _os.environ
data.setdefault("env", {}).update({
    "AGENTGUARD_AGENT_ID":                  _e["AGENTGUARD_AGENT_ID"],
    "AGENTGUARD_DEPLOYMENT_ID":             _e["AGENTGUARD_DEPLOYMENT_ID"],
    "AGENTGUARD_DEPLOYMENT_NAME":           _e["AGENTGUARD_DEPLOYMENT_NAME"],
    "AGENTGUARD_TIMEPLUS_URL":              _e["AGENTGUARD_TIMEPLUS_URL"],
    "AGENTGUARD_STREAM":                    "agentguard_hook_events",
    "AGENTGUARD_USERNAME":                  _e["AGENTGUARD_USERNAME"],
    "AGENTGUARD_PASSWORD":                  _e["AGENTGUARD_PASSWORD"],
    "AGENTGUARD_URL":                       _e["AGENTGUARD_URL"],
    "AGENTGUARD_HOLD_FAIL_POLICY":          _e["AGENTGUARD_HOLD_FAIL_POLICY"],
    "AGENTGUARD_HOLDS_ENABLED":             _e["AGENTGUARD_HOLDS_ENABLED"],
    "CLAUDE_CODE_ENABLE_TELEMETRY":         "1",
    "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA":  "1",
    "OTEL_METRICS_EXPORTER":                "otlp",
    "OTEL_LOGS_EXPORTER":                   "otlp",
    "OTEL_TRACES_EXPORTER":                 "otlp",
    "OTEL_EXPORTER_OTLP_PROTOCOL":          "http/protobuf",
    "OTEL_EXPORTER_OTLP_ENDPOINT":          _e["AGENTGUARD_OTEL_ENDPOINT"],
    "OTEL_METRIC_EXPORT_INTERVAL":          "10000",
    "OTEL_LOGS_EXPORT_INTERVAL":            "5000",
    "OTEL_LOG_TOOL_DETAILS":                "1",
})

with open(settings_path, "w") as f:
    json.dump(data, f, indent=2)
print("  Done: " + settings_path)
PYEOF

  echo ""
  echo "  ✓ Claude Code plugin installed"
  echo ""
  echo "Next steps:"
  echo "  1. Quit and reopen Claude Code"
  echo "  2. Open AgentGuard: ${AGENTGUARD_URL}"
fi

# ═══════════════════════════════════════════════════════════════════════════
# OPENCLAW
# ═══════════════════════════════════════════════════════════════════════════
if [[ "$AGENT_TYPE" == "openclaw" ]]; then
  echo ""
  echo "── OpenClaw ───────────────────────────────────────────────────────────────"
  require_cmd npm "Install Node.js >= 18 from https://nodejs.org"
  require_cmd python3 "Install Python 3 from https://python.org"

  read -rp "  OpenClaw project directory [$(pwd)]: " oc_dir
  OC_DIR="${oc_dir:-$(pwd)}"
  if [[ ! -d "$OC_DIR" ]]; then
    echo "Error: directory not found: $OC_DIR" >&2
    exit 1
  fi

  DERIVED_OTEL="$(_otel_proto "$TIMEPLUS_URL")://host.docker.internal:4318"
  read -rp "  OTel endpoint (host.docker.internal for Docker) [${DERIVED_OTEL}]: " otel_in
  OTEL_ENDPOINT="${otel_in:-$DERIVED_OTEL}"

  pushd "$OC_DIR" > /dev/null

  if [[ "$OFFLINE_MODE" == "true" ]]; then
    # ── Offline: install from local tgz ────────────────────────────────────
    PLUGIN_TGZ=$(find_package "OpenClaw" "${PACKAGE_DIR}/*agentguard-openclaw-plugin-*.tgz")
    echo ""
    echo "Installing agentguard-openclaw-plugin from ${PLUGIN_TGZ}..."
    npm install --legacy-peer-deps "$PLUGIN_TGZ"
  else
    # ── Online: install from npm registry ──────────────────────────────────
    echo ""
    echo "Installing @timeplus/agentguard-openclaw-plugin..."
    npm install @timeplus/agentguard-openclaw-plugin
  fi

  PLUGIN_PATH=$(node -e "console.log(require.resolve('@timeplus/agentguard-openclaw-plugin/package.json').replace(/\/package\.json$/, ''))" 2>/dev/null || echo "")
  popd > /dev/null

  CONFIG_FILE="${OC_DIR}/.openclaw/openclaw.json"
  echo ""
  echo "Patching ${CONFIG_FILE}..."
  AGENTGUARD_CONFIG_FILE="$CONFIG_FILE" \
  AGENTGUARD_OTEL_ENDPOINT="$OTEL_ENDPOINT" \
  AGENTGUARD_TIMEPLUS_URL="$TIMEPLUS_URL" \
  AGENTGUARD_USERNAME="$TIMEPLUS_USERNAME" \
  AGENTGUARD_PASSWORD="$TIMEPLUS_PASSWORD" \
  AGENTGUARD_DEPLOYMENT_ID="$DEPLOYMENT_ID" \
  AGENTGUARD_DEPLOYMENT_NAME="$DEPLOYMENT_NAME" \
  AGENTGUARD_URL="$AGENTGUARD_URL" \
  AGENTGUARD_HOLD_FAIL_POLICY="$HOLD_FAIL_POLICY" \
  AGENTGUARD_HOLDS_ENABLED="$HOLDS_ENABLED" \
  AGENTGUARD_PLUGIN_PATH="$PLUGIN_PATH" \
  python3 - <<PYEOF
import json, os, sys

_e = os.environ
path = _e["AGENTGUARD_CONFIG_FILE"]
if not os.path.exists(path):
    print(f"Error: {path} not found. Run 'make init' in your OpenClaw project first.", file=sys.stderr)
    sys.exit(1)

with open(path) as f:
    data = json.load(f)

data.setdefault("gateway", {})["bind"] = "lan"

data["diagnostics"] = {
    "enabled": True,
    "otel": {
        "enabled": True, "endpoint": _e["AGENTGUARD_OTEL_ENDPOINT"],
        "traces": True, "metrics": True, "logs": True,
        "serviceName": "openclaw", "sampleRate": 1, "flushIntervalMs": 3000,
    },
}

entries = data.setdefault("plugins", {}).setdefault("entries", {})
entries["diagnostics-otel"] = {"enabled": True}

load_paths = data["plugins"].setdefault("load", {}).setdefault("paths", [])
plugin_path = _e["AGENTGUARD_PLUGIN_PATH"]
if plugin_path and plugin_path not in load_paths:
    load_paths.append(plugin_path)

entries["agentguard"] = {
    "enabled": True,
    "config": {
        "timeplusUrl":     _e["AGENTGUARD_TIMEPLUS_URL"],
        "stream":          "agentguard_hook_events",
        "flushMs":         2000,
        "batchSize":       50,
        "username":        _e["AGENTGUARD_USERNAME"],
        "password":        _e["AGENTGUARD_PASSWORD"],
        "deploymentId":    _e["AGENTGUARD_DEPLOYMENT_ID"],
        "deploymentName":  _e["AGENTGUARD_DEPLOYMENT_NAME"],
        "agentguardUrl":   _e["AGENTGUARD_URL"],
        "holdFailPolicy":  _e["AGENTGUARD_HOLD_FAIL_POLICY"],
        "holdsEnabled":    _e["AGENTGUARD_HOLDS_ENABLED"].lower() != "false",
    },
}

with open(path, "w") as f:
    json.dump(data, f, indent=2)
print("  Done: " + path)
PYEOF

  if [[ -z "$PLUGIN_PATH" ]]; then
    echo "  Warning: could not resolve agentguard-openclaw-plugin install path." >&2
    echo "  Add the plugin path to plugins.load.paths in .openclaw/openclaw.json manually." >&2
  fi

  echo ""
  echo "  ✓ OpenClaw plugin installed"
  echo ""
  echo "Next steps:"
  echo "  1. Restart OpenClaw: make start"
  echo "  2. Open AgentGuard: ${AGENTGUARD_URL}"
fi

# ═══════════════════════════════════════════════════════════════════════════
# HERMES
# ═══════════════════════════════════════════════════════════════════════════
if [[ "$AGENT_TYPE" == "hermes" ]]; then
  echo ""
  echo "── Hermes ─────────────────────────────────────────────────────────────────"

  PIP_CMD=""
  command -v pip3  &>/dev/null && PIP_CMD="pip3"
  command -v pip   &>/dev/null && PIP_CMD="${PIP_CMD:-pip}"
  if [[ -z "$PIP_CMD" ]]; then
    echo "Error: pip not found. Install Python 3 from https://python.org" >&2
    exit 1
  fi

  echo ""
  if [[ "$OFFLINE_MODE" == "true" ]]; then
    # ── Offline: install from local wheel ──────────────────────────────────
    PLUGIN_WHL=$(find_package "Hermes" "${PACKAGE_DIR}/*agentguard*hermes*.whl")
    echo "Installing agentguard-hermes-plugin from ${PLUGIN_WHL}..."
    "$PIP_CMD" install --no-deps "$PLUGIN_WHL"
  else
    # ── Online: install from PyPI ───────────────────────────────────────────
    echo "Installing agentguard-hermes-plugin..."
    "$PIP_CMD" install agentguard-hermes-plugin
  fi

  agentguard-hermes install

  echo ""
  echo "Enable agentguard-hermes-plugin..."
  if command -v hermes &>/dev/null; then
    hermes plugins enable agentguard
  else
    echo "Warning: 'hermes' is not found — skipping plugin enable."
  fi

  EXPORT_BLOCK="$(printf '# AgentGuard\n'
printf 'export AGENTGUARD_TIMEPLUS_URL=%q\n'    "$TIMEPLUS_URL"
printf 'export AGENTGUARD_USERNAME=%q\n'         "$TIMEPLUS_USERNAME"
printf 'export AGENTGUARD_PASSWORD=%q\n'         "$TIMEPLUS_PASSWORD"
printf 'export AGENTGUARD_AGENT_ID=%q\n'         "$AGENT_ID"
printf 'export AGENTGUARD_DEPLOYMENT_ID=%q\n'    "$DEPLOYMENT_ID"
printf 'export AGENTGUARD_DEPLOYMENT_NAME=%q\n'  "$DEPLOYMENT_NAME"
printf 'export AGENTGUARD_URL=%q\n'              "$AGENTGUARD_URL"
printf 'export AGENTGUARD_HOLDS_ENABLED=%q\n'    "$HOLDS_ENABLED"
printf 'export AGENTGUARD_HOLD_FAIL_POLICY=%q\n' "$HOLD_FAIL_POLICY")"

  echo ""
  echo "Environment variables:"
  echo "$EXPORT_BLOCK"
  echo ""

  SHELL_PROFILE=""
  [[ -f "$HOME/.zshrc"  ]] && SHELL_PROFILE="$HOME/.zshrc"
  [[ -z "$SHELL_PROFILE" && -f "$HOME/.bashrc" ]] && SHELL_PROFILE="$HOME/.bashrc"

  if [[ -n "$SHELL_PROFILE" ]]; then
    read -rp "  Append to ${SHELL_PROFILE}? [Y/n]: " append_yn
    if [[ ! "$append_yn" =~ ^[Nn]$ ]]; then
      if grep -q "AGENTGUARD_TIMEPLUS_URL" "$SHELL_PROFILE" 2>/dev/null; then
        echo "  AgentGuard vars already present in ${SHELL_PROFILE} — skipping append."
        echo "  If your configuration changed, update these values manually:"
        echo "$EXPORT_BLOCK"
      else
        printf '\n%s\n' "$EXPORT_BLOCK" >> "$SHELL_PROFILE"
        echo "  ✓ Appended to ${SHELL_PROFILE}"
        echo "  Run: source ${SHELL_PROFILE}"
      fi
    fi
  else
    echo "  (No .zshrc or .bashrc found — add the export lines to your shell profile manually)"
  fi

  echo ""
  echo "  ✓ Hermes plugin installed"
  echo ""
  echo "Next steps:"
  echo "  1. Reload your shell and restart Hermes"
  echo "  2. Open AgentGuard: ${AGENTGUARD_URL}"
fi
