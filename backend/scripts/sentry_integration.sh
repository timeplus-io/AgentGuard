#!/usr/bin/env bash
#
# Sentry agent end-to-end integration test (spec docs/superpowers/specs/2026-07-22-sentry-agent-design.md).
#
# Drives the full Phase-1 chat-core surface against a LIVE Timeplus instance
# and a deterministic mock OpenAI-compatible model endpoint:
#
#   mock model server  →  build + run AgentGuard backend  →  login  →  POST /setup/provision
#   →  point the shared DLP/Sentry config catalog at the mock  →  POST /api/sentry/chat
#      with a forced tool call  →  assert the NDJSON stream carries tool_call + done + MOCK_ANSWER
#   →  replay the conversation via GET /api/sentry/conversations/:id and assert a role="tool" row
#
# Mirrors backend/scripts/dlp_integration.sh's harness conventions (env
# handling, login/provision payloads, server build+launch, cleanup traps).
#
# Requires: a reachable Timeplus (e.g. `make compose-up`), Go, python3, curl.
# No GitHub Actions wiring — run locally or in a job that provides Timeplus.
#
# Configuration (env, with sensible defaults for the docker-compose stack):
#   TIMEPLUS_HOST         localhost
#   TIMEPLUS_USER         proton
#   TIMEPLUS_PASSWORD     timeplus@t+
#   TIMEPLUS_PORT         3218    (Timeplus HTTP query port, used by the backend)
#   TIMEPLUS_NATIVE_PORT  8463    (native TCP, used by the backend for provisioning)
#   MOCK_PORT             4000    (host port the mock binds)
#   EMBED_DIM             8       (mock embedding dimension; unused by Sentry but shared with the DLP mock)
#   BACKEND_PORT          18081   (test backend port; distinct from dlp_integration.sh's 18080
#                                  so both scripts can run concurrently)
#   SENTRY_BASE_URL       http://host.docker.internal:MOCK_PORT/v1
#                         ^ URL the Timeplus UDFs/Sentry chat loop use to reach the mock. The
#                           default works when Timeplus runs in Docker; for a NATIVE Timeplus
#                           on this host set SENTRY_BASE_URL=http://localhost:MOCK_PORT/v1
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

TIMEPLUS_HOST="${TIMEPLUS_HOST:-localhost}"
TIMEPLUS_USER="${TIMEPLUS_USER:-proton}"
TIMEPLUS_PASSWORD="${TIMEPLUS_PASSWORD:-timeplus@t+}"
TIMEPLUS_HTTP_PORT="${TIMEPLUS_HTTP_PORT:-8123}"
TIMEPLUS_PORT="${TIMEPLUS_PORT:-3218}"
TIMEPLUS_NATIVE_PORT="${TIMEPLUS_NATIVE_PORT:-8463}"
MOCK_PORT="${MOCK_PORT:-4000}"
EMBED_DIM="${EMBED_DIM:-8}"
BACKEND_PORT="${BACKEND_PORT:-18081}"
SENTRY_BASE_URL="${SENTRY_BASE_URL:-http://host.docker.internal:${MOCK_PORT}/v1}"

BACKEND_URL="http://localhost:${BACKEND_PORT}"
WORKDIR="$(mktemp -d)"
COOKIES="$WORKDIR/cookies.txt"
MOCK_PID=""
BACKEND_PID=""
FAILED=0

c_green=$'\033[32m'; c_red=$'\033[31m'; c_dim=$'\033[2m'; c_off=$'\033[0m'
info()  { echo "${c_dim}·${c_off} $*"; }
pass()  { echo "${c_green}✓${c_off} $*"; }
fail()  { echo "${c_red}✗${c_off} $*"; FAILED=1; }

