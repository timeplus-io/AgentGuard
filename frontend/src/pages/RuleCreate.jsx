import { useState, useEffect } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import Editor from 'react-simple-code-editor'
import { highlight, languages } from 'prismjs/components/prism-core'
import 'prismjs/components/prism-sql'
import 'prismjs/themes/prism-tomorrow.css'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import PageHeader from '../components/PageHeader'
import ParamSchemaEditor, { validateKey, stripInternal, makeUid } from '../components/rules/ParamSchemaEditor'
import { hasRequiredFields } from '../components/rules/ruleDefinitionContract'
import { api } from '../api/client'

const defaultTemplate = `SELECT
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
WHERE 1 = 0 -- replace 1 = 0 with your detection condition; write a declared parameter as .Config.<key> inside double curly braces`

// emptyDefinition is the candidate Definition the editor mutates. It mirrors the
// /apps Definition shape: metadata + sql_template + config[] (typed param schema)
// + udfs[] (DDL strings).
const emptyDefinition = {
  id: '',
  name: '',
  version: '1.0.0',
  description: '',
  severity: 'warning',
  category: '',
  fp_rate: 'low',
  hook_sources: '',
  sql_template: defaultTemplate,
  config: [],
  udfs: [],
}

const inputCls =
  'w-full bg-surface-container-lowest rounded-sm px-3 py-2 text-xs text-gray-100 placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-[#ffb0cf]/50'
const labelCls = 'text-[10px] font-semibold text-gray-400 uppercase tracking-widest'

