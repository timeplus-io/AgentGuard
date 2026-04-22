# Hook-Based Blocking — Cross-Agent Investigation

**Status:** Investigation complete (2026-04-21). No implementation yet.
**Goal:** Determine whether AgentGuard can leverage existing hook mechanisms in OpenClaw, Claude Code, and Hermes to **prevent** (not just observe) risky tool calls and LLM calls.

---

## TL;DR

| Agent | Tool Call Blocking | LLM Call Blocking | Pre-Prompt Blocking |
|---|---|---|---|
| **Claude Code** | ✅ Rich, multi-modal | ❌ No direct API gate | ✅ `UserPromptSubmit` blocks the prompt before LLM sees it |
| **OpenClaw** | ✅ With approval/UI flow | ❌ `llm_input` is observe-only | ❌ `before_prompt_build` / `before_agent_start` are mutation-only (no `block` field, fail-open on throw) |
| **Hermes** | ✅ Supported in code (undocumented) | ❌ Not supported | ❌ Not supported |

**All three agents support tool-call blocking via their existing hook mechanisms.** None expose a native pre-LLM-API gate. The closest substitute is blocking the user prompt before it reaches the LLM (Claude Code only). Crucially, **returning `block` from a tool hook does not halt the agent loop** — the model receives the block reason as a tool error and can keep retrying (see [Post-Block Behavior](#post-block-behavior-why-return-block-isnt-enough)). AgentGuard's design — [Synchronous Hold-and-Wait](#proposed-design-synchronous-hold-and-wait) — exploits the fact that hooks run in the agent's hot path: the hook simply does not return until a human resolves the hold in AgentGuard, truly pausing the agent.

---

## 1. Claude Code

**Source:** [code.claude.com/docs/en/hooks.md](https://code.claude.com/docs/en/hooks.md)

### Tool Call Blocking — `PreToolUse`

The `PreToolUse` event fires before any tool executes. The hook can deny the call by writing JSON to stdout:

```json
{
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "permissionDecision": "deny",
    "permissionDecisionReason": "Destructive command blocked by hook"
  }
}
```

Or by exiting with code 2 and a stderr message.

Claude Code reads the decision, blocks the tool, and shows the model the reason — so the model can react and try a different approach.

### Hook Handler Types

Uniquely powerful: 4 different ways to make the decision, configured per matcher:

| Type | Description |
|---|---|
| `command` | Shell command on stdin/stdout |
| `http` | POST event to URL, get decision in response body |
| `prompt` | Send a single-turn prompt to a Claude model that returns yes/no |
| `agent` | Spawn a subagent with tools (Read, Grep, Glob) to investigate before deciding |

### Other Blocking-Capable Events

- `UserPromptSubmit` — fires before the user prompt reaches Claude. Can block content from ever reaching the LLM.
- `UserPromptExpansion` — fires before slash-command expansion. Can block.
- `PermissionDenied` — fires when a tool is auto-denied; hook can return `{retry: true}` to let the model retry.

### LLM Call Blocking

**No native pre-LLM-API gate.** But `UserPromptSubmit` is functionally equivalent for user-initiated turns: block the prompt → no API call.

Mid-turn LLM calls (during the agentic loop, after the user prompt) cannot be intercepted.

---

## 2. OpenClaw

**Source:** `/Users/gangtao/Code/gang/openclaw/`

### Tool Call Blocking — `before_tool_call`

The `before_tool_call` hook returns a `PluginHookBeforeToolCallResult | void`. The dispatcher in `src/agents/pi-tools.before-tool-call.ts:213` evaluates the result:

```typescript
if (hookResult?.block) {
  return {
    blocked: true,
    reason: hookResult.blockReason || "Tool call blocked by plugin hook"
  };
}
```

Tools are wrapped via `wrapToolWithBeforeToolCallHook` (`pi-tools.before-tool-call.ts:380`). When `outcome.blocked` is true, the wrapper throws before `execute()` runs:

```typescript
if (outcome.blocked) {
  throw new Error(outcome.reason);
}
```

### Approval Flow

Beyond a simple block, the hook can return `requireApproval` to surface a UI dialog:

- `allow_once` — proceed this time
- `allow_always` — proceed and remember
- `deny` — block
- `timeout` — fall back to configurable `timeoutBehavior` (default: deny)

### Param Mutation

The hook can also return modified `params` to rewrite the tool's arguments before execution.

### Fail-Closed Semantics

If the hook itself throws an exception, the tool is blocked with reason `"Tool call blocked because before_tool_call hook failed"` (`pi-tools.before-tool-call.ts:28`). This is the safe default — broken security plugins fail closed, not open.

### LLM Call Blocking

`llm_input` (`hook-types.ts:595`) signature is `=> Promise<void> | void` — **return value ignored, observation only**. No way to block an LLM call.

### Pre-Prompt Hooks Are Mutation-Only (Cannot Block)

A natural question: can the pre-prompt-phase hooks (`before_prompt_build`, `before_agent_start`, `before_model_resolve`) gate the LLM call? **No.** Reading the result type definitions in `src/plugins/hook-before-agent-start.types.ts`:

```typescript
// before_model_resolve — model selection only, no block field
export type PluginHookBeforeModelResolveResult = {
  modelOverride?: string;
  providerOverride?: string;
};

// before_prompt_build — prompt mutation only, no block field
export type PluginHookBeforePromptBuildResult = {
  systemPrompt?: string;
  prependContext?: string;
  prependSystemContext?: string;
  appendSystemContext?: string;
};

// before_agent_start — combines both, still no block field
export type PluginHookBeforeAgentStartResult =
  PluginHookBeforePromptBuildResult & PluginHookBeforeModelResolveResult;
```

These hooks are designed for **prompt enrichment** (RAG injection, system prompt customization) and **model routing** — not security gating.

**Throwing an exception doesn't help either.** The hook runner (`src/plugins/hook-runner-global.ts:43-46`) sets `fail-closed` only for `before_tool_call`:

```typescript
state.hookRunner = createHookRunner(registry, {
  catchErrors: true,
  failurePolicyByHook: {
    before_tool_call: "fail-closed",
  },
});
```

All other hooks fail-open — a thrown exception is logged and the agent continues. There is no fail-closed path for any pre-LLM hook.

### Other Hooks With Result Returns

These return `Result | void` types that *may* control flow but were not investigated in detail (none are pre-LLM-call):

- `before_agent_reply`, `inbound_claim`, `before_dispatch`, `reply_dispatch`, `message_sending`, `subagent_spawning`, `subagent_delivery_target`, `tool_result_persist`, `before_message_write`

---

## 3. Hermes

**Source:** `/Users/gangtao/Code/gang/hermes-agent/` and [hermes-agent.nousresearch.com/docs/user-guide/features/hooks](https://hermes-agent.nousresearch.com/docs/user-guide/features/hooks)

### The Public Doc Is Wrong

The Hermes hooks user-guide explicitly says:

> "All hooks are **fire-and-forget observers** whose return values are ignored — except `pre_llm_call`, which can inject context."

**This is inaccurate.** The code clearly supports `pre_tool_call` blocking. AgentGuard can use it.

### Tool Call Blocking — `pre_tool_call`

A plugin's `pre_tool_call` callback returns:

```python
{"action": "block", "message": "Reason the tool was blocked"}
```

The dispatcher in `hermes_cli/plugins.py:837-873`:

```python
def get_pre_tool_call_block_message(...) -> Optional[str]:
    hook_results = invoke_hook("pre_tool_call", ...)
    for result in hook_results:
        if not isinstance(result, dict):
            continue
        if result.get("action") != "block":
            continue
        message = result.get("message")
        if isinstance(message, str) and message:
            return message
    return None
```

The tool dispatcher in `model_tools.py:485-497` checks the result and short-circuits:

```python
if block_message is not None:
    return json.dumps({"error": block_message}, ensure_ascii=False)
```

The blocked outcome is fed back to the LLM as the tool result — the model sees `{"error": "Reason"}` and can adapt its strategy.

### Caveats

- The first valid `block` directive wins (no merging across plugins).
- Invalid or non-dict return values are silently ignored — observer-only hooks aren't affected.
- Other plugins' `pre_tool_call` callbacks still fire even after a block (they run as observers).

### LLM Call Blocking

No equivalent. `pre_llm_call` only allows context injection (concat to user message). `pre_api_request` is observation-only. There is no `get_pre_llm_call_block_message` or similar.

---

## Comparison: Block Contract Shapes

| Agent | Hook | Block return shape | Reason field |
|---|---|---|---|
| Claude Code | `PreToolUse` | `{"hookSpecificOutput": {"permissionDecision": "deny", "permissionDecisionReason": "..."}}` | `permissionDecisionReason` |
| OpenClaw | `before_tool_call` | `{block: true, blockReason: "..."}` (TS) | `blockReason` |
| Hermes | `pre_tool_call` | `{"action": "block", "message": "..."}` (Py) | `message` |

All three deliver the block reason back to the LLM as the tool result so the model can react. None of them surface the block to the *user* in a standardized way (that would be AgentGuard's job).

---

## Post-Block Behavior (Why "Return Block" Isn't Enough)

When the hook *returns* a block decision, the agent loop **continues**. The block reason becomes the tool result fed back to the LLM, which then decides what to do next:

| Agent | What happens after `block` is returned | Where in code |
|---|---|---|
| **Claude Code** | `permissionDecisionReason` is shown to Claude (the model). The agent loop continues. The model can retry a different tool, give up, or escalate. | Doc: *"For `"deny"`, [the reason is] shown to Claude"* |
| **OpenClaw** | Hook throws `new Error(outcome.reason)`. The agent runner catches it, converts to a tool error result, appends to conversation history, loop continues. | `pi-tool-definition-adapter.ts:332-334` |
| **Hermes** | Returns `json.dumps({"error": block_message})` as tool result, appended as `{"role": "tool", "content": ..., "tool_call_id": ...}`. Loop continues. | `model_tools.py:496-497` + `run_agent.py:8602-8607` |

**No tool-blocking hook can terminate the entire turn** in any of the three agents (Claude Code's `{"continue": false, "stopReason": "..."}` works only for `Stop`/`SubagentStop`/`TeammateIdle`/`TaskCreated`/`TaskCompleted` — not `PreToolUse`).

### Iteration Caps and Worst-Case Cost

The loop will eventually halt on its own — but the cap is high:

| Agent | Iteration cap per turn | Per-call timeout | Loop circuit breaker |
|---|---|---|---|
| **Hermes** | **90 LLM calls** (`run_agent.py:782`, `max_iterations: int = 90`); subagents capped at 50 | Per-provider request + stale-stream timeout | None — model just keeps calling |
| **OpenClaw** | Configurable (`tools.loopDetection`: `warningThreshold`, `criticalThreshold`, `globalCircuitBreakerThreshold`) | Per-provider timeout | **YES** — `tool-loop-detection.ts:520` halts on consecutive-call patterns: *"Session execution blocked to prevent resource waste"* |
| **Claude Code** | Internal cap (~25–50 in practice; not exposed via hooks) | Per-handler `timeout` (defaults: 600s command, 30s prompt, 60s agent) | None hook-visible |

A "stickily blocked" Hermes session worst-case = up to 90 LLM calls × tokens-per-call burned before the iteration budget runs out. **This is what motivates the design below.**

---

## Proposed Design: Synchronous Hold-and-Wait

**Don't return from the hook until the human resolves the block in AgentGuard.** Hooks run synchronously in the agent's hot path — if the hook never returns, the agent loop is paused. This is a *true* hard block (not a "block this call and let the model try the next one").

### The Insight

OpenClaw already ships this exact pattern as a first-class feature: `requireApproval` from `before_tool_call` long-polls `plugin.approval.waitDecision` for up to 120s, surfaces the question in the UI, and resumes when the user picks `allow_once` / `allow_always` / `deny`. We adopt the same model uniformly across all three agents.

### Flow

```
Hook fires (PreToolUse / pre_tool_call / before_tool_call)
         │
         ▼
Plugin sends event to agentguard_hook_events     (existing async POST)
         │
         ▼
Plugin calls AgentGuard backend:
   POST /api/holds   { session_id, agent_id, tool_name, args, ... }
         │
         ▼
Backend opens a HOLD record and:
   • If a recent open threat exists for this session → respond immediately with "block"
   • Else → wait briefly for the just-emitted event to flow through MVs (≤ N ms)
   • Else → respond "allow"
         │
   ┌─────┴─────┐
   │           │
 ALLOW       BLOCK
   │           │
   │           ▼
   │     Backend HOLDS the HTTP response open (long-poll, up to T seconds)
   │     UI displays the pending hold in the Threats panel + SSE notification
   │     Human clicks "Approve" or "Deny" in the UI
   │           │
   │           ▼
   │     Backend responds:
   │       • approve → "allow"  (tool runs)
   │       • deny    → "block"  (tool blocked, agent loop continues normally)
   │       • timeout → fail policy (recommended: "block")
   │           │
   ▼           ▼
Hook returns the decision to the agent
```

### Why This Is Better Than Returning Block Immediately

| Aspect | Return block immediately | Hold-and-wait (this design) |
|---|---|---|
| First risky call | Slips through (MV race) | **Blocked** — the hold pauses the call until backend can decide |
| Subsequent calls | Each blocked individually; LLM keeps retrying | None — the agent is paused, no retries possible |
| Token burn during hold | Up to 90 LLM calls (Hermes worst case) | Zero — agent is waiting, not generating |
| Human unblock UX | Poll → eventually next call sees cleared threat | Immediate — backend returns "allow" → exact tool resumes |
| Backend complexity | New `/api/threats/check` + cache + SSE for unblock | Long-poll endpoint, hold table |
| Per-agent contract | Each plugin's native block shape | Same — but the plugin holds the hook open |

### Per-Agent Hold Mechanics

The viability of hold-and-wait depends on how each agent treats a non-returning hook. **All three hang the loop**, but the timeout boundary differs:

| Agent | Hangs the loop? | Hook timeout (default) | Behavior at timeout |
|---|---|---|---|
| **Claude Code** | YES while the hook runs | 600s (command), 30s (prompt), 60s (agent) — per-handler `timeout` field | HTTP hooks: timeout = non-blocking error → execution continues (fail-open). Recommend setting `timeout: 600`. |
| **OpenClaw** | YES — natively designed for it | `approval.timeoutMs ?? 120_000` (2 min) | `timeoutBehavior`: `"deny"` (default) or `"allow"`. Also respects `signal.aborted` for run cancellation. |
| **Hermes** | YES — **indefinitely** | **None.** `for cb in callbacks: ret = cb(**kwargs)` — synchronous, no timeout, no abort signal | Hangs forever until process killed. **Plugin must self-impose a timeout.** |

### Per-Plugin Implementation Sketch

| Plugin | Approach |
|---|---|
| **OpenClaw** | Return `{ requireApproval: { pluginId: "agentguard", title: "AgentGuard hold", description: "...", severity: "warning", timeoutMs: 120_000, timeoutBehavior: "deny" } }` from `before_tool_call`. AgentGuard backend implements gateway-side `plugin.approval.request` and `plugin.approval.waitDecision`. |
| **Claude Code** | Hook (preferably `type: "http"` with `timeout: 600`) long-polls AgentGuard. Returns `{permissionDecision: "allow"}` or `{permissionDecision: "deny", permissionDecisionReason: "..."}` once decision arrives. |
| **Hermes** | `pre_tool_call` callback long-polls AgentGuard via `httpx.post(..., timeout=120)` (self-imposed timeout — Hermes provides none). Returns `{"action": "block", "message": ...}` if denied; returns `None` if approved. |

### Required Backend Work

| Component | Status | Notes |
|---|---|---|
| `POST /api/holds` long-poll endpoint | NEW | Opens a hold, returns when human decides or timeout. Long-poll up to ~120s server-side. |
| Hold table (or stream) | NEW | `(hold_id, session_id, agent_id, tool_name, args_summary, created_at, status, decided_by, decided_at)` |
| Threats UI surfaces holds | EXTEND | New "Pending Holds" section showing pending holds with Approve / Deny buttons. SSE push for new holds. |
| Auto-decide based on existing threats | NEW | If a related open threat already exists for this `(session, rule)` → decide immediately without long-poll. |
| Block-decision event | EXISTS | `agentguard_hook_events.hook_decision` + `block_reason` populated when the hook ultimately returns block. |
| Notification on hold | NEW | Extend SSE `/api/notifications/stream` to push `hold_pending` events so the UI updates in real time. |

### Required Plugin Work

All three plugins:
1. Send the `pre*` event to `agentguard_hook_events` (already done).
2. Issue the synchronous long-poll `POST /api/holds`. Wait for response.
3. Translate response into the agent's native block/allow shape.
4. After blocking, also POST a second event with `hook_decision="block"` and `block_reason=<reason>` so the audit trail is complete.

### Failure Modes

- **Backend unreachable**: hook must decide fail-open or fail-closed. **Recommendation: fail-closed** (deny) for explicit security holds — if the backend is the source of truth and unreachable, refusing the action is safer than allowing it. The existing event-ingest path stays fail-open (don't block on telemetry ingestion errors), but the hold endpoint is fail-closed. Make this configurable per-deployment.
- **Backend slow / busy**: long-poll naturally handles this; the hook simply waits. Up to the hook timeout boundary.
- **Hook timeout reached before human responds**: each agent's behavior differs (table above). For Hermes the plugin's self-imposed `httpx` timeout fires; for OpenClaw the `timeoutBehavior` fires; for Claude Code HTTP timeout means execution continues (fail-open) — we should override by returning `permissionDecision: "deny"` from the hook just before our self-imposed timeout window expires.
- **User session ends while held**: backend must mark the hold abandoned (no decision possible). Surfaces in audit log as `hold_abandoned`.
- **Human accidentally approves a malicious action**: not a system failure, but worth surfacing the full args + LLM context in the UI so the human has enough information to decide.

### UX Considerations

- Agent appears frozen during a hold. **Each plugin should signal this clearly.** Claude Code has `statusMessage` in hook config; OpenClaw `requireApproval` has `title`/`description`; Hermes has no built-in mechanism — would need to print to stderr or rely on the AgentGuard UI being open.
- Holds should be configurable per rule severity: `critical` → always hold; `warning` → log only (no hold); `info` → never hold.
- Long-poll connection cost is modest at our scale (handful of holds per session).

### What's NOT Possible (Recap)

- **Pre-LLM-API blocking** for OpenClaw and Hermes — confirmed unavailable. Hold-and-wait works for tool calls only. The closest LLM-call gate is Claude Code's `UserPromptSubmit` (user-initiated turns only).
- **Mid-turn LLM call interception** — would require a network-layer proxy (LiteLLM) instead of hooks.

---

## Open Questions

1. **Hold timeout default** — 120s matches OpenClaw's existing approval default. Reasonable as a starting point.
2. **Auto-decide window** — when the hook fires, how long should the backend wait for MVs to process the just-emitted event before responding "allow" if no threat materializes? Probably 300–500ms — long enough for typical Proton MV latency, short enough to not feel laggy when no threat exists.
3. **Per-rule hold policy** — config to map rule severity → hold/log/ignore. Start with `critical → hold, warning → log, info → ignore`.
4. **Hermes self-timeout boundary** — what should the plugin do if `httpx` times out? Return `{"action": "block", "message": "AgentGuard hold timed out, denying for safety"}` is the safe default.
5. **Approve-with-edit** — should the human be able to modify the tool args before approval? OpenClaw's `requireApproval` already supports this via `params` on the approval response. Worth adopting cross-agent.
6. **Provider gap for Hermes `llm_response`** — already known limitation, doesn't affect blocking design.
