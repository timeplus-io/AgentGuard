import { useTranslation } from 'react-i18next'

export default function BlockPolicySelect({ value, onChange, disabled }) {
  const { t } = useTranslation('rules')
  return (
    <select
      value={value || 'log_only'}
      onChange={e => onChange(e.target.value)}
      disabled={disabled}
      className="bg-gray-800 border border-gray-600 rounded-sm text-[10px] px-2 py-1 text-gray-100 focus:outline-none focus:border-gray-400 disabled:opacity-50"
    >
      <option value="log_only">{t('policy.log_only', { defaultValue: 'Log only' })}</option>
      <option value="auto_block">{t('policy.auto_block', { defaultValue: 'Auto-block' })}</option>
      <option value="hold">{t('policy.hold', { defaultValue: 'Hold (require approval)' })}</option>
    </select>
  );
}
