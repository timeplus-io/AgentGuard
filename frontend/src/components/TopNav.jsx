export default function TopNav({ title }) {
  return (
    <header className="top-nav fixed top-0 right-0 h-16 bg-[#15121d] z-50 flex justify-between items-center px-8 shadow-[0_0_12px_rgba(213,63,140,0.12)] backdrop-blur-md">
      <div className="flex items-center gap-4 flex-1">
        {title && (
          <h2 className="text-lg font-semibold bg-clip-text text-transparent bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] tracking-tight">{title}</h2>
        )}
      </div>
      <div className="flex items-center gap-6">
        <div className="flex items-center gap-4 text-zinc-400">
          <button className="hover:text-[#ffb0cf] transition-colors">
            <span className="material-symbols-outlined">monitor_heart</span>
          </button>
          <button className="hover:text-[#ffb0cf] transition-colors relative">
            <span className="material-symbols-outlined">notifications</span>
            <span className="absolute top-0 right-0 w-2 h-2 bg-[#ffb0cf] rounded-full"></span>
          </button>
        </div>
        <div className="h-8 w-px bg-[#574149] opacity-20"></div>
        <div className="flex items-center gap-3">
          <div className="text-right hidden sm:block">
            <p className="text-xs font-medium text-[#e7dff0]">Admin_Sentry</p>
            <p className="text-[10px] text-[#ffb0cf] uppercase font-semibold tracking-tighter">Level 4 Access</p>
          </div>
          <div className="w-8 h-8 rounded-sm bg-[#2c2834] flex items-center justify-center text-[#ffb0cf] font-semibold text-xs">AS</div>
        </div>
      </div>
    </header>
  )
}
