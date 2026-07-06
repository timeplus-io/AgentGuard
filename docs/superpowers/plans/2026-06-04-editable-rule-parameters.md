# Editable Rule Parameters Implementation Plan

> **For agentic workers:** Implement this plan in order. Keep each task reviewable and do not mix runtime lookup-set work into this branch.

## Phase Split (2026-06-09 revision)

This branch implements **Phase A only**. Phase B ships separately on the same architecture.

**Phase A (this branch):**

- Parameter types: `integer`, `number`, `boolean`, `string`, `enum`, `string_list`. (`regex` and `duration` are deferred; the type system must remain open for them.)
- Catalog `Definition` model, `sql_template`, parameter schema validation, canonical hashing, mechanical `query_sql` → `sql_template` rename.
- Strict compiler (validate/render/compatibility) and upgrade override preservation.
- New `agentguard_rules` snapshot schema with `snapshot_id`/`revision`; clean provisioning; no catalog seeding.
- Snapshot store + lifecycle service with per-rule serialization, candidate preflight, `NO DELAY` MV replacement, and rollback.
- API: `GET /api/rules/:id` (schema/defaults/overrides/effective/rendered), `POST /api/rules/:id/install` with typed overrides, `PUT /api/rules/:id/configuration`, bodyless `POST /api/rules/:id/upgrade`, plus existing toggle/policy/uninstall/sync through the service.
- `POST /api/rules` is retained as a thin create-custom-rule path: it creates a zero-parameter custom definition (origin `custom`, template ≡ rendered SQL) and installs it through the same lifecycle service. No raw-SQL editing of installed rules. Catalog-file-backed custom definition storage is acceptable only if the current implementation already has an equivalent; otherwise the definition content lives in the instance snapshot.
- Frontend: Configuration tab in `RuleDetail.jsx` with shared parameter controls under `frontend/src/components/rules/`; `client.js` updates. `RuleCreate.jsx` is minimally adapted to the surviving `POST /api/rules` contract — no authoring-editor refactor.
- Representative parameterization: `rp-003` (`string_list` + `integer`), one metrics rule with a numeric threshold, one no-parameter golden rendering case. (`duration`/`regex` examples deferred with their types.)

**Phase A additions from the 2026-06-09 Proton source review (proton-enterprise 3.3.1-rc.13):**

- MV health checks that consult `system.v_failed_mat_views` MUST exclude MVs that are intentionally paused (disabled rules); that view flags any MV not `Executing` in the last 5 minutes, so a permanently paused MV for a disabled rule is a false positive, not a failure.
- `SYSTEM PAUSE/RESUME MATERIALIZED VIEW ... PERMANENT` requires the `SYSTEM_SHUTDOWN` privilege. Setup diagnostics must check for it by name.
- Catalog-authoring docs must state that `string` parameters interpolated into `LIKE`/`ilike` patterns are NOT wildcard-escaped; templates that need substring matching against operator text must use `position()`/`multi_search_any` instead. The renderer escapes quotes and backslashes only.

**Phase B (deferred, do not implement on this branch):**

- Custom Rule Definition authoring editor (full `RuleCreate.jsx` refactor: template/schema/UDF editing, compile preview).
- Clone-to-custom workflow and `POST /api/rules/catalog/:id/clone`.
- `GET /api/rules/catalog/:id`, `POST /api/rules/catalog`, `PUT /api/rules/catalog/:id`, `POST /api/rules/catalog/compile`, and the staged catalog-file publish/rollback flow.
- `regex` and `duration` parameter types and their UI controls.

Sections below describe the full target architecture; where they conflict with this split, the split wins for this branch.

> **Salvage note:** an earlier draft implementation is preserved at branch `wip/33-editable-rule-params-draft`. Its `store.go` uses `arg_max(..., updated_at)` ordering (superseded — use `revision`), but its mechanical catalog seed rename and frontend parameter field component may be salvageable. Verify against this plan before reusing anything.

