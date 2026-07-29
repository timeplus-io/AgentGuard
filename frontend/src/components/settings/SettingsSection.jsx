import { useTranslation } from 'react-i18next'
import Toggle from '../Toggle'
import DLPConfigEditor from '../DLPConfigEditor'
import SentryConfigEditor from '../SentryConfigEditor'

// DLP keys that remain server-config (config.yaml): the enable switch and the
// embedding dimension (baked into the stream/index). Everything else — endpoint,
// models, timeouts, tokens, entity catalog — is edited live below via the config
// stream, so it's hidden from the flat key list to avoid two sources of truth.
const DLP_SERVER_KEYS = new Set(['dlp.enabled', 'dlp.embedding_dim'])

// Sentry keys that remain server-config (config.yaml): the enable switch and
// the SQL skill directory override. Everything else — chat_base_url,
// chat_api_key, chat_model, memory_enabled, embedding_base_url,
// embedding_api_key, embedding_model, embedding_dim,
// request_timeout_seconds, max_output_tokens — is a boot default only; the
// live value is edited below via the agentguard_sentry_config stream's
// `chat`/`memory` rows, so those keys are hidden from the flat key list to
// avoid two sources of truth (see registry.go's descriptions, which already
// point operators at Settings → Sentry Agent).
const SENTRY_SERVER_KEYS = new Set(['sentry.enabled', 'sentry.skill_dir'])

const SOURCE_KEYS = new Set(['default', 'file', 'env', 'flag'])

function SourceChip({ k }) {
  const { t } = useTranslation('settings')
  const label = SOURCE_KEYS.has(k.source) ? t(`source.${k.source}`) : k.source
  const title =
    k.source === 'env' ? t('source.envTitle', { envVar: k.env_var })
    : k.source === 'flag' ? t('source.flagTitle', { flag: k.flag })
    : undefined
  return (
    <span title={title} className="text-[10px] uppercase tracking-wide rounded-full px-2 py-0.5 bg-gray-700 text-gray-300">
      {label}
    </span>
  )
}

function KeyField({ k, draftValue, onChange }) {
  const { t } = useTranslation('settings')
  const disabled = !k.editable
  const value = draftValue
  const baseInput =
    'w-full bg-gray-900 text-gray-100 text-sm rounded-sm px-3 py-2 outline-none focus:bg-gray-800 disabled:opacity-50 disabled:cursor-not-allowed'

  let input
  if (k.type === 'bool') {
    input = <Toggle checked={!!value} onChange={(v) => onChange(v)} disabled={disabled} />
  } else if (k.type === 'enum') {
    input = (
      <select className={baseInput} value={value ?? ''} disabled={disabled} onChange={(e) => onChange(e.target.value)}>
        {k.enum_values?.map((opt) => <option key={opt} value={opt}>{opt}</option>)}
      </select>
    )
  } else if (k.type === 'secret') {
    input = (
      <input
        type="password"
        className={baseInput}
        disabled={disabled}
        placeholder={k.set ? t('field.secretSet') : t('field.secretNotSet')}
        value={value ?? ''}
        onChange={(e) => onChange(e.target.value)}
      />
    )
  } else if (k.type === 'int') {
    input = (
      <input
        type="number"
        className={baseInput}
        disabled={disabled}
        value={value ?? ''}
        onChange={(e) => onChange(e.target.value === '' ? '' : Number(e.target.value))}
      />
    )
  } else {
    input = (
      <input
        type="text"
        className={baseInput}
        disabled={disabled}
        value={value ?? ''}
        onChange={(e) => onChange(e.target.value)}
      />
    )
  }

  return (
    <div className="py-3">
      <div className="flex items-center justify-between gap-3 mb-1">
        <div className="flex items-center gap-2">
          <span className="text-sm text-gray-100">{t('configKeys.' + k.key.replace(/\./g, '_') + '.label', { defaultValue: k.label })}</span>
          <SourceChip k={k} />
          {k.restart && <span className="text-[10px] text-amber-400">{t('field.restartBadge')}</span>}
          {k.reprovision && <span className="text-[10px] text-amber-400">{t('field.reprovisionBadge')}</span>}
        </div>
      </div>
      <p className="text-xs text-gray-400 mb-2">{t('configKeys.' + k.key.replace(/\./g, '_') + '.description', { defaultValue: k.description })}</p>
      <div className="max-w-md">{input}</div>
    </div>
  )
}

