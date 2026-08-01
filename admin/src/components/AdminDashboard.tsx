import React, { useState, useEffect } from "react"
import { adminApi } from "../api"
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from "./ui/card"
import { Button } from "./ui/button"
import { Input } from "./ui/input"
import { Label } from "./ui/label"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "./ui/tabs"
import { Form } from "./ui/form"
import { useTheme } from "./theme-provider"
import { Shield, Server, Loader2, Sun, Moon, Monitor, BarChart3, Wrench } from "lucide-react"
import { SMTPTab } from "./SMTPTab"
import { MaintenanceTab } from "./MaintenanceTab"
import { ObservabilityTab } from "./ObservabilityTab"
import { FeedbackBanner } from "./FeedbackBanner"

export function AdminDashboard() {
  const [apiKey, setApiKey] = useState<string>(() => localStorage.getItem("booklet_admin_api_key") || "dev-admin-key")
  const [inputKey, setInputKey] = useState(apiKey)
  const [unlocked, setUnlocked] = useState(false)
  const [verifying, setVerifying] = useState(false)
  const [keyError, setKeyError] = useState<string | null>(null)
  const { theme, resolvedTheme, setTheme } = useTheme()

  const handleUnlock = async (e: React.FormEvent) => {
    e.preventDefault()
    setVerifying(true)
    setKeyError(null)
    try {
      await adminApi.getSMTPConfig(inputKey)
      setApiKey(inputKey)
      localStorage.setItem("booklet_admin_api_key", inputKey)
      setUnlocked(true)
    } catch {
      setKeyError("Invalid API key or backend unreachable. Check your ADMIN_API_KEY environment variable.")
    } finally {
      setVerifying(false)
    }
  }

  // If we already have a key saved, attempt auto-unlock
  useEffect(() => {
    if (apiKey) {
      adminApi.getSMTPConfig(apiKey)
        .then(() => setUnlocked(true))
        .catch(() => setUnlocked(false))
    }
  }, [apiKey])

  return (
    <div className="min-h-screen flex flex-col font-sans">
      {/* Header */}
      <header className="glass sticky top-0 z-50 px-6 py-4 flex items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <div className="bg-primary p-2 rounded-lg text-primary-foreground shadow-md shadow-primary/20">
            <Shield className="h-5 w-5" aria-hidden="true" />
          </div>
          <div>
            <h1 className="text-lg font-bold tracking-tight text-foreground m-0 leading-none">Booklet Admin</h1>
            <p className="text-[11px] text-muted-foreground leading-none mt-0.5">Control Panel</p>
          </div>
        </div>

        <div className="flex items-center gap-2">
          {/* Theme switcher */}
          <div className="hidden sm:flex items-center rounded-full border border-border bg-background/80 p-1">
            {(["system", "light", "dark"] as const).map(t => (
              <Button
                key={t}
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => setTheme(t)}
                className={`rounded-full h-7 px-3 text-xs font-medium transition-all capitalize ${
                  theme === t
                    ? "bg-primary text-primary-foreground shadow hover:bg-primary/90"
                    : "text-muted-foreground hover:text-foreground hover:bg-accent/20"
                }`}
              >
                {t === "system" ? <Monitor className="h-3.5 w-3.5" /> : t === "light" ? <Sun className="h-3.5 w-3.5" /> : <Moon className="h-3.5 w-3.5" />}
              </Button>
            ))}
          </div>
          <Button
            variant="outline"
            size="icon"
            className="sm:hidden"
            onClick={() => setTheme(resolvedTheme === "dark" ? "light" : "dark")}
          >
            {resolvedTheme === "dark" ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
          </Button>

          {unlocked && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                setUnlocked(false)
                localStorage.removeItem("booklet_admin_api_key")
              }}
              className="text-xs text-muted-foreground hover:text-destructive hover:border-destructive/40"
            >
              Lock
            </Button>
          )}
        </div>
      </header>

      <main className="flex-1 p-6 md:p-8 max-w-6xl mx-auto w-full">
        {!unlocked ? (
          /* ── Key prompt ── */
          <div className="flex min-h-[70vh] items-center justify-center">
            <Card className="glass border-border w-full max-w-md">
              <CardHeader className="text-center pb-4">
                <div className="mx-auto mb-4 p-4 rounded-2xl bg-primary/10 w-fit">
                  <Shield className="h-10 w-10 text-primary" />
                </div>
                <CardTitle className="text-2xl font-bold">Admin Access</CardTitle>
                <CardDescription>Enter your <code className="font-mono bg-muted px-1 py-0.5 rounded text-xs">ADMIN_API_KEY</code> to unlock the control panel.</CardDescription>
              </CardHeader>
              <Form>
                <form onSubmit={handleUnlock}>
                  <CardContent className="space-y-4">
                    {keyError && <FeedbackBanner msg={{ type: "error", text: keyError }} />}
                    <div className="space-y-1.5">
                      <Label htmlFor="api-key-input" className="text-xs font-bold text-muted-foreground">Admin API Key</Label>
                      <Input
                        id="api-key-input"
                        type="password"
                        placeholder="Enter your admin key…"
                        value={inputKey}
                        onChange={e => setInputKey(e.target.value)}
                        className="bg-background/50 border-border focus-visible:ring-primary"
                        autoFocus
                      />
                    </div>
                  </CardContent>
                  <CardFooter>
                    <Button type="submit" disabled={verifying || !inputKey} className="w-full bg-primary hover:bg-primary/90 text-primary-foreground font-bold gap-2">
                      {verifying ? <Loader2 className="h-4 w-4 animate-spin" /> : <Shield className="h-4 w-4" />}
                      Unlock Panel
                    </Button>
                  </CardFooter>
                </form>
              </Form>
            </Card>
          </div>
        ) : (
          /* ── Tabbed dashboard ── */
          <div className="space-y-6">
            <div>
              <h2 className="text-2xl font-bold text-foreground">Admin Control Panel</h2>
              <p className="text-muted-foreground text-sm mt-1">Manage global system settings, run maintenance tasks, and monitor observability.</p>
            </div>

            <Tabs defaultValue="smtp" className="space-y-4">
              <TabsList className="gap-1">
                <TabsTrigger value="smtp" id="tab-smtp" className="gap-1.5">
                  <Server className="h-3.5 w-3.5" /> SMTP Settings
                </TabsTrigger>
                <TabsTrigger value="maintenance" id="tab-maintenance" className="gap-1.5">
                  <Wrench className="h-3.5 w-3.5" /> Maintenance
                </TabsTrigger>
                <TabsTrigger value="observability" id="tab-observability" className="gap-1.5">
                  <BarChart3 className="h-3.5 w-3.5" /> Observability
                </TabsTrigger>
              </TabsList>

              <TabsContent value="smtp">
                <SMTPTab apiKey={apiKey} />
              </TabsContent>
              <TabsContent value="maintenance">
                <MaintenanceTab apiKey={apiKey} />
              </TabsContent>
              <TabsContent value="observability">
                <ObservabilityTab />
              </TabsContent>
            </Tabs>
          </div>
        )}
      </main>

      <footer className="py-5 border-t border-border text-center text-muted-foreground text-xs">
        Booklet Studio Admin Panel &copy; {new Date().getFullYear()} — Internal use only
      </footer>
    </div>
  )
}