**Goal:** Make typed, opt-in rule parameters the primary way operators customize installed rules. Rule definitions own versioned SQL templates and parameter schemas; installed rule instances own deployment overrides and last-known-good rendered SQL.

**Architecture:** Catalog YAML becomes the authoritative `RuleDefinition` store. A new compiler validates typed overrides and renders deterministic SQL from strict `{{params.<id>}}` placeholders. The append-only `agentguard_rules` stream becomes the complete `RuleInstance` snapshot log, ordered by a per-rule monotonic `revision` rather than timestamp ties. All install, configure, toggle, upgrade, custom-definition update, and uninstall operations go through one lifecycle service that validates first, changes the MV second, and appends the new instance snapshot last.

**Compatibility baseline:** This release does not preserve the existing `agentguard_rules` schema or rule API payloads. Existing deployments must cleanly reprovision rule resources. Do not add `ALTER STREAM` migrations, compatibility readers, dual writes, or legacy `query_sql` aliases.

---

## Final Decisions

- Every rule may declare parameters; there is no separate parameterized-rule type.
- A definition with no parameters is valid and renders unchanged.
- Catalog definitions use `sql_template`; deployed instances use `rendered_sql`.
- Operators can change only explicitly declared typed parameters. No parameter type may inject identifiers, operators, clauses, functions, or arbitrary SQL fragments.
- Built-in and imported definitions are immutable through the rule-instance UI. Structural changes require cloning into a custom definition.
- Custom definitions use the same schema, compiler, validation, install, and upgrade lifecycle as built-in definitions.
- Deployment overrides live only in `agentguard_rules.parameter_overrides_json`; they are never written into catalog YAML.
- `agentguard_rules` stores complete append-only instance snapshots. Do not create a separate `agentguard_rule_configs` stream.
- Rule Instance current state is ordered by per-rule `revision`, not by `updated_at`. Proton's `arg_max` is order-dependent and returns the first argument for a tied max value, so millisecond timestamp ordering is not sufficient for correctness.
- A Rule Instance stores the deployed UDF DDL alongside `rendered_sql`; rollback and restart/sync must not depend on whichever definition version is currently in the catalog.
- Rule instance mutations must preserve every field through a single snapshot writer. Handlers must not construct partial `INSERT INTO agentguard_rules` statements.
- Rule instance mutations must be serialized per `rule_id` before any compile/preflight/MV/snapshot sequence begins. A single-process backend may use an in-process lock; multi-replica deployments require a deployment-wide lock or single-writer rule-management mode before this feature is safe.
- MV replacement is application-level transactional behavior: validate first, replace second, persist last, and restore the previous MV on any failure.
- Proton DDL must not silently reuse stale rule MVs. Replacement paths drop with `NO DELAY` and create without `IF NOT EXISTS`; idempotent startup/sync may use existence checks before create.
- Timeplus UDF names are global. Rule definitions must use rule-scoped UDF names, and catalog validation must reject collisions across installed/current definitions before preflight or deployment.
- MV health checks must include Proton's failed-materialized-view state, not only `system.tables` existence.
- Runtime Lookup Sets remain part of the target architecture, but are deferred to a follow-up branch. This branch must not add `lookup_ref`, mutable lookup streams, or runtime parameter joins.
- Existing universal settings remain `enabled`, `block_policy`, and effective `severity`. New suppression and deployment/agent-scope models are deferred.

## Scope

**In scope**

- Typed definition parameters and constraints.
- Strict SQL template rendering and escaping.
- Explicit Rule Definition and Rule Instance models.
- Clean-provisioned `agentguard_rules` snapshot schema.
- Centralized instance persistence and MV lifecycle orchestration.
- Deterministic snapshot reconstruction, per-rule mutation serialization, and Proton-safe MV replacement semantics.
- Parameter editing, reset-to-default, upgrade compatibility, and audit metadata.
- Read-only rendered SQL for installed rules.
- Custom-definition authoring and clone-to-custom workflow.
- Representative parameterized catalog rules and full lifecycle tests.

