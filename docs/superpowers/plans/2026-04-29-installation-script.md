# Installation Script Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver an interactive shell script (`agents/install.sh`) that automates full AgentGuard plugin installation for Claude Code, OpenClaw, and Hermes, served from the backend and linked from the onboarding UI.

**Architecture:** A single `agents/install.sh` script prompts for all config interactively and runs npm/pip install + settings merges end-to-end. The backend embeds it via `backend/internal/scripts/` and serves it at `GET /api/onboarding/install-script`. The onboarding UI Step 1 gets a Download button + run command, keeping the config form as a "values to have ready" checklist.

**Tech Stack:** Bash, Python 3 (inline heredoc), Go embed, React/Vite, Gin

---

## File Map

| File | Action |
|------|--------|
| `agents/install.sh` | **Create** — interactive install script (canonical source) |
| `backend/internal/scripts/install.sh` | **Create** — symlink or copy; embedded by Go |
| `backend/internal/scripts/embed.go` | **Create** — `//go:embed install.sh` |
| `backend/internal/handlers/onboarding.go` | **Modify** — add `ServeInstallScript` handler |
| `backend/cmd/server/main.go` | **Modify** — register `GET /api/onboarding/install-script` |
| `frontend/src/pages/Onboarding.jsx` | **Modify** — replace Step 1 code blocks with download button |

> **Note on Go embed:** Go's `//go:embed` cannot reference paths with `../`. The script lives in `agents/install.sh` (canonical, user-runnable) and is **copied** to `backend/internal/scripts/install.sh` as part of the Makefile `build` target (via a new `scripts-sync` step, analogous to `web-sync`). For dev, the handler falls back to reading from disk.

---

## Task 1: Write the interactive install script

**Files:**
- Create: `agents/install.sh`

- [ ] **Step 1: Create the script**

