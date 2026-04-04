const severityStyles = {
  Critical: 'bg-[#93000a] text-[#ffb4ab]',
  Warning: 'bg-[#37333f] text-[#ffb2b8]',
  Info: 'bg-[#2c2834] text-zinc-400',
}

const statusStyles = {
  ACTIVE: 'bg-[#ffb0cf]/10 text-[#ffb0cf] border border-[#ffb0cf]/20',
  THROTTLED: 'bg-[#ffb2b8]/10 text-[#ffb2b8] border border-[#ffb2b8]/20',
  SUSPENDED: 'bg-[#ffb4ab]/10 text-[#ffb4ab] border border-[#ffb4ab]/20',
}

export function SeverityBadge({ severity }) {
  return (
    <span className={`flex items-center gap-2 px-2.5 py-1 text-[10px] font-bold uppercase rounded-full w-fit ${severityStyles[severity] || severityStyles.Info}`}>
      <span className={`w-1.5 h-1.5 rounded-full ${severity === 'Critical' ? 'bg-[#ffb4ab] animate-pulse' : severity === 'Warning' ? 'bg-[#ffb2b8]' : 'bg-zinc-400'}`}></span>
      {severity}
    </span>
  )
}

export function StatusBadge({ status }) {
  return (
    <span className={`px-2 py-0.5 rounded-full text-[10px] font-semibold ${statusStyles[status] || statusStyles.ACTIVE}`}>
      {status}
    </span>
  )
}
