import { describe, it, expect, afterEach } from 'vitest'
import i18n from './index.js'
import { formatNumber, formatCurrency } from './format.js'

afterEach(async () => { await i18n.changeLanguage('en') })

describe('format helpers', () => {
  it('formats numbers with the active locale grouping', () => {
    expect(formatNumber(1234567)).toBe('1,234,567')
  })
  it('formats currency', () => {
    expect(formatCurrency(12.5)).toBe('$12.50')
  })
})
