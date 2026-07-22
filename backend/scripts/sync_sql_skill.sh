#!/usr/bin/env bash
# Refresh the vendored timeplus-sql-guide skill from timeplus-io/AgentSkills.
set -euo pipefail
dst="$(cd "$(dirname "$0")/.." && pwd)/internal/sentry/skills/knowledge/timeplus-sql-guide"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
git clone --depth 1 https://github.com/timeplus-io/AgentSkills "$tmp/AgentSkills"
rm -rf "$dst"
mkdir -p "$dst"
cp -R "$tmp/AgentSkills/timeplus-sql-guide/." "$dst/"
find "$dst" -name '.DS_Store' -delete
echo "synced -> $dst"
