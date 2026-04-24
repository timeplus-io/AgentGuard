export default function BlockPolicySelect({ value, onChange, disabled }) {
  return (
    <select
      value={value || 'log_only'}
      onChange={e => onChange(e.target.value)}
      disabled={disabled}
      className="bg-gray-800 border border-gray-600 rounded-sm text-[10px] px-2 py-1 text-zinc-300 focus:outline-none focus:border-gray-400 disabled:opacity-50"
    >
      <option value="log_only">Log only</option>
      <option value="auto_block">Auto-block</option>
      <option value="hold">Hold (require approval)</option>
    </select>
  );
}
