// Sticky section nav for the Settings page (issue #199). Purely
// presentational: the page computes active id, dirty counts and flag sets.
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
  return (
    <nav className="w-48 shrink-0 sticky top-24 self-start flex flex-col gap-1">
      {sections.map((s) => {
        const active = s.id === activeId
        const dirty = dirtyCounts[s.id] || 0
        const restart = showRestart && restartIds.has(s.id)
        const reprovision = showReprovision && reprovisionIds.has(s.id)
        return (
          <button
            key={s.id}
            onClick={() => onSelect(s.id)}
            className={`flex items-center gap-2 px-3 py-2 rounded-sm text-left text-xs font-medium transition-all ${
              active ? 'text-pink-400 bg-gray-700' : 'text-gray-400 hover:bg-gray-700 hover:text-gray-100'
            }`}
          >
            <span className="flex-1 truncate">{s.title}</span>
            {dirty > 0 && (
              <span className="bg-[#ffb0cf] text-[#3d0023] text-[9px] font-bold rounded-full px-1.5 py-0.5 min-w-[16px] text-center">
                {dirty}
              </span>
            )}
            {(restart || reprovision) && (
              <span
                title={restart ? 'Restart required to apply saved changes' : 'Re-provision required to apply model changes'}
                className="w-1.5 h-1.5 rounded-full bg-amber-400 shrink-0"
              />
            )}
          </button>
        )
      })}
    </nav>
  )
}