```bash
cat > agents/install.sh << 'SCRIPT_EOF'
#!/usr/bin/env bash
set -euo pipefail

# ── Banner ─────────────────────────────────────────────────────────────────
echo ""
echo "  ╔═══════════════════════════════════════╗"
echo "  ║   AgentGuard Plugin Installer         ║"
echo "  ╚═══════════════════════════════════════╝"
echo ""

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
  echo ""
  printf '%s' "$val"
}

require_cmd() {
  if ! command -v "$1" &>/dev/null; then
    echo "Error: $1 not found. $2" >&2
    exit 1
  fi
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
AGENT_ID=$(prompt      "Agent ID (this machine's identifier)" "$(hostname)")
DEPLOYMENT_ID=$(prompt "Deployment ID"                        "local")
DEPLOYMENT_NAME=$(prompt "Deployment Name"                   "Local Dev")
TIMEPLUS_URL=$(prompt  "Timeplus URL"                         "http://localhost:3218")
TIMEPLUS_USERNAME=$(prompt "Timeplus Username"                "proton")
TIMEPLUS_PASSWORD=$(prompt_secret "Timeplus Password")
AGENTGUARD_URL=$(prompt "AgentGuard URL"                     "http://localhost:8080")

echo ""
read -rp "  Enable holds (synchronous tool-call blocking)? [Y/n]: " holds_yn
if [[ "${holds_yn,,}" == "n" ]]; then
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
  local url="$1"
  echo "$url" | sed 's|:[0-9]*$||' | sed -E 's|^https?://||'
}
_otel_proto() {
  echo "$1" | grep -oE '^https?'
}

# ═══════════════════════════════════════════════════════════════════════════
# CLAUDE CODE
# ═══════════════════════════════════════════════════════════════════════════
if [[ "$AGENT_TYPE" == "claude-code" ]]; then
  echo ""
  echo "── Claude Code ────────────────────────────────────────────────────────────"
  require_cmd npm "Install Node.js >= 18 from https://nodejs.org"
  require_cmd python3 "Install Python 3 from https://python.org"

  OTEL_ENDPOINT="$(_otel_proto "$TIMEPLUS_URL")://$(_otel_host "$TIMEPLUS_URL"):4318"

  echo ""
  echo "Installing @timeplus/agentguard-claudecode-plugin globally..."
  npm install -g @timeplus/agentguard-claudecode-plugin

  echo ""
  echo "Configuring ~/.claude/settings.json..."
  python3 - <<PYEOF
import json, os

settings_path = os.path.expanduser("~/.claude/settings.json")
hook_command  = "agentguard-hook"

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

data.setdefault("env", {}).update({
    "AGENTGUARD_AGENT_ID":                  "${AGENT_ID}",
    "AGENTGUARD_DEPLOYMENT_ID":             "${DEPLOYMENT_ID}",
    "AGENTGUARD_DEPLOYMENT_NAME":           "${DEPLOYMENT_NAME}",
    "AGENTGUARD_TIMEPLUS_URL":              "${TIMEPLUS_URL}",
    "AGENTGUARD_STREAM":                    "agentguard_hook_events",
    "AGENTGUARD_USERNAME":                  "${TIMEPLUS_USERNAME}",
    "AGENTGUARD_PASSWORD":                  "${TIMEPLUS_PASSWORD}",
    "AGENTGUARD_URL":                       "${AGENTGUARD_URL}",
    "AGENTGUARD_HOLD_FAIL_POLICY":          "${HOLD_FAIL_POLICY}",
    "AGENTGUARD_HOLDS_ENABLED":             "${HOLDS_ENABLED}",
    "CLAUDE_CODE_ENABLE_TELEMETRY":         "1",
    "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA":  "1",
    "OTEL_METRICS_EXPORTER":                "otlp",
    "OTEL_LOGS_EXPORTER":                   "otlp",
    "OTEL_TRACES_EXPORTER":                 "otlp",
    "OTEL_EXPORTER_OTLP_PROTOCOL":          "http/protobuf",
    "OTEL_EXPORTER_OTLP_ENDPOINT":          "${OTEL_ENDPOINT}",
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

  echo ""
  echo "Installing @timeplus/agentguard-openclaw-plugin..."
  cd "$OC_DIR"
  npm install @timeplus/agentguard-openclaw-plugin
  PLUGIN_PATH=$(node -e "console.log(require.resolve('@timeplus/agentguard-openclaw-plugin/package.json').replace(/\/package\.json$/, ''))" 2>/dev/null || echo "")

  CONFIG_FILE="${OC_DIR}/.openclaw/openclaw.json"
  echo ""
  echo "Patching ${CONFIG_FILE}..."
  python3 - <<PYEOF
import json, os, sys

path = "${CONFIG_FILE}"
if not os.path.exists(path):
    print(f"Error: {path} not found. Run 'make init' in your OpenClaw project first.", file=sys.stderr)
    sys.exit(1)

with open(path) as f:
    data = json.load(f)

data.setdefault("gateway", {})["bind"] = "lan"

data["diagnostics"] = {
    "enabled": True,
    "otel": {
        "enabled": True, "endpoint": "${OTEL_ENDPOINT}",
        "traces": True, "metrics": True, "logs": True,
        "serviceName": "openclaw", "sampleRate": 1, "flushIntervalMs": 3000,
    },
}

entries = data.setdefault("plugins", {}).setdefault("entries", {})
entries["diagnostics-otel"] = {"enabled": True}

load_paths = data["plugins"].setdefault("load", {}).setdefault("paths", [])
plugin_path = "${PLUGIN_PATH}"
if plugin_path and plugin_path not in load_paths:
    load_paths.append(plugin_path)

entries["agentguard"] = {
    "enabled": True,
    "config": {
        "timeplusUrl":     "${TIMEPLUS_URL}",
        "stream":          "agentguard_hook_events",
        "flushMs":         2000,
        "batchSize":       50,
        "username":        "${TIMEPLUS_USERNAME}",
        "password":        "${TIMEPLUS_PASSWORD}",
        "deploymentId":    "${DEPLOYMENT_ID}",
        "deploymentName":  "${DEPLOYMENT_NAME}",
        "agentguardUrl":   "${AGENTGUARD_URL}",
        "holdFailPolicy":  "${HOLD_FAIL_POLICY}",
        "holdsEnabled":    "${HOLDS_ENABLED}".lower() != "false",
    },
}

with open(path, "w") as f:
    json.dump(data, f, indent=2)
print("  Done: " + path)
PYEOF

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
  echo "Installing agentguard-hermes-plugin..."
  "$PIP_CMD" install agentguard-hermes-plugin
  agentguard-hermes install

  EXPORT_BLOCK="# AgentGuard
export AGENTGUARD_TIMEPLUS_URL=\"${TIMEPLUS_URL}\"
export AGENTGUARD_USERNAME=\"${TIMEPLUS_USERNAME}\"
export AGENTGUARD_PASSWORD=\"${TIMEPLUS_PASSWORD}\"
export AGENTGUARD_AGENT_ID=\"${AGENT_ID}\"
export AGENTGUARD_DEPLOYMENT_ID=\"${DEPLOYMENT_ID}\"
export AGENTGUARD_DEPLOYMENT_NAME=\"${DEPLOYMENT_NAME}\"
export AGENTGUARD_URL=\"${AGENTGUARD_URL}\"
export AGENTGUARD_HOLDS_ENABLED=\"${HOLDS_ENABLED}\"
export AGENTGUARD_HOLD_FAIL_POLICY=\"${HOLD_FAIL_POLICY}\""

  echo ""
  echo "Environment variables:"
  echo "$EXPORT_BLOCK"
  echo ""

  SHELL_PROFILE=""
  [[ -f "$HOME/.zshrc"  ]] && SHELL_PROFILE="$HOME/.zshrc"
  [[ -z "$SHELL_PROFILE" && -f "$HOME/.bashrc" ]] && SHELL_PROFILE="$HOME/.bashrc"

  if [[ -n "$SHELL_PROFILE" ]]; then
    read -rp "  Append to ${SHELL_PROFILE}? [Y/n]: " append_yn
    if [[ "${append_yn,,}" != "n" ]]; then
      printf '\n%s\n' "$EXPORT_BLOCK" >> "$SHELL_PROFILE"
      echo "  ✓ Appended to ${SHELL_PROFILE}"
      echo "  Run: source ${SHELL_PROFILE}"
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
SCRIPT_EOF
chmod +x agents/install.sh
```

