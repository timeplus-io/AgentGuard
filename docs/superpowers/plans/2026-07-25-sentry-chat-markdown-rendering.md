# Sentry Chat Markdown Rendering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render Sentry assistant chat responses as formatted Markdown (headings, GFM tables, lists, bold, inline + syntax-highlighted fenced code) instead of raw text, for both the live token stream and conversation replay.

**Architecture:** A new self-contained `frontend/src/components/Markdown.jsx` wraps `react-markdown` + `remark-gfm` with a theme-mapped `components` renderer and `react-syntax-highlighter`'s `PrismAsyncLight` for fenced code. `frontend/src/pages/Sentry.jsx`'s single assistant renderer (`MessageBubble`, which serves both live and replay) swaps its plain-text `<p>` for `<Markdown>`. No other page logic changes.

**Tech Stack:** React 18.3.1, Vite 5.4.8, `react-markdown` ^9, `remark-gfm` ^4, `react-syntax-highlighter` ^15. Spec: `docs/superpowers/specs/2026-07-25-sentry-chat-markdown-rendering-design.md`. Branch: `feature/207-sentry-agent-design`. Issue #213.

## Global Constraints

- **Frontend only.** No backend changes. Work from `frontend/`.
- **XSS-safe:** `react-markdown` defaults only — **NO** `rehype-raw`, **NO** `dangerouslySetInnerHTML`. Model-generated HTML must stay inert text.
- **Design tokens only** (match the existing app): `bg-surface-container`, `bg-surface-container-high`, `text-gray-100`, `text-gray-400`, link accent `#ffb0cf`, `rounded-sm`. **No** border lines for sectioning, **no** drop shadows, border radius ≤ 4px (`rounded-sm`).
- **Scope:** assistant text bubbles only. Do **NOT** change `ToolCard` (tool_call/tool_result), the user-message bubble, or the error bubble.
- **No unit-test harness exists** in `frontend/` (CI runs ESLint + Vite build only). Automated verification per task = `npm run lint` (0 errors) + `npm run build` (succeeds). The pre-existing "chunk > 500 KB" Vite warning is acceptable and out of scope. A live-browser visual pass is a controller/human follow-up after Task 2.
- Pin `react-markdown@^9` — this plan's `code` renderer targets the v9 component API (no `inline` prop).
- Commit after each task on branch `feature/207-sentry-agent-design`.

---

### Task 1: `Markdown` component + dependencies

**Files:**
- Modify: `frontend/package.json` (+ `package-lock.json`) — add three deps
- Create: `frontend/src/components/Markdown.jsx`

