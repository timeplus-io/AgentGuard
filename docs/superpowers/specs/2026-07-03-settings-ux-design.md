# Settings Page UX: Tabbed Sections & Navigation — Design

**Date:** 2026-07-03
**Issue:** [#199](https://github.com/timeplus-io/AgentGuard/issues/199)
**Scope:** Frontend only — no API or backend changes.

## Problem

The Settings page (`frontend/src/pages/Settings.jsx`) renders seven flat section
cards in one long vertical scroll. There is no navigation between sections, no
visual hierarchy, and the Semantic DLP card mixes three concerns (server config,
runtime config editor, re-provision operation) separated by an ad-hoc border —
which violates the "no border lines for sectioning" design rule. Restart /
reprovision banners sit at the top of the page, far from the keys that caused
them.

## Decisions

- **Tabbed sections** (chosen over anchored scroll): a left sticky sub-nav lists
  the sections; only the active section renders. Simplest unsaved-changes
  semantics; badges on nav items keep hidden-tab state visible.
- **No search/filter box** in this iteration. Revisit if the key count grows.
- **No backend changes.** The `/api/config` response already provides everything
  needed: per-key static `restart`/`reprovision` flags and the global
  `restart_required` / `reprovision_required` booleans.

## Design

### 1. Layout & navigation

Two-column layout inside the existing page shell (Sidebar / TopNav / Footer
untouched):

- **Left:** sticky section nav (~200px wide, `sticky` below the top nav)
  listing the sections from `data.sections` in API order (Server, Timeplus
  Connection, AgentGuard, Catalog, Approval Holds, Semantic DLP, Setup).
- **Right:** only the **active section** renders.

The active section id is stored in the URL hash (`/settings#dlp`) so tabs
deep-link and survive refresh. An unknown or absent hash falls back to the
first section. Switching tabs updates the hash without adding history entries
beyond normal hash navigation.

Styling follows the Digital Sentry system: active nav item = lighter background
tier (tonal layering), no borders, no shadows, radius ≤ 4px. Same interaction
pattern as the main Sidebar's NavLink highlighting.

### 2. Per-section state badges on the nav

| Badge | Trigger | Placement |
|-------|---------|-----------|
| Dirty count (pink chip, `N`) | Section has ≥1 key in `drafts`. Keys are mapped to sections via a key→section map built from `data.sections`. | The section's nav item |
| Restart dot (amber) | Global `restart_required` is true | Nav items of sections containing any key with `restart: true` (today: Server, Catalog) |
| Reprovision dot (amber) | Global `reprovision_required` is true | Nav item of sections containing any key with `reprovision: true` (today: Semantic DLP) |

Notes:

- The API does not report *which* keys differ from the running server, so the
  restart/reprovision dots are placed by static key flags gated on the global
  booleans. With the current registry this is exact (restart keys exist only in
  Server and Catalog; reprovision keys only in DLP).
- Top banners (save result, restart-required, reprovision-required with its
  action button) **stay** — they are global and visible on every tab. The nav
  badges add the "where" that banners lack.
- The fixed bottom save bar is unchanged and **global**: Save/Discard applies
  to all drafts across all tabs. Edits on a hidden tab are never lost; the
  dirty-count chip makes them visible.

### 3. DLP section restructure

The Semantic DLP tab becomes three sub-groups, separated by tonal blocks
(background tier shifts — the existing `border-t` is removed), each with one
line of explanatory copy:

1. **Server config** — the config.yaml keys `dlp.enabled` and
   `dlp.embedding_dim` (read-only), rendered by the normal key list. Copy notes
   these are boot defaults stored in `config.yaml`.
2. **Runtime configuration** — the existing `DLPConfigEditor` component,
   unchanged internally. Copy explains this is live state in the
   `agentguard_dlp_config` stream: changes apply immediately, no restart or
   re-provision, independent of the Save bar above.
3. **Operations** — the "Re-provision DLP" button with its explanation
   (re-detects embedding dimension, rebuilds the pipeline; run after switching
   the embedding model).

### 4. Code structure

- `frontend/src/pages/Settings.jsx` — keeps all state ownership (config data,
  drafts, fieldErrors, save/discard, banners, `tpTest`, `reprovisioning`) and
  the active-tab hash logic; renders `SettingsNav` + the active
  `SettingsSection` + banners + save bar. Slims down substantially.
- **New** `frontend/src/components/settings/SettingsNav.jsx` — presentational
  nav list. Props: sections, active id, per-section dirty counts, restart /
  reprovision section-id sets, onSelect.
- **New** `frontend/src/components/settings/SettingsSection.jsx` — renders one
  section's key list plus section-specific extras (Timeplus test-connection
  row for `timeplus`; the three DLP sub-groups for `dlp`). `KeyField` and
  `SourceChip` move here from `Settings.jsx`.
- `frontend/src/components/DLPConfigEditor.jsx` — untouched.

Preserved behaviors (acceptance criteria): draft/save/discard flow with the
fixed bottom bar, per-key source chips, secret handling (never echoed, blank =
keep), Timeplus test-connection using draft-or-saved host/port, field-level
validation errors rendered under the offending key, re-provision flow.

### 5. Error handling

Unchanged from today: load errors render inline; save errors set field-level
errors and an error banner. One addition: if a save returns field errors for
keys on a non-active tab, the dirty/error state remains visible via the nav
chip (field errors persist in state until the key is re-edited or discarded);
the user lands on whichever tab they were on — no auto-switching.

### 6. Verification

- `npm run lint` and `npm run build` pass.
- Manual walkthrough: edit keys on two different tabs → save bar counts both
  and nav chips show per-section counts → Save → banner + badges update after
  reload; Discard clears all chips. Hash deep-link (`/settings#dlp`) opens the
  DLP tab. Timeplus test-connection works with drafted values. DLP
  re-provision works from both the banner and the Operations sub-group.
- No settings design exists under `docs/spec/`, so there is no spec asset to
  update.