- [ ] **Step 2: Smoke-test the script is executable and prints the banner**

```bash
bash agents/install.sh <<< "q" 2>&1 | head -10 || true
```

Expected: prints the banner and the agent selection menu (then exits on invalid input — that's fine).

- [ ] **Step 3: Commit**

```bash
git add agents/install.sh
git commit -m "feat: add interactive AgentGuard plugin install script"
```

---

## Task 2: Embed the script in the backend

**Files:**
- Create: `backend/internal/scripts/` directory
- Create: `backend/internal/scripts/install.sh` (copy of agents/install.sh)
- Create: `backend/internal/scripts/embed.go`
- Modify: `Makefile` — add `scripts-sync` step

- [ ] **Step 1: Create the scripts directory and copy the script**

```bash
mkdir -p backend/internal/scripts
cp agents/install.sh backend/internal/scripts/install.sh
```

- [ ] **Step 2: Create the embed file**

Create `backend/internal/scripts/embed.go`:

```go
package scripts

import _ "embed"

//go:embed install.sh
var InstallScript []byte
```

- [ ] **Step 3: Add `scripts-sync` to the Makefile**

In `Makefile`, find the `web-sync` target and add `scripts-sync` right after it:

```makefile
## scripts-sync: copy agents/install.sh into the backend embed directory
scripts-sync:
	cp agents/install.sh backend/internal/scripts/install.sh
```

Then update the `build` target to include `scripts-sync`:

```makefile
## build: full production build — frontend → embed → single binary
build: frontend-build web-sync scripts-sync backend-build
```

- [ ] **Step 4: Verify Go can compile with the embed**

```bash
cd backend && CGO_ENABLED=0 go build -tags embed ./internal/scripts/...
```

Expected: no output (clean compile).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/scripts/embed.go backend/internal/scripts/install.sh Makefile
git commit -m "feat: embed install.sh in backend for serving"
```

---

## Task 3: Backend handler and route

**Files:**
- Modify: `backend/internal/handlers/onboarding.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Add the handler to `backend/internal/handlers/onboarding.go`**

Add this import and function at the end of the file (after the existing `generateClaudeCodeConfig` function):

```go
import (
    // existing imports ...
    "net/http"

    "github.com/gin-gonic/gin"
    "github.com/timeplus/agentguard/internal/scripts"
)

// ServeInstallScript serves the interactive install.sh for download.
func ServeInstallScript(c *gin.Context) {
    c.Header("Content-Disposition", `attachment; filename="agentguard-install.sh"`)
    c.Data(http.StatusOK, "text/x-shellscript", scripts.InstallScript)
}
```

> Note: the existing file already imports `"net/http"` and `"github.com/gin-gonic/gin"` — only add the `scripts` import.

- [ ] **Step 2: Register the route in `backend/cmd/server/main.go`**

Find the line:
```go
protected.POST("/onboarding/generate", handlers.GenerateConfig)
```

Add the new route directly below it:
```go
protected.GET("/onboarding/install-script", handlers.ServeInstallScript)
```

- [ ] **Step 3: Compile-check the backend**

```bash
cd backend && CGO_ENABLED=0 go build -tags embed ./...
```

Expected: no errors.

- [ ] **Step 4: Smoke-test the endpoint**

```bash
# Start the server (in a separate terminal or background)
cd backend && CGO_ENABLED=0 go build -tags embed -o server cmd/server/main.go && ./server &
sleep 2

# Login first to get a session cookie (using default credentials)
curl -s -c /tmp/ag_cookies.txt -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | head -c 200

# Download the script
curl -s -b /tmp/ag_cookies.txt http://localhost:8080/api/onboarding/install-script -o /tmp/test-install.sh
head -5 /tmp/test-install.sh
```

Expected: first line is `#!/usr/bin/env bash` and content matches `agents/install.sh`.

```bash
kill %1 2>/dev/null || true
```

- [ ] **Step 5: Commit**

```bash
git add backend/internal/handlers/onboarding.go backend/cmd/server/main.go
git commit -m "feat: add GET /api/onboarding/install-script endpoint"
```

---

## Task 4: Frontend — replace Step 1 code blocks with download button

**Files:**
- Modify: `frontend/src/pages/Onboarding.jsx`

- [ ] **Step 1: Add the `DownloadScriptButton` component**

In `Onboarding.jsx`, add this component after the `CodeBlock` component definition (around line 67):

```jsx
function DownloadScriptButton() {
  const [downloading, setDownloading] = useState(false)

  const handle = async () => {
    setDownloading(true)
    try {
      const res = await fetch('/api/onboarding/install-script')
      const blob = await res.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = 'agentguard-install.sh'
      a.click()
      URL.revokeObjectURL(url)
    } finally {
      setDownloading(false)
    }
  }

  return (
    <button
      onClick={handle}
      disabled={downloading}
      className="flex items-center gap-2 bg-primary-button px-5 py-2.5 rounded-sm text-xs font-semibold text-primary-button-text hover:scale-[1.02] active:scale-95 transition-all disabled:opacity-50 disabled:cursor-not-allowed"
    >
      <span className="material-symbols-outlined text-sm">download</span>
      {downloading ? 'Downloading…' : 'Download Install Script'}
    </button>
  )
}
```

- [ ] **Step 2: Replace the claude-code Step 1 content**

Find the block starting with `{step === 1 && selectedAgent === 'claude-code' && (` (around line 467).

Replace the entire JSX block (from the opening `<>` to the closing `</>`) with:

```jsx
{step === 1 && selectedAgent === 'claude-code' && (
  <>
    <div>
      <h2 className="text-xl font-semibold text-white tracking-tight">Install Plugin</h2>
      <p className="text-zinc-400 mt-1.5 text-xs leading-relaxed">
        Download and run the interactive install script. It will prompt you for the values below and configure everything automatically.
      </p>
    </div>

    <div className="flex flex-col gap-3">
      <DownloadScriptButton />
      <CodeBlock label="Then run" code="bash agentguard-install.sh" />
    </div>

    <div className="space-y-3">
      <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Values to have ready</p>
      <div className="grid grid-cols-1 gap-3">
        {[
          { label: 'Agent ID *', val: agentId, set: setAgentId, ph: 'your-hostname' },
          { label: 'Deployment ID', val: deploymentId, set: setDeploymentId, ph: 'local' },
          { label: 'Deployment Name', val: deploymentName, set: setDeploymentName, ph: 'Local Dev' },
          { label: 'Timeplus URL', val: timeplusUrl, set: setTimeplusUrl, ph: 'http://localhost:3218' },
          { label: 'Timeplus Username', val: timeplusUsername, set: setTimeplusUsername, ph: 'proton' },
          { label: 'Timeplus Password', val: timeplusPassword, set: setTimeplusPassword, ph: 'your-password', type: 'password' },
          { label: 'AgentGuard URL', val: agentguardUrl, set: setAgentguardUrl, ph: 'http://localhost:8080' },
        ].map(({ label, val, set, ph, type }) => (
          <div key={label} className="flex items-center gap-4">
            <label className="text-[10px] font-medium text-zinc-500 uppercase tracking-widest w-36 shrink-0">{label}</label>
            <input
              type={type || 'text'}
              value={val}
              onChange={e => set(e.target.value)}
              placeholder={ph}
              className="flex-1 bg-gray-800 border border-gray-600 text-xs py-2 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-[#e7dff0] font-mono"
            />
          </div>
        ))}
        <div className="flex items-center gap-4">
          <label className="text-[10px] font-medium text-zinc-500 uppercase tracking-widest w-36 shrink-0">Holds Enabled</label>
          <label className="flex items-center gap-2 cursor-pointer">
            <input type="checkbox" checked={holdsEnabled} onChange={e => setHoldsEnabled(e.target.checked)} className="accent-[#ffb0cf]" />
            <span className="text-[10px] text-zinc-400">{holdsEnabled ? 'Synchronous PreToolUse blocking on' : 'Observation-only — no blocking'}</span>
          </label>
        </div>
        <div className="flex items-center gap-4">
          <label className="text-[10px] font-medium text-zinc-500 uppercase tracking-widest w-36 shrink-0">Hold Fail Policy</label>
          <select value={holdFailPolicy} onChange={e => setHoldFailPolicy(e.target.value)} disabled={!holdsEnabled}
            className="flex-1 bg-gray-800 border border-gray-600 text-xs py-2 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-[#e7dff0] font-mono disabled:opacity-50">
            <option value="deny">deny — block on errors</option>
            <option value="allow">allow — pass through on errors</option>
          </select>
        </div>
      </div>
      <p className="text-[10px] text-zinc-500 leading-relaxed">* Agent ID identifies this Claude Code instance — use your hostname or machine name.</p>
    </div>
  </>
)}
```

- [ ] **Step 3: Replace the openclaw Step 1 content**

Find the block starting with `{step === 1 && selectedAgent === 'openclaw' && (` (around line 389).

Replace the entire JSX block with:

```jsx
{step === 1 && selectedAgent === 'openclaw' && (
  <>
    <div>
      <h2 className="text-xl font-semibold text-white tracking-tight">Install Plugin</h2>
      <p className="text-zinc-400 mt-1.5 text-xs leading-relaxed">
        Download and run the interactive install script from your OpenClaw project directory. It will prompt for the values below and patch your <code className="text-zinc-300">openclaw.json</code> automatically.
      </p>
    </div>

    <div className="flex flex-col gap-3">
      <DownloadScriptButton />
      <CodeBlock label="Then run from your OpenClaw project directory" code="bash agentguard-install.sh" />
    </div>

    <div className="space-y-3">
      <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Values to have ready</p>
      <div className="grid grid-cols-1 gap-3">
        {[
          { label: 'Deployment ID', val: deploymentId, set: setDeploymentId, ph: 'my-team' },
          { label: 'Deployment Name', val: deploymentName, set: setDeploymentName, ph: 'My Team' },
          { label: 'Timeplus URL', val: timeplusUrl, set: setTimeplusUrl, ph: 'http://localhost:3218' },
          { label: 'Timeplus Username', val: timeplusUsername, set: setTimeplusUsername, ph: 'proton' },
          { label: 'Timeplus Password', val: timeplusPassword, set: setTimeplusPassword, ph: 'your-password', type: 'password' },
          { label: 'AgentGuard URL', val: agentguardUrl, set: setAgentguardUrl, ph: 'http://localhost:8080' },
        ].map(({ label, val, set, ph, type }) => (
          <div key={label} className="flex items-center gap-4">
            <label className="text-[10px] font-medium text-zinc-500 uppercase tracking-widest w-36 shrink-0">{label}</label>
            <input
              type={type || 'text'}
              value={val}
              onChange={e => set(e.target.value)}
              placeholder={ph}
              className="flex-1 bg-gray-800 border border-gray-600 text-xs py-2 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-[#e7dff0] font-mono"
            />
          </div>
        ))}
        <div className="flex items-center gap-4">
          <label className="text-[10px] font-medium text-zinc-500 uppercase tracking-widest w-36 shrink-0">Holds Enabled</label>
          <label className="flex items-center gap-2 cursor-pointer">
            <input type="checkbox" checked={holdsEnabled} onChange={e => setHoldsEnabled(e.target.checked)} className="accent-[#ffb0cf]" />
            <span className="text-[10px] text-zinc-400">{holdsEnabled ? 'Synchronous PreToolUse blocking on' : 'Observation-only — no blocking'}</span>
          </label>
        </div>
        <div className="flex items-center gap-4">
          <label className="text-[10px] font-medium text-zinc-500 uppercase tracking-widest w-36 shrink-0">Hold Fail Policy</label>
          <select value={holdFailPolicy} onChange={e => setHoldFailPolicy(e.target.value)} disabled={!holdsEnabled}
            className="flex-1 bg-gray-800 border border-gray-600 text-xs py-2 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-[#e7dff0] font-mono disabled:opacity-50">
            <option value="deny">deny — block on errors</option>
            <option value="allow">allow — pass through on errors</option>
          </select>
        </div>
      </div>
    </div>
  </>
)}
```

- [ ] **Step 4: Replace the hermes Step 1 content**

Find the block starting with `{step === 1 && selectedAgent === 'hermes' && (` (around line 543).

Replace the entire JSX block with:

```jsx
{step === 1 && selectedAgent === 'hermes' && (
  <>
    <div>
      <h2 className="text-xl font-semibold text-white tracking-tight">Install Plugin</h2>
      <p className="text-zinc-400 mt-1.5 text-xs leading-relaxed">
        Download and run the interactive install script. It will pip-install the plugin, run <code className="text-zinc-300">agentguard-hermes install</code>, and offer to append the required env vars to your shell profile.
      </p>
    </div>

    <div className="flex flex-col gap-3">
      <DownloadScriptButton />
      <CodeBlock label="Then run" code="bash agentguard-install.sh" />
    </div>

    <div className="space-y-3">
      <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Values to have ready</p>
      <div className="grid grid-cols-1 gap-3">
        {[
          { label: 'Agent ID *', val: agentId, set: setAgentId, ph: 'your-hostname' },
          { label: 'Deployment ID', val: deploymentId, set: setDeploymentId, ph: 'local' },
          { label: 'Deployment Name', val: deploymentName, set: setDeploymentName, ph: 'Local Dev' },
          { label: 'Timeplus URL', val: timeplusUrl, set: setTimeplusUrl, ph: 'http://localhost:3218' },
          { label: 'Timeplus Username', val: timeplusUsername, set: setTimeplusUsername, ph: 'proton' },
          { label: 'Timeplus Password', val: timeplusPassword, set: setTimeplusPassword, ph: 'your-password', type: 'password' },
          { label: 'AgentGuard URL', val: agentguardUrl, set: setAgentguardUrl, ph: 'http://localhost:8080' },
        ].map(({ label, val, set, ph, type }) => (
          <div key={label} className="flex items-center gap-4">
            <label className="text-[10px] font-medium text-zinc-500 uppercase tracking-widest w-36 shrink-0">{label}</label>
            <input
              type={type || 'text'}
              value={val}
              onChange={e => set(e.target.value)}
              placeholder={ph}
              className="flex-1 bg-gray-800 border border-gray-600 text-xs py-2 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-[#e7dff0] font-mono"
            />
          </div>
        ))}
        <div className="flex items-center gap-4">
          <label className="text-[10px] font-medium text-zinc-500 uppercase tracking-widest w-36 shrink-0">Holds Enabled</label>
          <label className="flex items-center gap-2 cursor-pointer">
            <input type="checkbox" checked={holdsEnabled} onChange={e => setHoldsEnabled(e.target.checked)} className="accent-[#ffb0cf]" />
            <span className="text-[10px] text-zinc-400">{holdsEnabled ? 'Synchronous pre_tool_call blocking on' : 'Observation-only — no blocking'}</span>
          </label>
        </div>
        <div className="flex items-center gap-4">
          <label className="text-[10px] font-medium text-zinc-500 uppercase tracking-widest w-36 shrink-0">Hold Fail Policy</label>
          <select value={holdFailPolicy} onChange={e => setHoldFailPolicy(e.target.value)} disabled={!holdsEnabled}
            className="flex-1 bg-gray-800 border border-gray-600 text-xs py-2 px-3 rounded-sm focus:border-gray-400 focus:outline-none text-[#e7dff0] font-mono disabled:opacity-50">
            <option value="deny">deny — block on errors</option>
            <option value="allow">allow — pass through on errors</option>
          </select>
        </div>
      </div>
      <p className="text-[10px] text-zinc-500 leading-relaxed">* Agent ID identifies this Hermes instance — use your hostname or machine name.</p>
    </div>
  </>
)}
```

- [ ] **Step 5: Verify frontend compiles**

```bash
cd frontend && npm run build 2>&1 | tail -5
```

Expected: `built in Xs` with no errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/pages/Onboarding.jsx
git commit -m "feat: replace onboarding Step 1 code blocks with install script download button"
```

---

## Task 5: End-to-end verification

- [ ] **Step 1: Start dev stack**

```bash
make dev
```

Expected: backend starts on :8080, frontend on :5173.

- [ ] **Step 2: Open onboarding in browser**

Navigate to `http://localhost:5173/onboarding`.

Verify:
- Step 0 (Select Agent) shows all three agent cards
- Selecting "Claude Code" and clicking Next shows Step 1 with the Download button and "bash agentguard-install.sh" code block
- The "Values to have ready" form fields are present
- Clicking "Download Install Script" downloads `agentguard-install.sh`
- The downloaded file starts with `#!/usr/bin/env bash`

- [ ] **Step 3: Test agent switching**

On Step 1, go back to Step 0 and select "OpenClaw". Verify the Step 1 copy changes to mention `openclaw.json`.  
Repeat for "Hermes" — verify copy mentions shell profile.

- [ ] **Step 4: Run the script locally (Claude Code path)**

```bash
# Dry-run with echo to avoid actual npm install
bash -n agents/install.sh
echo "Syntax OK"
```

Expected: `Syntax OK` (bash syntax check passes).

- [ ] **Step 5: Final commit if any loose changes**

```bash
git status
# if clean: nothing to do
# if any staged: git add -A && git commit -m "chore: cleanup after e2e verification"
```
