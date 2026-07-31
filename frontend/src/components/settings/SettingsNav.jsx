import { useTranslation } from 'react-i18next'

// Purely presentational: the page computes active id, dirty counts and flag sets.
export default function SettingsNav({
  sections,
  activeId,
  dirtyCounts,
  restartIds,
  reprovisionIds,
  showRestart,
  showReprovision,
  onSelect,
}) {
  const { t } = useTranslation('settings')
  return (
    <nav className="flex flex-wrap items-center gap-x-6 gap-y-2 border-b border-gray-700">
      {sections.map((s) => {
        const active = s.id === activeId
        const dirty = dirtyCounts[s.id] || 0
        const restart = showRestart && restartIds.has(s.id)
        const reprovision = showReprovision && reprovisionIds.has(s.id)
        const stateMsgs = [
          restart && t('nav.stateRestart'),
          reprovision && t('nav.stateReprovision'),
        ].filter(Boolean)
        return (
          <button
            key={s.id}
            onClick={() => onSelect(s.id)}
            aria-current={active ? 'true' : undefined}
            className={`shrink-0 flex items-center gap-2 pb-3 text-sm font-semibold border-b-2 transition-colors ${
              active ? 'border-pink-500 text-gray-100' : 'border-transparent text-gray-400 hover:text-gray-100'
            }`}
          >
            <span className="flex-1 truncate">{t('configSections.' + s.id, { defaultValue: s.title })}</span>
            {dirty > 0 && (
              <span className="bg-[#ffb0cf] text-[#3d0023] text-[9px] font-bold rounded-full px-1.5 py-0.5 min-w-[16px] text-center">
                {dirty}
                <span className="sr-only">{t('nav.dirtySr', { count: dirty })}</span>
              </span>
            )}
            {stateMsgs.length > 0 && (
              <span title={stateMsgs.join('; ')} className="w-1.5 h-1.5 rounded-full bg-amber-400 shrink-0">
                <span className="sr-only">{stateMsgs.join('; ')}</span>
              </span>
            )}
          </button>
        )
      })}
    </nav>
  )
}
