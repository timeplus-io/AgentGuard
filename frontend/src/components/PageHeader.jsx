/**
 * Consistent page title header used across all main pages.
 *
 * Props:
 *   title     – page title string
 *   subtitle  – plain string or JSX (supports highlighted spans)
 *   actions   – JSX for the right-side action buttons
 */
export default function PageHeader({ title, subtitle, actions }) {
  return (
    <div className="flex flex-col md:flex-row md:items-end justify-between mb-10 gap-4">
      <div>
        <h2 className="text-3xl font-medium text-[#e7dff0] tracking-tight mb-2">{title}</h2>
        <p className="text-zinc-500 text-xs max-w-lg">{subtitle}</p>
      </div>
      {actions && (
        <div className="flex gap-3 shrink-0">{actions}</div>
      )}
    </div>
  )
}