**Out of scope**

- Backward-compatible schema/API behavior or data migration.
- Runtime Lookup Sets and `lookup_ref`.
- Arbitrary raw SQL editing on installed rules.
- Automatically exposing existing SQL literals as parameters.
- Parameterizing the full catalog in this branch.
- New suppression, deployment scope, or agent scope models.
- Secret values; parameters are persisted and rendered into SQL and must never be used as a secret store.

---

## Domain Contracts

### Rule Definition

`backend/internal/catalog` owns versioned definition content:

- ID, name, version, description, metadata, tags, and UDFs.
- `sql_template`.
- Ordered parameter schema and defaults.
- Definition origin: built-in, imported, or custom.
- A deterministic SHA-256 `definition_hash` computed from canonical definition content.

Rename the vague catalog `Entry` model to `Definition`. The catalog manager must validate definitions when loading embedded YAML, loading local YAML, importing archives, saving custom definitions, and cloning definitions.

Embedded definitions are always `builtin`. Imported definitions are `imported`. Definitions authored or cloned in AgentGuard are `custom`. Only `custom` definitions may be structurally edited in place.

Definition validation must parse declared UDF DDL enough to extract function names. UDF names must be deterministic and rule-scoped, for example `ag_<rule_id_normalized>_<purpose>`. A custom/imported definition may not declare a UDF name already owned by another installed definition because Proton `CREATE OR REPLACE FUNCTION` changes global function state.

### Rule Instance

An installed Rule Instance is deployment-local state:

- Definition identity: `rule_id`, `definition_version`, and `definition_hash`.
- Snapshot identity: `snapshot_id` and per-rule monotonic `revision`.
- Last-known-good deployment artifacts: `rendered_sql` and `deployed_udfs_json`.
- Operator-owned `parameter_overrides_json`.
- Universal state: `enabled`, `block_policy`, effective `severity`, and `deleted`.
- Definition metadata copied into the snapshot so instance reads remain complete and auditable.
- Audit metadata: `update_reason`, `updated_by`, and `updated_at`.

Current state is reconstructed with `arg_max(..., revision) GROUP BY rule_id`. Older snapshots retain removed overrides and previous rendered SQL for audit and rollback analysis. `updated_at` is audit metadata only and must not be the current-state ordering key.

### Parameter Schema

Use this catalog shape:

```yaml
parameters:
  - id: dangerous_tools
    label: Dangerous tools
    description: Tool names that should enter the high-risk branch.
    type: string_list
    default: [bash, shell, exec, run_terminal_cmd, computer]
    min_items: 1
    max_items: 64

  - id: command_chain_threshold
    label: Maximum chained commands
    description: Number of &&-separated commands that triggers detection.
    type: integer
    default: 15
    min: 1
    max: 100

sql_template: |
  ...
  lower(tool_name) IN {{params.dangerous_tools}}
  OR ((length(tool_input) - length(replace_all(tool_input, '&&', ''))) / 2)
     >= {{params.command_chain_threshold}}
```

Supported types:

| Type | Canonical value | SQL rendering | Constraints |
|---|---|---|---|
| `integer` | signed 64-bit integer | bare decimal integer | `min`, `max` |
| `number` | finite float64 | canonical decimal, never NaN/Inf | `min`, `max` |
| `boolean` | boolean | `true` or `false` | none |
| `string` | string | escaped single-quoted literal | `min_length`, `max_length` |
| `enum` | string | escaped single-quoted literal | non-empty `options` |
| `string_list` | unique ordered strings | parenthesized escaped literal list | `min_items`, `max_items`, item length |
| `regex` | string | escaped single-quoted literal | Go RE2 validation and length limit |
| `duration` | integer in a definition-fixed unit | bare decimal integer | `unit`, `min`, `max` |

Rules for the schema and renderer:

