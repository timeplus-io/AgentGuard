# AgentGuard / Timeplus Apps Alignment Mapping

## Overview

This document codifies the alignment between AgentGuard's external rule authoring surface and the [Timeplus Apps](https://apps.timeplus.com/) manifest specification, verified against the real `~/apps` repository.

AgentGuard intentionally aligns the parts authors see most often: `config:`, `key:`, `{{ .Config.<key> }}`, and the overlapping type tokens (`integer`, `float`, `bool`, `string`, `choice`, `list`). It is **not** wire-compatible with `/apps` manifests. AgentGuard keeps a strict SQL-literal compiler, uses typed native defaults instead of `/apps` string-encoded defaults, rejects secrets, and adds rule-engine-specific fields.

## Surface Alignment Decision

The external authoring contract adopts the `/apps` naming surface and overlapping type tokens; the internal strict engine and value semantics are AgentGuard-specific. Concretely:

| Concern | Phase A (before) | Final (`/apps`-aligned) |
|---|---|---|
| Parameter list YAML/JSON key | `parameters:` | `config:` |
| Parameter identifier field | `id` | `key` |
| Placeholder spelling | `{{params.<id>}}` (no whitespace) | `{{ .Config.<key> }}` (optional surrounding whitespace) |
| Shared metadata fields | — | `description`, `options`, `secret` (parsed only to reject `true`) |
| Omitted parameter type | rejected as unknown type | defaults to `string` (Apps semantics; applied catalog-wide via `NormalizeDefinition` on Save, local-dir load, zip import, compile preview, and installed-edit redeploy) |
| Unsupported `/apps` fields | — | `required` is not supported; every AG parameter must have a default |
| AG-only schema fields | — | `label`, `min`, `max`, `min_items`, `max_items`, `unit`, `aliases` (see Rule-Engine Constraints) |
| Template engine | strict scanner (kept) | strict scanner (kept — NOT Go text/template) |
| Type tokens | `integer/number/boolean/string/enum/string_list` (Phase A internal names) | **adopt the Apps tokens** `integer/float/bool/string/choice/list` + AG-only `regex`/`duration`; old names are rejected, not aliased |
| Default encoding | typed native values | typed native values (deliberate divergence from `/apps`, which encodes defaults as strings) |
| DB templating (`{{ .DB }}`) | n/a | not supported (single fixed deployment; document the divergence) |

## Type-Name Mapping

AgentGuard's parameter type tokens **are** the Timeplus Apps tokens for every overlapping type — there is no separate internal vocabulary to translate. AgentGuard adds two extensions that `/apps` does not define. The Phase A internal names (`number`/`boolean`/`enum`/`string_list`) were renamed to the Apps tokens before release and are rejected if submitted.

| AG token | `/apps` token | Status (Phase A name) |
|---|---|---|
| `integer` | `integer` | identical |
| `float` | `float` | identical (was `number`) |
| `bool` | `bool` | identical (was `boolean`) |
| `string` | `string` | identical |
| `choice` | `choice` | identical (was `enum`) |
| `list` | `list` | identical (was `string_list`) |
| `regex` | — | AG extension (no `/apps` type) |
| `duration` | — | AG extension (no `/apps` type) |

`/apps` also defines `multi_choice` (an **option-constrained** multi-select) and uses it in real apps. AgentGuard does not accept `multi_choice`; its only multi-value type is free-form `list`. Adding it would be a real feature (compiler rendering, UI, upgrade-compatibility), not a cleanup, so it is deferred rather than chased for shape parity.

## Rule-Engine Constraints (kept deliberately)

`/apps` config items carry no value-constraint fields at all. AgentGuard keeps a few — `min`/`max` (numeric bounds for `integer`/`float`/`duration`), `max_items` (caps `list` expansion), and `min_items` — **not** as shape-parity gaps to close, but because they protect generated SQL and operational correctness:

- **`min`/`max`** bound a threshold so an operator cannot author a rule that is dead (never fires) or pathologically noisy.
- **`max_items`** caps how large a rendered `IN (...)` list can grow; `min_items` is low-value because the empty list is already rejected globally.

These are author-time guardrails that render to safe literals and cost nothing for an `/apps` reader to ignore. `string` length bounds (`min_length`/`max_length`) were removed because they protected neither SQL safety nor operational correctness — that asymmetry is intentional. The honest contract: AG adopts `/apps` naming and overlapping type tokens, and keeps rule-engine constraints exactly where they guard the SQL it compiles.

## Default Encoding Divergence

Real `/apps` manifests encode `default` as a string regardless of type:

| Type | `/apps` default encoding | AgentGuard default encoding |
|---|---|---|
| `integer` | `"30"` | `30` |
| `float` | `"3.14"` | `3.14` |
| `bool` | `"true"` | `true` |
| `list` / `multi_choice` | JSON-array string, e.g. `'["ec2","s3"]'` | native YAML/JSON array, e.g. `[ec2, s3]` |

