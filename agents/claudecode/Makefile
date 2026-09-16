PWD := $(shell pwd)
PLUGIN_DIR := $(PWD)/agentguard-plugin
SETTINGS_FILE := $(HOME)/.claude/settings.json
AGENTGUARD_AGENT_ID ?= $(shell hostname)
AGENTGUARD_DEPLOYMENT_ID ?= local
AGENTGUARD_DEPLOYMENT_NAME ?= Local Dev
AGENTGUARD_TIMEPLUS_URL ?= http://localhost:3218
AGENTGUARD_DATABASE ?= ag
AGENTGUARD_STREAM ?= agentguard_hook_events
AGENTGUARD_USERNAME ?= proton
AGENTGUARD_PASSWORD ?= timeplus@t+
AGENTGUARD_URL ?= http://localhost:8080
AGENTGUARD_HOLDS_FAIL_POLICY ?= deny
AGENTGUARD_HOLDS_ENABLED ?= false

# OTEL configuration — shares Timeplus OTEL HTTP input on port 4318 with OpenClaw
OTEL_ENDPOINT ?= http://localhost:4318
OTEL_PROTOCOL ?= http/protobuf
OTEL_METRIC_EXPORT_INTERVAL ?= 10000
OTEL_LOGS_EXPORT_INTERVAL ?= 5000

.PHONY: configure unconfigure configure-otel unconfigure-otel start build install

define CONFIGURE_SCRIPT
import json, os, sys

settings_path = os.path.expanduser("$(SETTINGS_FILE)")
hook_command = 'node "$(PLUGIN_DIR)/dist/index.js"'

# PreToolUse must be synchronous so we can block the tool call.
# Other hooks stay async to avoid slowing the agent.
sync_hook_events  = ["PreToolUse"]
async_hook_events = [
    "SessionStart",
    "SessionEnd",
    "UserPromptSubmit",
    "PostToolUse",
    "PostToolUseFailure",
    "SubagentStart",
    "SubagentStop",
    "Stop",
    "PermissionDenied",
]

sync_hook_entry = {
    "matcher": "*",
    "hooks": [
        {
            "type": "command",
            "command": hook_command,
            "timeout": 600000,  # 600s — under Claude Code's hook timeout cap
        }
    ],
}

async_hook_entry = {
    "matcher": "*",
    "hooks": [
        {
            "type": "command",
            "command": hook_command,
            "async": True,
        }
    ],
}

os.makedirs(os.path.dirname(settings_path), exist_ok=True)
if os.path.exists(settings_path):
    with open(settings_path, "r") as f:
        data = json.load(f)
else:
    data = {}

hooks = data.setdefault("hooks", {})

def upsert(event, entry):
    entries = hooks.setdefault(event, [])
    # Remove any existing entry referencing our hook_command (so we replace
    # an old async/sync shape with the new one cleanly).
    entries[:] = [
        e for e in entries
        if not any(h.get("command") == hook_command for h in e.get("hooks", []))
    ]
    entries.append(entry)

for event in sync_hook_events:
    upsert(event, sync_hook_entry)
for event in async_hook_events:
    upsert(event, async_hook_entry)

plugin_env = {
    "AGENTGUARD_AGENT_ID":         "$(AGENTGUARD_AGENT_ID)",
    "AGENTGUARD_DEPLOYMENT_ID":    "$(AGENTGUARD_DEPLOYMENT_ID)",
    "AGENTGUARD_DEPLOYMENT_NAME":  "$(AGENTGUARD_DEPLOYMENT_NAME)",
    "AGENTGUARD_TIMEPLUS_URL":     "$(AGENTGUARD_TIMEPLUS_URL)",
    "AGENTGUARD_DATABASE":         "$(AGENTGUARD_DATABASE)",
    "AGENTGUARD_STREAM":           "$(AGENTGUARD_STREAM)",
    "AGENTGUARD_USERNAME":         "$(AGENTGUARD_USERNAME)",
    "AGENTGUARD_PASSWORD":         "$(AGENTGUARD_PASSWORD)",
    "AGENTGUARD_URL":              "$(AGENTGUARD_URL)",
    "AGENTGUARD_HOLDS_FAIL_POLICY": "$(AGENTGUARD_HOLDS_FAIL_POLICY)",
    "AGENTGUARD_HOLDS_ENABLED":    "$(AGENTGUARD_HOLDS_ENABLED)",
}
data.setdefault("env", {}).update(plugin_env)

with open(settings_path, "w") as f:
    json.dump(data, f, indent=2)
print("AgentGuard hooks configured in", settings_path)
endef
export CONFIGURE_SCRIPT

define UNCONFIGURE_SCRIPT
import json, os

settings_path = os.path.expanduser("$(SETTINGS_FILE)")
hook_command = 'node "$(PLUGIN_DIR)/dist/index.js"'
plugin_env_keys = [
    "AGENTGUARD_AGENT_ID",
    "AGENTGUARD_DEPLOYMENT_ID",
    "AGENTGUARD_DEPLOYMENT_NAME",
    "AGENTGUARD_TIMEPLUS_URL",
    "AGENTGUARD_DATABASE",
    "AGENTGUARD_STREAM",
    "AGENTGUARD_USERNAME",
    "AGENTGUARD_PASSWORD",
    "AGENTGUARD_URL",
    "AGENTGUARD_HOLDS_FAIL_POLICY",
    "AGENTGUARD_HOLDS_ENABLED",
]