- Parameter IDs and aliases must match `^[a-z][a-z0-9_]{0,63}$`.
- Parameter IDs are compatibility contracts. Renames require aliases on the replacement parameter.
- Every parameter must have a valid default so install-with-defaults always compiles.
- Every declared parameter must appear in `sql_template`; every placeholder must resolve to one declared parameter.
- A placeholder may appear multiple times.
- `string_list` values must be non-empty in this release; empty-list SQL semantics are rejected rather than guessed.
- Regex values must compile with Go's `regexp` package, which uses RE2 semantics.
- The renderer accepts only exact `{{params.<id>}}` placeholders. It must reject template actions, conditionals, functions, unresolved placeholders, and unknown namespaces.
- The compiler normalizes values before rendering and stores only normalized overrides.
- Effective values are `defaults + overrides`; only values different from the current default are persisted as overrides.

### Upgrade Compatibility

When applying a newer definition:

- Preserve an override when the same parameter ID exists with the exact same type and the value satisfies the new constraints.
- Apply a new parameter's catalog default.
- Map a renamed parameter through one explicit alias, then persist the override under the new ID.
- Remove overrides for removed parameters from the new snapshot, return a warning, and rely on prior append-only snapshots for audit history.
- Block the upgrade with `409 Conflict` when an override's type changed, an alias is ambiguous, or a preserved value violates new constraints.
- Never silently reset an operator override.
- Compute and persist the new `definition_hash` only after successful deployment.

---

## Provisioned Rule Instance Schema

Replace `ddlRulesStream` directly. Do not add an `ALTER STREAM` path.

```sql
CREATE STREAM agentguard_rules
(
  rule_id                    low_cardinality(string),
  snapshot_id                string,
  revision                   uint64,
  name                       string,
  definition_version         string,
  definition_hash            string,
  description                string,
  rendered_sql               string,
  deployed_udfs_json         string,
  parameter_overrides_json   string,
  enabled                    bool,
  deleted                    bool,
  severity                   low_cardinality(string),
  category                   low_cardinality(string),
  required_telemetry         string,
  fp_rate                    low_cardinality(string),
  hook_sources               string,
  block_policy               low_cardinality(string),
  update_reason              low_cardinality(string),
  updated_by                 string,
  updated_at                 datetime64(9)
)
```

Provisioning changes:

- Remove `seedCatalogRules`; definitions must not become instances merely because they exist in the catalog.
- Provision an empty `agentguard_rules` stream.
- Sync only installed, non-deleted Rule Instances.
- Read `rendered_sql` and `deployed_udfs_json`, never current catalog artifacts, when recreating a missing MV.
- Detect incompatible schemas during setup by comparing the complete expected column set and key column types (`snapshot_id`, `revision`, `rendered_sql`, `deployed_udfs_json`, `parameter_overrides_json`, `updated_at`) instead of counting a subset of columns. Fail with a clear clean-reprovisioning instruction instead of continuing against incompatible columns.

---

## Proton Compatibility Requirements

The plan assumes Proton Enterprise supports the core primitives used by the service:

- `CREATE STREAM IF NOT EXISTS` for provisioning.
- `CREATE MATERIALIZED VIEW ... INTO <stream> AS <select>` for rule MVs and validation MVs.
- `DROP VIEW IF EXISTS ... NO DELAY` and `DROP STREAM IF EXISTS ... NO DELAY` for immediate cleanup before same-name recreation.
- `SYSTEM PAUSE MATERIALIZED VIEW ... PERMANENT` and `SYSTEM RESUME MATERIALIZED VIEW ... PERMANENT` for disabled/enabled rules.
- SQL UDF DDL through `CREATE OR REPLACE FUNCTION`.
- `system.tables`, `system.columns`, and `system.v_failed_mat_views` for schema and MV health inspection.

DBMS constraints that shape the implementation:

