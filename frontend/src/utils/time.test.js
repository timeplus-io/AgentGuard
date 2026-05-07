import { describe, it, expect } from 'vitest'
import { formatLocalTime } from './time.js'

// All Proton datetime64 columns store UTC. The HTTP/native drivers return strings
// without a timezone suffix (e.g. "2026-05-06 10:42:11.123") or time.Time values
// serialized by Go as RFC3339 with "Z" or "+HH:MM". formatLocalTime must treat
// bare strings as UTC so they display in the user's local timezone correctly.

describe('formatLocalTime', () => {
  it('returns empty string for null/undefined', () => {
    expect(formatLocalTime(null)).toBe('')
    expect(formatLocalTime(undefined)).toBe('')
    expect(formatLocalTime('')).toBe('')
  })

  it('parses Proton bare datetime string (space separator, no Z) as UTC', () => {
    // "2026-05-06 10:42:11.123" arrives from Proton with no timezone marker.
    // Without appending Z, browsers treat this as LOCAL time — wrong for UTC+8 users.
    const result = formatLocalTime('2026-05-06 10:42:11.123')
    expect(typeof result).toBe('string')
    expect(result).not.toBe('')
    // Both representations of the same UTC instant must produce the same display string.
    const groundTruth = formatLocalTime('2026-05-06T10:42:11.123Z')
    expect(result).toBe(groundTruth)
  })

  it('parses RFC3339 with Z suffix as UTC', () => {
    const result = formatLocalTime('2026-05-06T10:42:11.000Z')
    expect(typeof result).toBe('string')
    expect(result).not.toBe('')
    // Same instant represented two ways must produce the same display string.
    const bareResult = formatLocalTime('2026-05-06 10:42:11.000')
    expect(result).toBe(bareResult)
  })

  it('parses RFC3339 with +00:00 offset as UTC', () => {
    const withOffset = formatLocalTime('2026-05-06T10:42:11.000+00:00')
    const withZ = formatLocalTime('2026-05-06T10:42:11.000Z')
    expect(withOffset).toBe(withZ)
  })

  it('returns non-empty string for valid timestamps', () => {
    expect(formatLocalTime('2026-05-06T10:42:11Z')).not.toBe('')
  })
})
