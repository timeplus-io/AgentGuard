// Pure helpers for the Settings page section nav (issue #199).
// All functions operate on the GET /api/config response shape:
// { sections: [{ id, title, keys: [{ key, restart, reprovision, ... }] }] }

export function keyToSectionMap(sections) {
  const map = {}
  for (const s of sections || []) {
    for (const k of s.keys || []) map[k.key] = s.id
  }
  return map
}

export function dirtyCountsBySection(drafts, keyToSection) {
  const counts = {}
  for (const key of Object.keys(drafts || {})) {
    const sid = (keyToSection || {})[key]
    if (!sid) continue
    counts[sid] = (counts[sid] || 0) + 1
  }
  return counts
}

export function sectionIdsWithFlag(sections, flag) {
  const out = new Set()
  for (const s of sections || []) {
    if ((s.keys || []).some((k) => k[flag])) out.add(s.id)
  }
  return out
}

export function activeSectionFromHash(hash, sections) {
  const id = (hash || '').replace(/^#/, '')
  if (sections?.some((s) => s.id === id)) return id
  return sections?.[0]?.id ?? null
}
