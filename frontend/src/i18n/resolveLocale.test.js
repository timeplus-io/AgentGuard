import { describe, it, expect, beforeEach, vi } from 'vitest'
import { readStoredLocale, storeLocale, resolveInitialLocale } from './resolveLocale.js'

beforeEach(() => localStorage.clear())

describe('locale resolution', () => {
  it('returns null when nothing stored', () => {
    expect(readStoredLocale()).toBeNull()
  })
  it('round-trips a stored, supported locale', () => {
    storeLocale('zh-CN')
    expect(readStoredLocale()).toBe('zh-CN')
  })
  it('ignores an unsupported stored value', () => {
    localStorage.setItem('agentguard.locale', 'fr')
    expect(readStoredLocale()).toBeNull()
  })
  it('resolveInitialLocale falls back to English with no storage', () => {
    expect(resolveInitialLocale()).toBe('en')
  })
})
