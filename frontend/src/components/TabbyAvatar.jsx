export default function TabbyAvatar({ className = 'h-6 w-6' }) {
  return (
    <span
      className={`relative inline-block shrink-0 overflow-hidden rounded-full bg-pink-100 ring-1 ring-pink-500/40 ${className}`}
      aria-hidden="true"
    >
      <img
        src="/static/tabby-avatar.png"
        alt=""
        className="pointer-events-none h-full w-full select-none object-cover"
      />
    </span>
  )
}