- Proton does not provide a transaction covering UDF DDL, MV DDL, and `agentguard_rules` append. The lifecycle service owns rollback and must serialize mutations per rule.
- Permanent MV pause/resume internally alters the MV query setting `pause_on_start`. Setup and diagnostics must validate the Timeplus user has privileges for stream/view/function DDL, inserts into AgentGuard streams, and `SYSTEM PAUSE/RESUME MATERIALIZED VIEW`.
- `CREATE MATERIALIZED VIEW IF NOT EXISTS` is acceptable only when an idempotent startup/sync path has already checked the existing MV is the intended one. Replacement paths must drop the old MV first and create without `IF NOT EXISTS` so stale MVs cannot be silently reused.
- `arg_max(..., updated_at)` is unsafe for current-state reconstruction because timestamp ties can select an older snapshot. Use `revision`.
- `system.v_failed_mat_views` reports MVs not running in the recent introspection window. Treat this as a health signal for operator visibility and post-deploy verification, but do not use it as the sole source of truth for existence.

---

## Lifecycle Service

Create `backend/internal/rules/` as the domain/service boundary:

| File | Responsibility |
|---|---|
| `types.go` | Parameter, definition, instance, effective configuration, and structured error types |
| `validate.go` | Definition schema validation and typed value normalization |
| `render.go` | Strict placeholder parsing, SQL literal rendering, and definition hashing |
| `compatibility.go` | Upgrade override preservation, aliases, warnings, and conflicts |
| `store.go` | Query current instances and append complete snapshots |
| `service.go` | Per-rule serialization plus install, configure, toggle, upgrade, update custom definition, and uninstall orchestration |

Keep Timeplus DDL operations in `backend/internal/timeplus/rules.go`, but extend that package with candidate validation and explicit replacement helpers.

The service must acquire the per-rule mutation lock before reading the current instance. The next `revision` is computed as `current.revision + 1` under that lock; install starts at revision `1`. If a distributed lock is not available, document that rule-management mutations are single-writer only and reject or disable multi-replica mutation mode.

### Candidate Validation

Before changing a real MV:

1. Validate the definition and overrides.
2. Render deterministic SQL.
3. Create a uniquely named disposable validation sink with the `agentguard_security_events` schema.
4. Create a uniquely named candidate MV targeting that sink.
5. Drop the candidate MV and sink with `NO DELAY` in deferred cleanup.

This preflight validates Timeplus parsing, source references, UDF availability, and required output compatibility without writing candidate detections into `agentguard_security_events`.

Timeplus UDFs are global and definitions currently use `CREATE OR REPLACE`. For an existing instance, candidate preflight must always restore `deployed_udfs_json` before returning, whether validation succeeds or fails. During the real replacement, drop the old MV with `NO DELAY` before applying candidate UDFs; rollback restores the previous UDF DDL before recreating the previous MV. New installs and custom definitions must still pass UDF name ownership validation before candidate preflight.

### Mutation Ordering

For an enabled existing instance:

1. Read the current complete instance snapshot.
2. Compile and preflight the candidate.
3. Drop the current MV with `NO DELAY`.
4. Apply candidate UDFs and create the candidate MV.
5. Append the candidate complete snapshot with the next `revision`.
6. If MV creation or snapshot persistence fails, drop any candidate MV and recreate the previous MV from the previous `rendered_sql` and `deployed_udfs_json`.
7. If rollback also fails, return a structured critical error and leave the persisted instance snapshot unchanged so `/rules/sync` can restore the previous MV.

For a disabled existing instance:

1. Compile and preflight the candidate.
2. Append the candidate snapshot with the next `revision`.
3. Do not create an MV.

For a new install:

1. Compile defaults plus requested overrides and preflight.
2. Create the MV if installing enabled.
3. Append the first instance snapshot with `revision = 1`.
4. Drop the new MV if snapshot persistence fails.

Toggle, policy, custom-definition update, upgrade, and uninstall operations must use the same service and rollback principles. Every operation must preserve all instance fields and increment `revision` exactly once for the accepted snapshot. Hold abandonment remains a post-persistence side effect when disabling a rule or changing its policy to `log_only`.