**Interfaces:**
- Produces: `export default function Markdown({ children })` — `children` is the raw markdown **string** (an assistant message's `content`). Renders themed HTML; safe for empty/partial input.

- [ ] **Step 1: Add the dependencies**

Run:
```bash
cd frontend && npm install react-markdown@^9 remark-gfm@^4 react-syntax-highlighter@^15
```
Expected: installs cleanly; `package.json` `dependencies` now include `react-markdown`, `remark-gfm`, `react-syntax-highlighter`.

- [ ] **Step 2: Verify the baseline build still passes with the new deps**

Run: `cd frontend && npm run build`
Expected: build succeeds (the pre-existing chunk-size warning may print — that is fine).

- [ ] **Step 3: Create `frontend/src/components/Markdown.jsx`**

Write exactly this file:
```jsx
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { PrismAsyncLight as SyntaxHighlighter } from 'react-syntax-highlighter'
import sql from 'react-syntax-highlighter/dist/esm/languages/prism/sql'
import json from 'react-syntax-highlighter/dist/esm/languages/prism/json'
import bash from 'react-syntax-highlighter/dist/esm/languages/prism/bash'
import yaml from 'react-syntax-highlighter/dist/esm/languages/prism/yaml'
import { oneDark } from 'react-syntax-highlighter/dist/esm/styles/prism'

// Only these grammars are registered; PrismAsyncLight loads them as separate
// lazy chunks. An unknown/absent language (or a partial fence mid-stream)
// renders unhighlighted — never an error.
SyntaxHighlighter.registerLanguage('sql', sql)
SyntaxHighlighter.registerLanguage('json', json)
SyntaxHighlighter.registerLanguage('bash', bash)
SyntaxHighlighter.registerLanguage('yaml', yaml)

// react-markdown v9 removed the `inline` prop: the `code` renderer is called
// for both inline and block code. Treat it as inline only when there is no
// language class AND no newline; everything else is a block (a language-less
// fenced block falls back to the 'text' grammar = no highlighting).
const components = {
  code({ className, children }) {
    const match = /language-(\w+)/.exec(className || '')
    const text = String(children ?? '')
    const isInline = !className && !text.includes('\n')
    if (isInline) {
      return (
        <code className="bg-surface-container-high rounded-sm px-1 py-0.5 text-[0.85em] font-mono text-gray-100">
          {children}
        </code>
      )
    }
    const language = match ? match[1] : 'text'
    return (
      <div className="bg-surface-container-high rounded-sm overflow-x-auto my-2">
        <SyntaxHighlighter
          language={language}
          style={oneDark}
          PreTag="div"
          customStyle={{ margin: 0, background: 'transparent', fontSize: '12px', padding: '10px' }}
          codeTagProps={{ style: { fontFamily: 'inherit', background: 'transparent' } }}
        >
          {text.replace(/\n$/, '')}
        </SyntaxHighlighter>
      </div>
    )
  },
  a({ href, children }) {
    return (
      <a href={href} className="text-[#ffb0cf] hover:underline" target="_blank" rel="noopener noreferrer">
        {children}
      </a>
    )
  },
  table({ children }) {
    return (
      <div className="overflow-x-auto my-2">
        <table className="text-xs border-collapse">{children}</table>
      </div>
    )
  },
  th({ children }) {
    return <th className="bg-surface-container-high text-left font-semibold px-2 py-1 text-gray-100">{children}</th>
  },
  td({ children }) {
    return <td className="bg-surface-container px-2 py-1 align-top">{children}</td>
  },
  ul({ children }) {
    return <ul className="list-disc pl-5 my-1 space-y-0.5">{children}</ul>
  },
  ol({ children }) {
    return <ol className="list-decimal pl-5 my-1 space-y-0.5">{children}</ol>
  },
  h1({ children }) {
    return <h1 className="text-base font-semibold mt-2 mb-1">{children}</h1>
  },
  h2({ children }) {
    return <h2 className="text-sm font-semibold mt-2 mb-1">{children}</h2>
  },
  h3({ children }) {
    return <h3 className="text-sm font-semibold mt-1.5 mb-0.5">{children}</h3>
  },
  h4({ children }) {
    return <h4 className="text-xs font-semibold uppercase tracking-wide text-gray-400 mt-1.5 mb-0.5">{children}</h4>
  },
  p({ children }) {
    return <p className="my-1 leading-relaxed">{children}</p>
  },
  blockquote({ children }) {
    return <blockquote className="bg-surface-container-high/50 rounded-sm px-3 py-1 my-1 text-gray-400">{children}</blockquote>
  },
  hr() {
    return <hr className="border-0 h-px bg-surface-container-high my-2" />
  },
}

// Markdown renders an assistant message's raw markdown string, styled to the
// Digital Sentry dark theme. XSS-safe: react-markdown defaults escape raw HTML;
// no rehype-raw / no dangerouslySetInnerHTML.
export default function Markdown({ children }) {
  return (
    <div className="text-sm text-gray-100 break-words [&>*:first-child]:mt-0 [&>*:last-child]:mb-0">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {children || ''}
      </ReactMarkdown>
    </div>
  )
}
```

- [ ] **Step 4: Lint + build the component**

Run: `cd frontend && npm run lint && npm run build`
Expected: `npm run lint` → 0 errors (pre-existing warnings elsewhere are fine); `npm run build` → succeeds. If ESLint flags an unused variable or an unknown token, fix it without changing behavior (do not spread unknown props onto DOM nodes; do not add new color tokens — reuse the ones listed in Global Constraints).

- [ ] **Step 5: Commit**

```bash
cd "$(git rev-parse --show-toplevel)"
git add frontend/package.json frontend/package-lock.json frontend/src/components/Markdown.jsx
git commit -m "feat(sentry): Markdown renderer component (react-markdown + gfm + prism)"
```

---

### Task 2: Render assistant bubbles with `Markdown` in Sentry.jsx

**Files:**
- Modify: `frontend/src/pages/Sentry.jsx` (import + the assistant branch of `MessageBubble`)

**Interfaces:**
- Consumes: `Markdown` from `../components/Markdown` (Task 1) — `<Markdown>{string}</Markdown>`.

**Context:** `MessageBubble({ item })` in `Sentry.jsx` is the single renderer for every message; the live stream and the replayed history (`role:'assistant', content`) both flow through it, so changing the assistant branch covers both. The assistant branch today is the final `return` in `MessageBubble`:
```jsx
  // assistant — no markdown renderer in the frontend (see SemanticDLP's
  // report pane); render as plain text preserving whitespace, same approach.
  return (
    <p className="max-w-2xl text-sm text-gray-100 whitespace-pre-wrap break-words leading-relaxed">
      {item.content}
      {item.pending && <span className="inline-block w-1.5 h-3.5 bg-primary/70 ml-0.5 align-text-bottom animate-pulse" />}
    </p>
  )
```

- [ ] **Step 1: Import the component**

At the top of `frontend/src/pages/Sentry.jsx`, with the other component imports, add:
```jsx
import Markdown from '../components/Markdown'
```

- [ ] **Step 2: Replace the assistant plain-text renderer**

In `MessageBubble`, replace the assistant `return` block shown in Context above with:
```jsx
  // assistant — render Markdown (headings, GFM tables, lists, code); the
  // pending cursor follows the rendered content while streaming.
  return (
    <div className="max-w-2xl">
      <Markdown>{item.content}</Markdown>
      {item.pending && <span className="inline-block w-1.5 h-3.5 bg-primary/70 ml-0.5 align-text-bottom animate-pulse" />}
    </div>
  )
```
Do **not** touch the `user` branch, the `error` branch, or `ToolCard` — leave their plain-text / mono rendering exactly as-is.

- [ ] **Step 3: Lint + build**

Run: `cd frontend && npm run lint && npm run build`
Expected: `npm run lint` → 0 errors; `npm run build` → succeeds.

- [ ] **Step 4: Static self-check of the diff**

Confirm by reading the diff:
- Only `Sentry.jsx` changed in this task (plus the import line).
- The assistant branch now returns `<div className="max-w-2xl"><Markdown>{item.content}</Markdown>…`.
- `user`, `error`, and `ToolCard` renderers are byte-for-byte unchanged.
- The streaming/`pending`/`finalizeOpenBubble` logic elsewhere in the file is untouched (this task only swaps how assistant `content` is displayed).

- [ ] **Step 5: Commit**

```bash
cd "$(git rev-parse --show-toplevel)"
git add frontend/src/pages/Sentry.jsx
git commit -m "feat(sentry): render assistant chat responses as Markdown (#213)"
```

---

## Manual verification (controller/human, after Task 2)

No frontend test runner exists, so a live visual pass confirms the behavior. With the stack running (`make dev` or docker compose) and Sentry pointed at a working model:

- Ask Sentry something that yields a heading, **bold**, a bulleted list, a GFM table, inline `code`, and a fenced ` ```sql ` block → all render formatted; the SQL block is highlighted; a wide table scrolls **inside its bubble** and the page body does not scroll horizontally.
- Reload and open the conversation from history → replayed assistant messages render as Markdown (not raw).
- While a long answer streams, an as-yet-unclosed code fence shows as plain/partial text and resolves to a highlighted block once closed — no console errors; the pending cursor still appears.
- Confirm unchanged: tool_call/tool_result cards, the user bubble, and error bubbles look exactly as before.

---

## Self-Review Notes (resolved inline)

- **Spec coverage:** §3 component → Task 1 (full file incl. themed `components`, PrismAsyncLight, curated grammars, table overflow wrapper, inline-vs-block code). §4 integration → Task 2 (single `MessageBubble` assistant branch = live + replay; tool/user/error unchanged). §5 streaming safety → Task 1 code heuristic (`'text'` fallback, no throw) + Task 2 pending cursor preserved. §6 security → Global Constraints + Task 1 (no rehype-raw / no dangerouslySetInnerHTML; links `rel="noopener noreferrer"`). §7 deps → Task 1 Step 1. §8 testing → per-task lint+build + the Manual verification section.
- **Placeholder scan:** none — every code step contains the full literal code.
- **Type consistency:** `Markdown({ children })` default export (Task 1) ↔ `import Markdown from '../components/Markdown'` + `<Markdown>{item.content}</Markdown>` (Task 2). Consistent.
- **YAGNI:** no code-block copy buttons, no user-message markdown, no extra grammars, no bundle-splitting of the component itself — all explicitly out of scope per spec §9.
