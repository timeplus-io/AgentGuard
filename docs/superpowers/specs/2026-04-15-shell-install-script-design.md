# Design: AgentGuard Shell Installation Script

**Date:** 2026-04-15  
**Status:** Approved

## Overview

A single POSIX shell script (`install/install.sh`) that downloads and installs the AgentGuard binary from S3, optionally sets it up as a background service, and is itself published to S3 on every release so users can install with a one-liner.

## User-Facing Install Command

```bash
# Install latest
curl -fsSL https://agentguard.s3.us-west-1.amazonaws.com/install.sh | sh

# Install specific version
curl -fsSL https://agentguard.s3.us-west-1.amazonaws.com/install.sh | sh -s -- --version v1.2.3
```

## Script Location

- **In repo:** `install/install.sh`
- **Published to S3:** `s3://agentguard/install.sh` on every `v*` tag push
- **Public URL:** `https://agentguard.s3.us-west-1.amazonaws.com/install.sh`

## Supported Platforms

| OS    | Arch  | Binary name                |
|-------|-------|----------------------------|
| macOS | arm64 | agentguard-darwin-arm64    |
| macOS | amd64 | agentguard-darwin-amd64    |
| Linux | arm64 | agentguard-linux-arm64     |
| Linux | amd64 | agentguard-linux-amd64     |

Script exits with a clear error on any other OS or architecture.

## Script Flow

### 1. Parse arguments
- Accept `--version <tag>` flag (e.g. `--version v1.2.3`)
- Default to `latest` if not provided

### 2. Detect OS and architecture
- `uname -s` → `Darwin` → `darwin`, `Linux` → `linux`; anything else → unsupported error
- `uname -m` → `x86_64` → `amd64`, `arm64`/`aarch64` → `arm64`; anything else → unsupported error

### 3. Build download URL
```
https://agentguard.s3.us-west-1.amazonaws.com/agentguard/<version>/agentguard-<os>-<arch>
```

### 4. Download binary
- Use `curl -fsSL` if available
- Fall back to `wget -qO-` if curl is not found
- If neither is available, exit with an error message

### 5. Install binary
- Write to a temp file, `chmod +x`
- Target: `/usr/local/bin/agentguard`
- If `/usr/local/bin` is not writable by current user, re-run the move with `sudo`

### 6. Verify installation
- Run `agentguard --version` to confirm the binary executes correctly
- Exit with error if it fails

### 7. Configure Timeplus connection
- If non-interactive (piped stdin), skip and print a note that the user should edit `~/.agentguard/config.yaml` manually
- Otherwise prompt for each field in order of importance. Each prompt shows the field name, a one-line explanation of why it is needed, and the default value in brackets. Pressing Enter accepts the default.

  **Timeplus connection (required for AgentGuard to function):**

  1. **Timeplus host** `[localhost]`  
     _The hostname or IP where Timeplus Enterprise is running. AgentGuard streams all agent events into Timeplus Enterprise and queries it for metrics, rules, and security events._

  2. **Timeplus HTTP port** `[3218]`  
     _Timeplus Enterprise HTTP API port. Used for SELECT queries and DDL (creating streams and materialized views)._

  3. **Timeplus native TCP port** `[8463]`  
     _Timeplus Enterprise native binary protocol port. Used for high-throughput event ingestion from agent plugins._

  4. **Timeplus user** `[default]`  
     _Username for authenticating with Timeplus Enterprise. Use the default unless your instance has custom access control enabled._

  5. **Timeplus password** `[]` _(hidden)_  
     _Password for the Timeplus user. Leave empty if your Timeplus Enterprise instance has no password set._

  **AgentGuard server:**

  6. **AgentGuard server port** `[8080]`  
     _Port AgentGuard listens on. Change this if 8080 is already in use on your machine._

  7. **AgentGuard base URL** `[http://localhost:8080]`  
     _The URL Timeplus Enterprise uses to call back AgentGuard when a security rule fires. Must be reachable from Timeplus Enterprise — change this if they run on different hosts (e.g. `http://agentguard:8080` in Docker)._
- Create `~/.agentguard/` directory if it does not exist (`mkdir -p`)
- Write `~/.agentguard/config.yaml` with all fields:

```yaml
server:
  port: <user input>

agentguard:
  url: <user input>

timeplus:
  host: <user input>
  port: <user input>
  native-port: <user input>
  user: <user input>
  password: <user input>
```

- If `~/.agentguard/config.yaml` already exists, ask: `Config file already exists. Overwrite? [y/N]` — skip on N

### 8. Optional service setup (interactive prompt)
- Print: `Install AgentGuard as a background service? [y/N]`
- If non-interactive (piped stdin), skip and print manual start instructions
- **macOS:** Write `~/Library/LaunchAgents/com.timeplus.agentguard.plist`, run `launchctl load`
- **Linux:** Write `/etc/systemd/system/agentguard.service` (requires sudo), run `sudo systemctl enable --now agentguard`

### 9. Print success summary
- Installed path and version
- Config file location (`~/.agentguard/config.yaml`)
- How to start/stop manually
- If service installed: how to check status

## Service Definitions

### macOS launchd plist (`com.timeplus.agentguard.plist`)
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.timeplus.agentguard</string>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/local/bin/agentguard</string>
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
```

### Linux systemd unit (`agentguard.service`)
```ini
[Unit]
Description=AgentGuard security monitoring
After=network.target

[Service]
ExecStart=/usr/local/bin/agentguard
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

## Error Handling

| Condition | Behaviour |
|-----------|-----------|
| Unsupported OS | Print error, exit 1 |
| Unsupported arch | Print error, exit 1 |
| curl and wget both missing | Print error, exit 1 |
| Download fails (non-200) | Print error with URL, exit 1 |
| Binary fails `--version` check | Print error, remove temp file, exit 1 |
| `/usr/local/bin` not writable, sudo fails | Print error, exit 1 |
| Config dir not creatable | Print warning, skip config write, continue |

## CI Change

Add one step to the `upload-s3` job in `.github/workflows/release.yml` (after the binary uploads):

```yaml
- name: Upload install script to S3
  run: |
    aws s3 cp install/install.sh "s3://${{ secrets.AWS_S3_BUCKET }}/install.sh"
    echo "Uploaded install.sh"
```

## What Is Not In Scope

- Homebrew formula (future)
- RPM package (future)
- Windows support
- Uninstall script
- Automatic updates
