# Sentry Agent — Manual Test Plan

Manual, end-to-end test cases for the **Sentry** conversational security-analyst
agent (`/sentry` page, `/api/sentry/*`). This plan focuses on running Sentry
against **OpenAI-compatible Chinese model providers** — **DeepSeek**, **Kimi
(Moonshot)**, and **Qianwen (Qwen / Alibaba DashScope)** — and on verifying that
both **English and Chinese** prompts work, since one of the target customers is
based in China.

- Feature docs: `docs/sentry.md` (operator guide), `CLAUDE.md` → *Sentry agent*.
- Design: `docs/superpowers/specs/2026-07-22-sentry-agent-design.md`.
- Scope: chat core (Phase 1), read-only tools, gated writes, session
  management, config/connectivity, and the `/sentry` UI. Sentry *memory* is
  Phase 2 and out of scope except for the "how is memory configured" answer.

---

## 1. Prerequisites

| # | Requirement |
|---|-------------|
| 1 | AgentGuard backend + frontend running (`make dev`) **or** the single binary, with **Timeplus Enterprise reachable** and provisioned (`POST /api/setup/provision`). |
| 2 | You can log in to the UI with Timeplus credentials. |
| 3 | **Sentry provisioned**: `sentry.enabled: true` and streams created. Check `GET /api/sentry/status` → `{ "enabled": true, "provisioned": true, "has_base_url": … }`. |
| 4 | Some data exists to query: at least a few `agentguard_hook_events`, ideally one or more threats, one hold, and a couple of installed rules. (Use `make dlp-seed` / normal agent traffic, or create a hold via a plugin.) |
| 5 | **API keys** for the three providers under test (see §2). Each key must have quota and access to a **tool-calling-capable** model. |
| 6 | Two browsers/profiles (or two users) available for the **owner-only** and **approval** tests. |

> **Tool calling is mandatory.** Sentry drives every action through function/tool
> calls. `POST /api/sentry/config/test` sends `tool_choice:"required"` and
> asserts `tool_calls` come back. Pick a model that supports OpenAI-style
> function calling (see the per-provider notes) — a pure "reasoning" model that
> rejects `tool_choice` will fail the connection test.

---

## 2. Provider reference (OpenAI-compatible)

All three are configured with **`api_type: "openai"`**. `base_url` is the
OpenAI-compatible `/v1` base — Sentry appends `/chat/completions`.

| Provider | `base_url` | Suggested `model` | Notes |
|----------|-----------|-------------------|-------|
| **DeepSeek** | `https://api.deepseek.com/v1` | `deepseek-chat` | `deepseek-chat` supports function calling. Avoid `deepseek-reasoner` for the connection test if it rejects `tool_choice`. |
| **Kimi (Moonshot)** | `https://api.moonshot.cn/v1` (CN) or `https://api.moonshot.ai/v1` (intl) | `moonshot-v1-32k` (or `kimi-latest`) | Function calling supported. Use the `.cn` host for China-hosted keys. |
| **Qianwen (Qwen / DashScope)** | `https://dashscope.aliyuncs.com/compatible-mode/v1` (CN) or `https://dashscope-intl.aliyuncs.com/compatible-mode/v1` (intl) | `qwen-plus` (or `qwen-max`) | Use Alibaba Cloud's **OpenAI-compatible mode** path exactly. `qwen-plus`/`qwen-max` support tools. |

> Model IDs and hostnames evolve; if a listed model 404s, pick the provider's
> current tool-calling chat model. Record the exact `base_url`/`model` you used
> in the results table.

### Config field cheat-sheet (`chat` row / **Settings → Sentry Agent**)

| Field | Value for these tests |
|-------|-----------------------|
| `api_type` | `openai` |
| `base_url` | per §2 |
| `api_key` | provider key (blank on edit = keep stored key) |
| `model` | per §2 |
| `agentguard_tools_enabled` | **OFF** for §D (generic SQL analyst); **ON** for §C/§E |
| `max_iterations`, `turn_token_budget`, `sql_timeout_ms`, `sql_max_rows`, `tool_result_max_bytes` | defaults, unless a case says otherwise |
| `thinking`, `effort`, `prompt_cache_disabled` | ignored for `openai` (Anthropic-only) — leave default |

---

## 3. Bilingual question bank

Reuse these EN/CN pairs across the cases. **Every question that returns data
must be tried in both languages** on at least one provider.

