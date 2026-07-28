import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import enCommon from './locales/en/common.json'
import zhCommon from './locales/zh-CN/common.json'
import enSettings from './locales/en/settings.json'
import zhSettings from './locales/zh-CN/settings.json'
import enAuth from './locales/en/auth.json'
import zhAuth from './locales/zh-CN/auth.json'
import enDashboard from './locales/en/dashboard.json'
import zhDashboard from './locales/zh-CN/dashboard.json'

export const SUPPORTED_LOCALES = ['en', 'zh-CN']
export const DEFAULT_LOCALE = 'en'

// Namespaces are registered here as page tasks add catalogs. common is the base.
const resources = {
  en: { common: enCommon, settings: enSettings, auth: enAuth, dashboard: enDashboard },
  'zh-CN': { common: zhCommon, settings: zhSettings, auth: zhAuth, dashboard: zhDashboard },
}

i18n.use(initReactI18next).init({
  resources,
  lng: DEFAULT_LOCALE,
  fallbackLng: DEFAULT_LOCALE,
  defaultNS: 'common',
  ns: ['common', 'settings', 'auth', 'dashboard'],
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
