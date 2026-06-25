import React from "react"

type ThemeMode = "system" | "light" | "dark"

type ThemeContextValue = {
  theme: ThemeMode
  resolvedTheme: "light" | "dark"
  setTheme: (theme: ThemeMode) => void
  toggleTheme: () => void
}

const STORAGE_KEY = "booklet-theme"
const ThemeContext = React.createContext<ThemeContextValue | null>(null)

const getSystemTheme = (): "light" | "dark" => {
  if (typeof window === "undefined") {
    return "dark"
  }

  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light"
}

export const ThemeProvider: React.FC<React.PropsWithChildren> = ({ children }) => {
  const [theme, setThemeState] = React.useState<ThemeMode>(() => {
    if (typeof window === "undefined") {
      return "system"
    }

    const storedTheme = window.localStorage.getItem(STORAGE_KEY)
    return storedTheme === "light" || storedTheme === "dark" || storedTheme === "system"
      ? storedTheme
      : "system"
  })
  const [systemTheme, setSystemTheme] = React.useState<"light" | "dark">(getSystemTheme)

  React.useEffect(() => {
    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)")
    const handleChange = () => setSystemTheme(mediaQuery.matches ? "dark" : "light")

    mediaQuery.addEventListener("change", handleChange)
    handleChange()

    return () => mediaQuery.removeEventListener("change", handleChange)
  }, [])

  React.useEffect(() => {
    const resolvedTheme = theme === "system" ? systemTheme : theme
    const root = document.documentElement

    root.classList.toggle("dark", resolvedTheme === "dark")
    root.dataset.theme = resolvedTheme
    root.style.colorScheme = resolvedTheme

    if (theme === "system") {
      window.localStorage.removeItem(STORAGE_KEY)
    } else {
      window.localStorage.setItem(STORAGE_KEY, theme)
    }
  }, [systemTheme, theme])

  const value = React.useMemo<ThemeContextValue>(() => {
    const resolvedTheme = theme === "system" ? systemTheme : theme

    return {
      theme,
      resolvedTheme,
      setTheme: setThemeState,
      toggleTheme: () => {
        setThemeState((current) => {
          if (current === "system") {
            return systemTheme === "dark" ? "light" : "dark"
          }

          return current === "dark" ? "light" : "dark"
        })
      },
    }
  }, [systemTheme, theme])

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export const useTheme = () => {
  const context = React.useContext(ThemeContext)

  if (!context) {
    throw new Error("useTheme must be used within ThemeProvider")
  }

  return context
}