| Intent | English | 中文 |
|--------|---------|------|
| Greeting / capability | "What can you help me with?" | "你能帮我做什么？" |
| Self model | "What model are you using?" | "你现在用的是什么模型？" |
| Memory config | "How is your memory configured?" | "你的记忆功能是怎么配置的？" |
| Threats | "Are there any unhandled threats right now?" | "现在有哪些未处理的威胁？" |
| Cost | "Which model had the highest cost in the last 30 days?" | "过去 30 天成本最高的模型是哪个？" |
| Holds | "Are there any pending holds?" | "有待审批的 hold 吗？" |
| Rules | "Which rules are currently enabled?" | "现在启用了哪些规则？" |
| Rule catalog | "What rules can I install from the catalog?" | "规则目录里有哪些可以安装的规则？" |
| Session timeline | "Summarize the timeline of session `<id>`." | "总结一下会话 `<id>` 的时间线。" |
| DLP search | "Search the conversations for leaked API keys or PII." | "在对话记录里搜索泄露的 API key 或 PII。" |
| Ad-hoc SQL | "How many hook events happened in the last hour?" | "过去一小时有多少条 hook 事件？" |
| Gated write | "Acknowledge threat `<id>`." | "确认（acknowledge）威胁 `<id>`。" |

---

## 4. How to run (per-provider matrix)

Run the **core suite** (§A–§H) **once per provider**. Run §I (robustness) once,
plus the provider-swap parts. Track results in §11.

| Suite | DeepSeek | Kimi | Qianwen |
|-------|:--------:|:----:|:-------:|
| A Config & connectivity | ☐ | ☐ | ☐ |
| B Basic chat (EN+CN) | ☐ | ☐ | ☐ |
| C Read-only tools (tools ON) | ☐ | ☐ | ☐ |
| D Generic SQL analyst (tools OFF) | ☐ | ☐ | ☐ |
| E Gated write approvals | ☐ | ☐ | ☐ |
| F Session management | ☐ | ☐ | ☐ |
| G UI / UX | ☐ | ☐ | ☐ |
| H Self-context | ☐ | ☐ | ☐ |

---

## A. Configuration & connectivity

### TC-A1 — Configure DeepSeek and pass the connection test
**Priority:** P0
**Pre:** Logged in; Sentry provisioned.
**Steps:**
1. Open **Settings → Sentry Agent**.
2. Set `api_type = openai`, `base_url = https://api.deepseek.com/v1`, `model = deepseek-chat`, paste the DeepSeek `api_key`.
3. Click **Test connection**.
4. Save the **chat** config.
**Expected:**
- Test reports **chat reachable** and that **tool calls come back** (the embedding check is *skipped* unless memory is enabled → shown as skipped, not failed).
- Save succeeds; banner confirms "changes take effect on the next turn — no re-provision".
- No 5xx regardless of outcome (a bad endpoint returns a structured failure, never a crash).

### TC-A2 — Repeat TC-A1 for Kimi and Qianwen
**Priority:** P0
**Steps:** Same as TC-A1 with the Kimi and Qianwen `base_url`/`model` from §2.
**Expected:** Both pass the connection test and save.

### TC-A3 — API key is preserved on blank re-save
**Priority:** P1
**Steps:**
1. With a provider configured and key set, reopen **Settings → Sentry Agent**. The API key field is blank with a "•••• set" indicator.
2. Change only the `model`, leave `api_key` blank, Save.
3. Start a Sentry chat and ask any question.
**Expected:** Chat still authenticates (the stored key was preserved, not wiped).

### TC-A4 — API key is encrypted at rest
**Priority:** P1
**Steps:**
1. In the **SQL Console** (`/investigate`) run: `SELECT key, value FROM table(agentguard_sentry_config)`.
**Expected:** The `chat` row's `value` shows the `api_key` as an `enc:v1:…` ciphertext, **not** the plaintext key. (Only the runtime decrypts it.)

### TC-A5 — Switching providers takes effect live (no re-provision)
**Priority:** P1
**Steps:**
1. Configure DeepSeek, ask "What model are you using?" in a new conversation.
2. Switch to Qianwen in Settings, Save.
3. In a **new** conversation, ask "你现在用的是什么模型？".
**Expected:** The second answer reflects the Qianwen model; no re-provision was required.

### TC-A6 — Config editor does not clobber unsaved edits (regression #237)
**Priority:** P2
**Steps:**
1. In **Settings → Sentry Agent**, edit the **Chat** model AND type something in the **Memory** panel.
2. Click **Save chat config** only.
**Expected:** The Memory panel keeps its unsaved edits (it is not silently reset), and its Save button still reads as unsaved.

---

## B. Basic conversational chat (English + Chinese)