For an installed custom-definition edit:

1. Validate the candidate definition and write it to a staging file, not the live catalog path.
2. Compile and preflight it against the current instance overrides.
3. Replace the MV when enabled.
4. Atomically publish the staged definition file and update the catalog manager.
5. Append the candidate instance snapshot last.
6. If catalog publish or snapshot persistence fails, restore the previous definition file and MV artifacts. If a failure is detected after the candidate snapshot was accepted, append the previous complete snapshot again with a rollback reason.

---

## API Changes

Retain installed-instance reads under `/api/rules` and definition discovery under `/api/rules/catalog`.

| Method | Path | Behavior |
|---|---|---|
| `GET` | `/api/rules/:id` | Return instance state, parameter schema, defaults, overrides, effective values, rendered SQL, and upgrade conflicts/warnings |
| `POST` | `/api/rules/:id/install` | Install using defaults plus optional typed overrides |
| `PUT` | `/api/rules/:id/configuration` | Replace the full parameter override map; omitted keys reset to defaults |
| `POST` | `/api/rules/:id/upgrade` | Upgrade from catalog only; remove the raw `query_sql` body |
| `GET` | `/api/rules/catalog/:id` | Return the complete Rule Definition for authoring or cloning |
| `POST` | `/api/rules/catalog/compile` | Validate a candidate definition plus overrides and return rendered SQL without persisting or changing an MV |
| `POST` | `/api/rules/catalog` | Create a custom Rule Definition |
| `PUT` | `/api/rules/catalog/:id` | Update a custom definition and safely redeploy its installed instance, if any |
| `POST` | `/api/rules/catalog/:id/clone` | Create an uninstalled custom definition from a built-in/imported/custom source |

API behavior:

- Validation errors return `400` with per-parameter field errors.
- Upgrade compatibility failures return `409` with conflicts and preserved/removed override details.
- MV/preflight failures return `500` without changing the current instance snapshot.
- Timeplus availability failures return `503`.
- Responses expose rendered SQL for inspection, but installed rules have no raw SQL edit endpoint.
- `updated_by` is taken from the authenticated `timeplus_user` context.
- Responses expose `snapshot_id`, `revision`, and MV health (`exists`, `failed_state`, `last_failed_at` when available) for diagnostics.

Remove the current `POST /api/rules` create-and-install shortcut and the ability for `POST /api/rules/:id/upgrade` to accept arbitrary `query_sql`.

---

## Frontend Changes

### Rule Detail

Update `frontend/src/pages/RuleDetail.jsx`:

- Add a `Configuration` tab before Detection SQL.
- Render controls from the backend parameter schema.
- Show default, effective value, and whether each value is overridden.
- Support field-level validation, reset-one, reset-all, save, and unsaved-change protection.
- Keep Detection SQL read-only and show `rendered_sql`.
- Show upgrade conflicts before the operator confirms an upgrade.
- Add `Clone as custom` for structural customization.

Use small shared controls under `frontend/src/components/rules/` for scalar, enum, list, regex, and duration parameters. Do not put parameter-type branching directly into `RuleDetail.jsx`.

### Definition Authoring

Refactor `frontend/src/pages/RuleCreate.jsx` into the custom Rule Definition editor:

- Edit `sql_template`, parameter schema, defaults, metadata, and UDFs.
- Preview the defaults-rendered SQL through the backend compiler.
- Test only compiled SQL; the browser must not implement its own renderer.
- Support clone-prefilled authoring and custom-definition edits.
- Require an explicit version change for edits to an installed custom definition.

### API Client

Update `frontend/src/api/client.js` for definition reads/writes, cloning, configuration replacement, compile preview, and the bodyless catalog upgrade operation.

---

## Representative Catalog Migration

Perform a mechanical rename from `query_sql` to `sql_template` for every embedded and test catalog definition without changing SQL behavior.

Then parameterize a small representative set:

