PWD := $(shell pwd)
PLUGIN_DIR := $(PWD)/agentshield-plugin
SETTINGS_FILE := $(HOME)/.claude/settings.json
AGENTSHIELD_AGENT_ID ?= $(shell hostname)
AGENTSHIELD_DEPLOYMENT_ID ?= local
AGENTSHIELD_DEPLOYMENT_NAME ?= Local Dev
AGENTSHIELD_TIMEPLUS_URL ?= http://localhost:3218
AGENTSHIELD_STREAM ?= agentshield_hook_events
AGENTSHIELD_USERNAME ?= proton
AGENTSHIELD_PASSWORD ?= timeplus@t+

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

hook_events = [
    "SessionStart",
    "SessionEnd",
    "UserPromptSubmit",
    "PreToolUse",
    "PostToolUse",
    "PostToolUseFailure",
    "SubagentStart",
    "SubagentStop",
    "Stop",
    "PermissionDenied",
]

hook_entry = {
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

for event in hook_events:
    entries = hooks.setdefault(event, [])
    # Dedup: skip if an entry with this exact command already exists
    already = any(
        h.get("command") == hook_command
        for e in entries
        for h in e.get("hooks", [])
    )
    if not already:
        entries.append(hook_entry)

with open(settings_path, "w") as f:
    json.dump(data, f, indent=2)
print("AgentShield hooks configured in", settings_path)
endef
export CONFIGURE_SCRIPT

define UNCONFIGURE_SCRIPT
import json, os

settings_path = os.path.expanduser("$(SETTINGS_FILE)")
hook_command = 'node "$(PLUGIN_DIR)/dist/index.js"'

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

with open(settings_path, "w") as f:
    json.dump(data, f, indent=2)
print("AgentShield hooks removed from", settings_path)
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
print("AgentShield OTEL configured in", settings_path)
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
print("AgentShield OTEL removed from", settings_path)
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
	AGENTSHIELD_AGENT_ID="$(AGENTSHIELD_AGENT_ID)" \
	AGENTSHIELD_DEPLOYMENT_ID="$(AGENTSHIELD_DEPLOYMENT_ID)" \
	AGENTSHIELD_DEPLOYMENT_NAME="$(AGENTSHIELD_DEPLOYMENT_NAME)" \
	AGENTSHIELD_TIMEPLUS_URL="$(AGENTSHIELD_TIMEPLUS_URL)" \
	AGENTSHIELD_STREAM="$(AGENTSHIELD_STREAM)" \
	AGENTSHIELD_USERNAME="$(AGENTSHIELD_USERNAME)" \
	AGENTSHIELD_PASSWORD="$(AGENTSHIELD_PASSWORD)" \
	claude
