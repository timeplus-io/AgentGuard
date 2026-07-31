// Pure helpers for the Settings page section nav (issue #199).
// All functions operate on the GET /api/config response shape:
// { sections: [{ id, title, keys: [{ key, restart, reprovision, ... }] }] }

const CONNECTION_SECTION_IDS = ['server', 'timeplus', 'agentguard', 'catalog']

export function groupSettingsSections(sections) {
  if (!sections?.length) return sections || []

  const connectionChildren = sections.filter((s) => CONNECTION_SECTION_IDS.includes(s.id))
  if (connectionChildren.length <= 1) return sections

  const connectionIds = new Set(connectionChildren.map((s) => s.id))
  const grouped = []
  let insertedConnection = false

  for (const section of sections) {
    if (connectionIds.has(section.id)) {
      if (!insertedConnection) {
        grouped.push({
          id: 'connection',
          title: 'Connection',
          keys: connectionChildren.flatMap((s) => s.keys || []),
          children: connectionChildren,
        })
        insertedConnection = true
      }
      continue
    }
    grouped.push(section)
  }

  return grouped
}

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
  const parent = sections?.find((s) => s.children?.some((child) => child.id === id))
  if (parent) return parent.id
  return sections?.[0]?.id ?? null
}
