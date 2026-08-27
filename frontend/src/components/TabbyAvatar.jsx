// `ring` is a prop rather than part of the base class: two ring-color
// utilities on one element resolve by stylesheet order, not className order,
// so callers could not reliably override a hardcoded ring color.
export default function TabbyAvatar({ className = 'h-6 w-6', ring = 'ring-pink-500/40' }) {
  return (
    <span
      className={`relative inline-block shrink-0 overflow-hidden rounded-full bg-pink-100 ring-1 ${ring} ${className}`}
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