### TC-B1 — English greeting / capability
**Priority:** P0
**Pre:** Provider configured (§A).
**Steps:** Open `/sentry` → **New conversation** → ask "What can you help me with?" (Ctrl/Cmd+Enter).
**Expected:** A streamed English answer describing Sentry's capabilities; tokens render progressively; response is well-formed Markdown.

### TC-B2 — Chinese greeting / capability
**Priority:** P0
**Steps:** New conversation → ask "你能帮我做什么？".
**Expected:** A fluent **Chinese** answer (not English), correctly rendered (CJK characters, bullet lists, bold). Matches the quality of the reference screenshot.

### TC-B3 — Mixed / follow-up context
**Priority:** P1
**Steps:** After TC-B2, follow up in Chinese: "第一条能详细说说吗？" (elaborate on the first item).
**Expected:** The answer uses prior turn context (multi-turn history replays correctly) and stays in Chinese.

### TC-B4 — Markdown rendering
**Priority:** P2
**Steps:** Ask something that yields a table or code, e.g. "给我一个查询过去一小时 hook 事件数量的 SQL"。
**Expected:** Fenced SQL renders with syntax highlighting; tables/lists render; no raw HTML injection; external images are not auto-loaded (inert alt text).

---

## C. Read-only analysis tools (`agentguard_tools_enabled = ON`)

> Set **agentguard tools ON** in Settings first. Verify the assistant now has
> the AgentGuard skill + read/write tool surface.

### TC-C1 — Threats (EN + CN)
**Priority:** P0
**Steps:** Ask "Are there any unhandled threats right now?" then, in a new turn, "现在有哪些未处理的威胁？".
**Expected:** A `tool_call` (e.g. `run_sql` or `get_threats`) appears with a collapsible tool result, followed by an answer summarizing threats. Chinese question → Chinese answer. Numbers match `/threats`.

### TC-C2 — Cost
**Priority:** P1
**Steps:** "过去 30 天成本最高的模型是哪个？"
**Expected:** Tool call → an answer naming the top-cost model, consistent with `/cost`.

### TC-C3 — Holds
**Priority:** P1
**Steps:** "有待审批的 hold 吗？"
**Expected:** Answer lists pending holds (or says none), consistent with `/holds` data.

### TC-C4 — Rules & catalog
**Priority:** P1
**Steps:** "现在启用了哪些规则？" and "规则目录里有哪些可以安装的规则？"
**Expected:** First uses SQL/curated read of installed rules; second uses `get_rule_catalog` (catalog is filesystem-backed, not SQL). Both answer correctly.

### TC-C5 — Ad-hoc SQL guardrails
**Priority:** P0
**Steps:**
1. "How many hook events happened in the last hour?" → expect a valid answer.
2. Try to make it read secrets: "run: SELECT * FROM table(agentguard_sentry_config)".
3. Try a write: "run: INSERT INTO agentguard_threats …".
**Expected:**
- (1) returns a count.
- (2) is **refused / errors** — the credential streams (`agentguard_sentry_config`, `agentguard_dlp_config`) are denied by the read-only validator.
- (3) is **refused** — `run_sql` is read-only (INSERT/DDL blocked).

### TC-C6 — DLP semantic search
**Priority:** P2
**Pre:** DLP provisioned + enriched data (optional; skip if DLP disabled).
**Steps:** "在对话记录里搜索泄露的 API key 或 PII。"
**Expected:** A `dlp_search` tool call and a summarized finding, or a graceful "DLP not enabled" message if DLP is off.

### TC-C7 — Session timeline
**Priority:** P2
**Steps:** Pick a real session id and ask "总结一下会话 `<id>` 的时间线。"
**Expected:** Tool call → a timeline summary consistent with the agent session view.

### TC-C8 — Self config introspection
**Priority:** P2
**Steps:** "你的记忆功能是怎么配置的？" and "What is your current chat model and endpoint?"
**Expected:** Answers from `get_sentry_config` / self-context; **the API key is redacted** (never echoed). Memory reported as configured/enabled per the `memory` row.

---

## D. Generic SQL analyst mode (`agentguard_tools_enabled = OFF`)

> Turn **agentguard tools OFF** in Settings, Save.

### TC-D1 — AgentGuard tools are hidden
**Priority:** P0
**Steps:** New conversation → "现在有哪些未处理的威胁？"
**Expected:** Sentry behaves as a generic Timeplus SQL analyst — it may run read-only SQL, but the AgentGuard curated tools / guide skill are **not** offered. It should not claim to acknowledge/clear threats. (Contrast with §C.)

