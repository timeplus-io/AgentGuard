import { useState, useEffect, useRef } from 'react'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import Footer from '../components/Footer'
import PageHeader from '../components/PageHeader'
import { api } from '../api/client'

const defaultQuery = `SELECT * FROM otel_traces
WHERE duration_ms > 500
LIMIT 100`

const staticTables = [
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
  { name: 'hook_events', open: false, columns: [] },
]

const savedQueries = [
  { icon: 'history', label: 'Recent Threats', sql: 'SELECT * FROM threats LIMIT 20' },
  { icon: 'auto_awesome', label: 'Active Traces', sql: "SELECT * FROM otel_traces WHERE duration_ms > 500 LIMIT 100" },
  { icon: 'bug_report', label: 'Latency Spikes', sql: "SELECT * FROM otel_metrics WHERE name = 'latency' LIMIT 50" },
]

export default function Investigate() {
  const [query, setQuery] = useState(defaultQuery)
  const [rows, setRows] = useState([])
  const [columns, setColumns] = useState([])
  const [streaming, setStreaming] = useState(false)
  const [error, setError] = useState(null)
  const [rowMeta, setRowMeta] = useState(null)
  const [availTables, setAvailTables] = useState(staticTables)
  const abortRef = useRef(null)
  const startTimeRef = useRef(null)

  useEffect(() => {
    api.listTables()
      .then(data => { if (data.tables) setAvailTables(data.tables) })
      .catch(() => {})
  }, [])

  const handleRun = async () => {
    if (streaming) {
      abortRef.current?.abort()
      return
    }

    const controller = new AbortController()
    abortRef.current = controller
    setRows([])
    setColumns([])
    setError(null)
    setRowMeta(null)
    setStreaming(true)
    startTimeRef.current = performance.now()
    let count = 0

    try {
      for await (const row of api.protonQuery(query, { signal: controller.signal })) {
        count++
        setRows(prev => {
          if (prev.length === 0) {
            setColumns(Object.keys(row))
          }
          return [...prev, row]
        })
      }
      const elapsed = ((performance.now() - startTimeRef.current) / 1000).toFixed(2)
      setRowMeta(`${count} rows · ${elapsed}s`)
    } catch (err) {
      if (err.name !== 'AbortError') {
        setError(err.message)
      }
    } finally {
      setStreaming(false)
    }
  }

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />
      <Footer />
      <main className="pl-64 pt-16 pb-8 min-h-screen">
        <div className="p-6 lg:p-10 flex flex-col gap-6" style={{ height: 'calc(100vh - 64px)' }}>
          <PageHeader
            title="SQL Console"
            subtitle="Query Proton/Timeplus streams directly using streaming SQL."
          />

          <div className="flex gap-6 flex-1 overflow-hidden min-h-0">
            {/* Left Panel */}
            <aside className="w-72 flex flex-col gap-4 overflow-hidden">
              {/* Table Schemas + Saved Queries */}
              <div className="flex-1 flex flex-col bg-[#2c2834] rounded-sm border border-[#574149]/10 overflow-hidden">
                <div className="p-4 border-b border-[#574149]/10 flex items-center justify-between">
                  <h3 className="text-[10px] font-bold text-[#ffb0cf] tracking-widest uppercase">Console Resources</h3>
                  <button className="text-zinc-500 hover:text-white transition-colors">
                    <span className="material-symbols-outlined text-xs">refresh</span>
                  </button>
                </div>
                <div className="flex-1 overflow-y-auto p-4 space-y-6 custom-scrollbar">
                  <div>
                    <h4 className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest mb-3 flex items-center gap-2">
                      <span className="material-symbols-outlined text-xs">database</span> Streams
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

                  <div>
                    <h4 className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest mb-3 flex items-center gap-2">
                      <span className="material-symbols-outlined text-xs">bookmark</span> Saved Queries
                    </h4>
                    <div className="space-y-1">
                      {savedQueries.map(sq => (
                        <button
                          key={sq.label}
                          onClick={() => setQuery(sq.sql)}
                          className="w-full text-left flex items-center gap-2 py-2 px-2 rounded-sm transition-all text-xs text-zinc-400 hover:bg-[#37333f] hover:text-white"
                        >
                          <span className="material-symbols-outlined text-xs">{sq.icon}</span>
                          {sq.label}
                        </button>
                      ))}
                    </div>
                  </div>
                </div>
              </div>
            </aside>

            {/* SQL Editor + Results */}
            <section className="flex-1 flex flex-col min-w-0 gap-4 overflow-hidden">
              {/* Editor */}
              <div className="flex flex-col bg-[#2c2834] rounded-sm border border-[#574149]/10 overflow-hidden">
                <div className="bg-[#211e29] px-6 py-3 flex justify-between items-center border-b border-[#574149]/10">
                  <div className="flex items-center gap-4">
                    <div className="flex items-center gap-2">
                      <span className="material-symbols-outlined text-xs text-zinc-500">terminal</span>
                      <span className="text-[10px] font-semibold text-zinc-400 tracking-widest uppercase">PROTON_SQL</span>
                    </div>
                    <div className="h-3 w-px bg-[#574149]/20"></div>
                    <div className="flex gap-2 items-center">
                      <span className={`w-1.5 h-1.5 rounded-full ${streaming ? 'bg-[#ffb0cf] animate-pulse' : 'bg-zinc-600'}`}></span>
                      <span className={`text-[10px] uppercase font-semibold tracking-tight ${streaming ? 'text-[#ffb0cf]' : 'text-zinc-600'}`}>
                        {streaming ? 'Streaming' : 'Idle'}
                      </span>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <button
                      onClick={() => setQuery('')}
                      className="px-3 py-1 text-[10px] font-semibold text-zinc-500 hover:text-white uppercase tracking-wider transition-colors"
                    >
                      Clear
                    </button>
                    <button
                      onClick={handleRun}
                      className={`px-5 py-1.5 font-semibold text-[10px] rounded-sm flex items-center gap-2 uppercase tracking-widest transition-all ${
                        streaming
                          ? 'bg-[#93000a] text-[#ffdad6] hover:opacity-90'
                          : 'bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] hover:scale-[0.98] shadow-[0_2px_12px_rgba(213,63,140,0.2)]'
                      }`}
                    >
                      <span className="material-symbols-outlined text-xs" style={{ fontVariationSettings: "'FILL' 1" }}>
                        {streaming ? 'stop' : 'play_arrow'}
                      </span>
                      {streaming ? 'Stop' : 'Execute'}
                    </button>
                  </div>
                </div>
                <textarea
                  value={query}
                  onChange={e => setQuery(e.target.value)}
                  className="h-40 bg-[#0f0c17] font-mono text-xs p-6 text-zinc-300 leading-relaxed resize-none focus:outline-none custom-scrollbar"
                  spellCheck={false}
                />
              </div>

              {/* Results */}
              <div className="flex-1 flex flex-col bg-[#2c2834] rounded-sm border border-[#574149]/10 overflow-hidden">
                <div className="p-4 flex items-center justify-between border-b border-[#574149]/10 shrink-0">
                  <div className="flex items-baseline gap-3">
                    <h3 className="text-base font-semibold text-[#e7dff0]">Results</h3>
                    {rowMeta && (
                      <span className="text-[10px] font-mono bg-[#0f0c17] px-2 py-0.5 rounded-sm text-zinc-500 border border-[#574149]/5">{rowMeta}</span>
                    )}
                    {streaming && (
                      <span className="text-[10px] font-mono bg-[#ffb0cf]/10 px-2 py-0.5 rounded-sm text-[#ffb0cf] border border-[#ffb0cf]/20">
                        {rows.length} rows streaming…
                      </span>
                    )}
                  </div>
                  <div className="flex gap-1">
                    <button className="p-1.5 hover:bg-[#37333f] rounded-sm text-zinc-500 transition-colors">
                      <span className="material-symbols-outlined text-base">download</span>
                    </button>
                    <button className="p-1.5 hover:bg-[#37333f] rounded-sm text-zinc-500 transition-colors">
                      <span className="material-symbols-outlined text-base">fullscreen</span>
                    </button>
                  </div>
                </div>

                {error && (
                  <div className="m-4 px-4 py-3 bg-[#93000a]/20 border border-[#ffb4ab]/20 rounded-sm text-xs text-[#ffb4ab] font-mono flex items-start gap-2">
                    <span className="material-symbols-outlined text-xs mt-0.5 shrink-0">error</span>
                    {error}
                  </div>
                )}

                <div className="flex-1 overflow-auto custom-scrollbar">
                  {rows.length === 0 && !streaming && !error ? (
                    <div className="h-full flex flex-col items-center justify-center gap-3 text-zinc-600">
                      <span className="material-symbols-outlined text-4xl">table_rows</span>
                      <p className="text-xs">Execute a query to see results</p>
                    </div>
                  ) : (
                    <table className="w-full text-left border-collapse">
                      <thead className="sticky top-0 z-10 bg-[#211e29] border-b border-[#574149]/10">
                        <tr>
                          {columns.map(col => (
                            <th key={col} className="px-4 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-widest whitespace-nowrap">{col}</th>
                          ))}
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-[#574149]/5 text-[11px] font-mono">
                        {rows.map((row, i) => (
                          <tr key={i} className={`hover:bg-[#211e29] transition-colors ${i === 0 && streaming ? 'text-[#ffb0cf]' : 'text-zinc-300'}`}>
                            {columns.map(col => (
                              <td key={col} className="px-4 py-3 whitespace-nowrap max-w-xs overflow-hidden text-ellipsis">
                                {String(row[col] ?? '')}
                              </td>
                            ))}
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  )}
                </div>
              </div>
            </section>
          </div>
        </div>
      </main>
    </div>
  )
}
