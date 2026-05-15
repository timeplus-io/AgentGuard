import { describe, it, expect } from 'vitest'
import { scoreRule, rankBySearch } from './ruleSearch.js'

// Regression coverage for github.com/timeplus-io/AgentGuard issue #124:
// "Prompt Injection" should rank "Prompt Injection Shield" ahead of rules
// that only mention prompt injection in their description.

const promptInjectionShield = {
  id: 'rp_001',
  name: 'Prompt Injection Shield',
  description: 'Detects untrusted instructions injected via tool output.',
  category: 'prompt_injection_and_hijacking',
}

const descriptionOnlyMatch = {
  id: 'rp_002',
  name: 'Outbound URL Allowlist',
  description: 'Mitigates prompt injection by restricting outbound URLs.',
  category: 'data_exfiltration_and_leakage',
}

const unrelated = {
  id: 'rp_003',
  name: 'Resource Throttle',
  description: 'Caps tool concurrency.',
  category: 'resource_exhaustion_and_dos',
}

describe('scoreRule', () => {
  it.each(['', '   ', '\t\n'])('returns a positive score for every rule when the query is %j (empty or whitespace-only)', (q) => {
    expect(scoreRule(q, promptInjectionShield)).toBeGreaterThan(0)
    expect(scoreRule(q, unrelated)).toBeGreaterThan(0)
  })

  it('returns 0 when nothing in the rule matches', () => {
    expect(scoreRule('quantum entanglement', promptInjectionShield)).toBe(0)
  })

  it('is case insensitive', () => {
    const a = scoreRule('prompt injection shield', promptInjectionShield)
    const b = scoreRule('PROMPT INJECTION SHIELD', promptInjectionShield)
    expect(a).toBe(b)
    expect(a).toBeGreaterThan(0)
  })

  it('ranks exact normalized name match highest', () => {
    const exact = scoreRule('prompt injection shield', promptInjectionShield)
    const prefix = scoreRule('prompt injection', promptInjectionShield)
    expect(exact).toBeGreaterThan(prefix)
  })

  it('ranks name-prefix match above a name-substring match', () => {
    const prefix = scoreRule('prompt', promptInjectionShield) // matches start of name
    const middle = scoreRule('injection', promptInjectionShield) // appears mid-name
    expect(prefix).toBeGreaterThan(middle)
  })

  it('ranks any name match above a description-only match', () => {
    // The core bug: "Prompt Injection Shield" must outrank a rule that only
    // mentions "prompt injection" in its description.
    const nameMatch = scoreRule('prompt injection', promptInjectionShield)
    const descMatch = scoreRule('prompt injection', descriptionOnlyMatch)
    expect(nameMatch).toBeGreaterThan(descMatch)
    expect(descMatch).toBeGreaterThan(0)
  })

  it('matches query tokens out of order in the name', () => {
    // "shield prompt" — different word order than the rule name — should still
    // score above a description-only match.
    const tokens = scoreRule('shield prompt', promptInjectionShield)
    const descOnly = scoreRule('prompt injection', descriptionOnlyMatch)
    expect(tokens).toBeGreaterThan(descOnly)
  })

  it('ranks category/tag matches above description-only matches', () => {
    const catRule = { id: 'rp_x', name: 'Untitled', description: '', category: 'credential_theft_and_harvesting' }
    const descRule = { id: 'rp_y', name: 'Untitled', description: 'credential theft helper', category: 'other' }
    expect(scoreRule('credential', catRule)).toBeGreaterThan(scoreRule('credential', descRule))
  })

  it('handles empty, missing-field, null, and undefined rule inputs defensively', () => {
    for (const r of [{}, null, undefined]) {
      expect(() => scoreRule('foo', r)).not.toThrow()
      expect(scoreRule('foo', r)).toBe(0)
      // An empty query still returns the "match all" positive score even when
      // the rule itself has nothing to match against.
      expect(scoreRule('', r)).toBeGreaterThan(0)
    }
  })

  it('matches when a query token is a substring of a name token (not just prefix)', () => {
    // name tokens after split: ['reinjection']. Query token 'injection' is a
    // substring (not a prefix) of 'reinjection' — tier-4 must still match.
    const rule = { id: 'rp_x', name: 'Block Reinjection Attempts', description: '', category: '' }
    expect(scoreRule('block injection', rule)).toBeGreaterThan(0)
  })

  it('matches multi-token queries against a multi-word category', () => {
    const rule = { id: 'rp_x', name: 'Stealthy Rule', description: '', category: 'credential_theft_and_harvesting' }
    // 'credential theft' tokens both appear in the normalized category string.
    expect(scoreRule('credential theft', rule)).toBeGreaterThan(0)
  })

  it('matches multi-token queries against a category when tokens are out of order', () => {
    // 'theft credential' is not a contiguous substring of the normalized
    // category 'credential theft and harvesting', so this exercises the
    // per-token category match path.
    const rule = { id: 'rp_x', name: 'Stealthy Rule', description: '', category: 'credential_theft_and_harvesting' }
    expect(scoreRule('theft credential', rule)).toBeGreaterThan(0)
  })

  it('matches against rule.tags when present', () => {
    const rule = { id: 'rp_x', name: 'Stealthy Rule', description: '', category: '', tags: ['llm-safety', 'owasp-llm01'] }
    expect(scoreRule('owasp', rule)).toBeGreaterThan(0)
    expect(scoreRule('llm-safety', rule)).toBeGreaterThan(0)
  })

  it('falls back to a cross-field token match for multi-token queries that span name+desc', () => {
    // No single field contains the full query, but every query token appears
    // somewhere across name+description. Lowest non-zero tier.
    const rule = { id: 'rp_x', name: 'Outbound Allowlist', description: 'Blocks unsafe domains.', category: '' }
    const score = scoreRule('outbound unsafe', rule)
    expect(score).toBeGreaterThan(0)
    // Must still rank below a clean description-only match on a competing rule.
    const cleanDescMatch = scoreRule('outbound unsafe', { id: 'rp_y', name: 'Other', description: 'outbound unsafe' })
    expect(cleanDescMatch).toBeGreaterThan(score)
  })

  it('does not invent a category match when the rule has no category or tags', () => {
    // Single-token query that is not present anywhere — must return 0, never
    // 30 via an empty-string category fluke.
    expect(scoreRule('quantum', { id: 'rp_x', name: 'A', description: 'B' })).toBe(0)
  })

  it.each(['-', '/', '_', '   -   ', '/-_'])(
    'treats delimiter-only query %j as semantically empty (no false ranking)',
    (q) => {
      // After tokenization, a query of only delimiters yields zero tokens.
      // Two problems can arise:
      //   1. `qTokens.every(...)` is vacuously true on [], so the category
      //      tier (30) fires for every rule even without a real cat/tag hit.
      //   2. Tier 1–3 (name substring) still runs against the literal `q`,
      //      so a rule whose name happens to contain the delimiter (e.g.
      //      `Rate-Limit Guard` for query `-`) gets tier-3 = 60 while a
      //      rule without that punctuation gets tier-5 = 30.
      // Result: delimiter-only queries silently reorder the list. The fix
      // must treat delimiter-only as semantically empty so every rule scores
      // equally and the page's default ordering is preserved.
      const punctuatedName    = { name: 'a-b/c_d', description: '', category: '' }
      const punctuationInDesc = { name: 'plain',   description: 'has - and / and _ here', category: '' }
      const noPunctuation     = { name: 'plain',   description: '',                       category: 'foo' }
      const scores = [punctuatedName, punctuationInDesc, noPunctuation].map(r => scoreRule(q, r))
      expect(new Set(scores).size).toBe(1)
      // Match-all (positive) is consistent with how whitespace-only queries behave.
      expect(scores[0]).toBeGreaterThan(0)
    },
  )
})

