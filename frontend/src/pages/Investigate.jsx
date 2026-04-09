import { useState, useEffect, useRef } from 'react'
import Editor from 'react-simple-code-editor'
import { highlight, languages } from 'prismjs/components/prism-core'
import 'prismjs/components/prism-sql'
import 'prismjs/themes/prism-tomorrow.css'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import Footer from '../components/Footer'
import PageHeader from '../components/PageHeader'
import { api } from '../api/client'

const defaultQuery = `SELECT hook_name, event_time, agent_id, tool_name, model, hook_decision
FROM agentshield_hook_events
LIMIT 50`

export default function Investigate() {
  const [query, setQuery] = useState(defaultQuery)
  const [rows, setRows] = useState([])
  const [columns, setColumns] = useState([])
  const [streaming, setStreaming] = useState(false)
  const [error, setError] = useState(null)
  const [rowMeta, setRowMeta] = useState(null)
  const [availTables, setAvailTables] = useState([])
  const [panelOpen, setPanelOpen] = useState(true)
  const abortRef = useRef(null)
  const startTimeRef = useRef(null)

  const fetchTables = () => {
    api.listTables()
      .then(data => { if (data.tables) setAvailTables(data.tables) })
      .catch(() => {})
  }

  useEffect(() => { fetchTables() }, [])

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
      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        <div className="p-6 lg:p-10 flex flex-col gap-6" style={{ height: 'calc(100vh - 64px)' }}>
          <PageHeader
            title="SQL Console"
            subtitle="Query Proton/Timeplus streams directly using streaming SQL."
          />

          <div className="flex gap-6 flex-1 overflow-hidden min-h-0">
            {/* Left Panel */}
            <aside className={`flex flex-col gap-4 overflow-hidden transition-all duration-200 ${panelOpen ? 'w-72' : 'w-8'}`}>
              <div className="flex-1 flex flex-col bg-[#2c2834] rounded-sm border border-[#574149]/10 overflow-hidden">
                <div className="p-4 border-b border-[#574149]/10 flex items-center justify-between shrink-0">
                  {panelOpen && <h3 className="text-[10px] font-bold text-[#ffb0cf] tracking-widest uppercase">Console Resources</h3>}
                  <div className="flex items-center gap-1 ml-auto">
                    {panelOpen && (
                      <button onClick={fetchTables} className="text-zinc-500 hover:text-white transition-colors">
                        <span className="material-symbols-outlined text-xs">refresh</span>
                      </button>
                    )}
                    <button onClick={() => setPanelOpen(v => !v)} className="text-zinc-500 hover:text-white transition-colors" title={panelOpen ? 'Collapse' : 'Expand'}>
                      <span className="material-symbols-outlined text-xs">{panelOpen ? 'left_panel_close' : 'left_panel_open'}</span>
                    </button>
                  </div>
                </div>
                {panelOpen && <div className="flex-1 overflow-y-auto p-4 custom-scrollbar">
                  <h4 className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest mb-3 flex items-center gap-2">
                    <span className="material-symbols-outlined text-xs">database</span> Streams
                  </h4>
                  {availTables.length === 0 ? (
                    <p className="text-[11px] text-zinc-600 px-2">Loading schemas…</p>
                  ) : (
                    <div className="space-y-1">
                      {availTables.map(table => (
                        <details key={table.name} className="group">
                          <summary className="flex items-center justify-between text-zinc-400 hover:text-white cursor-pointer py-1.5 px-2 rounded-sm hover:bg-[#37333f] transition-all list-none">
                            <span className="flex items-center gap-2 font-medium text-xs truncate">{table.name}</span>
                            <span className="material-symbols-outlined text-xs flex-shrink-0 group-open:rotate-90 transition-transform">chevron_right</span>
                          </summary>
                          {table.columns && table.columns.length > 0 && (
                            <div className="pl-4 pt-1 flex flex-col gap-0.5 border-l border-[#574149]/10 ml-3 mt-1 mb-1">
                              {table.columns.map(col => (
                                <div key={col.name} className="text-[11px] text-zinc-500 py-0.5 flex justify-between gap-2 px-2">
                                  <span className="truncate">{col.name}</span>
                                  <span className="text-zinc-600 font-mono text-[9px] flex-shrink-0">{col.type}</span>
                                </div>
                              ))}
                            </div>
                          )}
                        </details>
                      ))}
                    </div>
                  )}
                </div>}
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
                      <span className="text-[10px] font-semibold text-zinc-400 tracking-widest uppercase">SQL Query</span>
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
                <div className="h-40 overflow-auto custom-scrollbar bg-[#0f0c17]">
                  <Editor
                    value={query}
                    onValueChange={setQuery}
                    highlight={code => highlight(code, languages.sql, 'sql')}
                    padding={24}
                    style={{
                      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
                      fontSize: 12,
                      lineHeight: 1.6,
                      minHeight: '100%',
                      background: 'transparent',
                      color: '#d4d4d8',
                    }}
                    textareaClassName="focus:outline-none"
                  />
                </div>
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