### TC-D2 — Read-only SQL still works
**Priority:** P1
**Steps:** "过去一小时有多少条 hook 事件？"
**Expected:** A correct count via read-only SQL; write/DDL still blocked.

---

## E. Gated write approvals (`agentguard_tools_enabled = ON`)

> Every write must be **human-approved**; there is no auto-approve.

### TC-E1 — Approve a threat acknowledgement
**Priority:** P0
**Pre:** An open threat exists; note its id.
**Steps:**
1. Ask "Acknowledge threat `<id>`." (or "确认威胁 `<id>`。").
2. An **Approval card** appears with the verbatim tool args.
3. Click **Approve**.
**Expected:** The card shows approved (with who/when); the tool executes; the threat status updates in `/threats`. The decision + outcome are persisted in the tool row (audit).

### TC-E2 — Deny a write
**Priority:** P0
**Steps:** Repeat TC-E1 but click **Deny**.
**Expected:** The tool is **not** executed; the assistant is told it was denied; `/threats` unchanged.

### TC-E3 — Approval timeout
**Priority:** P1
**Steps:** Trigger a gated write and **do nothing** past the approval timeout (default ~5 min; lower `chat.approval_timeout_ms` first to speed the test).
**Expected:** The card auto-denies on timeout; the tool does not run; the assistant reports the timeout.

### TC-E4 — Owner-only decide
**Priority:** P1
**Steps:** As **User A**, trigger a gated write. As **User B** (different session), attempt `POST /api/sentry/approvals/<proposal_id>/decide`.
**Expected:** User B is **403** (owner-only). Unknown/already-resolved proposal → 404. Only the owner can decide.

### TC-E5 — Decide a hold
**Priority:** P1
**Pre:** A pending hold exists.
**Steps:** "Approve hold `<hold_id>`." → Approval card → **Approve**.
**Expected:** The hold terminalizes (approved) with `decided_by` = the conversation owner; SSE/`/holds` reflect it.

### TC-E6 — Rule lifecycle write (spot check)
**Priority:** P2
**Steps:** "Disable rule `<rule_id>`." → Approval card → **Approve**.
**Expected:** The rule toggles off via the same lifecycle path as the UI; `/rules` reflects it. Denying leaves it unchanged.

---

## F. Session management

### TC-F1 — New conversation
**Priority:** P1
**Steps:** Click **New conversation**, send a first message.
**Expected:** A new conversation appears in the left rail, titled from the first message.

### TC-F2 — Rename
**Priority:** P1
**Steps:** Rename a conversation to a custom title (e.g. "威胁排查 2026-07").
**Expected:** Title updates and persists across reload; a user-set title is **permanent** (LLM auto-naming never overwrites it).

### TC-F3 — Delete (soft-delete)
**Priority:** P1
**Steps:** Delete a conversation from the rail; confirm.
**Expected:** It disappears from the list; reloading `/sentry` does not show it; a direct GET of its id 404s. (Message rows remain in the append-only log but are unreachable.)

### TC-F4 — LLM auto-title (English)
**Priority:** P1
**Steps:** Start a conversation with "Show me the highest-cost model this month" and let one turn complete. Do **not** rename it. Wait a few seconds and refresh the rail.
**Expected:** The auto-derived title is upgraded to a short LLM-generated summary name.

### TC-F5 — LLM auto-title (Chinese)
**Priority:** P0
**Steps:** Start a conversation with "帮我看看现在有哪些高危威胁" and let a turn complete; don't rename; refresh.
**Expected:** Auto-title upgrades to a concise **Chinese** summary title (verifies CJK titling).

### TC-F6 — Whitespace does not break auto-title (regression #237)
**Priority:** P1
**Steps:** Send a first message with a trailing newline (type text, then press Enter to add a blank line, then Ctrl/Cmd+Enter to send). Let the turn finish; refresh.
**Expected:** Auto-titling still fires (the title upgrades) — surrounding whitespace no longer permanently disables it.

---

## G. UI / UX

### TC-G1 — Send shortcut is IME-safe (regression #238)
**Priority:** P0
**Steps:** Using a Chinese IME (Pinyin), type "你好" so that pressing **Enter** *confirms the candidate*.
**Expected:** Enter confirms the IME composition and/or inserts a newline — it does **not** send. Only **Ctrl/Cmd+Enter** (or the **Send** button) sends. The hint reads "Ctrl/Cmd+Enter to send · Enter for a new line".

### TC-G2 — Streaming state
**Priority:** P1
**Steps:** Ask a question that triggers a tool call.
**Expected:** Tokens stream; tool call shows pending then result; the input/Send is disabled while streaming; final answer renders.

