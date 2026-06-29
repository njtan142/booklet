import React, { useState, useEffect } from "react"
import { useForm } from "react-hook-form"
import { api } from "../api"
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from "./ui/card"
import { Button } from "./ui/button"
import { Input } from "./ui/input"
import { Label } from "./ui/label"
import { Select } from "./ui/select"
import { Form } from "./ui/form"
import { Mail, Shield, Server, CheckCircle2, AlertTriangle, Loader2, Save } from "lucide-react"

export const Settings: React.FC = () => {
  const form = useForm()

  // Admin API Key state (saved in localStorage for convenience)
  const [adminApiKey, setAdminApiKey] = useState<string>(() => {
    return localStorage.getItem("admin_api_key") || "dev-admin-key"
  })

  // SMTP configuration state
  const [host, setHost] = useState("")
  const [port, setPort] = useState(25)
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [encryption, setEncryption] = useState("none")
  const [fromEmail, setFromEmail] = useState("")
  const [fromName, setFromName] = useState("")

  // Test email state
  const [testEmail, setTestEmail] = useState("")

  // Status/feedback states
  const [loading, setLoading] = useState(false)
  const [testing, setTesting] = useState(false)
  const [message, setMessage] = useState<{ type: "success" | "error"; text: string } | null>(null)
  const [testMessage, setTestMessage] = useState<{ type: "success" | "error"; text: string } | null>(null)

  // Save key to localStorage
  const handleApiKeyChange = (val: string) => {
    setAdminApiKey(val)
    localStorage.setItem("admin_api_key", val)
  }

  // Load existing SMTP settings on load or when API key changes
  const loadSettings = async () => {
    if (!adminApiKey) return
    setLoading(true)
    setMessage(null)
    try {
      const data = await api.getSMTPConfig(adminApiKey)
      setHost(data.host || "")
      setPort(data.port || 25)
      setUsername(data.username || "")
      setPassword(data.password || "")
      setEncryption(data.encryption || "none")
      setFromEmail(data.from_email || "")
      setFromName(data.from_name || "")
    } catch (err: any) {
      console.error(err)
      setMessage({ type: "error", text: "Failed to load SMTP settings. Please verify your Admin API Key." })
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadSettings()
    // Pre-fill test email with current logged-in user if available
    api.getMe().then((status) => {
      if (status.authenticated && status.user?.email) {
        setTestEmail(status.user.email)
      }
    }).catch(() => {})
  }, [adminApiKey])

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoading(true)
    setMessage(null)
    try {
      const result = await api.saveSMTPConfig(adminApiKey, {
        host,
        port: Number(port),
        username,
        password,
        encryption,
        from_email: fromEmail,
        from_name: fromName,
      })
      setMessage({ type: "success", text: result.message || "SMTP configuration saved successfully." })
    } catch (err: any) {
      setMessage({ type: "error", text: err.message || "Failed to save configuration." })
    } finally {
      setLoading(false)
    }
  }

  const handleTest = async () => {
    if (!testEmail) {
      setTestMessage({ type: "error", text: "Please enter a recipient email address." })
      return
    }
    setTesting(true)
    setTestMessage(null)
    try {
      const result = await api.testSMTPConfig(
        adminApiKey,
        {
          host,
          port: Number(port),
          username,
          password,
          encryption,
          from_email: fromEmail,
          from_name: fromName,
        },
        testEmail
      )
      setTestMessage({ type: "success", text: result.message || "Test email sent successfully!" })
    } catch (err: any) {
      setTestMessage({ type: "error", text: err.message || "SMTP Connection test failed." })
    } finally {
      setTesting(false)
    }
  }

  return (
    <div className="space-y-6 max-w-4xl mx-auto">
      <div>
        <h2 className="text-2xl font-bold text-foreground m-0">Settings</h2>
        <p className="text-muted-foreground text-sm mt-1">Configure global application settings and SMTP mail parameters.</p>
      </div>

      <div className="grid grid-cols-1 gap-6">
        {/* Admin API Key Authentication */}
        <Card className="glass border-border">
          <CardHeader className="pb-4">
            <div className="flex items-center gap-2">
              <Shield className="h-5 w-5 text-primary" />
              <CardTitle className="text-base font-bold">Admin Authorization</CardTitle>
            </div>
            <CardDescription className="text-xs">
              Configure your administrative API key to retrieve and edit global system configurations.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="space-y-2">
              <Label htmlFor="admin-key" className="text-xs font-bold text-muted-foreground">Admin API Key</Label>
              <Input
                id="admin-key"
                type="password"
                placeholder="Enter ADMIN_API_KEY"
                value={adminApiKey}
                onChange={(e) => handleApiKeyChange(e.target.value)}
                className="bg-background/50 max-w-md border-border focus-visible:ring-primary"
              />
            </div>
          </CardContent>
        </Card>

        {/* SMTP settings */}
        <Card className="glass border-border">
          <CardHeader>
            <div className="flex items-center gap-2">
              <Server className="h-5 w-5 text-primary" />
              <CardTitle className="text-base font-bold">System-Wide SMTP Settings</CardTitle>
            </div>
            <CardDescription className="text-xs">
              Configure the global mail server used to send booklet PDFs and system alerts.
            </CardDescription>
          </CardHeader>
          
          <Form {...form}>
            <form onSubmit={handleSave}>
            <CardContent className="space-y-4">
              {message && (
                <div className={`flex items-center gap-3 p-3 rounded-lg border text-xs font-medium ${
                  message.type === "success" 
                    ? "bg-green-500/10 border-green-500/30 text-green-400" 
                    : "bg-destructive/10 border-destructive/30 text-destructive"
                }`}>
                  {message.type === "success" ? <CheckCircle2 className="h-4 w-4 shrink-0" /> : <AlertTriangle className="h-4 w-4 shrink-0" />}
                  <span>{message.text}</span>
                </div>
              )}

              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="smtp-host" className="text-xs font-bold text-muted-foreground">SMTP Server Host</Label>
                  <Input
                    id="smtp-host"
                    type="text"
                    placeholder="e.g. smtp.gmail.com"
                    value={host}
                    onChange={(e) => setHost(e.target.value)}
                    required
                    className="bg-background/50 border-border focus-visible:ring-primary"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="smtp-port" className="text-xs font-bold text-muted-foreground">SMTP Port</Label>
                  <Input
                    id="smtp-port"
                    type="number"
                    placeholder="e.g. 587"
                    value={port || ""}
                    onChange={(e) => setPort(Number(e.target.value))}
                    required
                    className="bg-background/50 border-border focus-visible:ring-primary"
                  />
                </div>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="smtp-username" className="text-xs font-bold text-muted-foreground">Username / Account</Label>
                  <Input
                    id="smtp-username"
                    type="text"
                    placeholder="SMTP server username"
                    value={username}
                    onChange={(e) => setUsername(e.target.value)}
                    className="bg-background/50 border-border focus-visible:ring-primary"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="smtp-password" className="text-xs font-bold text-muted-foreground">Password</Label>
                  <Input
                    id="smtp-password"
                    type="password"
                    placeholder={password ? "********" : "Enter password"}
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    className="bg-background/50 border-border focus-visible:ring-primary"
                  />
                </div>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="smtp-encryption" className="text-xs font-bold text-muted-foreground">Encryption Mode</Label>
                  <Select
                    id="smtp-encryption"
                    value={encryption}
                    onChange={(e) => setEncryption(e.target.value)}
                    className="bg-background/50 border-border focus-visible:ring-primary"
                  >
                    <option value="none">None (Plaintext/STARTTLS option)</option>
                    <option value="ssl">SSL / Implicit TLS (Port 465)</option>
                    <option value="starttls">STARTTLS / Explicit TLS (Port 587)</option>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="smtp-from-email" className="text-xs font-bold text-muted-foreground">From Email Address</Label>
                  <Input
                    id="smtp-from-email"
                    type="email"
                    placeholder="sender@example.com"
                    value={fromEmail}
                    onChange={(e) => setFromEmail(e.target.value)}
                    required
                    className="bg-background/50 border-border focus-visible:ring-primary"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="smtp-from-name" className="text-xs font-bold text-muted-foreground">From Display Name</Label>
                  <Input
                    id="smtp-from-name"
                    type="text"
                    placeholder="Booklet Studio"
                    value={fromName}
                    onChange={(e) => setFromName(e.target.value)}
                    className="bg-background/50 border-border focus-visible:ring-primary"
                  />
                </div>
              </div>
            </CardContent>
            
            <CardFooter className="flex items-center justify-between border-t border-border/40 pt-4">
              <Button 
                type="button" 
                variant="outline" 
                onClick={loadSettings}
                disabled={loading}
                className="text-xs"
              >
                Reload
              </Button>
              <Button 
                type="submit" 
                disabled={loading}
                className="bg-primary hover:bg-primary/90 text-primary-foreground font-bold shadow-md shadow-primary/20 flex items-center gap-1.5"
              >
                {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
                Save SMTP Settings
              </Button>
            </CardFooter>
          </form>
        </Form>
      </Card>

        {/* Connection Testing */}
        <Card className="glass border-border">
          <CardHeader>
            <div className="flex items-center gap-2">
              <Mail className="h-5 w-5 text-primary" />
              <CardTitle className="text-base font-bold">SMTP Connection Test</CardTitle>
            </div>
            <CardDescription className="text-xs">
              Send a test email to verify that Booklet Studio can communicate with your mail server successfully.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {testMessage && (
              <div className={`flex items-center gap-3 p-3 rounded-lg border text-xs font-medium ${
                testMessage.type === "success" 
                  ? "bg-green-500/10 border-green-500/30 text-green-400" 
                  : "bg-destructive/10 border-destructive/30 text-destructive"
              }`}>
                {testMessage.type === "success" ? <CheckCircle2 className="h-4 w-4 shrink-0" /> : <AlertTriangle className="h-4 w-4 shrink-0" />}
                <span>{testMessage.text}</span>
              </div>
            )}
            <div className="flex flex-col md:flex-row gap-4 items-end max-w-xl">
              <div className="flex-1 space-y-2 w-full">
                <Label htmlFor="test-recipient" className="text-xs font-bold text-muted-foreground">Recipient Email Address</Label>
                <Input
                  id="test-recipient"
                  type="email"
                  placeholder="recipient@example.com"
                  value={testEmail}
                  onChange={(e) => setTestEmail(e.target.value)}
                  className="bg-background/50 border-border focus-visible:ring-primary w-full"
                />
              </div>
              <Button
                type="button"
                onClick={handleTest}
                disabled={testing || !host}
                className="w-full md:w-auto bg-primary hover:bg-primary/90 text-primary-foreground font-bold shadow-md shadow-primary/20 flex items-center justify-center gap-1.5"
              >
                {testing ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
                Send Test Email
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
