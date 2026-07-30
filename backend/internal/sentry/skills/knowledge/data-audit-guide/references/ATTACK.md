# Agent Attack & Anomaly (A19–A23)

Each entry states the **goal** and an **example** to adapt to the user's request
and compose into the skeleton in `SKILL.md`. A19, A21, A22, and A23 are
**behavioral** (cross-event sequences or per-session aggregations), so their
examples are full queries; A20 is a single-event content check.

## Per-rule spec

### A19 提示词注入导致数据访问 (behavioral / sequence)
The rule is injection **then** data access — an injection marker in a user
message, followed by a tool call later in the same session. Detect the sequence,
not just the phrase.

```sql
WITH injections AS (
  SELECT session_id, min(event_time) AS inj_time
  FROM table(agentguard_cim_event)
  WHERE event_type = 'user_input'
    AND (
         match(lower(user_message), 'ignore [a-z ]{0,20}(previous|prior|all) instructions')
      OR multi_search_any(lower(user_message), ['reveal the system prompt', 'show system prompt', '忽略以上指令', '忽略之前的指令', 'read other user', 'access other user', '其他用户数据', 'call internal tool'])
    )
  GROUP BY session_id
)
SELECT e.event_time, e.agent_id, e.session_id, e.tool_name, left(e.tool_input, 200) AS evidence, i.inj_time
FROM table(agentguard_cim_event) AS e
JOIN injections AS i ON e.session_id = i.session_id
WHERE e.event_type = 'tool_invoke' AND e.event_time >= i.inj_time
ORDER BY e.event_time DESC
LIMIT 50
```
- The injection-marker list is the building block — extend it as needed. To
  gauge impact, compare tool activity before vs. after `inj_time`.
- Evidence: the injection phrase, the tool call that followed, `session_id`.

### A20 系统提示词或隐藏上下文泄露
System prompt / internal policy / tool defs / KB keys in a reply. Review first.
event_type = `llm_response`.
- Example: `multi_search_any(lower(assistant_message), ['system prompt','my instructions are','internal policy','tool definition','以下是我的系统提示','我的系统指令','my system message','hidden context','knowledge base key','begin system','<system>'])`
- Evidence: the leaked fragment (masked), `session_id`, `event_time`.

### A21 智能体行为与声明能力不一致 — proxy (aggregation)
No declared-capability field, so list the tool verbs each agent actually used and
eyeball against its stated role.

```sql
SELECT agent_id, tool_name, count() AS calls
FROM table(agentguard_cim_event)
WHERE event_type = 'tool_invoke' AND tool_name != ''
GROUP BY agent_id, tool_name
ORDER BY agent_id, calls DESC
LIMIT 200
```
- Evidence: agent, the out-of-role tools it used, call counts.

### A22 异常调用频率和调用链 (behavioral / aggregation)
Frequency and chain anomalies are a **per-session aggregation**, not a keyword in
one result: call volume, low tool diversity (recursion / tight loop), repeated
failures, and oversized replies.

```sql
SELECT session_id, agent_id,
       count_if(event_type = 'tool_invoke')                             AS tool_calls,
       count_distinct(tool_name)                                       AS distinct_tools,
       count_if(event_type = 'tool_complete' AND tool_success = false) AS failures,
       max(length(assistant_message))                                  AS max_reply_len
FROM table(agentguard_cim_event)
GROUP BY session_id, agent_id
HAVING tool_calls > 50 OR failures > 5 OR max_reply_len > 50000
ORDER BY tool_calls DESC
LIMIT 50
```
- Tune the thresholds to the deployment's baseline. High `tool_calls` with
  `distinct_tools = 1` is a recursion / tight-loop signal; many `failures` is a
  repeated-retry signal.
- Evidence: the session's counts, `session_id`; then drill into its call chain.

### A23 安全策略命中但未阻断 (listing)
A policy was flagged but the request still ran. Uses the native `hook_decision` /
`block_reason` columns — list every event carrying a policy decision, then check
whether execution continued in the same session.

```sql
SELECT event_time, event_type, session_id, tool_name, hook_decision, left(block_reason, 160) AS reason
FROM table(agentguard_cim_event)
WHERE hook_decision != '' OR block_reason != ''
ORDER BY event_time DESC
LIMIT 100
```
- Evidence: `hook_decision`, `block_reason`, whether a later `tool_complete` ran
  in the same `session_id`, `event_time`.
