export default function Toggle({ checked, onChange, disabled }) {
  return (
    <button
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={() => !disabled && onChange(!checked)}
      className={`relative w-9 h-5 shrink-0 rounded-full transition-colors ${
        disabled
          ? checked ? 'bg-pink-700 dark:bg-[#B83280]' : 'bg-gray-700 dark:bg-gray-500'
          : checked ? 'bg-[#D53F8C]' : 'bg-secondary-container'
      } ${disabled ? 'cursor-not-allowed' : 'cursor-pointer'}`}
    >
      <span className={`absolute top-[3px] w-[14px] h-[14px] rounded-full transition-transform ${
        disabled ? checked ? 'bg-gray-700 dark:bg-gray-400' : 'bg-white dark:bg-gray-400' : 'bg-white'
      } ${checked ? 'left-0 translate-x-[19px]' : 'left-[3px]'}`} />
    </button>
  )
}
