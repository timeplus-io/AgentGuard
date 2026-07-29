import { useTranslation } from 'react-i18next'

const severityStyles = {
  critical: { badge: 'bg-red-500 text-white', dot: 'bg-white animate-pulse', label: 'Critical' },
  warning:  { badge: 'bg-orange-900 text-orange-400', dot: 'bg-orange-400',               label: 'Warning' },
  info:     { badge: 'bg-gray-700 text-gray-100',   dot: 'bg-gray-200',                label: 'Info' },
}

const statusStyles = {
  ACTIVE: 'bg-[#ffb0cf]/10 text-primary border border-[#ffb0cf]/20',
  THROTTLED: 'bg-[#ffb2b8]/10 text-[#ffb2b8] border border-[#ffb2b8]/20',
  SUSPENDED: 'bg-[#ffb4ab]/10 text-error border border-[#ffb4ab]/20',
}

export function SeverityBadge({ severity }) {
  const { t } = useTranslation('threats')
  const key = severity?.toLowerCase() || 'info'
  const s = severityStyles[key] || severityStyles.info
  return (
    <span className={`flex items-center gap-2 px-2.5 py-1 text-[10px] font-black uppercase rounded-full w-fit ${s.badge}`}>
      <span className={`w-1.5 h-1.5 rounded-full ${s.dot}`} />
      {t(`severity.${key}`, { defaultValue: s.label })}
    </span>
  )
}

export function StatusBadge({ status }) {
  const { t } = useTranslation('threats')
  return (
    <span className={`px-2 py-0.5 rounded-full text-[10px] font-semibold ${statusStyles[status] || statusStyles.ACTIVE}`}>
      {t(`agentStatus.${status}`, { defaultValue: status })}
    </span>
  )
}

const threatStatusStyles = {
  open:         { label: 'Open',         cls: 'bg-red-900 text-red-400 border border-red-400' },
  acknowledged: { label: 'Acknowledged', cls: 'bg-yellow-900 text-yellow-400 border border-yellow-400' },
  cleared:      { label: 'Cleared',      cls: 'bg-green-900 text-green-400 border border-green-400' },
}

export function ThreatStatusBadge({ status }) {
  const { t } = useTranslation('threats')
  const { label, cls } = threatStatusStyles[status] || { label: status, cls: 'bg-zinc-500/10 text-gray-400 border border-zinc-400/30' }
  return (
    <span className={`text-[10px] font-bold uppercase rounded-full px-2 py-0.5 ${cls}`}>
      {threatStatusStyles[status] ? t(`threatStatus.${status}`, { defaultValue: label }) : status}
    </span>
  )
}
