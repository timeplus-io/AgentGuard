#!/usr/bin/env bash
#
# Semantic DLP end-to-end integration test (spec §9).
#
# Drives the full shipped surface against a LIVE Timeplus instance and a
# deterministic mock model endpoint:
#
#   mock model server  →  build + run AgentGuard backend  →  POST /setup/provision
#   →  insert sample hook events  →  enrichment MV pipeline  →  assert enriched rows
#   →  POST /api/dlp/query round-trip  →  /api/dlp/messages + /api/dlp/profiles
#
# Requires: a reachable Timeplus (e.g. `make compose-up`), Go, python3, curl.
# No GitHub Actions wiring — run locally or in a job that provides Timeplus.
#
# Configuration (env, with sensible defaults for the docker-compose stack):
#   TIMEPLUS_HOST         localhost
#   TIMEPLUS_USER         proton
#   TIMEPLUS_PASSWORD     timeplus@t+
#   TIMEPLUS_HTTP_PORT    8123    (ClickHouse-compatible HTTP, used by the loader)
#   TIMEPLUS_PORT         3218    (Timeplus HTTP query port, used by the backend)
#   TIMEPLUS_NATIVE_PORT  8463    (native TCP, used by the backend for provisioning)
#   MOCK_PORT             4000    (host port the mock binds)
#   EMBED_DIM             8       (mock embedding dimension; must match dlp.embedding_dim)
#   BACKEND_PORT          18080   (test backend port)
#   DLP_BASE_URL          http://host.docker.internal:MOCK_PORT/v1
#                         ^ URL the Timeplus UDFs use to reach the mock. The default
#                           works when Timeplus runs in Docker; for a NATIVE Timeplus
#                           on this host set DLP_BASE_URL=http://localhost:MOCK_PORT/v1
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
BACKEND_PORT="${BACKEND_PORT:-18080}"
DLP_BASE_URL="${DLP_BASE_URL:-http://host.docker.internal:${MOCK_PORT}/v1}"

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
echo "── Semantic DLP integration test ──────────────────────────────────────────"
info "Timeplus ${TIMEPLUS_HOST} (http:${TIMEPLUS_HTTP_PORT} query:${TIMEPLUS_PORT} native:${TIMEPLUS_NATIVE_PORT}), mock :${MOCK_PORT}, backend :${BACKEND_PORT}"
if [ "$(echo 'SELECT 1' | tp_http)" != "1" ]; then
  fail "Timeplus is not reachable on ${TIMEPLUS_HOST}:${TIMEPLUS_HTTP_PORT} — start it (e.g. \`make compose-up\`) and retry."
  exit 1
fi
pass "Timeplus reachable"

# ── 1. Mock model server ─────────────────────────────────────────────────────
python3 "$SCRIPT_DIR/dlp_mock_model.py" "$MOCK_PORT" "$EMBED_DIM" >"$WORKDIR/mock.log" 2>&1 &
MOCK_PID=$!
disown "$MOCK_PID" 2>/dev/null || true
for _ in $(seq 1 20); do
  curl -sf -m 2 "http://localhost:${MOCK_PORT}/health" >/dev/null 2>&1 && break
  sleep 0.25
done
curl -sf -m 2 "http://localhost:${MOCK_PORT}/models" >/dev/null 2>&1 \
  && pass "mock model server up (dim=${EMBED_DIM})" \
  || { fail "mock model server did not start (see $WORKDIR/mock.log)"; exit 1; }

# ── 2. Build + run the AgentGuard backend ────────────────────────────────────
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
  enabled: true
  base_url: ${DLP_BASE_URL}
  embedding_model: mock-embed
  embedding_dim: ${EMBED_DIM}
  risk_model: mock-risk
  summary_model: mock-summary
YAML

( cd "$REPO_DIR/backend" \
  && TIMEPLUS_USER="$TIMEPLUS_USER" TIMEPLUS_PASSWORD="$TIMEPLUS_PASSWORD" GIN_MODE=release \
     "$WORKDIR/server" --config "$WORKDIR/config.yaml" >"$WORKDIR/backend.log" 2>&1 ) &
BACKEND_PID=$!
disown "$BACKEND_PID" 2>/dev/null || true
for _ in $(seq 1 40); do
  curl -sf -m 2 "${BACKEND_URL}/api/auth/status" >/dev/null 2>&1 && break
  sleep 0.25
done
curl -sf -m 2 "${BACKEND_URL}/api/auth/status" >/dev/null 2>&1 \
  && pass "backend up on ${BACKEND_URL}" \
  || { fail "backend did not start (see $WORKDIR/backend.log)"; cat "$WORKDIR/backend.log"; exit 1; }

# ── 3. Provision (creates streams, CIM MVs, DLP stream/UDFs/MV) ───────────────
info "provisioning…"
prov="$(curl -s -m 120 -X POST "${BACKEND_URL}/api/setup/provision" \
  -H 'Content-Type: application/json' \
  -d "{\"host\":\"${TIMEPLUS_HOST}\",\"port\":${TIMEPLUS_PORT},\"native_port\":${TIMEPLUS_NATIVE_PORT},\"username\":\"${TIMEPLUS_USER}\",\"password\":\"${TIMEPLUS_PASSWORD}\"}" || true)"
if echo "$prov" | grep -q '"status":"error"'; then
  fail "provisioning reported an error:"; echo "$prov" | grep '"status":"error"'
  exit 1
fi
pass "provisioned (mv_dlp_enrich + UDFs point at ${DLP_BASE_URL})"

