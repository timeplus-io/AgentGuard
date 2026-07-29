import { useTranslation } from 'react-i18next'
import { SUPPORTED_LOCALES } from '../../i18n'
import { applyLocale, storeLocale, readStoredLocale } from '../../i18n/resolveLocale'

export default function LanguageSelector() {
  const { t, i18n } = useTranslation('settings')
  const current = readStoredLocale() || i18n.language || 'en'

  const onChange = (e) => {
    const loc = e.target.value
    storeLocale(loc)
    applyLocale(loc)
  }

  return (
    <div className="flex flex-col gap-1">
      <label htmlFor="ag-language" className="text-xs font-medium text-gray-300">
        {t('language.label')}
      </label>
      <select
        id="ag-language"
        value={SUPPORTED_LOCALES.includes(current) ? current : 'en'}
        onChange={onChange}
        className="bg-gray-700 text-gray-100 rounded-sm px-3 py-2 text-sm w-56"
      >
        {SUPPORTED_LOCALES.map((loc) => (
          <option key={loc} value={loc}>{t(`language.names.${loc}`, { defaultValue: loc })}</option>
        ))}
      </select>
      <p className="text-xs text-gray-400">{t('language.help')}</p>
    </div>
  )
}