This is deliberate. AgentGuard validates and renders rules at authoring/install time; typed defaults catch invalid definitions before a rule can deploy. A direct `/apps` config block with string-encoded numeric/bool/list defaults is therefore not accepted by AgentGuard without conversion.

## Strict Engine Rationale

AgentGuard retains its **strict, injection-safe parameter compiler** and does NOT adopt Go `text/template` execution, Sprig functions, conditionals, or any templating features beyond exact placeholder substitution.

**Rationale:**
- The renderer escapes quotes and backslashes only and substitutes exact placeholders (`{{ .Config.<key> }}`).
- No parameter type may inject identifiers, operators, SQL clauses, functions, or arbitrary SQL — all values are rendered as literals (strings as quoted strings, floats as decimal literals, choice/regex as quoted literals, etc.).
- This design prevents accidental or malicious SQL injection through rule parameters, a critical security property for a real-time security platform that executes parameterized rules against live data.

## Secret Parameters

`/apps` supports `secret: true` for external connection credentials. AgentGuard parses the field only to reject `secret: true` at validation time with an error message.

**Rationale:** Parameters are rendered into the rule's SQL template and are never suitable for storing secrets. If a configuration field requires sensitive data (API keys, credentials, etc.), it should be managed outside the rules system entirely (e.g., environment variables, vaults, or external authentication services). Parameters should only hold non-sensitive configuration values.

## Required Fields

`/apps` supports `required: true`, commonly with no default for secrets. AgentGuard does not support that model:

- every parameter must declare a valid default;
- every effective value is required when compiling;
- missing values fail validation rather than being prompted for later.

Accordingly, `required` is not part of the AgentGuard parameter schema.

## Database Templating Divergence

AgentGuard does **NOT** support `{{ .DB }}` templating (dynamic database/stream selection). This is a deliberate divergence from the `/apps` manifest specification.

**Rationale:** AgentGuard operates in a single, fixed deployment context — all rules query the same Timeplus Proton instance and the same `agentguard_cim_event` stream. There is no multi-tenant or multi-database scenario. Allowing `{{ .DB }}` would introduce deployment-time schema variance that complicates provisioning, testing, and rule portability. The strict design instead favors clarity: rules are authored against a known, fixed data model.

## Internal Go Identifiers

The Phase B refactoring renames the `catalog.Parameter` struct field from `ID` to `Key` to align with the `/apps` manifest field name.

**Mapping:**
- Go struct field: `catalog.Parameter.ID` → `catalog.Parameter.Key`
- YAML/JSON struct tag: `id` → `key`
- Go field name equivalence note: In AgentGuard's internal code, `id` (a rule scoped identifier) and `key` (the parameter name) are interchangeable; both refer to the same semantic entity, the unique identifier for a configuration parameter within a rule's scope.

**Helper functions preserved (unchanged semantics):**
- `ValidParameterID()` — validates parameter identifier syntax (regex: `^[a-z][a-z0-9_]{0,63}$`)
- `ParameterIDPattern` — the regex pattern constant
- `paramIDRe` — the compiled regex for identifier validation

These helpers still validate the parameter identifier/key syntax; only the struct field name changes.

## Notes for Implementers

1. **YAML/JSON Round-Trip:** When exporting rules to YAML or JSON for import/export operations, use `config:`, `key:`, `{{ .Config.<key> }}`, and AgentGuard's typed default encoding.

2. **Migration:** The `/apps` rename intentionally rotates the `Definition.Hash()` value for any parameterized definitions (the serialization field name changes from `id` to `key`). This is acceptable because the `agentguard_rules` snapshot stream is clean-reprovisioned at this baseline; no snapshot-migration logic is needed.

3. **Type Extensions:** `regex` and `duration` parameter types are new in Phase B and are NOT represented in the `/apps` manifest specification. They should be documented as AG-specific extensions when rules are exported or imported.

4. **Strict Rendering:** Parameter overrides are always rendered as literals. For example:
   - `integer` → bare decimal: `5`
   - `string` → single-quoted with escaped quotes: `'it''s'`
   - `regex` → single-quoted: `'[a-z0-9]+'`
   - `duration` → bare integer (unit is metadata only): `60`

5. **Placeholder Validation:** The template scanner accepts whitespace around the placeholder:
   - Valid: `{{ .Config.threshold }}`, `{{.Config.threshold}}`, `{{ .Config.threshold}}`
   - Invalid: `{{params.threshold}}` (legacy spelling, now rejected), `{{ threshold }}` (missing `.Config.` prefix)

6. **Catalog vs. Instance:** The `Definition` (in the catalog) owns the parameter schema and template. Installed `RuleInstance`s (snapshots in `agentguard_rules`) store the effective parameter values (overrides) and the compiled output (rendered SQL + deployed UDFs). Parameter edits are always made at the Definition level; instances inherit the new schema on upgrade or redeploy.
