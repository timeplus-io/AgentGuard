export const OPEN_TABBY_EVENT = 'agentguard:open-tabby'

export function openTabby() {
  window.dispatchEvent(new CustomEvent(OPEN_TABBY_EVENT))
}

// Single source of truth for "is the full-page Tabby chat showing?" — used by
// both the TopNav launcher (hide the button) and the floating panel (render
// null). Trailing slashes still match the route in React Router v6.
export function isTabbyPage(pathname) {
  return pathname.replace(/\/+$/, '') === '/sentry'
}

// Shared user-message bubble surface (page chat, floating panel, suggestion
// chips). text-gray-100 is intentionally fixed: darkMode is 'class' and the
// light theme inverts the gray ramp, so gray-100 reads dark-on-light and
// light-on-dark without a dark: variant.
export const TABBY_USER_BUBBLE_CLASS = 'bg-pink-100 dark:bg-pink-800 text-gray-100'
export const TABBY_USER_BUBBLE_HOVER_CLASS = 'hover:bg-pink-200 dark:hover:bg-pink-700'
