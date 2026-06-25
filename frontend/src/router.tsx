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
import { LogOut, BookOpen, Search, User } from "lucide-react"

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
      <div className="flex h-screen items-center justify-center bg-background text-white font-sans">
        <div className="flex flex-col items-center gap-4">
          <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent"></div>
          <p className="text-zinc-400 text-sm animate-pulse">Initializing Booklet Studio...</p>
        </div>
      </div>
    )
  }

  const isLoginPage = window.location.pathname === "/login"

  return (
    <QueryClientProvider client={queryClient}>
      <div className="min-h-screen flex flex-col font-sans">
        {!isLoginPage && authStatus.authenticated && (
          <header className="glass sticky top-0 z-50 px-6 py-4 flex items-center justify-between">
            <div className="flex items-center gap-3">
              <div className="bg-primary p-2 rounded-lg text-white shadow-md shadow-primary/20">
                <BookOpen className="h-6 w-6" aria-hidden="true" />
              </div>
              <div>
                <h1 className="text-xl font-bold tracking-tight text-white m-0 leading-none">Booklet Studio</h1>
                <p className="text-zinc-400 text-xs mt-1">Duplex Imposition & Semantic Search</p>
              </div>
            </div>

            <nav className="flex items-center gap-1">
              <Link 
                to="/" 
                activeProps={{ className: "bg-primary/10 text-primary border-primary/20" }}
                inactiveProps={{ className: "text-zinc-400 hover:text-white hover:bg-white/5 border-transparent" }}
                className="px-4 py-2 rounded-lg text-sm font-medium border transition-all flex items-center gap-2"
              >
                <BookOpen className="h-4 w-4" aria-hidden="true" />
                Dashboard
              </Link>
              <Link 
                to="/search" 
                activeProps={{ className: "bg-primary/10 text-primary border-primary/20" }}
                inactiveProps={{ className: "text-zinc-400 hover:text-white hover:bg-white/5 border-transparent" }}
                className="px-4 py-2 rounded-lg text-sm font-medium border transition-all flex items-center gap-2"
              >
                <Search className="h-4 w-4" aria-hidden="true" />
                Semantic Search
              </Link>
            </nav>

            <div className="flex items-center gap-4">
              <div className="hidden md:flex items-center gap-2 bg-zinc-900/50 border border-zinc-800/80 px-3 py-1.5 rounded-lg">
                <User className="h-4 w-4 text-zinc-400" aria-hidden="true" />
                <span className="text-zinc-300 text-xs font-medium">{authStatus.user?.name || authStatus.user?.email}</span>
              </div>
              <a 
                href={api.logoutUrl()} 
                className="p-2 text-zinc-400 hover:text-rose-400 hover:bg-rose-500/10 rounded-lg transition-all border border-transparent hover:border-rose-500/20"
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
          <footer className="py-6 border-t border-zinc-900/80 text-center text-zinc-400 text-xs">
            Booklet Studio &copy; {new Date().getFullYear()} &bull; SRE Instrumented &bull; Stateless Imposition
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
