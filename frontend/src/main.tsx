import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import "./index.css"
import { RouterProvider } from "@tanstack/react-router"
import { router } from "./router"
import { ThemeProvider } from "./components/theme-provider"

const savedTheme = window.localStorage.getItem("booklet-theme")
const systemTheme = window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light"
const resolvedTheme = savedTheme === "light" || savedTheme === "dark"
  ? savedTheme
  : systemTheme

document.documentElement.classList.toggle("dark", resolvedTheme === "dark")
document.documentElement.dataset.theme = resolvedTheme
document.documentElement.style.colorScheme = resolvedTheme

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider>
      <RouterProvider router={router} />
    </ThemeProvider>
  </StrictMode>
)
