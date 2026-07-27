import { createContext, useContext, useState, useEffect, useCallback } from 'react'

// Theme controller (#240). The active theme is a `light`/`dark` class on
// <html>; all color tokens resolve to CSS variables scoped to that class
// (see src/index.css). Default is dark — light is opt-in. A pre-hydration
// inline script in index.html applies the persisted class before React mounts
// to avoid a flash; this provider keeps it in sync afterward and persists the
// choice, mirroring the localStorage + root-class pattern Sidebar uses for its
// collapsed state.
const ThemeContext = createContext(null)

function initialTheme() {
  try {
    return localStorage.getItem('theme') === 'light' ? 'light' : 'dark'
  } catch {
    return 'dark'
  }
}

export function ThemeProvider({ children }) {
  const [theme, setTheme] = useState(initialTheme)

  useEffect(() => {
    const root = document.documentElement
    root.classList.remove('light', 'dark')
    root.classList.add(theme)
    try {
      localStorage.setItem('theme', theme)
    } catch {
      /* storage unavailable — theme still applies for this session */
    }
  }, [theme])

  const toggleTheme = useCallback(() => {
    setTheme((t) => (t === 'dark' ? 'light' : 'dark'))
  }, [])

  return (
    <ThemeContext.Provider value={{ theme, setTheme, toggleTheme }}>
      {children}
    </ThemeContext.Provider>
  )
}

export function useTheme() {
  return useContext(ThemeContext)
}
