#!/usr/bin/env bash
#
# Seed demo data for the Semantic DLP (/dlp) page.
#
# Unlike dlp_integration.sh, this does NOT provision or run the backend — it
# assumes a Timeplus instance that is already provisioned (streams + CIM MVs +
# mv_dlp_enrich + UDFs exist) and that a model endpoint is configured so the
# enrichment UDFs can populate entities / risk / embeddings. Use it to populate
# a running stack for a demo or manual UI walkthrough.
#
# If the stack is not provisioned yet, run the setup wizard once (or
# `make dlp-integration`, which provisions from scratch) before seeding.
#
# Configuration (env, defaults match the docker-compose stack):
#   TIMEPLUS_HOST       localhost
#   TIMEPLUS_USER       proton
#   TIMEPLUS_PASSWORD   timeplus@t+
#   TIMEPLUS_HTTP_PORT  8123     (ClickHouse-compatible HTTP)
#   REPEAT              1        (insert the dataset N times for more volume)
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

TIMEPLUS_HOST="${TIMEPLUS_HOST:-localhost}"
TIMEPLUS_USER="${TIMEPLUS_USER:-proton}"
TIMEPLUS_PASSWORD="${TIMEPLUS_PASSWORD:-timeplus@t+}"
TIMEPLUS_HTTP_PORT="${TIMEPLUS_HTTP_PORT:-8123}"
REPEAT="${REPEAT:-1}"

tp_http() {
  curl -s -m 20 "http://${TIMEPLUS_HOST}:${TIMEPLUS_HTTP_PORT}/" \
    -u "${TIMEPLUS_USER}:${TIMEPLUS_PASSWORD}" --data-binary @-
}

echo "── Seeding Semantic DLP demo data ─────────────────────────────────────────"

if [ "$(echo 'SELECT 1' | tp_http)" != "1" ]; then
  echo "✗ Timeplus not reachable on ${TIMEPLUS_HOST}:${TIMEPLUS_HTTP_PORT}; start it first." >&2
  exit 1
fi

exists="$(echo "SELECT count() FROM system.tables WHERE name = 'agentguard_dlp_enriched' AND database = current_database()" | tp_http || echo 0)"
if [ "${exists:-0}" -lt 1 ]; then
  echo "✗ agentguard_dlp_enriched does not exist — the stack is not provisioned." >&2
  echo "  Run the setup wizard (POST /api/setup/provision) or 'make dlp-integration' first." >&2
  exit 1
fi

before="$(echo 'SELECT count() FROM table(agentguard_dlp_enriched)' | tp_http || echo 0)"

TIMEPLUS_HOST="$TIMEPLUS_HOST" TIMEPLUS_USER="$TIMEPLUS_USER" TIMEPLUS_PASSWORD="$TIMEPLUS_PASSWORD" \
  python3 "$SCRIPT_DIR/dlp_load_events.py" --port "$TIMEPLUS_HTTP_PORT" --repeat "$REPEAT"

echo "· waiting for enrichment (hook_events → cim_event → dlp_enriched)…"
for _ in $(seq 1 40); do
  after="$(echo 'SELECT count() FROM table(agentguard_dlp_enriched)' | tp_http || echo 0)"
  if [ "${after:-0}" -gt "${before:-0}" ]; then break; fi
  sleep 1
done

echo "· enriched-row summary (by role):"
echo "SELECT role, count() AS rows, count_if(risk_score > 0) AS flagged, count_if(length(embedding) > 0) AS embedded
      FROM table(agentguard_dlp_enriched) GROUP BY role ORDER BY role" | \
  curl -s -m 20 "http://${TIMEPLUS_HOST}:${TIMEPLUS_HTTP_PORT}/?default_format=PrettyCompact" \
    -u "${TIMEPLUS_USER}:${TIMEPLUS_PASSWORD}" --data-binary @-

echo "✓ Seed complete — open /dlp to explore. Try: \"Who sent customer contact data to an external model?\""
