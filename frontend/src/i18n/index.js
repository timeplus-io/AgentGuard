import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import enCommon from './locales/en/common.json'
import zhCommon from './locales/zh-CN/common.json'

export const SUPPORTED_LOCALES = ['en', 'zh-CN']
export const DEFAULT_LOCALE = 'en'

// Namespaces are registered here as page tasks add catalogs. common is the base.
const resources = {
  en: { common: enCommon },
  'zh-CN': { common: zhCommon },
}

i18n.use(initReactI18next).init({
  resources,
  lng: DEFAULT_LOCALE,
  fallbackLng: DEFAULT_LOCALE,
  defaultNS: 'common',
  ns: ['common'],
  interpolation: { escapeValue: false }, // React already escapes
  returnEmptyString: false,
  initImmediate: false, // synchronous init so t() works immediately (tests + first paint)
})

// Apply the synchronous initial locale: localStorage choice, else English.
// Async server-default adoption (ui.default_language) happens in App after auth.
try {
  const stored = localStorage.getItem('agentguard.locale')
  const loc = ['en', 'zh-CN'].includes(stored) ? stored : 'en'
  i18n.changeLanguage(loc)
  if (typeof document !== 'undefined') document.documentElement.lang = loc
} catch { /* storage/document unavailable — stays on English */ }

export default i18n