- `rp-003`: `string_list` dangerous tools and `integer` chained-command threshold.
- One metrics rule with numeric thresholds.
- One fixed-unit window rule using `duration`.
- One rule or dedicated fixture using an operator-visible `regex`.
- One definition with no parameters to prove unchanged rendering.

For every migrated rule, add a golden assertion that rendering all defaults produces SQL equivalent to the pre-parameterization SQL after whitespace normalization. Do not expose internal signatures merely to increase type coverage; use test fixtures when a real rule parameter would not be operationally useful.

---

## File-Level Work Plan

- [ ] **Define and validate Rule Definitions.** Rename `catalog.Entry` to `catalog.Definition`; add `sql_template`, parameter schemas, origins, canonical hashing, and validation across load/save/import/export in `backend/internal/catalog/`.

- [ ] **Build the strict compiler.** Add `backend/internal/rules/{types,validate,render,compatibility}.go` with typed normalization, dedicated SQL literal renderers, placeholder validation, effective-value resolution, definition hashing, and upgrade compatibility.

- [ ] **Redefine clean provisioning.** Replace `ddlRulesStream`, add `snapshot_id`, `revision`, and `datetime64(9)` `updated_at`, remove catalog seeding, update startup/sync reads to `rendered_sql`, and add exact incompatible-schema detection in `backend/internal/timeplus/init.go`.

- [ ] **Centralize Rule Instance persistence.** Add `backend/internal/rules/store.go`; replace all `arg_max(query_sql, ...)` reads and repeated hand-written rule snapshot inserts with complete `RuleInstance` reads ordered by `revision` and one append helper, including deployed UDF artifacts.

- [ ] **Implement candidate validation and rollback-capable MV replacement.** Extend `backend/internal/timeplus/rules.go` with validation sinks, `NO DELAY` drops, non-`IF NOT EXISTS` replacement creates, UDF restoration, and failed-MV health reads. Add service tests that assert operation ordering and restoration on create or persistence failure.

- [ ] **Move lifecycle behavior into the service.** Refactor `backend/internal/handlers/rules.go` so install, configure, toggle, upgrade, custom-definition update, policy, uninstall, and sync delegate to `backend/internal/rules/service.go`. Add per-rule mutation serialization and document the single-writer/distributed-lock requirement.

- [ ] **Replace unsafe APIs and document contracts.** Register the definition/configuration/clone routes in `backend/cmd/server/main.go`, update models and `backend/internal/handlers/docs.go`, remove raw SQL upgrade/create shortcuts, and return structured validation/compatibility errors.

- [ ] **Migrate and parameterize catalog definitions.** Rename catalog fields mechanically, add representative parameterized rules/fixtures, and prove default-rendered SQL preserves current behavior.

- [ ] **Implement the configuration and authoring UI.** Add reusable rule parameter controls, update `RuleDetail.jsx`, refactor `RuleCreate.jsx`, implement clone/edit flows, and update `frontend/src/api/client.js`.

- [ ] **Document clean reprovisioning and author guidance.** Update `docs/design.md`, `docs/spec/api.md`, setup text, and catalog-authoring documentation with parameter contracts, security boundaries, upgrade behavior, and the future Lookup Set boundary.

- [ ] **Verify the complete lifecycle.** Run backend unit/integration tests, frontend tests/build/lint, catalog validation, and functional install/configure/upgrade/rollback/clone scenarios against a clean Timeplus deployment.

---

## Test Matrix

### Compiler and Catalog Unit Tests

- Every supported type renders correctly.
- Strings, lists, and regexes escape quotes and backslashes safely.
- Invalid types, bounds, enums, empty lists, duplicate IDs, alias collisions, unsupported placeholders, and unresolved placeholders fail.
- Regex length and RE2 validation fail before SQL rendering.
- NaN and infinity are rejected.
- Definitions with no parameters render unchanged.
- Default rendering matches golden SQL for representative rules.
- Catalog import/export preserves parameter schemas and templates.
- Catalog validation rejects duplicate or cross-definition UDF names and enforces rule-scoped UDF naming.
- Fuzz the placeholder scanner and string/list literal renderer.

