# Sentry — SQL & data authorization

How AgentGuard controls **which SQL a user can run and which data they can see**
through the Sentry agent (the `run_sql` tool, the curated read tools, and DLP
search). This is a layered model: AgentGuard gates *access to Sentry*, a
validator constrains the *shape* of SQL, and **Timeplus-native grants govern the
*scope* of data** — because Sentry runs SQL as the logged-in user, not as a
shared service account.

> Related: [`docs/sentry.md`](sentry.md) (operator guide), access control
> (RBAC) — issue #243 / the RBAC PR, and issue #245 (this doc).

---

## TL;DR — "Sentry runs SQL as you"

When you chat with Sentry, every query it runs against Timeplus executes with
**your** Timeplus credentials — the username and password you logged into
AgentGuard with. There is no privilege escalation through a service account.

Concretely (`backend/internal/…`):

- `auth.RequestCfg(c, cfg)` copies the session's `timeplus_user` /
  `timeplus_password` into the per-request config (`auth/middleware.go`).
- `SentryChat` builds the agent loop's `RunContext.Cfg` from that per-request
  config (`handlers/sentry.go`).
- `run_sql` runs `tp.QueryNative(&rc.Cfg.Timeplus, …)` (`sentry/skills/runsql.go`);
  the curated reads (`skills/curated.go`) and DLP search (`skills/dlpsearch.go`)
  query the same way; the conversation store reads/writes the Sentry streams
  with those same credentials (`sentry/store.go`).

**Implication:** to control what a person can read through Sentry, control that
person's **Timeplus grants**, and have them log into AgentGuard with those
credentials. If you log in as a Timeplus superuser (e.g. the default `proton`),
`run_sql` can read everything that user can — which in a fresh dev/test stack is
everything.

---

## The three authorization layers

