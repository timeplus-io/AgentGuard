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

.PHONY: configure unconfigure start build install

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

configure:
	@python3 -c "$$CONFIGURE_SCRIPT"

unconfigure:
	@python3 -c "$$UNCONFIGURE_SCRIPT"

build:
	cd $(PLUGIN_DIR) && npm install && npm run build

install: build configure

start: configure
	AGENTSHIELD_AGENT_ID="$(AGENTSHIELD_AGENT_ID)" \
	AGENTSHIELD_DEPLOYMENT_ID="$(AGENTSHIELD_DEPLOYMENT_ID)" \
	AGENTSHIELD_DEPLOYMENT_NAME="$(AGENTSHIELD_DEPLOYMENT_NAME)" \
	AGENTSHIELD_TIMEPLUS_URL="$(AGENTSHIELD_TIMEPLUS_URL)" \
	AGENTSHIELD_STREAM="$(AGENTSHIELD_STREAM)" \
	AGENTSHIELD_USERNAME="$(AGENTSHIELD_USERNAME)" \
	AGENTSHIELD_PASSWORD="$(AGENTSHIELD_PASSWORD)" \
	claude
