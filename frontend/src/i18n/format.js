import i18n from './index.js'

const locale = () => i18n.language || 'en'

export function formatDate(value, opts = { dateStyle: 'medium', timeStyle: 'short' }) {
  if (value == null) return ''
  const d = value instanceof Date ? value : new Date(value)
  if (isNaN(d.getTime())) return ''
  return new Intl.DateTimeFormat(locale(), opts).format(d)
}

export function formatNumber(value, opts = {}) {
  if (value == null || isNaN(value)) return ''
  return new Intl.NumberFormat(locale(), opts).format(value)
}

export function formatCurrency(value, currency = 'USD') {
  if (value == null || isNaN(value)) return ''
  return new Intl.NumberFormat(locale(), { style: 'currency', currency }).format(value)
}

const REL_STEPS = [
  ['year', 31536000], ['month', 2592000], ['day', 86400],
  ['hour', 3600], ['minute', 60], ['second', 1],
]

export function formatRelativeTime(value, nowMs) {
  if (value == null) return ''
  const then = (value instanceof Date ? value : new Date(value)).getTime()
  if (isNaN(then)) return ''
  const now = nowMs ?? Date.now()
  let delta = Math.round((then - now) / 1000)
  const rtf = new Intl.RelativeTimeFormat(locale(), { numeric: 'auto' })
  for (const [unit, secs] of REL_STEPS) {
    if (Math.abs(delta) >= secs || unit === 'second') {
      return rtf.format(Math.round(delta / secs), unit)
    }
  }
  return rtf.format(delta, 'second')
}
