# Sentry Chat — Render Assistant Responses as Markdown

> **Status:** Approved design
> **Issue:** [#213](https://github.com/timeplus-io/AgentGuard/issues/213); parent [#207](https://github.com/timeplus-io/AgentGuard/issues/207) / PR [#208](https://github.com/timeplus-io/AgentGuard/pull/208)
> **Date:** 2026-07-25

## 1. Goal

The Sentry analyst agent formats its answers as **Markdown** — headings, **bold**,
lists, GFM tables (threats / holds / cost), inline `code`, and fenced SQL/code
blocks. The `/sentry` chat UI currently renders assistant content as **plain
text** (`frontend/src/pages/Sentry.jsx`, `<p …>{item.content}</p>` with
`whitespace-pre-wrap`), so users see raw markup (`##`, `**…**`, `| --- |`,
backtick fences). Tables in particular are unreadable.

Render assistant message content as Markdown — for both the live token stream
and conversation replay — styled to the Digital Sentry dark theme, with fenced
code blocks syntax-highlighted, and XSS-safe.

## 2. Decisions (settled in brainstorming)

- **Library:** `react-markdown` + `remark-gfm`. React-idiomatic; safe by default
  (escapes HTML; **no** `rehype-raw`, **no** `dangerouslySetInnerHTML`); GFM adds
  tables / task lists / strikethrough.
- **Syntax highlighting:** yes, via `react-syntax-highlighter`'s **`PrismAsyncLight`**
  build — language grammars are dynamically imported at runtime, so they stay out
  of the main chunk (the Sentry bundle already exceeds Vite's 500 KB warning; this
  keeps the added weight lazy).
- **Scope:** assistant text bubbles only (live + replay). User bubbles, error
  bubbles, and `tool_call`/`tool_result` blocks are unchanged.

## 3. Component — `frontend/src/components/Markdown.jsx` (new)

A single focused component: `export default function Markdown({ children })`,
where `children` is the raw markdown string (the assistant message content).

```jsx
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { PrismAsyncLight as SyntaxHighlighter } from 'react-syntax-highlighter'
import sql from 'react-syntax-highlighter/dist/esm/languages/prism/sql'
import json from 'react-syntax-highlighter/dist/esm/languages/prism/json'
import bash from 'react-syntax-highlighter/dist/esm/languages/prism/bash'
import yaml from 'react-syntax-highlighter/dist/esm/languages/prism/yaml'
import { oneDark } from 'react-syntax-highlighter/dist/esm/styles/prism'

SyntaxHighlighter.registerLanguage('sql', sql)
SyntaxHighlighter.registerLanguage('json', json)
SyntaxHighlighter.registerLanguage('bash', bash)
SyntaxHighlighter.registerLanguage('yaml', yaml)
```

- **Only these languages are registered.** An unknown/absent language (or a
  partial fence mid-stream) falls back to an unstyled `<code>` block — never an
  error. `oneDark` is a dark Prism theme; if it clashes with the palette, override
  its background to a surface token via the `customStyle` prop (a plain, unhighlighted
  container that fits the theme is acceptable — highlighting is the enhancement,
  not the requirement).
- **`components` map** (design tokens only; no borders-for-sectioning, no shadows,
  radius ≤4px):
  - `code`: distinguish inline vs. block. react-markdown passes `inline` (or, in
    newer versions, the absence of a `language-*` className / node position marks
    inline) — implement the check defensively. **Inline** → a small chip:
    `bg-surface-container-high rounded-sm px-1 py-0.5 text-[0.85em] font-mono`.
    **Block** → `SyntaxHighlighter` with `language` parsed from the
    `` ```lang `` fence's `className` (`language-xxx`), `PreTag="div"`, and a
    `customStyle` giving `rounded-sm`, a surface background, and
    `overflow-x: auto`.
  - `table` → wrapped in `<div className="overflow-x-auto">` so wide tables scroll
    inside their own container; `th`/`td` use subtle background-tier separation
    (no border lines), small padding.
  - `a` → `text-[#ffb0cf] hover:underline`, `target="_blank"`,
    `rel="noopener noreferrer"`.
  - `ul`/`ol`/`li` → normal list markers with left padding; `h1`–`h4` → graded
    sizes/weights that stay within the bubble's scale; `p` → `leading-relaxed`;
    `blockquote` → left background-tier accent (not a border), muted text;
    `hr` → a thin background-tier rule.
- **Container class:** the component renders its children inside a wrapper carrying
  the existing bubble text tokens (`text-sm text-gray-100 break-words`) so it reads
  identically to the current plain-text bubble, just formatted. `whitespace-pre-wrap`
  is dropped (markdown block elements manage their own whitespace); hard line breaks
  within a paragraph rely on GFM/CommonMark soft-break behavior.

## 4. Integration — `frontend/src/pages/Sentry.jsx`

Two call sites render assistant text and must use `<Markdown>`:

1. **Live/streamed assistant bubble** (~L96-97 today): the plain-text renderer
   `<p className="… whitespace-pre-wrap …">{item.content}</p>` becomes
   `<div className="max-w-2xl"><Markdown>{item.content}</Markdown></div>`
   (keep the width constraint; move text tokens into the Markdown wrapper).
2. **Replay** rebuilds the same message shape from `agentguard_sentry_messages`
   (`role`/`content`, ~L168-169); the same assistant renderer is reused, so a
   single change covers both if they share the renderer. If live and replay use
   separate JSX branches, update both identically.

**Explicitly unchanged:**
- `tool_call` / `tool_result` blocks (L57-65) — raw JSON/text in mono code boxes.
- User message bubbles (L79-80) — plain text (`whitespace-pre-wrap`); users type
  plain text, and rendering their input as markdown could surprise.
- Error bubbles — plain text.

Import `Markdown` at the top of `Sentry.jsx`; no other page logic changes
(streaming accumulation, `finalizeOpenBubble`, event handling all stay as-is —
they operate on the `content` string, which `Markdown` simply consumes).

## 5. Streaming behavior

`content` grows token-by-token (`case 'token'` appends to `m.content`).
`react-markdown` re-parses the full string each render — fine for these response
sizes. Requirements:

- **No throw on partial input.** An unclosed ``` fence, a half-written table row,
  or a dangling `[` must render as best-effort text, not crash the bubble.
  `react-markdown` is tolerant; the code renderer must treat an unknown/partial
  language as plaintext.
- The `pending` cursor / bubble-finalize logic is untouched — it keys off
  `content` and the open-bubble ref, independent of how content is displayed.

## 6. Security

- No raw HTML rendering: `react-markdown` default (which escapes/ignores raw HTML)
  with **no** `rehype-raw` and **no** `dangerouslySetInnerHTML`. Model-generated
  `<script>`/`<img onerror=…>` etc. are inert text.
- Links open with `rel="noopener noreferrer"`.
- This keeps the page within the app's existing CSP posture; no new external
  requests (the Prism theme is bundled, grammars are local dynamic imports).

## 7. Dependencies

Add to `frontend/package.json`:
- `react-markdown`
- `remark-gfm`
- `react-syntax-highlighter`

All are pure JS, self-contained (no external CDN/runtime fetches). Grammars load
via local dynamic import (`PrismAsyncLight`), so the main chunk grows modestly and
the language packs are separate lazy chunks. The pre-existing Vite "chunk > 500 KB"
warning is not introduced by this change and is out of scope.

## 8. Testing / verification

The frontend has no unit-test harness (only ESLint + Vite build in CI), so
verification is lint + build + a manual browser pass:

- `cd frontend && npm run lint && npm run build` → clean (0 errors; the pre-existing
  chunk-size warning is acceptable).
- Manual: run a Sentry turn whose answer contains a heading, **bold**, a bulleted
  list, a GFM table, inline `code`, and a fenced ` ```sql ` block → all render
  formatted; the SQL block is highlighted; a wide table scrolls within its bubble
  and the page body does **not** scroll horizontally.
- Reload / open the conversation from history → the **replayed** assistant messages
  render identically (markdown, not raw).
- Mid-stream sanity: while a long answer streams, an as-yet-unclosed code fence
  shows as plain/partial text and resolves to a highlighted block once closed — no
  console errors.
- Unchanged surfaces confirmed: tool_call/tool_result boxes, user bubbles, and
  error bubbles look exactly as before.

## 9. Out of scope

- Markdown rendering on other surfaces (DLP summaries, etc.) — a later reuse of the
  same component if wanted.
- Syntax highlighting for languages beyond the registered set (add grammars as
  needed later).
- Copy-to-clipboard buttons on code blocks, collapse/expand, or other code-block
  affordances.
- Rendering user-typed input as markdown.
- Addressing the pre-existing bundle-size warning.
