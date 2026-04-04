export default function Footer() {
  return (
    <footer className="fixed bottom-0 left-64 right-0 h-8 bg-[#0f0c17] border-t border-[#574149]/10 px-6 flex items-center justify-between z-50">
      <div className="flex items-center gap-4">
        <div className="flex items-center gap-2">
          <span className="w-1.5 h-1.5 bg-emerald-500 rounded-full animate-pulse"></span>
          <span className="text-[9px] font-semibold text-zinc-400 uppercase tracking-tighter">System Health: Nominal</span>
        </div>
        <div className="h-3 w-px bg-[#574149] opacity-20"></div>
        <span className="text-[9px] font-semibold text-zinc-400 uppercase tracking-tighter">Latency: 24ms</span>
      </div>
      <div>
        <span className="text-[9px] font-semibold text-[#ffb0cf] uppercase tracking-tighter">Secure Link: AES-256-GCM Active</span>
      </div>
    </footer>
  )
}