export default function RuleCreate({ mode = 'create' }) {
  const navigate = useNavigate()
  const { id } = useParams()
  const isEdit = mode === 'edit'

  const [def, setDef] = useState(emptyDefinition)
  // originalVersion guards an edit: publishing an edit requires bumping version.
  const [originalVersion, setOriginalVersion] = useState(null)
  const [loading, setLoading] = useState(isEdit)
  const [loadError, setLoadError] = useState(null)

  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState(null)

  // Compile-preview state. The rendered SQL comes ONLY from the backend; the
  // browser never substitutes the template itself.
  const [compiling, setCompiling] = useState(false)
  const [renderedSql, setRenderedSql] = useState(null)
  const [compileError, setCompileError] = useState(null)
  const [fieldErrors, setFieldErrors] = useState([])

  // Prefill from the catalog in edit/clone mode.
  useEffect(() => {
    if (!isEdit || !id) return
    let cancelled = false
    setLoading(true)
    api
      .getCatalogRule(id)
      .then((d) => {
        if (cancelled) return
        const loaded = normalizeDefinition(d)
        setDef(loaded)
        setOriginalVersion(loaded.version)
      })
      .catch((e) => !cancelled && setLoadError(e?.message || 'Failed to load definition'))
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [isEdit, id])

  const setField = (key, val) => setDef((prev) => ({ ...prev, [key]: val }))

  const buildPayload = () => ({
    ...def,
    id: def.id.trim() || undefined,
    config: stripInternal(def.config),
    udfs: def.udfs.filter((u) => u.trim() !== ''),
  })

  const handleCompile = async () => {
    setCompiling(true)
    setCompileError(null)
    setFieldErrors([])
    setRenderedSql(null)
    try {
      const r = await api.compileCatalogRule(buildPayload())
      setRenderedSql(r.rendered_sql || '')
    } catch (e) {
      // Surface *ValidationError.fields and *DefinitionError messages inline.
      const fields = e?.body?.fields
      if (Array.isArray(fields)) setFieldErrors(fields)
      setCompileError(e?.message || 'Compile failed')
    } finally {
      setCompiling(false)
    }
  }

  const versionUnchanged = isEdit && originalVersion != null && def.version === originalVersion

  const handleSave = async () => {
    if (versionUnchanged) {
      setSaveError('Bump the version to publish an edit.')
      return
    }
    setSaving(true)
    setSaveError(null)
    setFieldErrors([])
    try {
      const payload = buildPayload()
      if (isEdit) {
        const res = await api.updateCatalogRule(id, payload)
        // The response carries an `instance` only when the rule is installed.
        // /rules/:id is instance-detail and 404s for a definition-only publish
        // (e.g. a freshly cloned, not-yet-installed rule), so route those to the
        // catalog list instead of a dead end.
        navigate(res?.instance ? `/rules/${id}` : '/rules')
      } else {
        await api.createCatalogRule(payload)
        navigate('/rules')
      }
    } catch (e) {
      const fields = e?.body?.fields
      if (Array.isArray(fields)) setFieldErrors(fields)
      setSaveError(e?.message || 'Failed to save definition')
    } finally {
      setSaving(false)
    }
  }

  // Local schema validity: every param key must be present and well-formed.
  const keyErrors = (def.config || []).some((p) => validateKey(p.key) != null)
  // Required-field completeness is derived from the shared contract (mirrors the
  // backend Definition.validate() required set), so the form can never quietly
  // require a field the backend treats as optional — notably description.
  const canSave =
    hasRequiredFields(def, { isEdit }) &&
    !keyErrors &&
    !versionUnchanged &&
    !saving

  if (loading) {
    return (
      <div className="min-h-screen bg-[#15121d] text-gray-100">
        <Sidebar />
        <TopNav />
        <main className="pl-sidebar pt-16 pb-8 min-h-screen">
          <div className="p-6 lg:p-10 space-y-6">
            {[1, 2, 3].map((i) => (
              <div key={i} className="bg-surface-container-low rounded-sm h-32 animate-pulse" />
            ))}
          </div>
        </main>
      </div>
    )
  }

  if (loadError) {
    return (
      <div className="min-h-screen bg-[#15121d] text-gray-100">
        <Sidebar />
        <TopNav />
        <main className="pl-sidebar pt-16 pb-8 min-h-screen flex items-center justify-center">
          <div className="flex flex-col items-center gap-3">
            <span className="material-symbols-outlined text-[#ffb4ab] text-4xl">error</span>
            <p className="text-sm text-[#ffb4ab]">{loadError}</p>
          </div>
        </main>
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-[#15121d] text-gray-100">
      <Sidebar />
      <TopNav />

      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        <div className="p-6 lg:p-10 flex flex-col gap-6">
          <PageHeader
            title={isEdit ? 'Edit Custom Definition' : 'New Custom Rule'}
            subtitle="Author a parameterized detection: SQL template, typed parameters, and UDFs."
            actions={
              <div className="flex items-center gap-3">
                <button
                  onClick={() => navigate(isEdit ? `/rules/${id}` : '/rules')}
                  className="px-4 py-2 text-xs font-semibold text-gray-400 hover:text-white transition-colors"
                >
                  Cancel
                </button>
                <button
                  onClick={handleSave}
                  disabled={!canSave}
                  className="px-5 py-2 text-xs font-semibold rounded-sm bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] disabled:opacity-40 disabled:cursor-not-allowed hover:scale-[0.98] transition-all"
                >
                  {saving ? 'Saving…' : isEdit ? 'Publish edit' : 'Create definition'}
                </button>
              </div>
            }
          />

          {/* Editing a definition republishes it and, when the rule is installed,
              drops/recreates its MV (Proton has no in-place ALTER). Self-gated in
              copy so it stays accurate for create and uninstalled-edit, which
              redeploy nothing. */}
          {isEdit && (
            <p className="text-[11px] text-gray-500">
              If this rule is installed, publishing redeploys its detection view and in-flight stateful windows reset.
            </p>
          )}

          {versionUnchanged && (
            <div className="px-4 py-3 bg-amber-500/10 border border-amber-500/20 rounded-sm text-xs text-amber-300 flex items-center gap-2">
              <span className="material-symbols-outlined text-sm shrink-0">info</span>
              Bump the version to publish an edit (current: {originalVersion}).
            </div>
          )}

          {saveError && (
            <div className="px-4 py-3 bg-[#93000a]/20 border border-[#ffb4ab]/20 rounded-sm text-xs text-[#ffb4ab] font-mono flex items-center gap-2">
              <span className="material-symbols-outlined text-xs shrink-0">error</span>
              {saveError}
            </div>
          )}

          <div className="flex gap-6 flex-col xl:flex-row min-h-0">
            {/* Left: metadata + params + UDFs */}
            <aside className="w-full xl:w-[26rem] shrink-0 space-y-6">
              {/* Metadata */}
              <div className="bg-surface-container-high rounded-sm p-6 flex flex-col gap-5">
                <h3 className="text-[10px] font-bold text-[#ffb0cf] tracking-widest uppercase">Rule Metadata</h3>

                <div className="flex flex-col gap-1.5">
                  <label htmlFor="rule-id" className={labelCls}>
                    Rule ID{!isEdit && ' *'}
                  </label>
                  <input
                    id="rule-id"
                    type="text"
                    value={def.id}
                    onChange={(e) => setField('id', e.target.value)}
                    placeholder={isEdit ? '' : 'required — lowercase id, e.g. my-rule'}
                    disabled={isEdit}
                    className={`${inputCls} font-mono disabled:opacity-60 disabled:cursor-not-allowed`}
                  />
                </div>

                <div className="flex flex-col gap-1.5">
                  <label htmlFor="rule-name" className={labelCls}>Name *</label>
                  <input id="rule-name" type="text" value={def.name} onChange={(e) => setField('name', e.target.value)} placeholder="My Custom Rule" className={inputCls} />
                </div>

                <div className="flex flex-col gap-1.5">
                  <label htmlFor="rule-version" className={labelCls}>Version *</label>
                  <input id="rule-version" type="text" value={def.version} onChange={(e) => setField('version', e.target.value)} placeholder="1.0.0" className={`${inputCls} font-mono`} />
                  {isEdit && <p className="text-[10px] text-gray-400">Bump to publish an edit to an installed definition.</p>}
                </div>

                <div className="flex flex-col gap-1.5">
                  <label htmlFor="rule-description" className={labelCls}>Description</label>
                  <textarea id="rule-description" value={def.description} onChange={(e) => setField('description', e.target.value)} placeholder="What does this rule detect?" rows={3} className={`${inputCls} resize-none`} />
                </div>

                <div className="flex flex-col gap-1.5">
                  <label htmlFor="rule-severity" className={labelCls}>Severity</label>
                  <select id="rule-severity" value={def.severity} onChange={(e) => setField('severity', e.target.value)} className={inputCls}>
                    <option value="critical">Critical</option>
                    <option value="warning">Warning</option>
                    <option value="info">Info</option>
                  </select>
                </div>

                <div className="flex flex-col gap-1.5">
                  <label htmlFor="rule-category" className={labelCls}>Category</label>
                  <input id="rule-category" type="text" value={def.category} onChange={(e) => setField('category', e.target.value)} placeholder="e.g. prompt_injection_and_hijacking" className={inputCls} />
                </div>

                <div className="flex flex-col gap-1.5">
                  <label htmlFor="rule-fp-rate" className={labelCls}>False Positive Rate</label>
                  <select id="rule-fp-rate" value={def.fp_rate} onChange={(e) => setField('fp_rate', e.target.value)} className={inputCls}>
                    <option value="low">Low</option>
                    <option value="medium">Medium</option>
                    <option value="high">High</option>
                  </select>
                </div>

                <div className="flex flex-col gap-1.5">
                  <label htmlFor="rule-hook-sources" className={labelCls}>Hook Sources</label>
                  <input id="rule-hook-sources" type="text" value={def.hook_sources} onChange={(e) => setField('hook_sources', e.target.value)} placeholder="e.g. before_tool_call" className={inputCls} />
                </div>
              </div>

              {/* Parameters */}
              <div className="bg-surface-container-high rounded-sm p-6 flex flex-col gap-4">
                <div>
                  <h3 className="text-[10px] font-bold text-[#ffb0cf] tracking-widest uppercase">Parameters</h3>
                  <p className="text-[10px] text-gray-400 mt-1">Typed knobs operators can override. Reference them in the template as <code className="text-gray-300 font-mono">{'{{ .Config.<key> }}'}</code>.</p>
                </div>
                <ParamSchemaEditor config={def.config} onChange={(config) => setField('config', config)} />
              </div>

              {/* UDFs */}
              <div className="bg-surface-container-high rounded-sm p-6 flex flex-col gap-4">
                <div className="flex items-center justify-between">
                  <h3 className="text-[10px] font-bold text-[#ffb0cf] tracking-widest uppercase">UDFs</h3>
                  <button
                    type="button"
                    onClick={() => setField('udfs', [...def.udfs, ''])}
                    className="flex items-center gap-1 text-[10px] font-semibold uppercase tracking-wide text-gray-400 hover:text-[#ffb0cf] transition-colors"
                  >
                    <span className="material-symbols-outlined text-[14px] align-middle">add</span>
                    Add UDF
                  </button>
                </div>
                {def.udfs.length === 0 && <p className="text-[10px] text-gray-400">Optional CREATE FUNCTION DDL statements this rule depends on.</p>}
                {def.udfs.map((udf, i) => (
                  <div key={i} className="space-y-1.5">
                    <div className="flex items-center justify-between">
                      <span className={labelCls}>UDF {i + 1}</span>
                      <button
                        type="button"
                        onClick={() => setField('udfs', def.udfs.filter((_, idx) => idx !== i))}
                        className="text-gray-400 hover:text-[#ffb4ab] transition-colors"
                        title="Remove UDF"
                      >
                        <span className="material-symbols-outlined text-[16px] align-middle">delete</span>
                      </button>
                    </div>
                    <textarea
                      value={udf}
                      onChange={(e) => setField('udfs', def.udfs.map((u, idx) => (idx === i ? e.target.value : u)))}
                      rows={4}
                      placeholder="CREATE FUNCTION my_udf(...) RETURNS ... AS ..."
                      className={`${inputCls} font-mono resize-y`}
                    />
                  </div>
                ))}
              </div>
            </aside>

            {/* Right: SQL template editor + compile preview */}
            <section className="flex-1 flex flex-col min-w-0 gap-4">
              {/* Template editor */}
              <div className="flex flex-col bg-surface-container-high rounded-sm overflow-hidden">
                <div className="bg-surface-container px-6 py-3 flex justify-between items-center">
                  <span className="text-[10px] font-semibold text-gray-400 tracking-widest uppercase">SQL Template</span>
                  <button
                    onClick={handleCompile}
                    disabled={compiling}
                    className="px-5 py-1.5 font-semibold text-[10px] rounded-sm flex items-center gap-2 uppercase tracking-widest transition-all bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] hover:scale-[0.98] disabled:opacity-50"
                  >
                    <span className="material-symbols-outlined text-xs" style={{ fontVariationSettings: "'FILL' 1" }}>
                      {compiling ? 'hourglass_empty' : 'play_arrow'}
                    </span>
                    {compiling ? 'Compiling…' : 'Compile preview'}
                  </button>
                </div>
                <div className="h-72 overflow-auto custom-scrollbar bg-[#0f0c17]">
                  <Editor
                    value={def.sql_template}
                    onValueChange={(v) => setField('sql_template', v)}
                    highlight={(code) => highlight(code, languages.sql, 'sql')}
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

              {/* Inline field/definition errors from compile or save */}
              {fieldErrors.length > 0 && (
                <div className="bg-[#93000a]/20 border border-[#ffb4ab]/20 rounded-sm p-4 space-y-2">
                  <p className="text-[10px] font-semibold uppercase tracking-widest text-[#ffb4ab]">Validation errors</p>
                  {fieldErrors.map((f, i) => (
                    <div key={i} className="text-xs text-[#ffb4ab] font-mono">
                      {f.parameter ? <code className="text-[#ffb0cf]">{f.parameter}</code> : null}
                      {f.parameter ? ': ' : ''}
                      {f.message}
                    </div>
                  ))}
                </div>
              )}

              {/* Compile preview (backend-rendered, read-only) */}
              <div className="flex-1 flex flex-col bg-surface-container-high rounded-sm overflow-hidden min-h-[16rem]">
                <div className="p-4 flex items-baseline gap-3 shrink-0 bg-surface-container">
                  <h3 className="text-base font-semibold text-gray-100">Compiled SQL</h3>
                  <span className="text-[10px] text-gray-400">Rendered by the backend from the template + parameter defaults. The browser does not render SQL.</span>
                </div>

                {compileError && (
                  <div className="m-4 px-4 py-3 bg-[#93000a]/20 border border-[#ffb4ab]/20 rounded-sm text-xs text-[#ffb4ab] font-mono flex items-start gap-2">
                    <span className="material-symbols-outlined text-xs mt-0.5 shrink-0">error</span>
                    {compileError}
                  </div>
                )}

                <div className="flex-1 overflow-auto custom-scrollbar p-4">
                  {renderedSql == null && !compileError ? (
                    <div className="h-full flex flex-col items-center justify-center gap-3 text-gray-400">
                      <span className="material-symbols-outlined text-4xl">terminal</span>
                      <p className="text-xs">Click "Compile preview" to render the detection SQL.</p>
                    </div>
                  ) : renderedSql != null ? (
                    <>
                      {/* Safe: renderedSql is backend-rendered SQL; Prism highlight() HTML-encodes text content before emitting spans — safe only as long as highlight() remains the XSS boundary. */}
                      <pre
                        className="bg-[#0f0c17] text-[11px] font-mono rounded p-4 overflow-x-auto whitespace-pre-wrap break-all leading-relaxed"
                        dangerouslySetInnerHTML={{ __html: highlight(renderedSql, languages.sql, 'sql') }}
                      />
                    </>
                  ) : null}
                </div>
              </div>
            </section>
          </div>
        </div>
      </main>
    </div>
  )
}

// normalizeDefinition coerces a catalog GET response into the editor's candidate
// shape, tolerating absent optional fields.
function normalizeDefinition(d) {
  const src = d || {}
  return {
    id: src.id ?? '',
    name: src.name ?? '',
    version: src.version ?? '1.0.0',
    description: src.description ?? '',
    severity: src.severity ?? 'warning',
    category: src.category ?? '',
    fp_rate: src.fp_rate ?? 'low',
    hook_sources: src.hook_sources ?? '',
    sql_template: src.sql_template ?? defaultTemplate,
    config: Array.isArray(src.config) ? src.config.map((row) => ({ ...row, _uid: makeUid() })) : [],
    udfs: Array.isArray(src.udfs) ? src.udfs : [],
  }
}