| Layer | Controls | Where | Lever |
|-------|----------|-------|-------|
| 1. AgentGuard RBAC | *Access* — can this user use Sentry at all? | `internal/rbac` (#243) | `ui:sentry`, `api:sentry:read/write` on the user's role |
| 2. SQL validator | *Shape* — is the SQL read-only and well-formed? | `ValidateReadOnlySQL` (`skills/runsql.go`) | Fixed rules (not configurable) |
| 3. Timeplus grants | *Scope* — which streams/rows can this user read? | Timeplus Enterprise RBAC | `GRANT SELECT ON <stream> TO <role>` |

```
 user ──▶ [1] AgentGuard RBAC gate ──▶ [2] SQL validator (shape) ──▶ [3] Timeplus grants (data scope)
          can reach Sentry?              read-only, table()-bounded,     runs as the user's Timeplus
          ui:/api:sentry                 denied credential streams        identity — grants decide the data
```

### Layer 1 — AgentGuard RBAC (access gate)

Whether a user can open Sentry and run its tools is governed by AgentGuard's
role-based access control (#243):

- `ui:sentry` — see the Sentry page.
- `api:sentry:read` — view existing conversations.
- `api:sentry:write` — start conversations and run tools (`run_sql`, etc.).

A role without these can't use Sentry. The built-in **viewer** role has
`ui:/api:sentry:read` but not `:write`, so it can read past conversations but not
run new queries. Manage this in **Access Control** (admin-only).

### Layer 2 — the SQL validator (shape, not scope)

`ValidateReadOnlySQL` restricts the *form* of every `run_sql` query. It is a
fixed security boundary, not operator-configurable:

- **Read-only** — must start with `SELECT`/`WITH`; `INSERT`/`ALTER`/`CREATE`/
  `DROP`/… and `SETTINGS`/`OUTFILE`/`INFILE` are rejected.
- **Historical reads are `table()`-bounded** — bare streaming reads are refused;
  `system.*` is limited to four diagnostic tables (`tables`, `columns`,
  `functions`, `v_failed_mat_views`), enforced even inside `table(...)`.
- **Credential streams are denied** — `agentguard_sentry_config` and
  `agentguard_dlp_config` cannot be read via `run_sql` (their API keys are also
  encrypted at rest).
- **Network UDFs are denied** — `dlp_*` functions (which call out to model
  endpoints) can't be invoked from `run_sql`.
- **LIMIT is injected/clamped** to bound result size.

**What it does *not* do:** it does not limit *which* application streams a valid
read-only query may touch. Within the rules above, the model can query any
stream the user's Timeplus grants allow. That scoping is Layer 3.

### Layer 3 — Timeplus grants (the data-scope lever)

Because Sentry runs SQL as the user, **Timeplus RBAC is where you decide what
data a Sentry analyst can actually read.** Give a person a least-privilege
Timeplus role, grant it `SELECT` only on the streams they should analyze, and
have them log into AgentGuard with that Timeplus user.

---

## Credential model: service account vs. session user

AgentGuard uses two identities. Knowing which is which matters for locking down
data access:

| Identity | Used for | Credentials |
|----------|----------|-------------|
| **Session user** | All interactive reads — the whole UI (dashboard, agents, threats, cost, rules, DLP, Investigate) **and Sentry** (`run_sql`, curated tools, DLP search, conversation storage) | The user's own `timeplus_user` / password, via `RequestCfg` |
| **Service account** | Server-to-server paths: alert webhook, plugin-driven holds, and RBAC metadata (`agentguard_roles` / `agentguard_user_roles`) | `TIMEPLUS_USER` / `TIMEPLUS_PASSWORD` env |

So: interactive data access (including everything Sentry reads for you) is bound
by **your** grants; only the background/service plumbing uses the service
account.

---

## Restricting a Sentry analyst's data access (worked example)

Goal: an analyst who can use Sentry to investigate **threats, rules, and agent
activity**, but cannot read cost or DLP data.

1. **Create a least-privilege Timeplus role and user** (run in the Timeplus
   console; adjust to your Timeplus Enterprise version's RBAC syntax):

   ```sql
   CREATE ROLE sentry_analyst;

   -- Data the analyst may analyze:
   GRANT SELECT ON default.agentguard_cim_event      TO sentry_analyst;
   GRANT SELECT ON default.agentguard_cim_metrics    TO sentry_analyst;
   GRANT SELECT ON default.agentguard_hook_events    TO sentry_analyst;
   GRANT SELECT ON default.agentguard_security_events TO sentry_analyst;
   GRANT SELECT ON default.agentguard_threats        TO sentry_analyst;
   GRANT SELECT ON default.agentguard_rules          TO sentry_analyst;
   GRANT SELECT ON default.agentguard_holds          TO sentry_analyst;
   -- (NOT granted: agentguard_cost_budgets, agentguard_pricing,
   --  agentguard_dlp_enriched → the analyst can't read cost or DLP data.)

   -- Streams Sentry itself needs on the user's behalf (see next section):
   GRANT SELECT           ON default.agentguard_sentry_config        TO sentry_analyst;
   GRANT SELECT, INSERT   ON default.agentguard_sentry_conversations TO sentry_analyst;
   GRANT SELECT, INSERT   ON default.agentguard_sentry_messages      TO sentry_analyst;

   CREATE USER alice IDENTIFIED BY 'a-strong-password';
   GRANT sentry_analyst TO alice;
   ```

2. **In AgentGuard:** assign `alice` an AgentGuard role that includes
   `ui:sentry` + `api:sentry:read/write` (Access Control → Users). This is Layer 1.

3. **Have `alice` log into AgentGuard with her Timeplus credentials.** Now when
   she asks Sentry to "show cost by model", `run_sql`'s query against
   `agentguard_cost_budgets` / `agentguard_pricing` fails at the Timeplus grant
   check — Sentry surfaces the error and cannot read that data. Threats/rules/
   agent queries work.

> The exact grant model (roles, `GRANT … ON db.table`, row policies) follows
> your Timeplus Enterprise version. Row-level policies in Timeplus also apply,
> so you can scope by rows (e.g. a deployment) as well as by stream.

## Minimum grants a Sentry user needs

For Sentry to function at all, the user's Timeplus role must be able to read the
streams Sentry touches on their behalf. Minimum for the **read** experience:

- `SELECT` on **`agentguard_sentry_config`** — the chat loop loads its model
  endpoint from here (`LoadChatCfg`). (The `api_key` is encrypted at rest, so
  granting `SELECT` does not expose it.)
- `SELECT, INSERT` on **`agentguard_sentry_conversations`** and
  **`agentguard_sentry_messages`** — Sentry persists and replays conversations
  under the user's identity.
- `SELECT` on whatever **data streams** you want them to analyze (see the
  example). The curated tools specifically read `agentguard_threats`,
  `agentguard_rules`, `agentguard_holds`, `agentguard_cim_event`,
  `agentguard_cim_metrics`, `agentguard_pricing`; DLP search reads
  `agentguard_dlp_enriched` (+ `agentguard_dlp_config`).

If you also enable Sentry's **gated write tools** (`agentguard_tools_enabled`;
e.g. acknowledge/clear threat, decide hold, rule lifecycle), the user's Timeplus
role additionally needs `INSERT` on the corresponding streams
(`agentguard_threats`, holds, `agentguard_rules`) — those writes also run under
the user's identity, after the in-app approval gate.

## What the validator guarantees — and what it doesn't

**Guarantees:** every `run_sql` query is read-only, `table()`-bounded for
historical reads, can't read the credential config streams, can't invoke network
UDFs, and is LIMIT-bounded.

**Does not:** decide *which* application streams a valid query may read (that's
Timeplus grants), and there is no per-query human approval for **reads** — reads
execute directly within the user's grants. Human approval applies only to the
**gated write** tools.

## Recommendations

- **Don't give Sentry users superuser Timeplus credentials** if you want to
  bound their reach. Create scoped Timeplus roles and log in as those.
- Use **AgentGuard RBAC** (Layer 1) to decide *who* gets Sentry at all, and
  **Timeplus grants** (Layer 3) to decide *what data* each of them can reach.
- Treat the two together: RBAC without scoped Timeplus grants still lets a
  Sentry user read anything their Timeplus identity can.

## Possible future enhancements (not implemented)

Captured in issue #245 for discussion — not part of the current model:

- A per-role / per-deployment **stream allowlist** for `run_sql`
  (defense-in-depth on top of Timeplus grants).
- Surfacing the effective Timeplus identity in the Sentry UI ("running as
  `<user>`") so the active scope is visible.
- An optional **read-approval** or redaction mode for designated sensitive
  streams.
