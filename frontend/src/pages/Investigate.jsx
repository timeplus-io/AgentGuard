import { useState, useEffect } from 'react'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import Footer from '../components/Footer'
import PageHeader from '../components/PageHeader'
import { api } from '../api/client'

const defaultQuery = `SELECT * FROM otel_traces
WHERE duration_ms > 500
AND service_name = 'auth-service'
LIMIT 100;`

const staticResults = [
  { ts: '14:02:34.212', traceId: '8f2a1...9c4e', service: 'auth-service', duration: '842.1ms', status: 'TIMEOUT', live: true },
  { ts: '14:02:33.982', traceId: '3d4f2...7b1a', service: 'auth-service', duration: '512.4ms', status: 'OK', live: false },
  { ts: '14:02:33.411', traceId: 'a9c8b...2f4e', service: 'auth-service', duration: '632.0ms', status: 'OK', live: false },
  { ts: '14:02:32.109', traceId: '7e21a...4d3c', service: 'auth-service', duration: '1204.8ms', status: 'ERR_500', live: false },
]

const tables = [
  {
    name: 'otel_traces',
    open: true,
    columns: [
      { name: 'trace_id', type: 'STRING' },
      { name: 'span_id', type: 'STRING' },
      { name: 'service_name', type: 'STRING' },
      { name: 'duration_ms', type: 'FLOAT64' },
    ]
  },
  { name: 'otel_logs', open: false, columns: [] },
]

const savedQueries = [
  { icon: 'history', label: 'Recent Threats', active: false },
  { icon: 'auto_awesome', label: 'Active Traces', active: true },
  { icon: 'bug_report', label: 'Latency Spikes', active: false },
]

const statusStyle = {
  TIMEOUT: 'bg-[#ffb4ab]/10 text-[#ffb4ab] border border-[#ffb4ab]/20',
  OK: 'bg-emerald-400/10 text-emerald-400 border border-emerald-400/20',
  ERR_500: 'bg-[#ffb4ab]/10 text-[#ffb4ab] border border-[#ffb4ab]/20',
}

