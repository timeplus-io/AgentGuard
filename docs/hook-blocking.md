# Hook-Based Blocking — Cross-Agent Investigation

**Status:** Investigation complete (2026-04-21). No implementation yet.
**Goal:** Determine whether AgentGuard can leverage existing hook mechanisms in OpenClaw, Claude Code, and Hermes to **prevent** (not just observe) risky tool calls and LLM calls.

---

## TL;DR

| Agent | Tool Call Blocking | LLM Call Blocking | Pre-Prompt Blocking |
|---|---|---|---|
| **Claude Code** | ✅ Rich, multi-modal | ❌ No direct API gate | ✅ `UserPromptSubmit` blocks the prompt before LLM sees it |
| **OpenClaw** | ✅ With approval/UI flow | ❌ `llm_input` is observe-only | ✅ Partial (`before_prompt_build`, `before_agent_start`) |
| **Hermes** | ✅ Supported in code (undocumented) | ❌ Not supported | ❌ Not supported |

**All three agents support tool-call blocking via their existing hook mechanisms.** None expose a native pre-LLM-API gate. The closest substitute is blocking the user prompt before it reaches the LLM (Claude Code only).

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

### Other Hooks With Result Returns (Potentially Blocking)

These return `Result | void` types that may control flow — worth investigating for AgentGuard's purposes:

- `before_model_resolve`
- `before_prompt_build`
- `before_agent_start`
- `before_agent_reply`
- `inbound_claim`
- `before_dispatch`
- `reply_dispatch`
- `message_sending`
- `subagent_spawning`
- `subagent_delivery_target`
- `tool_result_persist`
- `before_message_write`

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

## Implications for AgentGuard

The existing AgentGuard plugins all set `hook_decision = "observe"` and `block_reason = ""`. To move from observation to prevention, the following components need to exist:

### 1. Synchronous Rule Evaluation Path

Hook callbacks are invoked synchronously in the agent's hot path. AgentGuard rules currently evaluate via Timeplus materialized views (asynchronous, eventually consistent). For blocking we need either:

- **Local sync evaluation**: Run a subset of fast/critical rules (regex, allowlist/denylist, tool-arg patterns) in-process inside the plugin. No network round-trip.
- **Sync AgentGuard API**: Plugin POSTs the event to AgentGuard and waits for a `block`/`allow` decision. Latency budget probably <100ms; longer would degrade agent responsiveness.

A hybrid is likely best: small set of pre-cached "deny-list" rules in the plugin for instant decisions, with optional escalation to the backend for complex cases.

### 2. Decision Persistence

When a hook blocks, the plugin must also send the event to `agentguard_hook_events` with `hook_decision = "block"` and `block_reason = "..."` so the UI shows what was prevented. The schema already has these columns — just need to actually populate them.

### 3. Per-Agent Adapters

Each plugin translates the AgentGuard decision into the agent's native block contract:

| Adapter | Output |
|---|---|
| `claudecode` plugin | Print JSON with `permissionDecision: "deny"` to stdout |
| `openclaw` plugin | Return `{block: true, blockReason: ...}` from the hook |
| `hermes` plugin | Return `{"action": "block", "message": ...}` from the callback |

### 4. UI Surface

Threats page should distinguish blocked-and-prevented vs detected-after-the-fact. The existing `hook_decision` column makes this trivial — just filter on `block` to show the prevention timeline.

### What's NOT Possible

- **Pre-LLM-API blocking** is unavailable in OpenClaw and Hermes. Claude Code's `UserPromptSubmit` is the closest substitute but only catches user-initiated turns, not mid-loop LLM calls.
- **Mid-turn LLM call interception** (e.g., to enforce "don't call this provider for this prompt") cannot be done via hooks in any of the three agents. Would require a network-layer proxy (LiteLLM) instead.

---

## Open Questions

1. **Latency budget** — what's the maximum time the hook can hang before degrading agent UX? Need to measure per-agent.
2. **Fail-open vs fail-closed** — if the AgentGuard backend is unreachable, should the plugin allow or deny? OpenClaw's `before_tool_call` defaults to fail-closed; we should match that default for consistency.
3. **Rule scope for sync eval** — which existing seed rules (rp-001 … rp-084) can be evaluated in-process without Timeplus? Most are SQL with lookups against historical context — those won't translate. Pure regex/pattern rules are good candidates.
4. **Provider gap for Hermes `llm_response`** — already known limitation. Doesn't affect blocking design.
