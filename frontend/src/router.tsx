import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  Link,
  useNavigate
} from "@tanstack/react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import React, { useEffect, useState } from "react"
import { api } from "./api"
import type { AuthStatus } from "./api"
import { LogOut, BookOpen, Search, User, Sun, Moon, Monitor } from "lucide-react"
import { Card } from "./components/ui/card"
import { Button } from "./components/ui/button"
import { useTheme } from "./components/theme-provider"

import { Dashboard } from "./components/Dashboard"
import { SemanticSearch } from "./components/SemanticSearch"
import { Login } from "./components/Login"

// Create TanStack Query Client
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: false,
      refetchOnWindowFocus: false,
    },
  },
})

// Layout Component
const RootLayout: React.FC = () => {
  const [authStatus, setAuthStatus] = useState<AuthStatus | null>(null)
  const navigate = useNavigate()
  const { theme, resolvedTheme, setTheme } = useTheme()

  useEffect(() => {
    api.getMe()
      .then((status) => {
        setAuthStatus(status)
        if (!status.authenticated && window.location.pathname !== "/login") {
          navigate({ to: "/login" })
        }
      })
      .catch(() => {
        setAuthStatus({ authenticated: false })
        if (window.location.pathname !== "/login") {
          navigate({ to: "/login" })
        }
      })
  }, [navigate])

  if (authStatus === null) {
    return (
      <div className="flex h-screen items-center justify-center bg-background text-foreground font-sans">
        <div className="flex flex-col items-center gap-4">
          <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent"></div>
          <p className="text-muted-foreground text-sm animate-pulse">Initializing Booklet Studio...</p>
        </div>
      </div>
    )
  }

  const isLoginPage = window.location.pathname === "/login"

  return (
    <QueryClientProvider client={queryClient}>
      <div className="min-h-screen flex flex-col font-sans">
        {!isLoginPage && authStatus.authenticated && (
          <header className="glass sticky top-0 z-50 px-6 py-4 flex items-center justify-between gap-4">
            <div className="flex items-center gap-3">
              <Card className="bg-primary p-2 rounded-lg text-primary-foreground shadow-md shadow-primary/20">
                <BookOpen className="h-6 w-6" aria-hidden="true" />
              </Card>
              <div>
                <h1 className="text-xl font-bold tracking-tight text-foreground m-0 leading-none">Booklet Studio</h1>
              </div>
            </div>

            <nav className="flex items-center gap-1">
              <Link
                to="/"
                activeProps={{ className: "bg-primary/15 text-primary border-primary/30" }}
                inactiveProps={{ className: "text-muted-foreground hover:text-foreground hover:bg-muted/60 border-transparent" }}
                className="px-4 py-2 rounded-lg text-sm font-medium border transition-all flex items-center gap-2"
              >
                <BookOpen className="h-4 w-4" aria-hidden="true" />
                Dashboard
              </Link>
              <Link
                to="/search"
                activeProps={{ className: "bg-primary/15 text-primary border-primary/30" }}
                inactiveProps={{ className: "text-muted-foreground hover:text-foreground hover:bg-muted/60 border-transparent" }}
                className="px-4 py-2 rounded-lg text-sm font-medium border transition-all flex items-center gap-2"
              >
                <Search className="h-4 w-4" aria-hidden="true" />
                Semantic Search
              </Link>

            </nav>

            <div className="flex items-center gap-2 md:gap-3">
              <div className="hidden lg:flex items-center rounded-full border border-border bg-background/80 p-1">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => setTheme("system")}
                  className={`rounded-full h-7 px-3 text-xs font-medium transition-all ${
                    theme === "system"
                      ? "bg-primary text-primary-foreground shadow hover:bg-primary/90"
                      : "text-muted-foreground hover:text-foreground hover:bg-accent/50"
                  }`}
                >
                  <Monitor className="mr-1.5 h-3.5 w-3.5" aria-hidden="true" />
                  System
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => setTheme("light")}
                  className={`rounded-full h-7 px-3 text-xs font-medium transition-all ${
                    theme === "light"
                      ? "bg-primary text-primary-foreground shadow hover:bg-primary/90"
                      : "text-muted-foreground hover:text-foreground hover:bg-accent/50"
                  }`}
                >
                  <Sun className="mr-1.5 h-3.5 w-3.5" aria-hidden="true" />
                  Light
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => setTheme("dark")}
                  className={`rounded-full h-7 px-3 text-xs font-medium transition-all ${
                    theme === "dark"
                      ? "bg-primary text-primary-foreground shadow hover:bg-primary/90"
                      : "text-muted-foreground hover:text-foreground hover:bg-accent/50"
                  }`}
                >
                  <Moon className="mr-1.5 h-3.5 w-3.5" aria-hidden="true" />
                  Dark
                </Button>
              </div>

              <Button variant="outline" size="icon" className="lg:hidden" onClick={() => setTheme(resolvedTheme === "dark" ? "light" : "dark")} aria-label="Toggle theme">
                {resolvedTheme === "dark" ? <Sun className="h-4 w-4" aria-hidden="true" /> : <Moon className="h-4 w-4" aria-hidden="true" />}
              </Button>

              <Card className="hidden md:flex items-center gap-2 bg-background/80 border border-border px-3 py-1.5 rounded-lg">
                <User className="h-4 w-4 text-muted-foreground" aria-hidden="true" />
                <span className="text-foreground text-xs font-medium">{authStatus.user?.name || authStatus.user?.email}</span>
              </Card>
              <a
                href={api.logoutUrl()}
                className="p-2 text-muted-foreground hover:text-accent hover:bg-accent/10 rounded-lg transition-all border border-transparent hover:border-accent/20"
                aria-label="Log Out"
              >
                <LogOut className="h-5 w-5" aria-hidden="true" />
              </a>
            </div>
          </header>
        )}

        <main className="flex-1 p-6 md:p-8 max-w-7xl mx-auto w-full">
          <Outlet />
        </main>

        {!isLoginPage && (
          <footer className="py-6 border-t border-border text-center text-muted-foreground text-xs">
            Booklet Studio &copy; {new Date().getFullYear()}
          </footer>
        )}
      </div>
    </QueryClientProvider>
  )
}

// Create Routes
const rootRoute = createRootRoute({
  component: RootLayout,
})

export const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: Dashboard,
})

export const searchRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/search",
  component: SemanticSearch,
})

export const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  component: Login,
})

const routeTree = rootRoute.addChildren([indexRoute, searchRoute, loginRoute])

export const router = createRouter({ routeTree })

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router
  }
}
