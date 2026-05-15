import { useEffect, useMemo, useState } from 'react'
import {
  Bar,
  BarChart,
  Cell,
  CartesianGrid,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { api } from '../api/client'

const TIME_WINDOWS = {
  '1m': { label: '1m', durationMs: 60_000, bucketMs: 5_000, tickMs: 1_000 },
  '1h': { label: '1h', durationMs: 60 * 60_000, bucketMs: 5 * 60_000, tickMs: 5_000 },
  '24h': { label: '24h', durationMs: 24 * 60 * 60_000, bucketMs: 2 * 60 * 60_000, tickMs: 30_000 },
}

const SEVERITIES = ['critical', 'warning', 'info']

const SEVERITY_META = {
  critical: { label: 'Critical', color: '#dd3950', text: 'text-red-500', icon: 'gpp_bad' },
  warning: { label: 'Warning', color: '#ff7c27', text: 'text-orange-400', icon: 'warning' },
  info: { label: 'Info', color: '#7d7b82', text: 'text-gray-500', icon: 'info' },
}

const PALETTE = [
  // Start with the app's 500-level colors, then extend for larger agent/rule sets.
  '#d53f8c',
  '#9d55ec',
  '#09a5c6',
  '#23ad8f',
  '#dd6b21',
  '#7475df',
  '#dba400',
  '#7d7b82',
  '#ff73b6',
  '#b275ff',
  '#0bc5ea',
  '#27cca8',
  '#ff7c27',
  '#8890ff',
  '#ffbf00',
  '#b5b4b8',
  '#ffaad2',
  '#c89cff',
  '#51d6f0',
  '#65dbc1',
  '#ffa165',
  '#aab0ff',
  '#ffd149',
  '#dad9db',
]

function parseTime(ts) {
  if (!ts) return null
  let normalized = ts
  if (typeof ts === 'string') {
    normalized = ts.trim().replace(' ', 'T').replace(/\s+UTC$/, 'Z').replace(/\s+\+0000$/, 'Z')
    if (!/[zZ]$|[+-]\d{2}:?\d{2}$/.test(normalized)) {
      normalized = `${normalized}Z`
    }
  }
  const date = new Date(normalized)
  return Number.isNaN(date.getTime()) ? null : date
}

function formatTick(ms, windowKey) {
  const date = new Date(ms)
  if (windowKey === '1m') {
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' })
  }
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })
}

function severityOf(value) {
  const key = value?.toLowerCase()
  return SEVERITIES.includes(key) ? key : 'info'
}

function getBreakdownValue(threat, breakdown) {
  if (breakdown === 'agent') return threat.agent_id || 'unknown-agent'
  if (breakdown === 'rule') return threat.rule_name || threat.rule_id || 'Unknown rule'
  return severityOf(threat.severity)
}

function buildSeries(values, breakdown) {
  return values.map((value, index) => ({
    key: `s${index}`,
    value,
    label: breakdown === 'severity' ? SEVERITY_META[value]?.label || value : value,
    color: breakdown === 'severity' ? SEVERITY_META[value]?.color || '#e5e7eb' : PALETTE[index % PALETTE.length],
  }))
}

