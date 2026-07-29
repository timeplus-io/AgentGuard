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
import enAgents from './locales/en/agents.json'
import zhAgents from './locales/zh-CN/agents.json'
import enThreats from './locales/en/threats.json'
import zhThreats from './locales/zh-CN/threats.json'
import enRules from './locales/en/rules.json'
import zhRules from './locales/zh-CN/rules.json'
import enCost from './locales/en/cost.json'
import zhCost from './locales/zh-CN/cost.json'
import enDlp from './locales/en/dlp.json'
import zhDlp from './locales/zh-CN/dlp.json'
import enSentry from './locales/en/sentry.json'
import zhSentry from './locales/zh-CN/sentry.json'
import enOnboarding from './locales/en/onboarding.json'
import zhOnboarding from './locales/zh-CN/onboarding.json'
import enInvestigate from './locales/en/investigate.json'
import zhInvestigate from './locales/zh-CN/investigate.json'

export const SUPPORTED_LOCALES = ['en', 'zh-CN']
export const DEFAULT_LOCALE = 'en'

// Namespaces are registered here as page tasks add catalogs. common is the base.
const resources = {
  en: { common: enCommon, settings: enSettings, auth: enAuth, dashboard: enDashboard, agents: enAgents, threats: enThreats, rules: enRules, cost: enCost, dlp: enDlp, sentry: enSentry, onboarding: enOnboarding, investigate: enInvestigate },
  'zh-CN': { common: zhCommon, settings: zhSettings, auth: zhAuth, dashboard: zhDashboard, agents: zhAgents, threats: zhThreats, rules: zhRules, cost: zhCost, dlp: zhDlp, sentry: zhSentry, onboarding: zhOnboarding, investigate: zhInvestigate },
}

i18n.use(initReactI18next).init({
  resources,
  lng: DEFAULT_LOCALE,
  fallbackLng: DEFAULT_LOCALE,
  defaultNS: 'common',
  ns: ['common', 'settings', 'auth', 'dashboard', 'agents', 'threats', 'rules', 'cost', 'dlp', 'sentry', 'onboarding', 'investigate'],
  interpolation: { escapeValue: false }, // React already escapes
  returnEmptyString: false,
  initImmediate: false, // synchronous init so t() works immediately (tests + first paint)
})

// Apply the synchronous initial locale: localStorage choice, else English.
// Async server-default adoption (ui.default_language) happens in App after auth.
try {
  const stored = localStorage.getItem('agentguard.locale')
  const loc = SUPPORTED_LOCALES.includes(stored) ? stored : DEFAULT_LOCALE
  i18n.changeLanguage(loc)
  if (typeof document !== 'undefined') document.documentElement.lang = loc
} catch { /* storage/document unavailable — stays on English */ }

export default i18n