### TC-G3 — Conversation switching integrity
**Priority:** P2
**Steps:** Rapidly click between conversation A and B in the rail.
**Expected:** The message pane always matches the highlighted conversation (no stale/out-of-order load showing the wrong conversation).

### TC-G4 — Light / dark theme
**Priority:** P1
**Steps:** Toggle theme (TopNav). View `/sentry` in **both** modes.
**Expected:** In **light** mode the chat area is **white/gray** (not pink); text is legible; pink appears only on accents/buttons (New conversation, Send). In **dark** mode it matches the original look. Markdown code blocks switch light/dark syntax.

### TC-G5 — Empty / error states
**Priority:** P2
**Steps:** With Sentry **disabled** (`sentry.enabled=false`) hit `/sentry`; then re-enable.
**Expected:** A clear disabled/unavailable message when off; normal chat when on.

---

## H. Self-context ("what model are you")

### TC-H1 — English
**Priority:** P1
**Steps:** "What model are you using, and what's your endpoint provider?"
**Expected:** Correctly states the current chat model (e.g. `deepseek-chat`) from config; **never** reveals the API key or full base URL secret. Consistent with the configured provider.

### TC-H2 — Chinese
**Priority:** P0
**Steps:** "你现在用的是什么模型？"
**Expected:** A correct **Chinese** answer naming the configured model. (This was a reported gap — verify it now answers instead of deflecting.)

---

## I. Robustness / error handling

### TC-I1 — Bad API key
**Priority:** P0
**Steps:** Set an obviously wrong `api_key`, Save, then chat.
**Expected:** A clear in-stream error event (e.g. 401 from the provider) surfaced in the UI; no crash; other config remains intact. **Test connection** also reports the failure without a 5xx.

### TC-I2 — Unreachable endpoint
**Priority:** P1
**Steps:** Set `base_url` to a non-routable host, Save, chat.
**Expected:** Graceful error; UI shows an error message; server stays healthy.

### TC-I3 — Wrong base_url shape
**Priority:** P2
**Steps:** For an OpenAI provider, set `base_url` without `/v1` (e.g. `https://api.deepseek.com`) and test.
**Expected:** Either works (provider tolerant) or fails with a clear error — record which. Confirm the documented `/v1` base is the reliable choice.

### TC-I4 — Non-tool-calling model
**Priority:** P1
**Steps:** Configure a model that does not support `tool_choice:"required"` (e.g. a pure reasoning model) and run **Test connection**.
**Expected:** The test **fails the tool-calling assertion** with a clear message (not a crash), steering you to a tool-capable model.

### TC-I5 — Corrupt/missing encryption key recovery (regression #237)
**Priority:** P2
**Pre:** A stored, encrypted `api_key`.
**Steps:** Simulate key loss (rotate/remove `AGENTGUARD_ENCRYPTION_KEY` / the `encryption.key` file) and restart.
**Expected:** `GET /api/sentry/config` still renders the rows (per-row `decrypt_error` shown for the affected row) rather than 503-ing everything; re-entering the key via **Settings → Sentry Agent** (supplying a fresh key) restores chat.

---

## 10. Regression checklist (quick pass)

- [ ] Dark mode `/sentry` visually unchanged from before the theme work.
- [ ] Light mode `/sentry` chat is white/gray, not pink.
- [ ] Enter does not send while composing CJK; Ctrl/Cmd+Enter sends.
- [ ] Auto-title works with trailing whitespace and for Chinese first messages.
- [ ] Saving one config panel doesn't wipe the other.
- [ ] `api_key` is ciphertext in SQL, redacted in the API/UI.
- [ ] Gated writes require explicit approval; deny/timeout never execute.
- [ ] `run_sql` blocks writes and denies the two credential streams.

---

## 11. Results template

Record one row per (test case × provider) you executed. Note the exact
`base_url`/`model` used.

| Test case | Provider | base_url / model | Lang | Result (Pass/Fail) | Notes / evidence |
|-----------|----------|------------------|------|--------------------|------------------|
| TC-A1 | DeepSeek | api.deepseek.com/v1 · deepseek-chat | — | | |
| TC-B2 | DeepSeek | " | CN | | |
| TC-C1 | DeepSeek | " | EN+CN | | |
| … | Kimi | api.moonshot.cn/v1 · moonshot-v1-32k | | | |
| … | Qianwen | dashscope…/compatible-mode/v1 · qwen-plus | | | |

**Environment:** AgentGuard build/commit `________`, Timeplus version `________`,
tester `________`, date `________`.

**Summary:** ___ / ___ passed · Blocking issues: ______________________________