### Compatibility Tests

- Same ID and same type preserves an override.
- New parameter uses its default.
- Alias migrates an override to the new ID.
- Removed parameter returns a warning and disappears only from the new snapshot.
- Type changes, alias ambiguity, and tightened invalid constraints block upgrade.
- No conflict path silently resets an override.

### Lifecycle Service Tests

- Install with defaults and with overrides.
- Configure enabled rule: preflight, replace MV, append snapshot.
- Configure disabled rule: preflight and append without MV creation.
- Invalid configuration performs no MV or snapshot write.
- Concurrent mutations for the same rule are serialized and produce strictly increasing revisions.
- Candidate MV failure preserves the previous snapshot and restores the previous MV.
- Snapshot append failure after replacement restores the previous MV.
- Restart/sync recreates the installed MV from snapshot SQL and UDFs even when the current catalog definition is newer or absent.
- Toggle and policy writes preserve rendered SQL and overrides.
- Upgrade preserves compatible overrides and blocks incompatible ones.
- Uninstall and custom-definition update follow rollback ordering.
- Every appended row is a complete instance snapshot with actor, reason, `snapshot_id`, and monotonic `revision`.
- Same-millisecond snapshots reconstruct the newest revision deterministically.
- Replacement creates fail instead of silently reusing a stale MV if the old MV was not actually dropped.
- MV status reports `system.v_failed_mat_views` failures as unhealthy.

### Integration and Frontend Tests

- Clean provisioning creates the new empty instance stream and no catalog-derived instances.
- Provisioning fails clearly when `agentguard_rules` has legacy columns, missing `revision`, or incompatible key column types.
- Install, configure, restart/sync, disable, enable, upgrade, and uninstall work against Timeplus.
- Candidate validation rejects a query with missing required output columns without emitting detections.
- Rule replacement and rollback use `DROP VIEW ... NO DELAY` and do not mask stale MVs with `CREATE ... IF NOT EXISTS`.
- Setup diagnostics expose missing Timeplus privileges for stream/view/function DDL and MV pause/resume.
- Rule Detail edits/reset/save reflect authoritative effective values.
- Clone-to-custom preserves the source template/schema but creates a new definition ID.
- Custom definition preview/test uses backend-rendered SQL.
- Upgrade conflict and rollback errors are visible and actionable.

Verification commands:

```bash
cd backend && CGO_ENABLED=0 go test ./...
cd backend && CGO_ENABLED=0 go build ./...
cd frontend && npm test
cd frontend && npm run build
cd frontend && npm run lint
make build
```

---

## Rollout and Acceptance Criteria

Rollout requires a documented clean reprovision of rule resources. The application must fail clearly when pointed at the legacy `agentguard_rules` schema; it must not partially operate or silently discard configuration.

The feature is complete when:

- A rule without parameters installs and runs unchanged.
- A parameterized rule installs from defaults, accepts valid typed overrides, rejects invalid overrides, and exposes deterministic rendered SQL.
- Operators cannot inject arbitrary SQL through parameter values or the installed-rule API.
- All rule instance mutations write complete snapshots with audit metadata.
- Current rule state is reconstructed deterministically from `revision`, including same-millisecond update bursts.
- Same-rule concurrent configuration attempts cannot interleave MV replacement and snapshot writes.
- Enabled-rule configuration and upgrades either deploy fully or restore the previous MV and persisted instance state.
- MV replacement cannot silently retain a stale MV, and MV health exposes failed Proton materialized views.
- Compatible upgrades preserve overrides; incompatible upgrades are blocked with actionable conflicts.
- Built-in structural customization is available only through clone-to-custom.
- Existing deployments receive an explicit clean-reprovision requirement.
- Runtime Lookup Sets remain unimplemented and clearly documented as the next phase.

## Open Questions

- None. These decisions define the implementation baseline for `feat/33-editable-rule-parameters`.
