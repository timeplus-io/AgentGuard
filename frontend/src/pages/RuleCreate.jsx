import { useState, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import Editor from 'react-simple-code-editor'
import { highlight, languages } from 'prismjs/components/prism-core'
import 'prismjs/components/prism-sql'
import 'prismjs/themes/prism-tomorrow.css'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import PageHeader from '../components/PageHeader'
import { api } from '../api/client'

const defaultSQL = `SELECT
  now64(3)                                          AS detected_at,
  '__rule_id__'                                     AS rule_id,
  '__rule_name__'                                   AS rule_name,
  'warning'                                         AS severity,
  agent_id,
  deployment_id,
  session_id,
  run_id,
  hook_name,
  tool_name,
  concat('Custom rule triggered: ', hook_name)      AS message,
  event_data
FROM agentguard_hook_events
WHERE 1 = 0 -- replace with your detection condition`

export default function RuleCreate() {
  const navigate = useNavigate()
  const [form, setForm] = useState({
    id: '',
    name: '',
    description: '',
    severity: 'warning',
    category: '',
    fp_rate: 'low',
    hook_sources: '',
  })
  const [sql, setSql] = useState(defaultSQL)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState(null)

  const [testRows, setTestRows] = useState([])
  const [testColumns, setTestColumns] = useState([])
  const [testStreaming, setTestStreaming] = useState(false)
  const [testError, setTestError] = useState(null)
  const [testMeta, setTestMeta] = useState(null)
  const abortRef = useRef(null)
  const startTimeRef = useRef(null)

  const setField = (key, val) => setForm(prev => ({ ...prev, [key]: val }))

  const handleTestRun = async () => {
    if (testStreaming) {
      abortRef.current?.abort()
      return
    }
    const controller = new AbortController()
    abortRef.current = controller
    setTestRows([])
    setTestColumns([])
    setTestError(null)
    setTestMeta(null)
    setTestStreaming(true)
    startTimeRef.current = performance.now()
    let count = 0
    try {
      for await (const row of api.protonQuery(sql, { signal: controller.signal })) {
        if (count >= 20) { controller.abort(); break }
        count++
        setTestRows(prev => {
          if (prev.length === 0) setTestColumns(Object.keys(row))
          return [...prev, row]
        })
      }
      const elapsed = ((performance.now() - startTimeRef.current) / 1000).toFixed(2)
      setTestMeta(`${count} rows · ${elapsed}s`)
    } catch (err) {
      if (err.name !== 'AbortError') setTestError(err.message)
    } finally {
      setTestStreaming(false)
    }
  }

  const handleSave = async () => {
    setSaving(true)
    setSaveError(null)
    try {
      await api.createRule({
        id: form.id.trim() || undefined,
        name: form.name,
        description: form.description,
        query_sql: sql,
        severity: form.severity,
        category: form.category,
        fp_rate: form.fp_rate,
        hook_sources: form.hook_sources,
      })
      navigate('/rules')
    } catch (err) {
      setSaveError(err.message || 'Failed to create rule')
    } finally {
      setSaving(false)
    }
  }

  const canSave = form.name.trim() !== '' && form.description.trim() !== '' && sql.trim() !== '' && !saving

  const inputCls = 'bg-[#0f0c17] border border-[#574149]/20 rounded-sm px-3 py-2 text-xs text-[#e7dff0] placeholder-zinc-600 focus:outline-none focus:border-[#ffb0cf]/40'
  const labelCls = 'text-[10px] font-semibold text-zinc-500 uppercase tracking-widest'

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />

      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        <div className="p-6 lg:p-10 flex flex-col gap-6" style={{ height: 'calc(100vh - 64px)' }}>
          <PageHeader
            title="New Custom Rule"
            subtitle="Write a detection SQL query against agentguard_hook_events."
            actions={
              <div className="flex items-center gap-3">
                <button
                  onClick={() => navigate('/rules')}
                  className="px-4 py-2 text-xs font-semibold text-zinc-400 hover:text-white transition-colors"
                >
                  Cancel
                </button>
                <button
                  onClick={handleSave}
                  disabled={!canSave}
                  className="px-5 py-2 text-xs font-semibold rounded-sm bg-primary-button text-primary-button-text disabled:opacity-40 disabled:cursor-not-allowed hover:scale-[0.98] transition-all"
                >
                  {saving ? 'Saving…' : 'Save Rule'}
                </button>
              </div>
            }
          />

          {saveError && (
            <div className="px-4 py-3 bg-[#93000a]/20 border border-[#ffb4ab]/20 rounded-sm text-xs text-[#ffb4ab] font-mono flex items-center gap-2">
              <span className="material-symbols-outlined text-xs shrink-0">error</span>
              {saveError}
            </div>
          )}

          <div className="flex gap-6 flex-1 overflow-hidden min-h-0">
            {/* Left: metadata */}
            <aside className="w-80 shrink-0 overflow-y-auto custom-scrollbar">
              <div className="bg-[#2c2834] rounded-sm border border-[#574149]/10 p-6 flex flex-col gap-5">
                <h3 className="text-[10px] font-bold text-[#ffb0cf] tracking-widest uppercase">Rule Metadata</h3>

                <div className="flex flex-col gap-1.5">
                  <label htmlFor="rule-id" className={labelCls}>Rule ID <span className="normal-case text-zinc-600">(optional)</span></label>
                  <input
                    id="rule-id"
                    type="text"
                    value={form.id}
                    onChange={e => setField('id', e.target.value)}
                    placeholder="auto-generated if blank"
                    className={`${inputCls} font-mono`}
                  />
                  <p className="text-[10px] text-zinc-600">Letters, digits, hyphens, underscores. Max 64 chars. Must be unique.</p>
                </div>

                <div className="flex flex-col gap-1.5">
                  <label htmlFor="rule-name" className={labelCls}>Name *</label>
                  <input
                    id="rule-name"
                    type="text"
                    value={form.name}
                    onChange={e => setField('name', e.target.value)}
                    placeholder="My Custom Rule"
                    className={inputCls}
                  />
                </div>

                <div className="flex flex-col gap-1.5">
                  <label htmlFor="rule-description" className={labelCls}>Description *</label>
                  <textarea
                    id="rule-description"
                    value={form.description}
                    onChange={e => setField('description', e.target.value)}
                    placeholder="What does this rule detect?"
                    rows={3}
                    className={`${inputCls} resize-none`}
                  />
                </div>

                <div className="flex flex-col gap-1.5">
                  <label htmlFor="rule-severity" className={labelCls}>Severity</label>
                  <select id="rule-severity" value={form.severity} onChange={e => setField('severity', e.target.value)} className={inputCls}>
                    <option value="critical">Critical</option>
                    <option value="warning">Warning</option>
                    <option value="info">Info</option>
                  </select>
                </div>

                <div className="flex flex-col gap-1.5">
                  <label htmlFor="rule-category" className={labelCls}>Category</label>
                  <input
                    id="rule-category"
                    type="text"
                    value={form.category}
                    onChange={e => setField('category', e.target.value)}
                    placeholder="e.g. injection"
                    className={inputCls}
                  />
                </div>

                <div className="flex flex-col gap-1.5">
                  <label htmlFor="rule-fp-rate" className={labelCls}>False Positive Rate</label>
                  <select id="rule-fp-rate" value={form.fp_rate} onChange={e => setField('fp_rate', e.target.value)} className={inputCls}>
                    <option value="low">Low</option>
                    <option value="medium">Medium</option>
                    <option value="high">High</option>
                  </select>
                </div>

                <div className="flex flex-col gap-1.5">
                  <label htmlFor="rule-hook-sources" className={labelCls}>Hook Sources</label>
                  <input
                    id="rule-hook-sources"
                    type="text"
                    value={form.hook_sources}
                    onChange={e => setField('hook_sources', e.target.value)}
                    placeholder="e.g. before_tool_call"
                    className={inputCls}
                  />
                </div>

                <p className="text-[10px] text-zinc-600 leading-relaxed">
                  Use <code className="text-zinc-500">'__rule_id__'</code> and <code className="text-zinc-500">'__rule_name__'</code> in your SQL — they are substituted with the generated ID and rule name on save.
                </p>
              </div>
            </aside>

            {/* Right: SQL editor + test results */}
            <section className="flex-1 flex flex-col min-w-0 gap-4 overflow-hidden">
              {/* Editor */}
              <div className="flex flex-col bg-[#2c2834] rounded-sm border border-[#574149]/10 overflow-hidden">
                <div className="bg-[#211e29] px-6 py-3 flex justify-between items-center border-b border-[#574149]/10">
                  <div className="flex items-center gap-4">
                    <span className="text-[10px] font-semibold text-zinc-400 tracking-widest uppercase">Detection SQL</span>
                    <div className="h-3 w-px bg-[#574149]/20" />
                    <div className="flex gap-2 items-center">
                      <span className={`w-1.5 h-1.5 rounded-full ${testStreaming ? 'bg-[#ffb0cf] animate-pulse' : 'bg-zinc-600'}`} />
                      <span className={`text-[10px] uppercase font-semibold tracking-tight ${testStreaming ? 'text-[#ffb0cf]' : 'text-zinc-600'}`}>
                        {testStreaming ? 'Running' : 'Idle'}
                      </span>
                    </div>
                  </div>
                  <button
                    onClick={handleTestRun}
                    className={`px-5 py-1.5 font-semibold text-[10px] rounded-sm flex items-center gap-2 uppercase tracking-widest transition-all ${
                      testStreaming
                        ? 'bg-[#93000a] text-[#ffdad6] hover:opacity-90'
                        : 'bg-primary-button text-primary-button-text hover:scale-[0.98] shadow-[0_2px_12px_rgba(213,63,140,0.2)]'
                    }`}
                  >
                    <span className="material-symbols-outlined text-xs" style={{ fontVariationSettings: "'FILL' 1" }}>
                      {testStreaming ? 'stop' : 'play_arrow'}
                    </span>
                    {testStreaming ? 'Stop' : 'Test Run'}
                  </button>
                </div>
                <div className="h-48 overflow-auto custom-scrollbar bg-[#0f0c17]">
                  <Editor
                    value={sql}
                    onValueChange={setSql}
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

              {/* Test Results */}
              <div className="flex-1 flex flex-col bg-[#2c2834] rounded-sm border border-[#574149]/10 overflow-hidden">
                <div className="p-4 flex items-baseline gap-3 border-b border-[#574149]/10 shrink-0">
                  <h3 className="text-base font-semibold text-[#e7dff0]">Test Results</h3>
                  {testMeta && (
                    <span className="text-[10px] font-mono bg-[#0f0c17] px-2 py-0.5 rounded-sm text-zinc-500 border border-[#574149]/5">{testMeta}</span>
                  )}
                  {testStreaming && (
                    <span className="text-[10px] font-mono bg-[#ffb0cf]/10 px-2 py-0.5 rounded-sm text-[#ffb0cf] border border-[#ffb0cf]/20">
                      {testRows.length} rows streaming…
                    </span>
                  )}
                </div>

                {testError && (
                  <div className="m-4 px-4 py-3 bg-[#93000a]/20 border border-[#ffb4ab]/20 rounded-sm text-xs text-[#ffb4ab] font-mono flex items-start gap-2">
                    <span className="material-symbols-outlined text-xs mt-0.5 shrink-0">error</span>
                    {testError}
                  </div>
                )}

                <div className="flex-1 overflow-auto custom-scrollbar">
                  {testRows.length === 0 && !testStreaming && !testError ? (
                    <div className="h-full flex flex-col items-center justify-center gap-3 text-zinc-600">
                      <span className="material-symbols-outlined text-4xl">play_circle</span>
                      <p className="text-xs">Click "Test Run" to preview query results</p>
                    </div>
                  ) : (
                    <table className="w-full text-left border-collapse">
                      <thead className="sticky top-0 z-10 bg-[#211e29] border-b border-[#574149]/10">
                        <tr>
                          {testColumns.map(col => (
                            <th key={col} className="px-4 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-widest whitespace-nowrap">{col}</th>
                          ))}
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-[#574149]/5 text-[11px] font-mono">
                        {testRows.map((row, i) => (
                          <tr key={i} className="hover:bg-[#211e29] transition-colors text-zinc-300">
                            {testColumns.map(col => (
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
