# Sensitive Data Handling (A01–A07)

Each rule: **definition** → **status/where** → **fields & gaps** → **recipe** →
**evidence**. Masked-fragment discipline: never echo a full secret; show a
short masked slice.

## A01 用户输入敏感数据检测 — ✅ `dsa-a01`
Sensitive PII (ID/bank-card/password/key/customer data) in user input.
- Fields: `user_message` on `event_type='user_input'`.
- Detections:
  ```sql
  SELECT detected_at, agent_id, session_id, message
  FROM table(agentguard_security_events)
  WHERE rule_id = 'dsa-a01' AND detected_at >= now() - INTERVAL 7 DAY
  ORDER BY detected_at DESC LIMIT 100
  ```
- Or semantic: `dlp_search` for "customer identity or account numbers in user input".
- Evidence: A01, risk, session_id/run_id, masked hit, PII type, event_time.

## A02 输入模型前未脱敏 — 🔍 investigate
Sensitive fields still plaintext when sent to the model.
- Gap: needs raw-input vs. gateway-request comparison and `content_direction`
  — not in CIM (#261). Best-effort proxy: same sensitive markers present on
  both a `user_input` and the following `llm_request` in one session.
  ```sql
  SELECT session_id, count() FROM table(agentguard_cim_event)
  WHERE event_type IN ('user_input','llm_request')
    AND multi_search_any(lower(user_message), ['password','身份证','api key'])
  GROUP BY session_id
  ```
- Evidence: mark "masking state not captured — proxy only".

## A03 模型输出敏感信息 — ✅ `dsa-a03`
PII/account/auth data in model replies; highest risk if not from current
session.
- Fields: `assistant_message` on `event_type='llm_response'` (the model-text column is `assistant_message`; `llm_response` is an event_type value, not a column).
- Detections: `rule_id='dsa-a03'`. Cross-session origin = not captured; note it.

## A04 工具参数携带非必要敏感数据 — ✅ `dsa-a04`
Sensitive fields in tool params not needed for the operation.
- Fields: `tool_input`, `tool_name` on `tool_invoke`.
- Detections: `rule_id='dsa-a04'`.

## A05 工具返回结果未脱敏 — ✅ `dsa-a05`
Full customer data returned and written back without masking.
- Fields: `tool_result` on `tool_complete`.
- Detections: `rule_id='dsa-a05'`.

## A06 身份鉴别信息进入对话链路 — ✅ `dsa-a06`
Credentials anywhere in the chain — direct high risk.
- Fields: all content columns, all event types.
- Detections: `rule_id='dsa-a06'`. This is a priority rule — review first.

## A07 数据汇聚后风险升级 — ✅ `dsa-a07`
Identity-linkable data co-occurring (aggregation).
- Fields: `assistant_message`+`tool_result`.
- Detections: `rule_id='dsa-a07'`. Multi-turn aggregation across events is
  investigate-only — use `dlp_search` over a session to confirm linkage.
