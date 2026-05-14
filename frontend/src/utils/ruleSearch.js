// Lightweight relevance scoring for the Rule Packs search box.
//
// Higher score = more relevant. Score 0 means no match. Tiers are spaced so
// any name match beats any description-only match — see issue #124, where
// "Prompt Injection" was returning rules whose descriptions merely mention
// prompt injection ahead of the rule literally named "Prompt Injection Shield".
//
// Tiers (highest to lowest):
//   100  exact normalized name match
//    80  name starts with the query
//    60  name contains the query as a substring
//    50  every query token appears as a name token (any order)
//    30  category or tag match
//    20  description contains the query
//    10  every query token appears somewhere across name/desc/category/tags

const TOKEN_SPLIT = /[\s_\-/]+/

function tokenize(s) {
  return s.split(TOKEN_SPLIT).filter(Boolean)
}

export function scoreRule(query, rule) {
  const q = (query || '').trim().toLowerCase()
  if (!q) return 1

  // Delimiter-only queries (e.g. "-", "/", "_") tokenize to nothing. Without
  // this guard, two things go wrong: name-substring (tier 3) fires whenever
  // a rule's name happens to contain the punctuation, and `qTokens.every(...)`
  // is vacuously true on the empty token list so the category tier triggers
  // for every rule. Treat as semantically empty — same as a blank query.
  const qTokens = tokenize(q)
  if (qTokens.length === 0) return 1

  const name = (rule?.name || '').toLowerCase()
  const desc = (rule?.description || '').toLowerCase()
  const cat = (rule?.category || '').toLowerCase().replace(/[_-]/g, ' ')
  const tags = Array.isArray(rule?.tags)
    ? rule.tags.map(t => String(t).toLowerCase())
    : []

  if (name && name === q) return 100
  if (name && name.startsWith(q)) return 80
  if (name && name.includes(q)) return 60

  const nameTokens = tokenize(name)

  if (qTokens.length > 1 && nameTokens.length > 0
    && qTokens.every(qt => nameTokens.some(nt => nt.startsWith(qt) || nt.includes(qt)))) {
    return 50
  }

  const tagMatches = (needle) => tags.some(t => t.includes(needle))
  if (cat.includes(q) || tagMatches(q)) return 30
  if (qTokens.every(qt => cat.includes(qt) || tagMatches(qt))) return 30

  if (desc.includes(q)) return 20

  if (qTokens.length > 1) {
    const haystack = `${name} ${desc} ${cat} ${tags.join(' ')}`
    if (qTokens.every(qt => haystack.includes(qt))) return 10
  }

  return 0
}

// rankBySearch returns `items` filtered by relevance against `query`, sorted
// by score (highest first) with `tieBreak` as the secondary comparator. When
// the query is empty or whitespace-only, returns items in `tieBreak` order
// unchanged — preserving the page's default ordering.
//
// `getSearchFields` maps a domain item to the shape scoreRule expects:
//   { name, description, category, tags }.
// Most callers pass the item through directly when its fields already match.
export function rankBySearch(items, query, getSearchFields, tieBreak) {
  if (!query || !query.trim()) return [...items].sort(tieBreak)
  return items
    .map(item => ({ item, score: scoreRule(query, getSearchFields(item)) }))
    .filter(({ score }) => score > 0)
    .sort((a, b) => (b.score - a.score) || tieBreak(a.item, b.item))
    .map(({ item }) => item)
}
