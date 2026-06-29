import React, { useState, useEffect, useCallback } from "react"
import { adminApi, type DocumentInfo, type SMTPConfig } from "../api"
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from "./ui/card"
import { Button } from "./ui/button"
import { Input } from "./ui/input"
import { Label } from "./ui/label"
import { Select } from "./ui/select"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "./ui/tabs"
import { Form } from "./ui/form"
import { useTheme } from "./theme-provider"
import {
  Shield, Server, Mail, Wrench, BarChart3,
  CheckCircle2, AlertTriangle, Loader2, Save, Send,
  RefreshCw, Play, BookOpen, Sun, Moon, Monitor,
  ExternalLink, Database, Activity, Clock, FileText,
  TrendingUp, Zap, Eye
} from "lucide-react"

type Feedback = { type: "success" | "error"; text: string } | null

// ─── Stat Card ───────────────────────────────────────────────────────────────
function StatCard({ label, value, icon: Icon, color }: {
  label: string; value: number; icon: React.ElementType; color: string
}) {
  return (
    <Card className="glass border-border">
      <CardContent className="pt-5 pb-4">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">{label}</p>
            <p className={`text-3xl font-bold mt-1 ${color}`}>{value}</p>
          </div>
          <div className={`p-3 rounded-xl bg-current/10 ${color}`}>
            <Icon className="h-5 w-5" />
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

// ─── Feedback Banner ─────────────────────────────────────────────────────────
function FeedbackBanner({ msg }: { msg: Feedback }) {
  if (!msg) return null
  return (
    <div className={`flex items-center gap-3 p-3 rounded-lg border text-xs font-medium ${
      msg.type === "success"
        ? "bg-green-500/10 border-green-500/30 text-green-500"
        : "bg-destructive/10 border-destructive/30 text-destructive"
    }`}>
      {msg.type === "success"
        ? <CheckCircle2 className="h-4 w-4 shrink-0" />
        : <AlertTriangle className="h-4 w-4 shrink-0" />}
      <span>{msg.text}</span>
    </div>
  )
}

// ─── SMTP Tab ────────────────────────────────────────────────────────────────
function SMTPTab({ apiKey }: { apiKey: string }) {
  const [cfg, setCfg] = useState<SMTPConfig>({ host: "", port: 587, username: "", password: "", encryption: "starttls", from_email: "", from_name: "" })
  const [testEmail, setTestEmail] = useState("")
  const [loading, setLoading] = useState(false)
  const [testing, setTesting] = useState(false)
  const [msg, setMsg] = useState<Feedback>(null)
  const [testMsg, setTestMsg] = useState<Feedback>(null)

  const load = useCallback(async () => {
    if (!apiKey) return
    setLoading(true); setMsg(null)
    try {
      const data = await adminApi.getSMTPConfig(apiKey)
      setCfg(data)
    } catch {
      setMsg({ type: "error", text: "Failed to load SMTP config. Check your Admin API Key." })
    } finally { setLoading(false) }
  }, [apiKey])

  useEffect(() => { load() }, [load])

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault(); setLoading(true); setMsg(null)
    try {
      const res = await adminApi.saveSMTPConfig(apiKey, cfg)
      setMsg({ type: "success", text: res.message || "SMTP configuration saved." })
    } catch (err: unknown) {
      setMsg({ type: "error", text: err instanceof Error ? err.message : "Save failed." })
    } finally { setLoading(false) }
  }

  const handleTest = async () => {
    if (!testEmail) { setTestMsg({ type: "error", text: "Enter a recipient email first." }); return }
    setTesting(true); setTestMsg(null)
    try {
      const res = await adminApi.testSMTPConfig(apiKey, cfg, testEmail)
      setTestMsg({ type: "success", text: res.message || "Test email sent!" })
    } catch (err: unknown) {
      setTestMsg({ type: "error", text: err instanceof Error ? err.message : "Test failed." })
    } finally { setTesting(false) }
  }

  const field = (id: string, label: string, value: string | number, onChange: (v: string) => void, opts?: { type?: string; placeholder?: string; required?: boolean }) => (
    <div className="space-y-1.5">
      <Label htmlFor={id} className="text-xs font-bold text-muted-foreground">{label}</Label>
      <Input id={id} type={opts?.type || "text"} placeholder={opts?.placeholder} value={value} onChange={e => onChange(e.target.value)}
        required={opts?.required} className="bg-background/50 border-border focus-visible:ring-primary" />
    </div>
  )

  return (
    <div className="space-y-5">
      <Card className="glass border-border">
        <CardHeader className="pb-4">
          <div className="flex items-center gap-2">
            <Server className="h-5 w-5 text-primary" />
            <CardTitle className="text-base font-bold">Mail Server Configuration</CardTitle>
          </div>
          <CardDescription className="text-xs">Configure the global SMTP server used to send booklet PDFs and system alerts.</CardDescription>
        </CardHeader>
        <Form>
          <form onSubmit={handleSave}>
          <CardContent className="space-y-4">
            <FeedbackBanner msg={msg} />
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              {field("smtp-host", "SMTP Server Host", cfg.host, v => setCfg(c => ({ ...c, host: v })), { placeholder: "smtp.gmail.com", required: true })}
              {field("smtp-port", "SMTP Port", cfg.port, v => setCfg(c => ({ ...c, port: Number(v) })), { type: "number", placeholder: "587", required: true })}
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              {field("smtp-user", "Username / Account", cfg.username, v => setCfg(c => ({ ...c, username: v })), { placeholder: "your@email.com" })}
              {field("smtp-pass", "Password", cfg.password, v => setCfg(c => ({ ...c, password: v })), { type: "password", placeholder: cfg.password ? "••••••••" : "Enter password" })}
            </div>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
              <div className="space-y-1.5">
                <Label htmlFor="smtp-enc" className="text-xs font-bold text-muted-foreground">Encryption</Label>
                <Select id="smtp-enc" value={cfg.encryption} onChange={e => setCfg(c => ({ ...c, encryption: e.target.value }))} className="bg-background/50 border-border focus-visible:ring-primary">
                  <option value="none">None (Plaintext)</option>
                  <option value="ssl">SSL / Implicit TLS (465)</option>
                  <option value="starttls">STARTTLS / Explicit TLS (587)</option>
                </Select>
              </div>
              {field("smtp-from-email", "From Email", cfg.from_email, v => setCfg(c => ({ ...c, from_email: v })), { type: "email", placeholder: "noreply@example.com", required: true })}
              {field("smtp-from-name", "From Display Name", cfg.from_name, v => setCfg(c => ({ ...c, from_name: v })), { placeholder: "Booklet Studio" })}
            </div>
          </CardContent>
          <CardFooter className="flex items-center justify-between border-t border-border/40 pt-4">
            <Button type="button" variant="outline" onClick={load} disabled={loading} className="text-xs gap-1.5">
              <RefreshCw className="h-3.5 w-3.5" /> Reload
            </Button>
            <Button type="submit" disabled={loading} className="bg-primary hover:bg-primary/90 text-primary-foreground font-bold gap-1.5">
              {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
              Save Configuration
            </Button>
          </CardFooter>
          </form>
        </Form>
      </Card>

      <Card className="glass border-border">
        <CardHeader>
          <div className="flex items-center gap-2">
            <Mail className="h-5 w-5 text-primary" />
            <CardTitle className="text-base font-bold">SMTP Connection Test</CardTitle>
          </div>
          <CardDescription className="text-xs">Send a test email to verify the mail server is reachable and authenticated.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <FeedbackBanner msg={testMsg} />
          <div className="flex flex-col md:flex-row gap-3 items-end max-w-xl">
            <div className="flex-1 space-y-1.5 w-full">
              <Label htmlFor="test-recipient" className="text-xs font-bold text-muted-foreground">Recipient Email</Label>
              <Input id="test-recipient" type="email" placeholder="recipient@example.com"
                value={testEmail} onChange={e => setTestEmail(e.target.value)}
                className="bg-background/50 border-border focus-visible:ring-primary" />
            </div>
            <Button type="button" onClick={handleTest} disabled={testing || !cfg.host}
              className="w-full md:w-auto bg-primary hover:bg-primary/90 text-primary-foreground font-bold gap-1.5">
              {testing ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}
              Send Test Email
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

// ─── Maintenance Tab ──────────────────────────────────────────────────────────
function MaintenanceTab({ apiKey }: { apiKey: string }) {
  const [docs, setDocs] = useState<DocumentInfo[]>([])
  const [loadingDocs, setLoadingDocs] = useState(false)
  const [cleaning, setCleaning] = useState(false)
  const [cleanMsg, setCleanMsg] = useState<Feedback>(null)
  const [resumingId, setResumingId] = useState<string | null>(null)
  const [resumeMsg, setResumeMsg] = useState<Feedback>(null)

  const loadDocs = useCallback(async () => {
    setLoadingDocs(true)
    try { setDocs(await adminApi.listDocuments()) }
    catch { setDocs([]) }
    finally { setLoadingDocs(false) }
  }, [])

  useEffect(() => { loadDocs() }, [loadDocs])

  const counts = {
    queued: docs.filter(d => d.status === "queued").length,
    processing: docs.filter(d => d.status === "processing").length,
    ready: docs.filter(d => d.status === "ready").length,
    failed: docs.filter(d => d.status === "failed").length,
  }

  const handleClean = async () => {
    if (!apiKey) { setCleanMsg({ type: "error", text: "Admin API Key required." }); return }
    setCleaning(true); setCleanMsg(null)
    try {
      const res = await adminApi.cleanStaleProcesses(apiKey)
      setCleanMsg({ type: "success", text: res.message || "Stale processes cleaned." })
      await loadDocs()
    } catch (err: unknown) {
      setCleanMsg({ type: "error", text: err instanceof Error ? err.message : "Cleanup failed." })
    } finally { setCleaning(false) }
  }

  const handleResume = async (id: string) => {
    setResumingId(id); setResumeMsg(null)
    try {
      const res = await adminApi.resumeDocument(id)
      setResumeMsg({ type: "success", text: res.message || `Document ${id} resumed.` })
      await loadDocs()
    } catch (err: unknown) {
      setResumeMsg({ type: "error", text: err instanceof Error ? err.message : "Resume failed." })
    } finally { setResumingId(null) }
  }

  const statusBadge = (status: DocumentInfo["status"]) => {
    const map: Record<string, string> = {
      ready: "bg-green-500/15 text-green-500 border-green-500/30",
      processing: "bg-blue-500/15 text-blue-400 border-blue-500/30",
      queued: "bg-yellow-500/15 text-yellow-500 border-yellow-500/30",
      failed: "bg-destructive/15 text-destructive border-destructive/30",
    }
    return (
      <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-bold border ${map[status] || ""}`}>
        {status.toUpperCase()}
      </span>
    )
  }

  const actionDocs = docs.filter(d => d.status === "failed" || d.status === "processing" || d.status === "queued")

  return (
    <div className="space-y-5">
      {/* Stat cards */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard label="Queued" value={counts.queued} icon={Clock} color="text-yellow-500" />
        <StatCard label="Processing" value={counts.processing} icon={Activity} color="text-blue-400" />
        <StatCard label="Ready" value={counts.ready} icon={CheckCircle2} color="text-green-500" />
        <StatCard label="Failed" value={counts.failed} icon={AlertTriangle} color="text-destructive" />
      </div>

      {/* Clean stale */}
      <Card className="glass border-border">
        <CardHeader className="pb-3">
          <div className="flex items-center gap-2">
            <Wrench className="h-5 w-5 text-primary" />
            <CardTitle className="text-base font-bold">Stale Process Cleanup</CardTitle>
          </div>
          <CardDescription className="text-xs">
            Marks documents stuck in <strong>processing</strong> or <strong>queued</strong> state for more than 15 minutes as <strong>failed</strong>, freeing them for manual resumption.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <FeedbackBanner msg={cleanMsg} />
          <Button onClick={handleClean} disabled={cleaning || !apiKey}
            className="bg-primary hover:bg-primary/90 text-primary-foreground font-bold gap-1.5">
            {cleaning ? <Loader2 className="h-4 w-4 animate-spin" /> : <Wrench className="h-4 w-4" />}
            Clean Stale Processes
          </Button>
        </CardContent>
      </Card>

      {/* Document list */}
      <Card className="glass border-border">
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <FileText className="h-5 w-5 text-primary" />
              <CardTitle className="text-base font-bold">Document Library</CardTitle>
            </div>
            <Button variant="outline" size="sm" onClick={loadDocs} disabled={loadingDocs} className="text-xs gap-1.5">
              {loadingDocs ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
              Refresh
            </Button>
          </div>
          <CardDescription className="text-xs">
            Documents requiring attention (queued, processing, or failed). Ready documents are hidden.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <FeedbackBanner msg={resumeMsg} />
          {loadingDocs ? (
            <div className="flex items-center justify-center py-10 gap-3 text-muted-foreground text-sm">
              <Loader2 className="h-5 w-5 animate-spin" /> Loading documents…
            </div>
          ) : actionDocs.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-10 gap-2 text-muted-foreground">
              <CheckCircle2 className="h-8 w-8 text-green-500/60" />
              <p className="text-sm font-medium">All documents are healthy</p>
            </div>
          ) : (
            <div className="space-y-2 mt-2">
              {actionDocs.map(doc => (
                <div key={doc.id}
                  className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 p-3 rounded-lg border border-border/50 bg-muted/30 hover:bg-muted/50 transition-colors">
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="text-sm font-semibold text-foreground truncate">{doc.name}</span>
                      {statusBadge(doc.status)}
                    </div>
                    <p className="text-xs text-muted-foreground mt-0.5">
                      {doc.split_pages}/{doc.total_pages} pages split · {doc.parsed_pages} parsed ·
                      Updated {new Date(doc.updated_at).toLocaleString()}
                    </p>
                  </div>
                  {(doc.status === "failed" || doc.status === "processing") && (
                    <Button size="sm" variant="outline" onClick={() => handleResume(doc.id)}
                      disabled={resumingId === doc.id}
                      className="shrink-0 gap-1.5 text-xs border-primary/40 text-primary hover:bg-primary/10">
                      {resumingId === doc.id ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />}
                      Resume
                    </Button>
                  )}
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

// ─── Observability Tab ────────────────────────────────────────────────────────
function ObservabilityTab() {
  const metrics = [
    { icon: TrendingUp, title: "HTTP Request Rate", query: "sum(rate(http_requests_total[5m])) by (method, status, path)", desc: "Requests per second grouped by method, path, and HTTP status code." },
    { icon: Zap, title: "HTTP Latency (p95)", query: "histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket[5m])) by (le, path))", desc: "95th-percentile response latency per API endpoint." },
    { icon: FileText, title: "Document Upload Rate", query: "sum(rate(document_uploads_total[5m])) by (status)", desc: "PDF upload throughput split by success/failure status." },
    { icon: BookOpen, title: "Booklet Compilation (p90)", query: "histogram_quantile(0.90, sum(rate(booklet_compilation_duration_seconds_bucket[5m])) by (le))", desc: "90th-percentile PDF imposition compilation duration." },
    { icon: Eye, title: "Vector Search (p90)", query: "histogram_quantile(0.90, sum(rate(vector_search_duration_seconds_bucket[5m])) by (le))", desc: "90th-percentile semantic vector search query latency." },
  ]

  return (
    <div className="space-y-5">
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <a href="http://localhost:3002" target="_blank" rel="noopener noreferrer"
          className="block glass-interactive rounded-xl p-5 group">
          <div className="flex items-start gap-4">
            <div className="p-3 rounded-xl bg-primary/10 text-primary">
              <BarChart3 className="h-6 w-6" />
            </div>
            <div className="flex-1">
              <div className="flex items-center gap-2">
                <h3 className="font-bold text-foreground">Grafana Dashboard</h3>
                <ExternalLink className="h-3.5 w-3.5 text-muted-foreground group-hover:text-primary transition-colors" />
              </div>
              <p className="text-xs text-muted-foreground mt-1">Pre-provisioned SRE dashboard with time-series panels for all instrumented metrics. Running on port <strong>3002</strong>.</p>
              <p className="text-xs font-mono text-primary/80 mt-2">http://localhost:3002</p>
            </div>
          </div>
        </a>

        <a href="http://localhost:9090" target="_blank" rel="noopener noreferrer"
          className="block glass-interactive rounded-xl p-5 group">
          <div className="flex items-start gap-4">
            <div className="p-3 rounded-xl bg-accent/10 text-accent">
              <Database className="h-6 w-6" />
            </div>
            <div className="flex-1">
              <div className="flex items-center gap-2">
                <h3 className="font-bold text-foreground">Prometheus Console</h3>
                <ExternalLink className="h-3.5 w-3.5 text-muted-foreground group-hover:text-accent transition-colors" />
              </div>
              <p className="text-xs text-muted-foreground mt-1">Raw metric scraper console. Explore and run PromQL queries directly against the backend. Running on port <strong>9090</strong>.</p>
              <p className="text-xs font-mono text-accent/80 mt-2">http://localhost:9090</p>
            </div>
          </div>
        </a>
      </div>

      <Card className="glass border-border">
        <CardHeader className="pb-3">
          <div className="flex items-center gap-2">
            <Activity className="h-5 w-5 text-primary" />
            <CardTitle className="text-base font-bold">Instrumented Metrics</CardTitle>
          </div>
          <CardDescription className="text-xs">All metrics are exposed at <code className="font-mono bg-muted px-1 py-0.5 rounded">/metrics</code> and scraped by Prometheus every 15 seconds.</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-3">
            {metrics.map(({ icon: Icon, title, query, desc }) => (
              <div key={title} className="p-3 rounded-lg border border-border/50 bg-muted/20 space-y-1.5">
                <div className="flex items-center gap-2">
                  <Icon className="h-4 w-4 text-primary shrink-0" />
                  <span className="text-sm font-semibold text-foreground">{title}</span>
                </div>
                <p className="text-xs text-muted-foreground">{desc}</p>
                <code className="block text-[10px] font-mono bg-muted/60 text-muted-foreground px-2 py-1.5 rounded border border-border/40 break-all">{query}</code>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

// ─── Root Dashboard ───────────────────────────────────────────────────────────
export function AdminDashboard() {
  const [apiKey, setApiKey] = useState<string>(() => localStorage.getItem("booklet_admin_api_key") || "dev-admin-key")
  const [inputKey, setInputKey] = useState(apiKey)
  const [unlocked, setUnlocked] = useState(false)
  const [verifying, setVerifying] = useState(false)
  const [keyError, setKeyError] = useState<string | null>(null)
  const { theme, resolvedTheme, setTheme } = useTheme()

  const handleUnlock = async (e: React.FormEvent) => {
    e.preventDefault()
    setVerifying(true); setKeyError(null)
    try {
      await adminApi.getSMTPConfig(inputKey)
      setApiKey(inputKey)
      localStorage.setItem("booklet_admin_api_key", inputKey)
      setUnlocked(true)
    } catch {
      setKeyError("Invalid API key or backend unreachable. Check your ADMIN_API_KEY environment variable.")
    } finally { setVerifying(false) }
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
              <Button key={t} type="button" variant="ghost" size="sm" onClick={() => setTheme(t)}
                className={`rounded-full h-7 px-3 text-xs font-medium transition-all capitalize ${
                  theme === t
                    ? "bg-primary text-primary-foreground shadow hover:bg-primary/90"
                    : "text-muted-foreground hover:text-foreground hover:bg-accent/20"
                }`}>
                {t === "system" ? <Monitor className="h-3.5 w-3.5" /> : t === "light" ? <Sun className="h-3.5 w-3.5" /> : <Moon className="h-3.5 w-3.5" />}
              </Button>
            ))}
          </div>
          <Button variant="outline" size="icon" className="sm:hidden"
            onClick={() => setTheme(resolvedTheme === "dark" ? "light" : "dark")}>
            {resolvedTheme === "dark" ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
          </Button>

          {unlocked && (
            <Button variant="outline" size="sm" onClick={() => { setUnlocked(false); localStorage.removeItem("booklet_admin_api_key") }}
              className="text-xs text-muted-foreground hover:text-destructive hover:border-destructive/40">
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
                    <Input id="api-key-input" type="password" placeholder="Enter your admin key…"
                      value={inputKey} onChange={e => setInputKey(e.target.value)}
                      className="bg-background/50 border-border focus-visible:ring-primary" autoFocus />
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
