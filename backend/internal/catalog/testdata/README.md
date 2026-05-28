# Live MV test fixtures

Test data for validating catalog rules against a running Timeplus Enterprise
instance. The Go test suite (`go test ./internal/catalog/...`) does **not**
consume these files yet — they are committed as a manual repro runbook so
SQL-shape edits to a rule can be quickly validated end-to-end.

## rp-013 (Anomalous LLM Output Token Spike)

`rp013_live_fixtures.sql` contains a single 36-row `INSERT` covering six
detection scenarios in `event_time` order. Inserting in chronological order
matters: streaming aggregations drop events that arrive with `event_time`
older than the current watermark.

### Run it

```bash
# 1. From the repo root, bring up Timeplus Enterprise 3.2.1.
docker compose up -d timeplus
until [ "$(docker inspect timeplus --format='{{.State.Health.Status}}')" = "healthy" ]; do sleep 2; done

# 2. Provision the streams rp-013 reads from / writes to.
TPCLI() { docker exec -i timeplus timeplusd client --port 8463 --user proton --password 'timeplus@t+' "$@"; }

TPCLI --query "DROP VIEW   IF EXISTS mv_rule_rp013"
TPCLI --query "DROP STREAM IF EXISTS agentguard_security_events"
TPCLI --query "DROP STREAM IF EXISTS agentguard_cim_metrics"

TPCLI --query "
CREATE STREAM agentguard_cim_metrics (
  event_time    datetime64(3),
  metric_name   low_cardinality(string),
  metric_value  float64,
  agent_type    low_cardinality(string),
  agent_id      string,
  deployment_id low_cardinality(string),
  session_id    string,
  tags          map(low_cardinality(string), string)
)"

TPCLI --query "
CREATE STREAM agentguard_security_events (
  detected_at   datetime64(3),
  rule_id       low_cardinality(string),
  rule_name     low_cardinality(string),
  severity      low_cardinality(string),
  agent_id      string,
  deployment_id low_cardinality(string),
  session_id    string,
  run_id        string,
  hook_name     low_cardinality(string),
  tool_name     string,
  message       string,
  event_data    string
)"

# 3. Install rp-013 as a materialized view. Note the MV name has no dash:
#    backend/internal/timeplus/rules.go's MVName() strips dashes from the
#    rule_id, so "rp-013" → "mv_rule_rp013". Match that here.
python3 - <<'PY' > /tmp/rp013_mv.sql
import yaml
with open('backend/internal/catalog/seed/rp-013.yaml') as f:
    rule = yaml.safe_load(f)
print("CREATE MATERIALIZED VIEW mv_rule_rp013 INTO agentguard_security_events AS")
# Strip the leading -- changelog comments so the parser only sees SQL.
in_header = True
for line in rule['query_sql'].split('\n'):
    if in_header and (line.strip().startswith('--') or line.strip() == ''):
        continue
    in_header = False
    print(line)
print(';')
PY
TPCLI --query "DROP VIEW IF EXISTS mv_rule_rp013"
TPCLI --multiquery < /tmp/rp013_mv.sql

# 4. Load the fixtures (this directory). The fixture file deliberately keeps
#    all -- comments OUTSIDE the VALUES body — Timeplus's INSERT parser
#    rejects '--' between value tuples with Code: 27.
TPCLI --query "$(cat backend/internal/catalog/testdata/rp013_live_fixtures.sql)"

# 5. Wait briefly for the streaming MV to drain, then inspect detections.
sleep 5
TPCLI --query "
SELECT session_id, count() AS detections
FROM   table(agentguard_security_events)
GROUP  BY session_id
ORDER  BY session_id
FORMAT TSV"
```

### Expected detection counts

| `session_id`  | Scenario                                              | Branch        | Expected | Duplicate factor |
| ---           | ---                                                   | ---           | ---      | ---              |
| `s1-abs`      | absolute spike (10 K output, 500 input)               | A (tumble)    | 1        | 1×               |
| `s2-codegen`  | codegen suppression (10 K output, 5 K input)          | —             | 0        | n/a              |
| `s3-relative` | 5x relative spike (5 baseline + 1 spike)              | B (hop)       | 4        | 4×               |
| `s4-sustained`| 3 high outputs close together                         | B (hop)       | 3        | 3×               |
| `s5-boundary` | 1 + 2 high outputs split across 5-min tumble boundary | B (hop)       | 4        | 4×               |
| `s6-drip`     | 3 high outputs spaced 8 min apart (documented gap)    | —             | 0        | n/a              |

`s5-boundary` and `s6-drip` are the regression-guard scenarios for the
hop-based Branch B: `s5-boundary` confirms hop's overlapping windows
correctly cover the patterns that plain tumble misses; `s6-drip` confirms
the documented "low-cadence drip" gap is intentional, not accidental.

`s3-relative`, `s4-sustained`, and `s5-boundary` produce **bounded
duplicates** (up to `window / slide = 5` rows per qualifying pattern).
`agentguard_threats` + `mv_notify_filter` deduplicate active threat state
and notification fanout, but raw detection-count and event-count views
will reflect the underlying detection rows. This is by design — see the
SQL changelog comment in `seed/rp-013.yaml` for the full discussion.

### Cleanup

```bash
docker compose down -v
```

## rp-018 (Token-Drain Cost Amplification)

`rp018_live_fixtures.sql` covers the v0.3.0 event-window rewrite:
token-only, repeated-tool-only, content-only, a true Clawdrain confluence,
a 14-minute run that crosses the old 10-minute wall-clock boundary, and the
`llm.total_tokens` double-count guard.

Expected detections after loading the fixture:

| `session_id` | Expected |
| --- | --- |
| `s1-token-only` | 0 |
| `s2-calls-only` | 0 |
| `s3-content-only` | 0 |
| `s4-true` | 1 |
| `s5-fourteen-min-run` | 1 |
| `s6-double-count` | 0 |

### Prerequisites

The rp-018 MV requires the **hybrid** hash-table aggregator. The seed SQL pins
this via `SETTINGS default_hash_table = 'hybrid'`; do not strip it. The memory
aggregator throws `NOT_IMPLEMENTED` for `EMIT AFTER SESSION CLOSE`. See the
SQL comment block in `seed/rp-018.yaml` for the source-tree citations.

### Truncation caveat

Runs longer than 15 minutes without a canonical `session_end` row are split
into consecutive 15-minute `MAXSPAN` sub-windows and evaluated independently.
Attacks that spread their load evenly across the boundary may slip under every
sub-window's thresholds. Agents that emit `session_end` at the end of a run
bypass this entirely.
