import React, { useState, useEffect, useCallback } from "react"
import { adminApi, type SMTPConfig } from "../api"
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from "./ui/card"
import { Button } from "./ui/button"
import { Input } from "./ui/input"
import { Label } from "./ui/label"
import { Select } from "./ui/select"
import { Form } from "./ui/form"
import { Server, Mail, Loader2, Save, Send, RefreshCw } from "lucide-react"
import { FeedbackBanner, type Feedback } from "./FeedbackBanner"

interface SMTPTabProps {
  apiKey: string
}

export function SMTPTab({ apiKey }: SMTPTabProps) {
  const [cfg, setCfg] = useState<SMTPConfig>({
    host: "",
    port: 587,
    username: "",
    password: "",
    encryption: "starttls",
    from_email: "",
    from_name: ""
  })
  const [testEmail, setTestEmail] = useState("")
  const [loading, setLoading] = useState(false)
  const [testing, setTesting] = useState(false)
  const [msg, setMsg] = useState<Feedback>(null)
  const [testMsg, setTestMsg] = useState<Feedback>(null)

  const load = useCallback(async () => {
    if (!apiKey) return
    setLoading(true)
    setMsg(null)
    try {
      const data = await adminApi.getSMTPConfig(apiKey)
      setCfg(data)
    } catch {
      setMsg({ type: "error", text: "Failed to load SMTP config. Check your Admin API Key." })
    } finally {
      setLoading(false)
    }
  }, [apiKey])

  useEffect(() => {
    load()
  }, [load])

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoading(true)
    setMsg(null)
    try {
      const res = await adminApi.saveSMTPConfig(apiKey, cfg)
      setMsg({ type: "success", text: res.message || "SMTP configuration saved." })
    } catch (err: unknown) {
      setMsg({ type: "error", text: err instanceof Error ? err.message : "Save failed." })
    } finally {
      setLoading(false)
    }
  }

  const handleTest = async () => {
    if (!testEmail) {
      setTestMsg({ type: "error", text: "Enter a recipient email first." })
      return
    }
    setTesting(true)
    setTestMsg(null)
    try {
      const res = await adminApi.testSMTPConfig(apiKey, cfg, testEmail)
      setTestMsg({ type: "success", text: res.message || "Test email sent!" })
    } catch (err: unknown) {
      setTestMsg({ type: "error", text: err instanceof Error ? err.message : "Test failed." })
    } finally {
      setTesting(false)
    }
  }

  const field = (
    id: string,
    label: string,
    value: string | number,
    onChange: (v: string) => void,
    opts?: { type?: string; placeholder?: string; required?: boolean }
  ) => (
    <div className="space-y-1.5">
      <Label htmlFor={id} className="text-xs font-bold text-muted-foreground">{label}</Label>
      <Input
        id={id}
        type={opts?.type || "text"}
        placeholder={opts?.placeholder}
        value={value}
        onChange={e => onChange(e.target.value)}
        required={opts?.required}
        className="bg-background/50 border-border focus-visible:ring-primary"
      />
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
                  <Select
                    id="smtp-enc"
                    value={cfg.encryption}
                    onChange={e => setCfg(c => ({ ...c, encryption: e.target.value }))}
                    className="bg-background/50 border-border focus-visible:ring-primary"
                  >
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
              <Input
                id="test-recipient"
                type="email"
                placeholder="recipient@example.com"
                value={testEmail}
                onChange={e => setTestEmail(e.target.value)}
                className="bg-background/50 border-border focus-visible:ring-primary"
              />
            </div>
            <Button
              type="button"
              onClick={handleTest}
              disabled={testing || !cfg.host}
              className="w-full md:w-auto bg-primary hover:bg-primary/90 text-primary-foreground font-bold gap-1.5"
            >
              {testing ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}
              Send Test Email
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
