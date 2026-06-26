import { describe, it, expect } from 'vitest'
import { canonicalMultiChoiceValue, toggleMultiChoiceValue } from './paramValues.js'

describe('multi_choice value helpers', () => {
  it('canonicalizes selections in declaration order', () => {
    expect(canonicalMultiChoiceValue(['read', 'write', 'delete'], ['delete', 'read'])).toEqual([
      'read',
      'delete',
    ])
  })

  it('adds a selected option in declaration order instead of click order', () => {
    expect(toggleMultiChoiceValue(['read', 'write', 'delete'], ['write'], 'read')).toEqual(['read', 'write'])
  })

  it('removes a selected option and keeps the remaining declaration order', () => {
    expect(toggleMultiChoiceValue(['read', 'write', 'delete'], ['delete', 'read'], 'read')).toEqual(['delete'])
  })

  it('drops values outside the declared option list', () => {
    expect(canonicalMultiChoiceValue(['read', 'write'], ['admin', 'write'])).toEqual(['write'])
  })
})
