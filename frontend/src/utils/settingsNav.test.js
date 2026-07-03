import { describe, it, expect } from 'vitest'
import {
  keyToSectionMap,
  dirtyCountsBySection,
  sectionIdsWithFlag,
  activeSectionFromHash,
} from './settingsNav.js'

// Shape mirrors GET /api/config: sections -> keys, with static per-key
// restart/reprovision flags (see backend/internal/handlers/settings.go).
const sections = [
  { id: 'server', title: 'Server', keys: [{ key: 'server.port', restart: true }] },
  { id: 'timeplus', title: 'Timeplus Connection', keys: [{ key: 'timeplus.host' }, { key: 'timeplus.port' }] },
  { id: 'dlp', title: 'Semantic DLP', keys: [{ key: 'dlp.enabled' }, { key: 'dlp.embedding_model', reprovision: true }] },
]

describe('keyToSectionMap', () => {
  it('maps every dotted key to its section id', () => {
    const m = keyToSectionMap(sections)
    expect(m['server.port']).toBe('server')
    expect(m['timeplus.host']).toBe('timeplus')
    expect(m['dlp.embedding_model']).toBe('dlp')
  })

  it('returns an empty map for undefined sections', () => {
    expect(keyToSectionMap(undefined)).toEqual({})
  })
})

describe('dirtyCountsBySection', () => {
  const map = keyToSectionMap(sections)

  it('counts dirty keys per section', () => {
    const drafts = { 'timeplus.host': 'x', 'timeplus.port': 1234, 'dlp.enabled': true }
    expect(dirtyCountsBySection(drafts, map)).toEqual({ timeplus: 2, dlp: 1 })
  })

  it('ignores draft keys not present in any section', () => {
    expect(dirtyCountsBySection({ 'ghost.key': 1 }, map)).toEqual({})
  })

  it('returns an empty object for no drafts', () => {
    expect(dirtyCountsBySection({}, map)).toEqual({})
    expect(dirtyCountsBySection(undefined, map)).toEqual({})
  })

  it('returns an empty object when the key map is undefined', () => {
    expect(dirtyCountsBySection({ 'timeplus.host': 'x' }, undefined)).toEqual({})
  })
})

describe('sectionIdsWithFlag', () => {
  it('collects section ids containing a restart-flagged key', () => {
    expect(sectionIdsWithFlag(sections, 'restart')).toEqual(new Set(['server']))
  })

  it('collects section ids containing a reprovision-flagged key', () => {
    expect(sectionIdsWithFlag(sections, 'reprovision')).toEqual(new Set(['dlp']))
  })

  it('returns an empty set for undefined sections', () => {
    expect(sectionIdsWithFlag(undefined, 'restart')).toEqual(new Set())
  })
})

describe('activeSectionFromHash', () => {
  it('returns the section id matching the hash', () => {
    expect(activeSectionFromHash('#dlp', sections)).toBe('dlp')
  })

  it('accepts a hash without the leading #', () => {
    expect(activeSectionFromHash('timeplus', sections)).toBe('timeplus')
  })

  it('falls back to the first section for an unknown hash', () => {
    expect(activeSectionFromHash('#nope', sections)).toBe('server')
  })

  it('falls back to the first section for an empty hash', () => {
    expect(activeSectionFromHash('', sections)).toBe('server')
    expect(activeSectionFromHash(undefined, sections)).toBe('server')
  })

  it('returns null when there are no sections', () => {
    expect(activeSectionFromHash('#dlp', [])).toBeNull()
    expect(activeSectionFromHash('#dlp', undefined)).toBeNull()
  })
})