describe('rankBySearch', () => {
  // Mirror the installed-rules shape on RulePacks.jsx: `desc` instead of `description`,
  // and an `enabled` flag that drives the tie-breaker.
  const installedShape = [
    { id: 'rp_001', name: 'Prompt Injection Shield', desc: 'Detects injected instructions.', category: 'prompt_injection_and_hijacking', enabled: true },
    { id: 'rp_002', name: 'Outbound URL Allowlist',  desc: 'Mitigates prompt injection by restricting URLs.', category: 'data_exfiltration_and_leakage', enabled: true },
    { id: 'rp_003', name: 'Resource Throttle',       desc: 'Caps tool concurrency.', category: 'resource_exhaustion_and_dos', enabled: false },
    { id: 'rp_004', name: 'Alpha Rule',              desc: 'No mention of relevant terms.', category: 'other', enabled: true },
  ]

  const installedFields = (r) => ({ name: r.name, description: r.desc, category: r.category })
  const installedTieBreak = (a, b) => {
    if (a.enabled !== b.enabled) return a.enabled ? -1 : 1
    return (a.name || '').localeCompare(b.name || '')
  }
  const alphaTieBreak = (a, b) => (a.name || '').localeCompare(b.name || '')

  it.each(['', '   '])('returns items sorted by tieBreak when query is %j (empty or whitespace-only)', (q) => {
    const ids = rankBySearch(installedShape, q, installedFields, installedTieBreak).map(r => r.id)
    // enabled-first, then alphabetical: Alpha Rule, Outbound URL Allowlist, Prompt Injection Shield (all enabled, alpha order),
    // then Resource Throttle (disabled).
    expect(ids).toEqual(['rp_004', 'rp_002', 'rp_001', 'rp_003'])
  })

  it('drops zero-score items and ranks by relevance for a real query', () => {
    const ids = rankBySearch(installedShape, 'Prompt Injection', installedFields, installedTieBreak).map(r => r.id)
    // rp_001 (name match) > rp_002 (desc-only) > [rp_003, rp_004 dropped — no match anywhere]
    expect(ids).toEqual(['rp_001', 'rp_002'])
  })

  it('respects category-tier matching using the field carried from server state', () => {
    // Searching the category — a rule whose name/desc never mention "exfiltration"
    // still matches because its category is data_exfiltration_and_leakage.
    const ids = rankBySearch(installedShape, 'exfiltration', installedFields, installedTieBreak).map(r => r.id)
    expect(ids).toEqual(['rp_002'])
  })

  it('falls back to tieBreak when multiple items share the same score', () => {
    const items = [
      { id: 'b', name: 'Prompt Injection Shield', desc: 'x', category: 'x', enabled: true },
      { id: 'a', name: 'Prompt Injection Shield', desc: 'y', category: 'y', enabled: true },
    ]
    // Both score 100 (exact name match). Tie-break is alphabetical on name —
    // identical names, so order is stable; we rely on Array#sort being stable
    // (TC39 guarantee since 2019) to preserve input order: b, a.
    const ids = rankBySearch(items, 'Prompt Injection Shield', installedFields, installedTieBreak).map(r => r.id)
    expect(ids).toEqual(['b', 'a'])
  })

  it('uses identity field mapping when the item already matches scoreRule shape', () => {
    const catalogShape = [
      { id: 'rp_c1', name: 'Sensitive Data Scanner', description: 'scans for PII', category: 'sensitive_data_exposure' },
      { id: 'rp_c2', name: 'Other', description: 'no match', category: 'other' },
    ]
    const ids = rankBySearch(catalogShape, 'sensitive', e => e, alphaTieBreak).map(e => e.id)
    expect(ids).toEqual(['rp_c1'])
  })

  it('does not mutate the input array', () => {
    const original = [...installedShape]
    rankBySearch(installedShape, '', installedFields, installedTieBreak)
    expect(installedShape).toEqual(original)
  })
})