export default function Investigate() {
  const [query, setQuery] = useState(defaultQuery)
  const [results, setResults] = useState(staticResults)
  const [loading, setLoading] = useState(false)
  const [rowCount, setRowCount] = useState('423 rows / 12.4ms')
  const [availTables, setAvailTables] = useState(tables)

  useEffect(() => {
    api.listTables()
      .then(data => {
        if (data.tables) setAvailTables(data.tables)
      })
      .catch(() => {})
  }, [])

  const handleRun = async () => {
    setLoading(true)
    try {
      const data = await api.runQuery(query)
      if (data.rows) setResults(data.rows)
      if (data.meta) setRowCount(data.meta)
    } catch {
      // keep static
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />
      <Footer />
      <main className="pl-64 pt-16 pb-8 min-h-screen">
        <div className="p-6 lg:p-10 space-y-8 flex flex-col" style={{ height: 'calc(100vh - 64px)' }}>
          <PageHeader
            title="SQL Console"
            subtitle="Investigate distributed traces and logs using streaming SQL queries."
          />

          <div className="flex gap-6 flex-1 overflow-hidden min-h-0">
            {/* Left Panel */}
            <aside className="w-72 flex flex-col bg-[#2c2834] rounded-sm border border-[#574149]/10 overflow-hidden">
              <div className="p-4 border-b border-[#574149]/10 flex items-center justify-between">
                <h3 className="text-[10px] font-bold text-[#ffb0cf] tracking-widest uppercase">Console Resources</h3>
                <button className="text-zinc-500 hover:text-white transition-colors"><span className="material-symbols-outlined text-xs">refresh</span></button>
              </div>
              <div className="flex-1 overflow-y-auto p-4 space-y-8 custom-scrollbar">
                {/* Table Schemas */}
                <div>
                  <h4 className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest mb-3 flex items-center gap-2">
                    <span className="material-symbols-outlined text-xs">database</span> Table Schemas
                  </h4>
                  <div className="space-y-1">
                    {availTables.map(table => (
                      <details key={table.name} className="group" open={table.open}>
                        <summary className="flex items-center justify-between text-zinc-400 hover:text-white cursor-pointer py-1.5 px-2 rounded-sm hover:bg-[#37333f] transition-all list-none">
                          <span className="flex items-center gap-2 font-medium text-xs">{table.name}</span>
                          <span className="material-symbols-outlined text-xs group-open:rotate-90 transition-transform">chevron_right</span>
                        </summary>
                        {table.columns && table.columns.length > 0 && (
                          <div className="pl-4 pt-1 flex flex-col gap-1 border-l border-[#574149]/10 ml-3 mt-1">
                            {table.columns.map(col => (
                              <div key={col.name} className="text-[11px] text-zinc-500 py-1 flex justify-between px-2">
                                <span>{col.name}</span>
                                <span className="text-zinc-600 font-mono text-[9px]">{col.type}</span>
                              </div>
                            ))}
                          </div>
                        )}
                      </details>
                    ))}
                  </div>
                </div>

                {/* Saved Queries */}
                <div>
                  <h4 className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest mb-3 flex items-center gap-2">
                    <span className="material-symbols-outlined text-xs">bookmark</span> Saved Queries
                  </h4>
                  <div className="space-y-1">
                    {savedQueries.map(sq => (
                      <button key={sq.label} className={`w-full text-left flex items-center gap-2 py-2 px-2 rounded-sm transition-all text-xs ${sq.active ? 'text-[#ffb0cf] bg-[#ffb0cf]/10 font-medium' : 'text-zinc-400 hover:bg-[#37333f] hover:text-white'}`}>
                        <span className="material-symbols-outlined text-xs">{sq.icon}</span>
                        {sq.label}
                      </button>
                    ))}
                  </div>
                </div>
              </div>
            </aside>

            {/* SQL Editor + Results */}
            <section className="flex-1 flex flex-col min-w-0 gap-6 overflow-hidden">
              {/* Editor */}
              <div className="flex flex-col bg-[#2c2834] rounded-sm border border-[#574149]/10 overflow-hidden">
                <div className="bg-[#211e29] px-6 py-3 flex justify-between items-center border-b border-[#574149]/10">
                  <div className="flex items-center gap-4">
                    <div className="flex items-center gap-2">
                      <span className="material-symbols-outlined text-xs text-zinc-500">terminal</span>
                      <span className="text-[10px] font-semibold text-zinc-400 tracking-widest uppercase">STREAMING_SQL_01</span>
                    </div>
                    <div className="h-3 w-px bg-[#574149]/20"></div>
                    <div className="flex gap-2 items-center">
                      <span className="w-1.5 h-1.5 rounded-full bg-[#ffb0cf] animate-pulse"></span>
                      <span className="text-[10px] text-[#ffb0cf] uppercase font-semibold tracking-tight">Live Session</span>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <button className="px-3 py-1 text-[10px] font-semibold text-zinc-500 hover:text-white uppercase tracking-wider transition-colors">Format</button>
                    <button onClick={() => setQuery('')} className="px-3 py-1 text-[10px] font-semibold text-zinc-500 hover:text-white uppercase tracking-wider transition-colors">Clear</button>
                    <button
                      onClick={handleRun}
                      disabled={loading}
                      className="px-5 py-1.5 bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] font-semibold text-[10px] rounded-sm flex items-center gap-2 uppercase tracking-widest hover:scale-[0.98] transition-transform shadow-[0_2px_12px_rgba(213,63,140,0.2)] disabled:opacity-70"
                    >
                      <span className="material-symbols-outlined text-xs" style={{ fontVariationSettings: "'FILL' 1" }}>play_arrow</span>
                      {loading ? 'Running...' : 'Execute'}
                    </button>
                  </div>
                </div>
                <textarea
                  value={query}
                  onChange={e => setQuery(e.target.value)}
                  className="h-48 bg-[#0f0c17] font-mono text-xs p-6 text-zinc-300 leading-relaxed resize-none focus:outline-none custom-scrollbar"
                  spellCheck={false}
                />
              </div>

              {/* Results */}
              <div className="flex-1 flex flex-col bg-[#2c2834] rounded-sm border border-[#574149]/10 overflow-hidden">
                <div className="p-4 flex items-center justify-between border-b border-[#574149]/10">
                  <div className="flex items-baseline gap-3">
                    <h3 className="text-base font-semibold text-[#e7dff0]">Query Results</h3>
                    <span className="text-[10px] font-mono bg-[#0f0c17] px-2 py-0.5 rounded-sm text-zinc-500 border border-[#574149]/5">{rowCount}</span>
                  </div>
                  <div className="flex gap-1">
                    <button className="p-1.5 hover:bg-[#37333f] rounded-sm text-zinc-500 transition-colors"><span className="material-symbols-outlined text-base">download</span></button>
                    <button className="p-1.5 hover:bg-[#37333f] rounded-sm text-zinc-500 transition-colors"><span className="material-symbols-outlined text-base">fullscreen</span></button>
                  </div>
                </div>
                <div className="flex-1 overflow-auto custom-scrollbar">
                  <table className="w-full text-left border-collapse">
                    <thead className="sticky top-0 z-10 bg-[#211e29] border-b border-[#574149]/10">
                      <tr>
                        {['Timestamp', 'Trace ID', 'Service', 'Duration', 'Status'].map((h, i) => (
                          <th key={h} className={`px-6 py-4 text-[10px] font-bold text-zinc-500 uppercase tracking-widest ${i === 3 ? 'text-right' : ''}`}>{h}</th>
                        ))}
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-[#574149]/5 text-[11px] font-mono">
                      {results.map((row, i) => (
                        <tr key={i} className="hover:bg-[#211e29] transition-colors group">
                          <td className="px-6 py-4">
                            {row.live ? (
                              <div className="flex items-center gap-2 text-[#ffb0cf] font-semibold">
                                <span className="w-1.5 h-1.5 rounded-full bg-[#ffb0cf] animate-pulse"></span>
                                {row.ts}
                              </div>
                            ) : (
                              <span className="text-zinc-500">{row.ts}</span>
                            )}
                          </td>
                          <td className="px-6 py-4 text-zinc-500">{row.traceId}</td>
                          <td className="px-6 py-4"><span className="bg-[#0f0c17] text-zinc-300 px-2 py-0.5 rounded-sm border border-[#574149]/5">{row.service}</span></td>
                          <td className={`px-6 py-4 text-right font-semibold ${row.status === 'OK' ? 'text-emerald-400' : 'text-[#ffb4ab]'}`}>{row.duration}</td>
                          <td className="px-6 py-4"><span className={`px-2 py-0.5 rounded-sm text-[10px] font-semibold ${statusStyle[row.status] || ''}`}>{row.status}</span></td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            </section>
          </div>
        </div>
      </main>
    </div>
  )
}