function useThreatsData() {
  const [threats, setThreats] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [lastRefreshedAt, setLastRefreshedAt] = useState(null)

  useEffect(() => {
    let cancelled = false

    async function fetchThreats() {
      try {
        const limit = 500
        let page = 1
        let total = 0
        const next = []

        do {
          const data = await api.getThreats({ page, limit })
          next.push(...(data.threats || []))
          total = data.total || next.length
          page += 1
        } while (next.length < total)

        if (!cancelled) {
          setThreats(next)
          setError(null)
          setLastRefreshedAt(new Date())
        }
      } catch (err) {
        if (!cancelled) setError(err?.message || 'Failed to load threats')
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    fetchThreats()
    const timer = setInterval(fetchThreats, 5_000)
    return () => {
      cancelled = true
      clearInterval(timer)
    }
  }, [])

  return { threats, loading, error, lastRefreshedAt }
}

function ViewToggle({ mode, onChange }) {
  const nextMode = mode === 'chart' ? 'table' : 'chart'
  const title = nextMode === 'table' ? 'View Table' : 'View Chart'
  const label = nextMode === 'table' ? 'Show Table' : 'Show Chart'

  return (
    <button
      type="button"
      onClick={() => onChange(nextMode)}
      title={title}
      className="h-7 px-2 flex items-center gap-2 rounded-sm text-pink-400 hover:text-pink-300 hover:bg-gray-700 transition-colors"
    >
      <span className="material-symbols-outlined text-base">
        {nextMode === 'table' ? 'table_chart' : 'bar_chart'}
      </span>
      <span className="text-[11px] font-semibold">{label}</span>
    </button>
  )
}

function ChartTooltip({ active, payload, label }) {
  if (!active || !payload?.length) return null
  const visible = payload.filter(item => Number(item.value) > 0)
  const total = visible.reduce((sum, item) => sum + (Number(item.value) || 0), 0)

  return (
    <div className="bg-gray-900 border border-gray-700 rounded-sm p-3 shadow-xl">
      <p className="text-xs font-mono text-gray-100 mb-2">{label}</p>
      <div className="space-y-1">
        {visible.map(item => (
          <div key={item.dataKey} className="flex items-center justify-between gap-6 text-[11px]">
            <span style={{ color: item.color }}>{item.name}</span>
            <span className="font-mono text-gray-100">{item.value}</span>
          </div>
        ))}
        <div className="pt-1 mt-1 border-t border-gray-700 flex items-center justify-between gap-6 text-[11px]">
          <span className="text-gray-300">Total</span>
          <span className="font-mono text-gray-100">{total}</span>
        </div>
      </div>
    </div>
  )
}

function PieTooltip({ active, payload }) {
  if (!active || !payload?.length) return null
  const item = payload[0]?.payload
  if (!item) return null

  return (
    <div className="bg-gray-900 border border-gray-700 rounded-sm p-3 shadow-xl">
      <div className="flex items-center justify-between gap-6 text-[11px]">
        <span style={{ color: item.color }}>{item.label}</span>
        <span className="font-mono text-gray-100">{item.value}</span>
      </div>
    </div>
  )
}

function CheckboxMark({ checked }) {
  return (
    <span
      className={`w-4 h-4 rounded-[4px] shrink-0 border flex items-center justify-center transition-colors ${
        checked
          ? 'bg-pink-500 border-pink-500 text-white'
          : 'bg-gray-800 border-gray-600 text-transparent group-hover:border-pink-400'
      }`}
    >
      {checked && (
        <span className="material-symbols-outlined text-[12px] leading-none">
          check
        </span>
      )}
    </span>
  )
}

function MetricCard({ label, value, severity, icon, delta }) {
  const meta = severity ? SEVERITY_META[severity] : null
  const iconName = icon || meta?.icon || 'shield'

  return (
    <div className="bg-gray-800 border border-gray-700 p-4 rounded-sm flex flex-col justify-between min-h-[96px]">
      <div className="flex items-start justify-between gap-3">
        <span className="text-[10px] font-semibold text-gray-300 uppercase tracking-widest">{label}</span>
        <span
          className={`material-symbols-outlined ${meta?.text || 'text-[#ffb0cf]'}`}
          style={{ fontVariationSettings: "'FILL' 1" }}
        >
          {iconName}
        </span>
      </div>
      <div className="mt-3">
        <p className={`text-2xl font-bold ${meta?.text || 'text-gray-100'}`}>{value}</p>
        <p className="text-[10px] font-semibold text-gray-300 mt-1">{delta}</p>
      </div>
    </div>
  )
}

function ChartControls({ title, view, onViewChange, options, selected, onToggle, onSelectAll, allowAgent = false }) {
  const views = allowAgent ? ['severity', 'agent', 'rule'] : ['severity', 'rule']
  const allSelected = options.length > 0 && options.every(option => selected.includes(option.value))

  return (
    <aside className="bg-gray-900 border-l border-gray-700 p-4 w-[280px] shrink-0">
      <div className="space-y-5">
        <div>
          <p className="text-[10px] font-bold text-gray-300 uppercase tracking-widest mb-2">{title}</p>
          <div className="inline-flex bg-gray-900 border border-gray-700 rounded-sm overflow-hidden">
            {views.map(item => (
              <button
                key={item}
                type="button"
                onClick={() => onViewChange(item)}
                className={`px-3 py-1.5 text-[11px] font-semibold capitalize transition-colors ${
                  view === item
                    ? 'bg-gray-800 text-pink-400'
                    : 'text-gray-100 hover:text-pink-300 hover:bg-gray-800/60'
                }`}
              >
                {item === 'rule' ? 'Rule' : item}
              </button>
            ))}
          </div>
        </div>

        <div>
          <p className="text-[10px] font-bold text-gray-300 uppercase tracking-widest mb-2">Filter By</p>
          <div className="space-y-2 max-h-[250px] overflow-y-auto custom-scrollbar pr-1">
            <label className="group flex items-center gap-2 text-xs text-gray-100 cursor-pointer pb-2 border-b border-gray-700">
              <input
                type="checkbox"
                checked={allSelected}
                onChange={() => onSelectAll(allSelected ? [] : options.map(option => option.value))}
                className="sr-only"
              />
              <CheckboxMark checked={allSelected} />
              <span className="font-semibold">Select All</span>
            </label>
            {options.map(option => {
              const checked = selected.includes(option.value)
              return (
                <label key={option.value} className="group flex items-center gap-2 text-xs text-gray-100 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={() => onToggle(option.value)}
                    className="sr-only"
                  />
                  <CheckboxMark checked={checked} />
                  <span className="truncate" title={option.label}>{option.label}</span>
                </label>
              )
            })}
          </div>
        </div>
      </div>
    </aside>
  )
}

function GenericTable({ axisLabel, rows, series }) {
  return (
    <div className="h-[380px] overflow-auto">
      <table className="w-full text-left border-collapse">
        <thead className="bg-gray-900">
          <tr>
            <th className="px-4 py-3 text-[10px] font-bold text-gray-300 uppercase tracking-widest">{axisLabel}</th>
            {series.map(item => (
              <th key={item.key} className="px-4 py-3 text-[10px] font-bold text-gray-300 uppercase tracking-widest">
                {item.label}
              </th>
            ))}
            <th className="px-4 py-3 text-[10px] font-bold text-gray-300 uppercase tracking-widest">Total</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-700">
          {rows.map(row => (
            <tr key={row.axis} className="hover:bg-gray-700/30">
              <td className="px-4 py-3 text-xs font-mono text-gray-100">{row.label}</td>
              {series.map(item => (
                <td key={item.key} className="px-4 py-3 text-xs font-mono" style={{ color: item.color }}>
                  {row[item.key] || 0}
                </td>
              ))}
              <td className="px-4 py-3 text-xs font-mono text-gray-100">{row.total}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function SortableGenericTable({ axisLabel, rows, series }) {
  const [sort, setSort] = useState({ key: 'total', dir: 'desc' })

  const sorted = useMemo(() => {
    return [...rows].sort((a, b) => {
      const av = sort.key === 'axis' ? a.label : Number(a[sort.key] || 0)
      const bv = sort.key === 'axis' ? b.label : Number(b[sort.key] || 0)
      const result = typeof av === 'string' ? av.localeCompare(bv) : av - bv
      return sort.dir === 'asc' ? result : -result
    })
  }, [rows, sort])

  const setSortKey = (key) => {
    setSort(prev => ({
      key,
      dir: prev.key === key && prev.dir === 'desc' ? 'asc' : 'desc',
    }))
  }

  const HeaderButton = ({ sortKey, children }) => (
    <button
      type="button"
      onClick={() => setSortKey(sortKey)}
      className="flex items-center gap-1 text-[10px] font-bold text-pink-400 uppercase tracking-widest hover:text-pink-300"
    >
      {children}
      {sort.key === sortKey && (
        <span className="material-symbols-outlined text-[13px]">
          {sort.dir === 'asc' ? 'arrow_upward' : 'arrow_downward'}
        </span>
      )}
    </button>
  )

  return (
    <div className="h-[380px] overflow-auto">
      <table className="w-full text-left border-collapse">
        <thead className="bg-gray-900">
          <tr>
            <th className="px-4 py-3"><HeaderButton sortKey="axis">{axisLabel}</HeaderButton></th>
            {series.map(item => (
              <th key={item.key} className="px-4 py-3">
                <HeaderButton sortKey={item.key}>{item.label}</HeaderButton>
              </th>
            ))}
            <th className="px-4 py-3"><HeaderButton sortKey="total">Total</HeaderButton></th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-700">
          {sorted.map(row => (
            <tr key={row.axis} className="hover:bg-gray-700/30">
              <td className="px-4 py-3 text-xs font-mono text-gray-100">{row.label}</td>
              {series.map(item => (
                <td key={item.key} className="px-4 py-3 text-xs font-mono" style={{ color: item.color }}>
                  {row[item.key] || 0}
                </td>
              ))}
              <td className="px-4 py-3 text-xs font-mono text-gray-100">{row.total}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function EmptyState({ message }) {
  return (
    <div className="h-full flex flex-col items-center justify-center text-xs text-gray-400 text-center">
      <span>{message || 'Nothing to show here.'}</span>
      {!message && <span>Select keys in the panel to view data.</span>}
    </div>
  )
}

function formatRefreshTime(date) {
  return date
    ? date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
    : 'last refresh'
}

function noThreatsMessage(lastRefreshedAt) {
  return `No threats as of ${formatRefreshTime(lastRefreshedAt)}`
}

function NoThreatsOverlay({ lastRefreshedAt }) {
  return (
    <div className="absolute inset-0 flex items-center justify-center pointer-events-none">
      <span className="text-xs text-gray-400 bg-gray-800/80 border border-gray-700 px-3 py-2 rounded-sm">
        {noThreatsMessage(lastRefreshedAt)}
      </span>
    </div>
  )
}

function EmptyChart({ message }) {
  return (
    <div className="h-[380px]">
      <EmptyState message={message} />
    </div>
  )
}

function toggleSelected(value, selected, setSelected) {
  setSelected(prev => {
    if (prev.includes(value) && prev.length === 1) return prev
    return prev.includes(value) ? prev.filter(item => item !== value) : [...prev, value]
  })
}

function sameValues(a, b) {
  return a.length === b.length && a.every((value, index) => value === b[index])
}

function valuesKey(values) {
  return values.join('\u001f')
}

function ChartLegend({ series, hoveredKey, onHover }) {
  if (series.length === 0) return null

  return (
    <div className="px-5 pb-4 flex flex-wrap items-center gap-x-5 gap-y-2">
      {series.map(item => (
        <div
          key={item.key}
          onMouseEnter={() => onHover(item.key)}
          onMouseLeave={() => onHover(null)}
          className={`flex items-center gap-2 text-[11px] text-gray-300 transition-opacity cursor-default ${
            hoveredKey && hoveredKey !== item.key ? 'opacity-50' : 'opacity-100'
          }`}
        >
          <span className="w-2.5 h-2.5 rounded-sm" style={{ background: item.color }} />
          <span>{item.label}</span>
        </div>
      ))}
    </div>
  )
}

function PieLegend({ items, hoveredKey, onHover }) {
  if (items.length === 0) return null

  return (
    <div className="px-5 pb-5 flex flex-wrap items-center gap-x-4 gap-y-2 overflow-y-auto custom-scrollbar">
      {items.map(item => (
        <div
          key={item.key}
          onMouseEnter={() => onHover(item.key)}
          onMouseLeave={() => onHover(null)}
          className={`flex items-center gap-2 text-[11px] text-gray-300 transition-opacity cursor-default ${
            hoveredKey && hoveredKey !== item.key ? 'opacity-50' : 'opacity-100'
          }`}
        >
          <span className="w-2.5 h-2.5 rounded-sm" style={{ background: item.color }} />
          <span className="truncate max-w-[160px]" title={item.label}>{item.label}</span>
        </div>
      ))}
    </div>
  )
}

function CircleTable({ rows, valueLabel }) {
  return (
    <div className="h-[300px] overflow-auto">
      <table className="w-full text-left border-collapse">
        <thead className="bg-gray-900">
          <tr>
            <th className="px-4 py-3 text-[10px] font-bold text-gray-300 uppercase tracking-widest">Series</th>
            <th className="px-4 py-3 text-[10px] font-bold text-gray-300 uppercase tracking-widest">{valueLabel}</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-700">
          {rows.map(row => (
            <tr key={row.key} className="hover:bg-gray-700/30">
              <td className="px-4 py-3 text-xs text-gray-100">{row.label}</td>
              <td className="px-4 py-3 text-xs font-mono text-gray-100">{row.value}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function CircleChartCard({ title, data, loading, valueLabel, lastRefreshedAt }) {
  const [hoveredKey, setHoveredKey] = useState(null)
  const [mode, setMode] = useState('chart')
  const total = data.reduce((sum, item) => sum + item.value, 0)

  return (
    <div className="bg-gray-800 border border-gray-700 rounded-sm min-w-0">
      <div className="h-10 pl-5 pr-3 border-b border-gray-700 flex items-center justify-between">
        <h3 className="text-sm font-semibold text-gray-100">{title}</h3>
        <ViewToggle mode={mode} onChange={setMode} />
      </div>
      {mode === 'table' ? (
        total === 0 ? (
          <div className="h-[300px]">
            <EmptyState message={loading ? 'Loading…' : noThreatsMessage(lastRefreshedAt)} />
          </div>
        ) : (
          <CircleTable rows={data} valueLabel={valueLabel} />
        )
      ) : total === 0 ? (
        <div className="h-[300px]">
          <EmptyState message={loading ? 'Loading…' : noThreatsMessage(lastRefreshedAt)} />
        </div>
      ) : (
        <div className="h-[300px] flex flex-col">
          <div className="h-[210px] p-4 pb-1 shrink-0">
            <ResponsiveContainer width="100%" height="100%">
              <PieChart>
                <Tooltip content={<PieTooltip />} />
                <Pie
                  data={data}
                  dataKey="value"
                  nameKey="label"
                  innerRadius="58%"
                  outerRadius="82%"
                  stroke="#231f2b"
                  strokeWidth={2}
                  isAnimationActive={false}
                >
                  {data.map(item => (
                    <Cell
                      key={item.key}
                      fill={item.color}
                      fillOpacity={hoveredKey && hoveredKey !== item.key ? 0.5 : 1}
                      onMouseEnter={() => setHoveredKey(item.key)}
                      onMouseLeave={() => setHoveredKey(null)}
                    />
                  ))}
                </Pie>
              </PieChart>
            </ResponsiveContainer>
          </div>
          <PieLegend items={data} hoveredKey={hoveredKey} onHover={setHoveredKey} />
        </div>
      )}
    </div>
  )
}

function StackedChart({ rows, series, hoveredKey, onHover, xKey = 'label' }) {
  return (
    <ResponsiveContainer width="100%" height="100%">
      <BarChart data={rows}>
        <CartesianGrid stroke="#374151" vertical={false} />
        <XAxis dataKey={xKey} stroke="#71717a" fontSize={11} tickLine={false} axisLine={{ stroke: '#4b5563' }} interval={0} />
        <YAxis allowDecimals={false} stroke="#71717a" fontSize={11} tickLine={false} axisLine={{ stroke: '#4b5563' }} />
        <Tooltip content={<ChartTooltip />} cursor={{ fill: 'rgba(255,255,255,0.04)' }} />
        {series.map((item, index) => (
          <Bar
            key={item.key}
            dataKey={item.key}
            name={item.label}
            stackId="stack"
            fill={item.color}
            stroke="#231f2b"
            strokeWidth={1}
            fillOpacity={hoveredKey && hoveredKey !== item.key ? 0.5 : 1}
            onMouseEnter={() => onHover(item.key)}
            onMouseLeave={() => onHover(null)}
            radius={index === series.length - 1 ? [2, 2, 0, 0] : undefined}
          />
        ))}
      </BarChart>
    </ResponsiveContainer>
  )
}

export default function ThreatAnalytics() {
  const [windowKey, setWindowKey] = useState('1h')
  const [statusFilter, setStatusFilter] = useState('all')
  const [now, setNow] = useState(() => Date.now())
  const [volumeMode, setVolumeMode] = useState('chart')
  const [agentMode, setAgentMode] = useState('chart')
  const [volumeBreakdown, setVolumeBreakdown] = useState('severity')
  const [agentBreakdown, setAgentBreakdown] = useState('severity')
  const [volumeSelected, setVolumeSelected] = useState(SEVERITIES)
  const [agentSelected, setAgentSelected] = useState(SEVERITIES)
  const [hoveredVolumeSeries, setHoveredVolumeSeries] = useState(null)
  const [hoveredAgentSeries, setHoveredAgentSeries] = useState(null)
  const { threats, loading, error, lastRefreshedAt } = useThreatsData()

  useEffect(() => {
    const tickMs = TIME_WINDOWS[windowKey].tickMs
    const timer = setInterval(() => setNow(Date.now()), tickMs)
    return () => clearInterval(timer)
  }, [windowKey])

  const analytics = useMemo(() => {
    const cfg = TIME_WINDOWS[windowKey]
    const end = now
    const start = end - cfg.durationMs
    const bucketCount = Math.ceil(cfg.durationMs / cfg.bucketMs)

    const allWindowThreats = threats.filter(threat => {
      const lastSeen = parseTime(threat.last_seen || threat.first_seen)
      return lastSeen && lastSeen.getTime() >= start && lastSeen.getTime() <= end
    })
    const windowThreats = statusFilter === 'active'
      ? allWindowThreats.filter(threat => threat.status !== 'cleared')
      : allWindowThreats

    const records = []
    for (const threat of windowThreats) {
      const fallbackTime = threat.last_seen || threat.first_seen
      const events = threat.events?.length
        ? threat.events
        : [{ detected_at: fallbackTime }]

      for (const event of events) {
        const detectedAt = parseTime(event.detected_at || fallbackTime)
        if (!detectedAt) continue
        const ms = detectedAt.getTime()
        if (ms < start || ms > end) continue
        records.push({ threat, ms })
      }
    }

    const severityCounts = { critical: 0, warning: 0, info: 0 }
    for (const threat of windowThreats) {
      severityCounts[severityOf(threat.severity)] += 1
    }

    const allAgents = Array.from(new Set(threats.map(t => t.agent_id || 'unknown-agent'))).sort()
    const allRules = Array.from(new Set(threats.map(t => t.rule_name || t.rule_id || 'Unknown rule'))).sort()

    const makeOptions = (breakdown) => {
      const values = breakdown === 'severity' ? SEVERITIES : breakdown === 'agent' ? allAgents : allRules
      return buildSeries(values, breakdown)
    }

    const buildVolume = (breakdown, selected) => {
      const series = makeOptions(breakdown).filter(item => selected.includes(item.value))
      const rows = Array.from({ length: bucketCount }, (_, i) => {
        const bucketStart = start + i * cfg.bucketMs
        const row = { axis: bucketStart, label: formatTick(bucketStart, windowKey), total: 0 }
        for (const item of series) row[item.key] = 0
        return row
      })
      const seriesByValue = new Map(series.map(item => [item.value, item]))

      for (const record of records) {
        const value = getBreakdownValue(record.threat, breakdown)
        const item = seriesByValue.get(value)
        if (!item) continue
        const bucketIndex = Math.min(bucketCount - 1, Math.max(0, Math.floor((record.ms - start) / cfg.bucketMs)))
        rows[bucketIndex][item.key] += 1
        rows[bucketIndex].total += 1
      }

      return { rows, series, total: rows.reduce((sum, row) => sum + row.total, 0) }
    }

    const buildAgentRows = (breakdown, selected) => {
      const series = makeOptions(breakdown).filter(item => selected.includes(item.value))
      const seriesByValue = new Map(series.map(item => [item.value, item]))
      const rowsByAgent = new Map()

      for (const threat of windowThreats) {
        const agent = threat.agent_id || 'unknown-agent'
        if (!rowsByAgent.has(agent)) {
          const row = { axis: agent, label: agent, total: 0 }
          for (const item of series) row[item.key] = 0
          rowsByAgent.set(agent, row)
        }
        const value = getBreakdownValue(threat, breakdown)
        const item = seriesByValue.get(value)
        if (!item) continue
        const row = rowsByAgent.get(agent)
        row[item.key] += 1
        row.total += 1
      }

      const rows = Array.from(rowsByAgent.values())
        .filter(row => row.total > 0)
        .sort((a, b) => b.total - a.total || a.label.localeCompare(b.label))
        .slice(0, 12)

      return { rows, series }
    }

    const buildPieData = (breakdown) => {
      const series = makeOptions(breakdown)
      const seriesByValue = new Map(series.map(item => [item.value, item]))
      const counts = new Map(series.map(item => [item.value, 0]))

      for (const threat of windowThreats) {
        const value = getBreakdownValue(threat, breakdown)
        if (!counts.has(value)) counts.set(value, 0)
        counts.set(value, counts.get(value) + 1)
      }

      return Array.from(counts.entries())
        .map(([value, count]) => {
          const item = seriesByValue.get(value) || buildSeries([value], breakdown)[0]
          return { ...item, value, count, label: item.label, color: item.color }
        })
        .filter(item => item.count > 0)
        .sort((a, b) => b.count - a.count || a.label.localeCompare(b.label))
        .map(item => ({ key: item.key, label: item.label, value: item.count, color: item.color }))
    }

    return {
      totalThreats: allWindowThreats.length,
      activeThreats: allWindowThreats.filter(t => t.status !== 'cleared').length,
      severityCounts,
      pies: {
        severity: buildPieData('severity'),
        agent: buildPieData('agent'),
        rule: buildPieData('rule'),
      },
      volume: buildVolume(volumeBreakdown, volumeSelected),
      agent: buildAgentRows(agentBreakdown, agentSelected),
      options: {
        severity: makeOptions('severity'),
        agent: makeOptions('agent'),
        rule: makeOptions('rule'),
      },
    }
  }, [agentBreakdown, agentSelected, now, statusFilter, threats, volumeBreakdown, volumeSelected, windowKey])

  const volumeOptionKey = valuesKey(analytics.options[volumeBreakdown].map(item => item.value))
  const agentOptionKey = valuesKey(analytics.options[agentBreakdown].map(item => item.value))

  useEffect(() => {
    const valid = analytics.options[volumeBreakdown].map(item => item.value)
    setVolumeSelected(prev => {
      const filtered = prev.filter(item => valid.includes(item))
      const next = filtered.length > 0 || prev.length === 0 ? filtered : valid.slice(0, Math.min(valid.length, 8))
      return sameValues(prev, next) ? prev : next
    })
  }, [volumeBreakdown, volumeOptionKey])

  useEffect(() => {
    const valid = analytics.options[agentBreakdown].map(item => item.value)
    setAgentSelected(prev => {
      const filtered = prev.filter(item => valid.includes(item))
      const next = filtered.length > 0 || prev.length === 0 ? filtered : valid.slice(0, Math.min(valid.length, 8))
      return sameValues(prev, next) ? prev : next
    })
  }, [agentBreakdown, agentOptionKey])

  return (
    <section className="space-y-6">
      <div className="border-t border-gray-700" />

      <div className="sticky top-10 z-20 -mx-6 lg:-mx-10 px-6 lg:px-10 py-4 bg-gray-900/95 border-b border-gray-700 flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div className="flex items-center gap-3">
          <h2 className="text-xl font-semibold text-gray-100">Security Posture</h2>
          <span className="text-[10px] font-mono text-gray-400">Refreshing every 5s</span>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <span className="text-[10px] font-semibold text-gray-300 uppercase tracking-widest">Threat Status</span>
          <div className="inline-flex bg-gray-900 border border-gray-700 rounded-sm overflow-hidden">
            {[
              ['all', 'All'],
              ['active', 'Open'],
            ].map(([key, label]) => (
              <button
                key={key}
                type="button"
                onClick={() => setStatusFilter(key)}
                className={`px-4 py-2 text-xs font-semibold transition-colors ${
                  statusFilter === key
                    ? 'bg-gray-800 text-pink-400'
                    : 'text-gray-100 hover:text-pink-300 hover:bg-gray-800/60'
                }`}
              >
                {label}
              </button>
            ))}
          </div>
          <span className="text-[10px] font-semibold text-gray-300 uppercase tracking-widest">Time Window</span>
          <div className="inline-flex bg-gray-900 border border-gray-700 rounded-sm overflow-hidden">
            {Object.keys(TIME_WINDOWS).map(key => (
              <button
                key={key}
                type="button"
                onClick={() => setWindowKey(key)}
                className={`px-4 py-2 text-xs font-semibold transition-colors ${
                  windowKey === key
                    ? 'bg-gray-800 text-pink-400'
                    : 'text-gray-100 hover:text-pink-300 hover:bg-gray-800/60'
                }`}
              >
                {TIME_WINDOWS[key].label}
              </button>
            ))}
          </div>
        </div>
      </div>

      {error && (
        <div className="flex items-center gap-3 px-4 py-3 bg-[#93000a]/20 border border-[#ffb4ab]/30 rounded-sm text-[#ffb4ab] text-xs">
          <span className="material-symbols-outlined text-base shrink-0">error</span>
          <span>{error}</span>
        </div>
      )}

      <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-5 gap-4">
        <MetricCard label="Total Threats" value={loading ? '—' : analytics.totalThreats} icon="shield" delta="in selected window" />
        <MetricCard label="Open Threats" value={loading ? '—' : analytics.activeThreats} icon="crisis_alert" delta="not cleared" />
        <MetricCard label="Critical" value={loading ? '—' : analytics.severityCounts.critical} severity="critical" delta="highest severity" />
        <MetricCard label="Warning" value={loading ? '—' : analytics.severityCounts.warning} severity="warning" delta="needs review" />
        <MetricCard label="Info" value={loading ? '—' : analytics.severityCounts.info} severity="info" delta="low severity" />
      </div>

      <div className="bg-gray-800 border border-gray-700 rounded-sm">
        <div className="flex items-stretch">
          <div className="min-w-0 flex-1">
            <div className="h-10 pl-5 pr-3 border-b border-gray-700 flex items-center justify-between">
              <h3 className="text-sm font-semibold text-gray-100">Threat Volume Over Time</h3>
              <ViewToggle mode={volumeMode} onChange={setVolumeMode} />
            </div>
            {analytics.volume.series.length === 0 ? (
              <EmptyChart message={loading ? 'Loading threat volume…' : undefined} />
            ) : volumeMode === 'table' ? (
              <GenericTable axisLabel="Time" rows={analytics.volume.rows} series={analytics.volume.series} />
            ) : (
              <>
                <div className="h-[320px] p-5 pb-3 relative">
                  <StackedChart
                    rows={analytics.volume.rows}
                    series={analytics.volume.series}
                    hoveredKey={hoveredVolumeSeries}
                    onHover={setHoveredVolumeSeries}
                  />
                  {analytics.volume.total === 0 && <NoThreatsOverlay lastRefreshedAt={lastRefreshedAt} />}
                </div>
                <div className="h-[60px] overflow-y-auto custom-scrollbar">
                  <ChartLegend
                    series={analytics.volume.series}
                    hoveredKey={hoveredVolumeSeries}
                    onHover={setHoveredVolumeSeries}
                  />
                </div>
              </>
            )}
          </div>
          <ChartControls
            title="Series By"
            view={volumeBreakdown}
            onViewChange={setVolumeBreakdown}
            options={analytics.options[volumeBreakdown]}
            selected={volumeSelected}
            onToggle={(value) => toggleSelected(value, volumeSelected, setVolumeSelected)}
            onSelectAll={setVolumeSelected}
            allowAgent
          />
        </div>
      </div>

      <div className="bg-gray-800 border border-gray-700 rounded-sm">
        <div className="flex items-stretch">
          <div className="min-w-0 flex-1">
            <div className="h-10 pl-5 pr-3 border-b border-gray-700 flex items-center justify-between">
              <h3 className="text-sm font-semibold text-gray-100">Threats Per Agent</h3>
              <ViewToggle mode={agentMode} onChange={setAgentMode} />
            </div>
            {analytics.agent.series.length === 0 ? (
              <EmptyChart message={loading ? 'Loading threats per agent…' : undefined} />
            ) : agentMode === 'table' ? (
              <SortableGenericTable axisLabel="Agent" rows={analytics.agent.rows} series={analytics.agent.series} />
            ) : (
              <>
                <div className="h-[320px] p-5 pb-3 relative">
                  <StackedChart
                    rows={analytics.agent.rows}
                    series={analytics.agent.series}
                    hoveredKey={hoveredAgentSeries}
                    onHover={setHoveredAgentSeries}
                  />
                  {analytics.agent.rows.length === 0 && <NoThreatsOverlay lastRefreshedAt={lastRefreshedAt} />}
                </div>
                <div className="h-[60px] overflow-y-auto custom-scrollbar">
                  <ChartLegend
                    series={analytics.agent.series}
                    hoveredKey={hoveredAgentSeries}
                    onHover={setHoveredAgentSeries}
                  />
                </div>
              </>
            )}
          </div>
          <ChartControls
            title="Series By"
            view={agentBreakdown}
            onViewChange={setAgentBreakdown}
            options={analytics.options[agentBreakdown]}
            selected={agentSelected}
            onToggle={(value) => toggleSelected(value, agentSelected, setAgentSelected)}
            onSelectAll={setAgentSelected}
          />
        </div>
      </div>

      <div className="grid grid-cols-3 gap-4">
        <CircleChartCard title="Threats by Severity" data={analytics.pies.severity} loading={loading} valueLabel={statusFilter === 'active' ? 'Open' : 'Total'} lastRefreshedAt={lastRefreshedAt} />
        <CircleChartCard title="Threats by Agent" data={analytics.pies.agent} loading={loading} valueLabel={statusFilter === 'active' ? 'Open' : 'Total'} lastRefreshedAt={lastRefreshedAt} />
        <CircleChartCard title="Threats by Rule" data={analytics.pies.rule} loading={loading} valueLabel={statusFilter === 'active' ? 'Open' : 'Total'} lastRefreshedAt={lastRefreshedAt} />
      </div>
    </section>
  )
}
