import { describe, it, expect } from 'vitest'
import { packageMemberSet, scopeToPackage } from './packageScope.js'

describe('packageMemberSet', () => {
  it('returns null for no package (the "All" selection)', () => {
    expect(packageMemberSet(null)).toBeNull()
    expect(packageMemberSet(undefined)).toBeNull()
  })

  it('returns a Set of the package member rule ids', () => {
    const set = packageMemberSet({ id: 'core', rules: ['rp-001', 'rp-002'] })
    expect(set).toBeInstanceOf(Set)
    expect(set.has('rp-001')).toBe(true)
    expect(set.has('rp-002')).toBe(true)
    expect(set.has('rp-003')).toBe(false)
  })

  it('tolerates a package with no rules array', () => {
    expect(packageMemberSet({ id: 'x' }).size).toBe(0)
  })
})

describe('scopeToPackage', () => {
  const items = [{ id: 'rp-001' }, { id: 'rp-002' }, { id: 'rp-003' }]

  it('returns all items unchanged when memberSet is null (All)', () => {
    expect(scopeToPackage(items, null)).toBe(items)
  })

  it('keeps only items whose id is in the member set, preserving order', () => {
    const set = new Set(['rp-003', 'rp-001'])
    expect(scopeToPackage(items, set)).toEqual([{ id: 'rp-001' }, { id: 'rp-003' }])
  })

  it('supports a custom id accessor', () => {
    const rows = [{ ruleId: 'a' }, { ruleId: 'b' }]
    const set = new Set(['b'])
    expect(scopeToPackage(rows, set, r => r.ruleId)).toEqual([{ ruleId: 'b' }])
  })
})