cleanup() {
  [ -n "$BACKEND_PID" ] && kill "$BACKEND_PID" 2>/dev/null || true
  [ -n "$MOCK_PID" ] && kill "$MOCK_PID" 2>/dev/null || true
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

tp_http() {  # run SQL over the ClickHouse HTTP interface, echo the response
  curl -s -m 20 "http://${TIMEPLUS_HOST}:${TIMEPLUS_HTTP_PORT}/" \
    -u "${TIMEPLUS_USER}:${TIMEPLUS_PASSWORD}" --data-binary @-
}

# ── 0. Preconditions ─────────────────────────────────────────────────────────
echo "── Sentry agent integration test ──────────────────────────────────────────"
info "Timeplus ${TIMEPLUS_HOST} (http:${TIMEPLUS_HTTP_PORT} query:${TIMEPLUS_PORT} native:${TIMEPLUS_NATIVE_PORT}), mock :${MOCK_PORT}, backend :${BACKEND_PORT}"
if [ "$(echo 'SELECT 1' | tp_http)" != "1" ]; then
  fail "Timeplus is not reachable on ${TIMEPLUS_HOST}:${TIMEPLUS_HTTP_PORT} — start it (e.g. \`make compose-up\`) and retry."
  exit 1
fi
pass "Timeplus reachable"

# ── 1. Mock model server (dlp_mock_model.py; Task 14 added streaming tool-calls) ─
python3 "$SCRIPT_DIR/dlp_mock_model.py" "$MOCK_PORT" "$EMBED_DIM" >"$WORKDIR/mock.log" 2>&1 &
MOCK_PID=$!
disown "$MOCK_PID" 2>/dev/null || true
for _ in $(seq 1 20); do
  curl -sf -m 2 "http://localhost:${MOCK_PORT}/health" >/dev/null 2>&1 && break
  sleep 0.25
done
curl -sf -m 2 "http://localhost:${MOCK_PORT}/models" >/dev/null 2>&1 \
  && pass "mock model server up" \
  || { fail "mock model server did not start (see $WORKDIR/mock.log)"; exit 1; }

# ── 2. Build + run the AgentGuard backend (sentry.enabled: true, dlp.enabled: false) ─
info "building backend (CGO_ENABLED=0)…"
( cd "$REPO_DIR/backend" && CGO_ENABLED=0 go build -o "$WORKDIR/server" cmd/server/main.go )
pass "backend built"

cat >"$WORKDIR/config.yaml" <<YAML
server:
  port: ${BACKEND_PORT}
timeplus:
  host: ${TIMEPLUS_HOST}
  port: ${TIMEPLUS_PORT}
  native-port: ${TIMEPLUS_NATIVE_PORT}
setup:
  complete: false
dlp:
  enabled: false
sentry:
  enabled: true
YAML

( cd "$REPO_DIR/backend" \
  && TIMEPLUS_USER="$TIMEPLUS_USER" TIMEPLUS_PASSWORD="$TIMEPLUS_PASSWORD" GIN_MODE=release \
     exec "$WORKDIR/server" --config "$WORKDIR/config.yaml" >"$WORKDIR/backend.log" 2>&1 ) &
BACKEND_PID=$!
disown "$BACKEND_PID" 2>/dev/null || true
for _ in $(seq 1 40); do
  curl -sf -m 2 "${BACKEND_URL}/api/auth/status" >/dev/null 2>&1 && break
  sleep 0.25
done
curl -sf -m 2 "${BACKEND_URL}/api/auth/status" >/dev/null 2>&1 \
  && pass "backend up on ${BACKEND_URL}" \
  || { fail "backend did not start (see $WORKDIR/backend.log)"; cat "$WORKDIR/backend.log"; exit 1; }

# ── 3. Login (Sentry endpoints are behind auth) ──────────────────────────────
curl -s -m 10 -c "$COOKIES" -X POST "${BACKEND_URL}/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"${TIMEPLUS_USER}\",\"password\":\"${TIMEPLUS_PASSWORD}\"}" >/dev/null
grep -q agentguard_session "$COOKIES" \
  && pass "authenticated as ${TIMEPLUS_USER}" \
  || { fail "login failed"; exit 1; }

# ── 4. Provision (creates streams, CIM MVs, Sentry conversation streams + config seed) ─
info "provisioning…"
prov="$(curl -s -m 120 -b "$COOKIES" -X POST "${BACKEND_URL}/api/setup/provision" \
  -H 'Content-Type: application/json' \
  -d "{\"host\":\"${TIMEPLUS_HOST}\",\"port\":${TIMEPLUS_PORT},\"native_port\":${TIMEPLUS_NATIVE_PORT},\"username\":\"${TIMEPLUS_USER}\",\"password\":\"${TIMEPLUS_PASSWORD}\"}" || true)"
if echo "$prov" | grep -q '"status":"error"'; then
  fail "provisioning reported an error:"; echo "$prov" | grep '"status":"error"'
  exit 1
fi
pass "provisioned (agentguard_sentry_conversations/messages + sentry_chat config seed)"

# ── 5. Point the shared config catalog at the mock model ────────────────────
info "pointing the shared DLP/Sentry config catalog at the mock…"
shared_cfg=$(cat <<JSON
{"value":{"base_url":"http://localhost:${MOCK_PORT}/v1","api_key":"x","models":{"chat":{"model":"mock-chat"},"embedding":{"model":"mock-embed"}}}}
JSON
)
curl -sf -m 10 -b "$COOKIES" -X PUT "${BACKEND_URL}/api/dlp/config/shared" \
  -H 'Content-Type: application/json' -d "$shared_cfg" >/dev/null \
  && pass "shared config points at mock (${BACKEND_URL} -> localhost:${MOCK_PORT})" \
  || { fail "failed to update shared config"; exit 1; }

# ── 6. Chat round-trip: force a tool call, assert the NDJSON stream ─────────
info "running chat round-trip with a forced tool call…"
OUT="$(curl -sf -m 60 -b "$COOKIES" -X POST "${BACKEND_URL}/api/sentry/chat" \
  -H 'Content-Type: application/json' \
  -d '{"message":"MOCK_TOOL_CALL {\"name\":\"list_streams\",\"arguments\":{}}"}')"
echo "$OUT" >"$WORKDIR/chat.ndjson"
python3 - "$WORKDIR/chat.ndjson" <<'PY' || FAILED=1
import sys, json
events = []
for line in open(sys.argv[1]):
    line = line.strip()
    if not line:
        continue
    events.append(json.loads(line))
types = [e.get("event") for e in events]
ok = True
def check(cond, label):
    global ok
    print(("\033[32m✓\033[0m " if cond else "\033[31m✗\033[0m ") + label)
    ok = ok and cond
check("meta" in types, "meta event streamed")
tool_calls = [e for e in events if e.get("event") == "tool_call"]
check(any(e.get("name") == "list_streams" for e in tool_calls), "tool_call event for list_streams")
check(any(e.get("event") == "tool_result" for e in events), "tool_result event streamed")
check("done" in types, "done event streamed")
check(any("MOCK_ANSWER" in (e.get("text") or "") for e in events), "MOCK_ANSWER token streamed")
conv_ids = {e.get("conversation_id") for e in events if e.get("conversation_id")}
check(len(conv_ids) == 1, f"single conversation_id in stream ({conv_ids})")
if conv_ids:
    with open(sys.argv[1] + ".cid", "w") as f:
        f.write(next(iter(conv_ids)))
sys.exit(0 if ok else 1)
PY
CID="$(cat "$WORKDIR/chat.ndjson.cid" 2>/dev/null || true)"
[ -n "$CID" ] || { fail "could not extract conversation_id from chat stream"; cat "$WORKDIR/chat.ndjson"; exit 1; }

# ── 7. Conversation replay round-trip ────────────────────────────────────────
# The chat turn's messages are written via native-batch inserts into an
# append-only stream; table(...) reads can lag the write by a beat, so poll
# briefly (mirrors dlp_integration.sh §6's enrichment-pipeline wait) instead
# of asserting on a single immediate read.
info "replaying conversation ${CID}…"
replay=""
for _ in $(seq 1 20); do
  replay="$(curl -sf -m 15 -b "$COOKIES" "${BACKEND_URL}/api/sentry/conversations/${CID}")"
  echo "$replay" | grep -q '"role":"tool"' && break
  sleep 0.5
done
echo "$replay" | grep -q '"role":"tool"' \
  && pass "replay includes a role=\"tool\" message" \
  || { fail "replay missing role=\"tool\" message"; echo "$replay"; exit 1; }

# ── Summary ──────────────────────────────────────────────────────────────────
echo "───────────────────────────────────────────────────────────────────────────"
if [ "$FAILED" -eq 0 ]; then
  echo "${c_green}PASS${c_off} — sentry integration: OK"
  exit 0
fi
echo "${c_red}FAIL${c_off} — one or more checks failed (logs in backend.log/mock.log were under $WORKDIR)."
exit 1