hook_events = [
    "SessionStart", "SessionEnd", "UserPromptSubmit", "PreToolUse",
    "PostToolUse", "PostToolUseFailure", "SubagentStart", "SubagentStop",
    "Stop", "PermissionDenied",
]

if not os.path.exists(settings_path):
    print("No settings file found at", settings_path)
    exit(0)

with open(settings_path, "r") as f:
    data = json.load(f)

hooks = data.get("hooks", {})
for event in hook_events:
    if event not in hooks:
        continue
    hooks[event] = [
        e for e in hooks[event]
        if not any(h.get("command") == hook_command for h in e.get("hooks", []))
    ]
    if not hooks[event]:
        del hooks[event]

env = data.get("env", {})
for key in plugin_env_keys:
    env.pop(key, None)
if not env:
    data.pop("env", None)

with open(settings_path, "w") as f:
    json.dump(data, f, indent=2)
print("AgentGuard hooks removed from", settings_path)
endef
export UNCONFIGURE_SCRIPT

define CONFIGURE_OTEL_SCRIPT
import json, os

settings_path = os.path.expanduser("$(SETTINGS_FILE)")
otel_env = {
    "CLAUDE_CODE_ENABLE_TELEMETRY":       "1",
    "OTEL_METRICS_EXPORTER":              "otlp",
    "OTEL_LOGS_EXPORTER":                 "otlp",
    "OTEL_TRACES_EXPORTER":               "otlp",
    "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA": "1",
    "OTEL_EXPORTER_OTLP_PROTOCOL":        "$(OTEL_PROTOCOL)",
    "OTEL_EXPORTER_OTLP_ENDPOINT":        "$(OTEL_ENDPOINT)",
    "OTEL_METRIC_EXPORT_INTERVAL":        "$(OTEL_METRIC_EXPORT_INTERVAL)",
    "OTEL_LOGS_EXPORT_INTERVAL":          "$(OTEL_LOGS_EXPORT_INTERVAL)",
    "OTEL_LOG_TOOL_DETAILS":              "1",
}

os.makedirs(os.path.dirname(settings_path), exist_ok=True)
if os.path.exists(settings_path):
    with open(settings_path, "r") as f:
        data = json.load(f)
else:
    data = {}

data.setdefault("env", {}).update(otel_env)

with open(settings_path, "w") as f:
    json.dump(data, f, indent=2)
print("AgentGuard OTEL configured in", settings_path)
endef
export CONFIGURE_OTEL_SCRIPT

define UNCONFIGURE_OTEL_SCRIPT
import json, os

settings_path = os.path.expanduser("$(SETTINGS_FILE)")
otel_keys = [
    "CLAUDE_CODE_ENABLE_TELEMETRY",
    "OTEL_METRICS_EXPORTER",
    "OTEL_LOGS_EXPORTER",
    "OTEL_TRACES_EXPORTER",
    "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA",
    "OTEL_EXPORTER_OTLP_PROTOCOL",
    "OTEL_EXPORTER_OTLP_ENDPOINT",
    "OTEL_METRIC_EXPORT_INTERVAL",
    "OTEL_LOGS_EXPORT_INTERVAL",
    "OTEL_LOG_TOOL_DETAILS",
]

if not os.path.exists(settings_path):
    print("No settings file found at", settings_path)
    exit(0)

with open(settings_path, "r") as f:
    data = json.load(f)

env = data.get("env", {})
for key in otel_keys:
    env.pop(key, None)
if not env:
    data.pop("env", None)

with open(settings_path, "w") as f:
    json.dump(data, f, indent=2)
print("AgentGuard OTEL removed from", settings_path)
endef
export UNCONFIGURE_OTEL_SCRIPT

configure:
	@python3 -c "$$CONFIGURE_SCRIPT"

unconfigure:
	@python3 -c "$$UNCONFIGURE_SCRIPT"

configure-otel:
	@python3 -c "$$CONFIGURE_OTEL_SCRIPT"

unconfigure-otel:
	@python3 -c "$$UNCONFIGURE_OTEL_SCRIPT"

build:
	cd $(PLUGIN_DIR) && npm install && npm run build

install: build configure configure-otel

start: configure configure-otel
	AGENTGUARD_AGENT_ID="$(AGENTGUARD_AGENT_ID)" \
	AGENTGUARD_DEPLOYMENT_ID="$(AGENTGUARD_DEPLOYMENT_ID)" \
	AGENTGUARD_DEPLOYMENT_NAME="$(AGENTGUARD_DEPLOYMENT_NAME)" \
	AGENTGUARD_TIMEPLUS_URL="$(AGENTGUARD_TIMEPLUS_URL)" \
	AGENTGUARD_DATABASE="$(AGENTGUARD_DATABASE)" \
	AGENTGUARD_STREAM="$(AGENTGUARD_STREAM)" \
	AGENTGUARD_USERNAME="$(AGENTGUARD_USERNAME)" \
	AGENTGUARD_PASSWORD="$(AGENTGUARD_PASSWORD)" \
	claude
