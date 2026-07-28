import { describe, it, expect, afterEach } from 'vitest'
import i18n from './index.js'
import { formatNumber, formatCurrency, formatDate, formatRelativeTime } from './format.js'

afterEach(async () => { await i18n.changeLanguage('en') })

describe('format helpers', () => {
  it('formats numbers with the active locale grouping', () => {
    expect(formatNumber(1234567)).toBe('1,234,567')
  })
  it('formats currency', () => {
    expect(formatCurrency(12.5)).toBe('$12.50')
  })
  it('formatDate returns empty string for null', () => {
    expect(formatDate(null)).toBe('')
  })
  it('formatDate returns empty string for undefined', () => {
    expect(formatDate(undefined)).toBe('')
  })
  it('formatDate formats a valid Date to a non-empty string', () => {
    expect(formatDate(new Date('2026-01-15T10:00:00Z')).length).toBeGreaterThan(0)
  })
  it('formatRelativeTime returns empty string for null', () => {
    expect(formatRelativeTime(null)).toBe('')
  })
  it('formatRelativeTime formats a valid Date to a non-empty string', () => {
    const result = formatRelativeTime(new Date('2026-01-01T00:00:00Z'), new Date('2026-01-01T00:01:00Z').getTime())
    expect(result).toBeTruthy()
  })
})
