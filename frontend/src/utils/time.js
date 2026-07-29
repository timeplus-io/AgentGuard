import i18n from '../i18n'

// Format a Proton/Go timestamp for local-time display.
//
// Proton returns datetime64 values as bare strings without a timezone marker
// (e.g. "2026-05-06 10:42:11.123"). The ECMAScript spec treats date-time strings
// without a timezone offset as LOCAL time, so naively passing these to new Date()
// produces a time shifted by the user's UTC offset. We fix this by appending "Z"
// to any string that does not already carry a timezone, ensuring the value is
// always interpreted as UTC before being converted to local time for display.
export function formatLocalTime(ts) {
  if (!ts) return ''
  try {
    let s = typeof ts === 'string' ? ts.replace(' ', 'T') : ts
    if (typeof s === 'string' && !s.endsWith('Z') && !/[+-]\d{2}:\d{2}$/.test(s)) {
      s = s + 'Z'
    }
    const d = new Date(s)
    if (isNaN(d)) return String(ts)
    return d.toLocaleTimeString(i18n.language || undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  } catch {
    return String(ts)
  }
}