// Renders one settings section card. Section-specific extras: the Timeplus
// test-connection row, and the DLP card's three tonal sub-groups
// (server config / runtime config / operations).
export default function SettingsSection({
  section,
  draftFor,
  setDraft,
  fieldErrors,
  tpTest,
  onTestTimeplus,
  reprovisioning,
  onReprovision,
  configSavedAt,
}) {
  const { t } = useTranslation('settings')
  const keys = section.id === 'dlp'
    ? section.keys.filter((k) => DLP_SERVER_KEYS.has(k.key))
    : section.id === 'sentry'
    ? section.keys.filter((k) => SENTRY_SERVER_KEYS.has(k.key))
    : section.keys

  const keyList = (
    <div className="divide-y divide-gray-700/50">
      {keys.map((k) => (
        <div key={k.key}>
          <KeyField k={k} draftValue={draftFor(k)} onChange={(v) => setDraft(k.key, v)} />
          {fieldErrors[k.key] && <p className="text-error text-xs pb-2">{fieldErrors[k.key]}</p>}
        </div>
      ))}
    </div>
  )

  if (section.id === 'sentry') {
    return (
      <section className="bg-gray-800 rounded-sm px-6 py-4">
        <h3 className="text-base font-medium text-gray-100 mb-3">{t('configSections.' + section.id, { defaultValue: section.title })}</h3>
        <div className="flex flex-col gap-4">
          <div className="bg-gray-900/40 rounded-sm px-4 py-3">
            <h4 className="text-sm font-semibold text-gray-100 mb-1">{t('section.serverConfigHeading')}</h4>
            <p className="text-xs text-gray-400">
              {t('section.sentry.serverConfigDesc')}
            </p>
            {keyList}
          </div>
          <div className="bg-gray-900/40 rounded-sm px-4 py-3">
            <h4 className="text-sm font-semibold text-gray-100 mb-1">{t('section.sentry.modelConfigHeading')}</h4>
            <p className="text-xs text-gray-400 mb-2">
              {t('section.sentry.modelConfigDesc')}
            </p>
            <SentryConfigEditor reloadToken={configSavedAt} />
          </div>
        </div>
      </section>
    )
  }

  if (section.id !== 'dlp') {
    return (
      <section className="bg-gray-800 rounded-sm px-6 py-4">
        <h3 className="text-base font-medium text-gray-100 mb-2">{t('configSections.' + section.id, { defaultValue: section.title })}</h3>
        {keyList}
        {section.id === 'timeplus' && (
          <div className="mt-3 flex items-center gap-3">
            <button onClick={onTestTimeplus} className="rounded-sm px-3 py-1.5 text-xs bg-gray-700 text-gray-100 hover:bg-gray-600">{t('section.timeplus.testConnection')}</button>
            {tpTest?.pending && <span className="text-xs text-gray-400">{t('section.timeplus.testing')}</span>}
            {tpTest && !tpTest.pending && (
              <span className={`text-xs ${tpTest.ok ? 'text-emerald-400' : 'text-error'}`}>
                {tpTest.ok ? t('section.timeplus.connectionOk') : t('section.timeplus.connectionFailed', { error: tpTest.error })}
              </span>
            )}
          </div>
        )}
      </section>
    )
  }

  return (
    <section className="bg-gray-800 rounded-sm px-6 py-4">
      <h3 className="text-base font-medium text-gray-100 mb-3">{t('configSections.' + section.id, { defaultValue: section.title })}</h3>
      <div className="flex flex-col gap-4">
        <div className="bg-gray-900/40 rounded-sm px-4 py-3">
          <h4 className="text-sm font-semibold text-gray-100 mb-1">{t('section.serverConfigHeading')}</h4>
          <p className="text-xs text-gray-400">
            {t('section.dlp.serverConfigDesc')}
          </p>
          {keyList}
        </div>
        <div className="bg-gray-900/40 rounded-sm px-4 py-3">
          <h4 className="text-sm font-semibold text-gray-100 mb-1">{t('section.dlp.runtimeConfigHeading')}</h4>
          <p className="text-xs text-gray-400 mb-2">
            {t('section.dlp.runtimeConfigDesc')}
          </p>
          <DLPConfigEditor />
        </div>
        <div className="bg-gray-900/40 rounded-sm px-4 py-3">
          <h4 className="text-sm font-semibold text-gray-100 mb-1">{t('section.dlp.operationsHeading')}</h4>
          <div className="flex items-center gap-3">
            <button onClick={onReprovision} disabled={reprovisioning}
              className="rounded-sm px-3 py-1.5 text-xs bg-gray-700 text-gray-100 hover:bg-gray-600 disabled:opacity-50">
              {reprovisioning ? t('actions.reprovisioning') : t('section.dlp.reprovisionButton')}
            </button>
            <span className="text-[11px] text-gray-500">
              {t('section.dlp.reprovisionHint')}
            </span>
          </div>
        </div>
      </div>
    </section>
  )
}
