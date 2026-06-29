import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import "./index.css"
import { ThemeProvider } from "./components/theme-provider"
import { AdminDashboard } from "./components/AdminDashboard"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } },
})

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <ThemeProvider defaultTheme="dark" storageKey="booklet-admin-ui-theme">
        <AdminDashboard />
      </ThemeProvider>
    </QueryClientProvider>
  </StrictMode>
)