# ── 4. Login (DLP endpoints are behind auth) ─────────────────────────────────
curl -s -m 10 -c "$COOKIES" -X POST "${BACKEND_URL}/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"${TIMEPLUS_USER}\",\"password\":\"${TIMEPLUS_PASSWORD}\"}" >/dev/null
grep -q agentguard_session "$COOKIES" \
  && pass "authenticated as ${TIMEPLUS_USER}" \
  || { fail "login failed"; exit 1; }

# ── 5. Insert sample hook events ─────────────────────────────────────────────
TIMEPLUS_HOST="$TIMEPLUS_HOST" TIMEPLUS_USER="$TIMEPLUS_USER" TIMEPLUS_PASSWORD="$TIMEPLUS_PASSWORD" \
  python3 "$SCRIPT_DIR/dlp_load_events.py" --port "$TIMEPLUS_HTTP_PORT" >/dev/null
pass "inserted sample hook events"

# ── 6. Wait for enrichment, assert enriched rows ─────────────────────────────
info "waiting for the enrichment pipeline (hook_events → cim_event → dlp_enriched)…"
status=""
for _ in $(seq 1 40); do
  status="$(curl -s -m 10 -b "$COOKIES" "${BACKEND_URL}/api/dlp/status")"
  embedded="$(echo "$status" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("embedded_rows",0))' 2>/dev/null || echo 0)"
  [ "${embedded:-0}" -gt 0 ] && break
  sleep 1
done
python3 - "$status" <<'PY' || FAILED=1
import sys, json
s = json.loads(sys.argv[1] or "{}")
ok = True
def check(cond, label):
    global ok
    print(("\033[32m✓\033[0m " if cond else "\033[31m✗\033[0m ") + label)
    ok = ok and cond
check(s.get("stream_exists"), "agentguard_dlp_enriched exists")
check(s.get("mv_exists"), "mv_dlp_enrich exists")
udfs = s.get("udfs", {})
check(all(udfs.values()) and len(udfs) == 6, f"all 6 DLP UDFs present ({sum(bool(v) for v in udfs.values())}/6)")
check(s.get("total_rows", 0) > 0, f"enriched rows present (total={s.get('total_rows')})")
check(s.get("embedded_rows", 0) > 0, f"embeddings generated (embedded={s.get('embedded_rows')})")
check(s.get("flagged_rows", 0) > 0, f"risk flagged (flagged={s.get('flagged_rows')})")
sys.exit(0 if ok else 1)
PY

# Entity-extraction spot check, directly over the stream.
ent="$(echo "SELECT count() FROM table(agentguard_dlp_enriched) WHERE not empty(entities['phone']) OR not empty(entities['email']) OR not empty(entities['secret'])" | tp_http)"
[ "${ent:-0}" -gt 0 ] && pass "entities extracted (phone/email/secret rows: ${ent})" || fail "no entity rows found"

# ── 7. Query round-trip (the NDJSON forensic funnel) ─────────────────────────
info "running query round-trip…"
curl -s -m 120 -b "$COOKIES" -X POST "${BACKEND_URL}/api/dlp/query" \
  -H 'Content-Type: application/json' \
  -d '{"question":"Who sent customer contact data to an external model?"}' >"$WORKDIR/query.ndjson"
python3 - "$WORKDIR/query.ndjson" <<'PY' || FAILED=1
import sys, json
stages, answer = [], None
for line in open(sys.argv[1]):
    line = line.strip()
    if not line:
        continue
    obj = json.loads(line)
    stages.append(obj.get("stage"))
    if obj.get("stage") == "answer":
        answer = obj
    if obj.get("stage") == "error":
        print("\033[31m✗\033[0m query streamed an error:", obj.get("error")); sys.exit(1)
ok = True
def check(cond, label):
    global ok
    print(("\033[32m✓\033[0m " if cond else "\033[31m✗\033[0m ") + label); ok = ok and cond
for st in ("intent", "candidates", "reranked", "answer"):
    check(st in stages, f"stage '{st}' streamed")
check(bool(answer and answer.get("report")), "answer carries a report")
ev = (answer or {}).get("evidence") or []
check(len(ev) >= 1, f"answer cites evidence ({len(ev)} message(s))")
check(any(e.get("risk_tags") for e in ev), "at least one cited message is risk-tagged")
sys.exit(0 if ok else 1)
PY

# ── 8. Browser + profiles APIs ───────────────────────────────────────────────
msgs="$(curl -s -m 15 -b "$COOKIES" "${BACKEND_URL}/api/dlp/messages?risk_tag=PII&limit=100")"
mtotal="$(echo "$msgs" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("total",0))' 2>/dev/null || echo 0)"
[ "${mtotal:-0}" -gt 0 ] && pass "GET /api/dlp/messages?risk_tag=PII → ${mtotal} rows" || fail "messages browser returned no PII rows"

prof="$(curl -s -m 15 -b "$COOKIES" "${BACKEND_URL}/api/dlp/profiles?days=30")"
pagents="$(echo "$prof" | python3 -c 'import sys,json;print(len(json.load(sys.stdin).get("top_agents",[])))' 2>/dev/null || echo 0)"
[ "${pagents:-0}" -gt 0 ] && pass "GET /api/dlp/profiles → ${pagents} top agent(s)" || fail "profiles returned no agents"

# ── Summary ──────────────────────────────────────────────────────────────────
echo "───────────────────────────────────────────────────────────────────────────"
if [ "$FAILED" -eq 0 ]; then
  echo "${c_green}PASS${c_off} — Semantic DLP end-to-end integration succeeded."
  exit 0
fi
echo "${c_red}FAIL${c_off} — one or more checks failed (logs in backend.log/mock.log were under $WORKDIR)."
exit 1
