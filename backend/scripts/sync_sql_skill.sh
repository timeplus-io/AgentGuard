#!/usr/bin/env bash
# Refresh the vendored timeplus-sql-guide skill from timeplus-io/AgentSkills.
#
# references/ is synced verbatim from upstream. SKILL.md is NOT synced: the
# local copy is adapted for Sentry's internal run_sql execution channel
# (issue #215) — upstream's curl/REST execution instructions don't apply.
# The script prints a diff against upstream's SKILL.md so knowledge-section
# drift can be folded into the local copy by hand.
set -euo pipefail
dst="$(cd "$(dirname "$0")/.." && pwd)/internal/sentry/skills/knowledge/timeplus-sql-guide"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
git clone --depth 1 https://github.com/timeplus-io/AgentSkills "$tmp/AgentSkills"
src="$tmp/AgentSkills/timeplus-sql-guide"
rm -rf "$dst/references"
mkdir -p "$dst"
cp -R "$src/references" "$dst/references"
find "$dst" -name '.DS_Store' -delete
echo "synced references/ -> $dst"
echo
echo "SKILL.md is locally adapted for run_sql and was NOT overwritten."
echo "Upstream SKILL.md drift (fold knowledge changes in manually):"
diff -u "$dst/SKILL.md" "$src/SKILL.md" || true
