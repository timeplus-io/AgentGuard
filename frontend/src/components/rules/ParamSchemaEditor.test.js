import { describe, it, expect } from 'vitest'
import { validateKey, PARAM_TYPES, stripInternal } from './ParamSchemaEditor.jsx'

// The component itself renders DOM (no jsdom in this suite), but its key
// validation, type vocabulary, and payload-normalisation logic are pure and
// load-bearing: the editor's Save guard, the backend's accepted parameter
// types, and the _uid-stripping guarantee all depend on them.

describe('PARAM_TYPES', () => {
  it('exposes exactly the eight supported parameter type tokens (Apps vocabulary + AG extensions)', () => {
    expect(PARAM_TYPES).toEqual([
      'integer',
      'float',
      'bool',
      'string',
      'choice',
      'list',
      'regex',
      'duration',
    ])
  })
})

describe('validateKey', () => {
  it('rejects an empty key', () => {
    expect(validateKey('')).toBeTruthy()
    expect(validateKey(undefined)).toBeTruthy()
  })

  it.each(['threshold', 'a', 'max_count', 'x9', 'abc_123_def'])(
    'accepts the well-formed key %j',
    (k) => {
      expect(validateKey(k)).toBeNull()
    },
  )

  it.each([
    ['Threshold', 'leading uppercase'],
    ['1abc', 'leading digit'],
    ['_abc', 'leading underscore'],
    ['ab-cd', 'hyphen'],
    ['ab cd', 'space'],
    ['abcé', 'non-ascii'],
  ])('rejects %j (%s)', (k) => {
    expect(validateKey(k)).toBeTruthy()
  })

  it('rejects a key longer than 64 characters but accepts exactly 64', () => {
    const k64 = 'a' + 'b'.repeat(63) // 64 chars total
    const k65 = 'a' + 'b'.repeat(64) // 65 chars total
    expect(validateKey(k64)).toBeNull()
    expect(validateKey(k65)).toBeTruthy()
  })
})

describe('stripInternal', () => {
  it('removes _uid from each config entry and preserves all real fields', () => {
    const config = [
      { key: 'threshold', label: 'Threshold', type: 'integer', default: 10, min: 1, max: 100, _uid: '_p1' },
      { key: 'pattern', label: 'Pattern', type: 'regex', default: '.*', min_length: 0, _uid: '_p2' },
    ]
    const result = stripInternal(config)
    // _uid must be absent from every entry
    expect(result[0]).not.toHaveProperty('_uid')
    expect(result[1]).not.toHaveProperty('_uid')
    // All real fields must be preserved
    expect(result[0]).toEqual({ key: 'threshold', label: 'Threshold', type: 'integer', default: 10, min: 1, max: 100 })
    expect(result[1]).toEqual({ key: 'pattern', label: 'Pattern', type: 'regex', default: '.*', min_length: 0 })
  })

  it('is a no-op on entries that have no _uid', () => {
    const config = [{ key: 'count', label: 'Count', type: 'integer', default: 5 }]
    expect(stripInternal(config)).toEqual(config)
  })

  it('returns an empty array unchanged', () => {
    expect(stripInternal([])).toEqual([])
  })

  it('passes through non-array values unchanged', () => {
    expect(stripInternal(null)).toBeNull()
    expect(stripInternal(undefined)).toBeUndefined()
  })

  it('does not mutate the original config array', () => {
    const row = { key: 'x', type: 'string', default: '', _uid: '_p99' }
    const config = [row]
    stripInternal(config)
    expect(row).toHaveProperty('_uid') // original still has _uid
  })
})
