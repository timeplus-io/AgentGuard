import i18n, { SUPPORTED_LOCALES, DEFAULT_LOCALE } from './index.js'

const KEY = 'agentguard.locale'

export function readStoredLocale() {
  try {
    const v = localStorage.getItem(KEY)
    return SUPPORTED_LOCALES.includes(v) ? v : null
  } catch { return null }
}

export function storeLocale(locale) {
  try { localStorage.setItem(KEY, locale) } catch { /* storage unavailable */ }
}

export function applyLocale(locale) {
  const loc = SUPPORTED_LOCALES.includes(locale) ? locale : DEFAULT_LOCALE
  i18n.changeLanguage(loc)
  try { document.documentElement.lang = loc } catch { /* no document */ }
  return loc
}

export function resolveInitialLocale() {
  return readStoredLocale() || DEFAULT_LOCALE
}

// After auth/config are available: if the user hasn't chosen, adopt the
// deployment default from ui.default_language. Fail-open to English.
// The api client is imported dynamically here (not at module top-level) so
// that pure-function tests (readStoredLocale/storeLocale/resolveInitialLocale)
// don't have to load the proton driver under jsdom.
export async function adoptServerDefault() {
  if (readStoredLocale()) return
  try {
    const { api } = await import('../api/client')
    const cfg = await api.getConfig()
    const key = (cfg.sections || [])
      .flatMap((s) => s.keys || [])
      .find((k) => k.key === 'ui.default_language')
    const val = key && key.value
    if (val && SUPPORTED_LOCALES.includes(val)) applyLocale(val)
  } catch { /* not authenticated / config unavailable — stay on English */ }
}
