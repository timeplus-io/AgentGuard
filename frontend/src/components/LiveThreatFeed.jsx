const severityCard = {
  critical: { border: 'border-l-[#ffb4ab]', iconColor: 'text-error' },
  warning:  { border: 'border-l-[#ffb2b8]', iconColor: 'text-[#ffb2b8]' },
  info:     { border: 'border-l-zinc-700',  iconColor: 'text-gray-400'  },
}

const CATEGORY_ICONS = {
  prompt_injection_and_hijacking:  'phishing',
  sensitive_data_exposure:         'visibility_off',
  agent_privilege_escalation:      'admin_panel_settings',
  supply_chain_compromise:         'hub',
  resource_exhaustion_and_dos:     'speed',
  data_exfiltration_and_leakage:   'data_loss_prevention',
  credential_theft_and_harvesting: 'key_off',
  lateral_movement_and_pivoting:   'swap_horiz',
}

function formatThreatTime(ts) {
  if (!ts) return '—'
  try {
    return new Date(typeof ts === 'string' ? ts.replace(' ', 'T') : ts)
      .toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  } catch { return String(ts) }
}

export function ThreatFeedItem({ threat, index = 0, ruleCategory = {}, compact = false, onClick }) {
  const sev = threat.severity?.toLowerCase() || 'info'
  const { border, iconColor } = severityCard[sev] || severityCard.info
  const catIcon = CATEGORY_ICONS[ruleCategory[threat.rule_id]] || 'shield'
  const detectedAt = threat.detected_at || threat.last_seen
  const message = threat.message || threat.events?.[0]?.message || 'No message available.'
  const hookName = threat.hook_name || threat.events?.[0]?.hook_name

  return (
    <button
      type="button"
      onClick={() => onClick?.(threat)}
      className={`w-full text-left flex items-start gap-4 bg-gray-800 border border-gray-600 rounded-sm border-l-4 ${border} cursor-pointer transition-colors hover:bg-gray-700/60 ${compact ? 'p-3' : 'p-4'}`}
    >
      <div className="mt-1">
        <span className={`material-symbols-outlined ${iconColor}`} style={{ fontVariationSettings: "'FILL' 1" }}>
          {catIcon}
        </span>
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex justify-between items-start gap-2">
          <h4 className="text-xs font-semibold text-gray-100 truncate">{threat.rule_name}</h4>
          <span className="text-[10px] font-mono text-gray-400 whitespace-nowrap flex-shrink-0">
            {formatThreatTime(detectedAt)}
          </span>
        </div>
        <p className={`text-xs text-gray-400 mt-1 italic ${compact ? 'line-clamp-2' : 'line-clamp-1'}`}>
          {message}
        </p>
        <div className="flex gap-2 mt-3 flex-wrap">
          {threat.agent_id && (
            <span className="px-2 py-0.5 bg-gray-600 text-gray-100 text-[9px] font-semibold uppercase rounded-full">
              {threat.agent_id}
            </span>
          )}
          {hookName && (
            <span className="px-2 py-0.5 bg-gray-600 text-gray-100 text-[9px] font-semibold uppercase rounded-full">
              {hookName}
            </span>
          )}
          {compact && index === 0 && (
            <span className="px-2 py-0.5 bg-[#ffb0cf]/10 text-primary text-[9px] font-semibold uppercase rounded-full">
              Latest
            </span>
          )}
        </div>
      </div>
    </button>
  )
}

export function LiveThreatFeedList({
  threats,
  connected,
  error,
  ruleCategory,
  compact = false,
  emptyMessage,
  onThreatClick,
}) {
  if (threats.length === 0) {
    return (
      <p className="text-xs py-4 flex items-center gap-2">
        {error
          ? <><span className="material-symbols-outlined text-sm text-red-400">error</span><span className="text-red-400 font-mono">{error}</span></>
          : <span className="text-gray-400">{emptyMessage || (connected ? 'Waiting for events…' : 'Monitoring…')}</span>
        }
      </p>
    )
  }

  return threats.map((threat, i) => (
    <ThreatFeedItem
      key={threat.threat_id || `${threat.detected_at || threat.last_seen}-${threat.rule_id}-${i}`}
      threat={threat}
      index={i}
      ruleCategory={ruleCategory}
      compact={compact}
      onClick={onThreatClick}
    />
  ))
}
