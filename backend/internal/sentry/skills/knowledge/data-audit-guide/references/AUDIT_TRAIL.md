# Audit Trail & Traceability (A24–A27)

All four need fields CIM does not yet capture (#261); treat as data-quality
investigations, not detections.

## A24 审计字段缺失 — 🔍 investigate
Events missing user/tenant/time/session/agent/model/tool/op-type.
- Measurable proxy — count events with empty key fields:
  ```sql
  SELECT
    countIf(session_id = '') AS no_session,
    countIf(agent_id = '')   AS no_agent,
    countIf(run_id = '')     AS no_run
  FROM table(agentguard_cim_event)
  WHERE event_time >= now() - INTERVAL 1 DAY
  ```
- `user_id`, `tenant_id`, `role_id`, versions are structurally absent — report "not captured".

## A25 调用链断裂 — 🔍 investigate
Tool call not associable to its conversation/model/user, or result to reply.
- Proxy — tool events whose `run_id`/`session_id` is empty:
  ```sql
  SELECT count() FROM table(agentguard_cim_event)
  WHERE event_type IN ('tool_invoke','tool_complete') AND (run_id = '' OR session_id = '')
  ```
- Full `trace_id`/`parent_event_id` linkage is #261.

## A26 模型和策略版本不可追溯 — 🔍 investigate
Missing model/agent/prompt/tool/policy version.
- `model` exists; `model_version`, `prompt_version`, `tool_version`,
  `policy_version` do not (#261). Report "not captured".

## A27 日志内容过度留存 — 🔍 investigate
Full conversation/tool result retained when only a summary/hash is needed.
- Governance/config concern, not derivable from event data. Advise reviewing
  retention config; no detection.
