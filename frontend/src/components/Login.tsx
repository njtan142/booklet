import React from "react"
import { useForm } from "react-hook-form"
import { api } from "../api"
import { Button } from "./ui/button"
import { Card } from "./ui/card"
import { Input } from "./ui/input"
import { Label } from "./ui/label"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "./ui/tabs"
import { Form, FormField, FormItem, FormControl } from "./ui/form"
import { BookOpen, Shield, Key } from "lucide-react"

// Custom SVGs/Components for premium feature iconography
const SignatureIcon: React.FC<React.SVGProps<SVGSVGElement>> = (props) => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" {...props}>
    <path d="M4 16c3-8 5-11 8-11 4 0 2 8 6 8 3 0 4-2 4-4s-3-2-5 1c-2 3-5 7-9 7-3 0-4-2-2-5 2-3 5-3 6-1" />
  </svg>
)

const StorageBucketIcon: React.FC<React.SVGProps<SVGSVGElement>> = (props) => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" {...props}>
    <ellipse cx="12" cy="5" rx="9" ry="3" />
    <path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5" />
    <path d="M3 12c0 1.66 4 3 9 3s9-1.34 9-3" />
  </svg>
)

const SearchVectorIcon: React.FC<React.SVGProps<SVGSVGElement>> = (props) => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" {...props}>
    <circle cx="11" cy="11" r="8" />
    <path d="m21 21-4.3-4.3" />
    <path d="M8 11h6" />
    <path d="M11 8v6" />
  </svg>
)

const ObservableGraphIcon: React.FC<React.SVGProps<SVGSVGElement>> = (props) => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" {...props}>
    <path d="M3 3v18h18" />
    <path d="m18.7 8-5.1 5.2-2.8-2.7L7 14.3" />
  </svg>
)

// OIDC Provider Logos (Custom SVGs matching authentic brand colors)
const KeycloakLogo: React.FC = () => (
  <svg viewBox="0 0 32 32" className="h-5 w-5" fill="none">
    <path d="M16 2L6 8v16l10 6 10-6V8L16 2z" fill="url(#keycloak-g1)" />
    <path d="M16 8a8 8 0 100 16 8 8 0 000-16z" fill="#1e1b4b" opacity="0.4" />
    <path d="M16 11c-2.76 0-5 2.24-5 5s2.24 5 5 5 5-2.24 5-5-2.24-5-5-5zm0 7c-1.1 0-2-.9-2-2s.9-2 2-2 2 .9 2 2-.9 2-2 2z" fill="#60a5fa" />
    <defs>
      <linearGradient id="keycloak-g1" x1="0" y1="0" x2="32" y2="32">
        <stop offset="0%" stopColor="#3b82f6" />
        <stop offset="100%" stopColor="#0ea5e9" />
      </linearGradient>
    </defs>
  </svg>
)

const AuthentikLogo: React.FC = () => (
  <svg viewBox="0 0 32 32" className="h-5 w-5" fill="none">
    <rect width="32" height="32" rx="8" fill="#1e293b" />
    <path d="M16 6L7 11v10l9 5 9-5V11L16 6zm0 3.3l6.3 3.5v7l-6.3 3.5-6.3-3.5v-7L16 9.3z" fill="#38bdf8" />
    <path d="M16 12.5a3.5 3.5 0 100 7 3.5 3.5 0 000-7z" fill="#0284c7" />
  </svg>
)

const OktaLogo: React.FC = () => (
  <svg viewBox="0 0 32 32" className="h-5 w-5" fill="none">
    <circle cx="16" cy="16" r="12" stroke="#007dc1" strokeWidth="4.5" />
    <circle cx="16" cy="16" r="5" fill="#007dc1" />
  </svg>
)

const ActiveDirectoryLogo: React.FC = () => (
  <svg viewBox="0 0 32 32" className="h-5 w-5" fill="none">
    <path d="M5 5h10v10H5z" fill="#f25022" />
    <path d="M17 5h10v10H17z" fill="#7fba00" />
    <path d="M5 17h10v10H5z" fill="#00a4ef" />
    <path d="M17 17h10v10H17z" fill="#ffb900" />
  </svg>
)

const GoogleLogo: React.FC = () => (
  <svg viewBox="0 0 24 24" className="h-5 w-5">
    <path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z" fill="#4285F4" />
    <path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" fill="#34A853" />
    <path d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.06H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.94l2.85-2.22.81-.63z" fill="#FBBC05" />
    <path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.06l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" fill="#EA4335" />
  </svg>
)

const GitLabLogo: React.FC = () => (
  <svg viewBox="0 0 32 32" className="h-5 w-5">
    <path d="m28.2 11.2-.2-.5-4.2-12.8c-.1-.3-.3-.4-.6-.4s-.5.1-.6.4l-3.2 9.7H12.6L9.4 7.9c-.1-.3-.3-.4-.6-.4s-.5.1-.6.4L4 21.1l-.2.5c-.5 1.6 0 3.3 1.4 4.3l10.3 7.5c.3.2.7.2 1 0l10.3-7.5c1.4-1 1.9-2.7 1.4-4.3z" fill="#e24329" />
  </svg>
)

export const Login: React.FC = () => {
  // Show the Developer Bypass tab when running via the Vite dev server,
  // OR when the bundle was built with VITE_DEV_BYPASS_ENABLED=true (Docker dev).
  const isDev = import.meta.env.DEV || import.meta.env.VITE_DEV_BYPASS_ENABLED === "true"

  const devForm = useForm({
    defaultValues: {
      email: "dev@example.com",
      name: "Developer User",
    },
  })

  const handleMockLogin = devForm.handleSubmit((values) => {
    if (values.email) {
      const loginUrl = `${api.devLoginUrl()}?email=${encodeURIComponent(values.email)}&name=${encodeURIComponent(values.name)}`
      window.location.href = loginUrl
    }
  })

  return (
    <div className="relative min-h-[92vh] flex items-center justify-center overflow-visible px-4 py-8">
      {/* Stars & Cosmic Lines SVG Background */}
      <div className="absolute inset-0 pointer-events-none overflow-hidden z-0">
        {/* Glow Spots */}
        <div className="absolute top-[10%] left-[20%] w-[350px] h-[350px] bg-primary/12 rounded-full blur-[100px] animate-glow-cosmic" />
        <div className="absolute bottom-[20%] right-[15%] w-[450px] h-[450px] bg-accent/12 rounded-full blur-[120px] animate-glow-cosmic" style={{ animationDelay: "2s" }} />

        {/* Network Constellations */}
        <svg className="absolute top-[5%] right-[5%] w-[450px] h-[400px] text-primary/10 opacity-60 hidden md:block" fill="none" stroke="currentColor" strokeWidth="1">
          <line x1="80" y1="120" x2="260" y2="180" stroke="currentColor" />
          <line x1="260" y1="180" x2="360" y2="60" stroke="currentColor" />
          <line x1="260" y1="180" x2="310" y2="350" stroke="currentColor" strokeDasharray="4 4" />
          <line x1="80" y1="120" x2="160" y2="300" stroke="currentColor" />
          <line x1="160" y1="300" x2="310" y2="350" stroke="currentColor" />
          <line x1="360" y1="60" x2="420" y2="150" stroke="currentColor" />
          
          <circle cx="80" cy="120" r="5" fill="#a78bfa" className="animate-pulse" />
          <circle cx="260" cy="180" r="7" fill="#8b5cf6" />
          <circle cx="360" cy="60" r="4" fill="#c084fc" />
          <circle cx="160" cy="300" r="5" fill="#6366f1" />
          <circle cx="310" cy="350" r="6" fill="#ec4899" />
          <circle cx="420" cy="150" r="4" fill="#a5b4fc" className="animate-ping" style={{ animationDuration: "3s" }} />
        </svg>

        <svg className="absolute bottom-[10%] left-[2%] w-[300px] h-[250px] text-accent/10 opacity-50 hidden md:block" fill="none" stroke="currentColor" strokeWidth="1">
          <line x1="50" y1="50" x2="180" y2="100" stroke="currentColor" />
          <line x1="180" y1="100" x2="220" y2="210" stroke="currentColor" />
          <line x1="50" y1="50" x2="100" y2="180" stroke="currentColor" />

          <circle cx="50" cy="50" r="4" fill="#818cf8" />
          <circle cx="180" cy="100" r="5" fill="#6366f1" />
          <circle cx="220" cy="210" r="4" fill="#c084fc" />
          <circle cx="100" cy="180" r="5" fill="#a78bfa" />
        </svg>

        {/* Floating 3D Crystals */}
        <div className="absolute top-[22%] left-[10%] animate-float opacity-75 hidden xl:block">
          <svg viewBox="0 0 100 120" className="w-16 h-20 text-primary/25" fill="none" stroke="currentColor" strokeWidth="1">
            <polygon points="50,10 15,50 50,110" fill="rgb(245 224 0 / 0.05)" />
            <polygon points="50,10 85,50 50,110" fill="rgb(254 94 65 / 0.08)" />
            <polygon points="15,50 85,50 50,110" fill="rgb(0 0 0 / 0.04)" />
            <line x1="50" y1="10" x2="50" y2="110" stroke="rgb(var(--foreground) / 0.35)" strokeWidth="1.2" />
            <line x1="15" y1="50" x2="85" y2="50" stroke="rgb(var(--foreground) / 0.15)" />
          </svg>
        </div>

        <div className="absolute bottom-[15%] right-[10%] animate-float-reverse opacity-70 hidden xl:block" style={{ animationDelay: "1.5s" }}>
          <svg viewBox="0 0 100 120" className="w-14 h-18 text-accent/20" fill="none" stroke="currentColor" strokeWidth="1">
            <polygon points="50,15 20,55 50,105" fill="rgb(245 224 0 / 0.05)" />
            <polygon points="50,15 80,55 50,105" fill="rgb(254 94 65 / 0.06)" />
            <line x1="50" y1="15" x2="50" y2="105" stroke="rgb(var(--foreground) / 0.25)" strokeWidth="1" />
          </svg>
        </div>

        <div className="absolute bottom-[5%] left-[25%] animate-float-slow opacity-60 hidden md:block">
          <svg viewBox="0 0 80 100" className="w-10 h-14 text-primary/20" fill="none" stroke="currentColor" strokeWidth="1">
            <polygon points="40,10 15,45 40,90" fill="rgb(245 224 0 / 0.05)" />
            <polygon points="40,10 65,45 40,90" fill="rgb(0 0 0 / 0.05)" />
            <line x1="40" y1="10" x2="40" y2="90" stroke="rgb(var(--foreground) / 0.2)" />
          </svg>
        </div>

        {/* Floating Folded Booklets */}
        <div className="absolute bottom-[18%] right-[22%] animate-float opacity-65 hidden lg:block" style={{ animationDelay: "0.5s" }}>
          <svg viewBox="0 0 100 100" className="w-24 h-24 text-accent/20" fill="none" stroke="currentColor" strokeWidth="1">
            <path d="M15,28 Q35,18 50,32 Q65,18 85,28 V78 Q65,68 50,82 Q35,68 15,78 Z" fill="rgb(9 99 63 / 0.06)" />
            <path d="M50,32 V82" stroke="rgb(var(--foreground) / 0.25)" strokeWidth="1.5" />
            <path d="M20,34 Q35,26 50,38 Q65,26 80,34" stroke="rgb(var(--foreground) / 0.15)" />
          </svg>
        </div>

        <div className="absolute bottom-[10%] left-[10%] animate-float-reverse opacity-70 hidden md:block" style={{ animationDelay: "2.5s" }}>
          <svg viewBox="0 0 100 100" className="w-20 h-20 text-primary/20" fill="none" stroke="currentColor" strokeWidth="1">
            <path d="M15,28 Q35,18 50,32 Q65,18 85,28 V78 Q65,68 50,82 Q35,68 15,78 Z" fill="rgb(55 100 48 / 0.06)" />
            <path d="M50,32 V82" stroke="rgb(var(--foreground) / 0.2)" />
          </svg>
        </div>

        {/* Perspective grid at the bottom */}
        <div className="perspective-grid-container">
          <div className="perspective-grid" />
        </div>
      </div>

      {/* Main Layout Container: Side cards + Centered Login modal */}
      <div className="relative w-full max-w-5xl flex items-center justify-center z-10 py-6">
        
        {/* LEFT TAB: Studio Tools (Offset slightly behind main modal) */}
        <Card className="absolute left-[3%] w-[250px] h-[500px] glass rounded-3xl p-6 hidden lg:flex flex-col items-center border border-border opacity-40 shadow-2xl z-0 -translate-x-[20%] rotate-[-3deg] scale-[0.92] select-none">
          <h3 className="text-muted-foreground text-sm font-bold tracking-wider uppercase mb-1">Studio Tools</h3>
          <div className="w-full h-[1px] bg-border my-4" />
          
          <div className="flex flex-col gap-6 w-full items-center mt-4">
            {/* Signature tool selected */}
            <Card className="w-14 h-14 rounded-2xl bg-primary/10 border border-primary/30 flex items-center justify-center text-primary shadow-inner">
              <SignatureIcon className="h-6 w-6" />
            </Card>

            <div className="w-14 h-14 rounded-2xl bg-background/60 border border-border flex items-center justify-center text-muted-foreground">
              <StorageBucketIcon className="h-6 w-6 text-muted-foreground" />
            </div>

            <div className="w-14 h-14 rounded-2xl bg-background/60 border border-border flex items-center justify-center text-muted-foreground">
              <SearchVectorIcon className="h-6 w-6 text-muted-foreground" />
            </div>

            <div className="w-14 h-14 rounded-2xl bg-background/60 border border-border flex items-center justify-center text-muted-foreground">
              <ObservableGraphIcon className="h-6 w-6 text-muted-foreground" />
            </div>
          </div>
        </Card>

        {/* RIGHT TAB: Templates (Offset slightly behind main modal) */}
        <Card className="absolute right-[3%] w-[250px] h-[500px] glass rounded-3xl p-6 hidden lg:flex flex-col items-center border border-border opacity-40 shadow-2xl z-0 translate-x-[20%] rotate-[3deg] scale-[0.92] select-none">
          <h3 className="text-muted-foreground text-sm font-bold tracking-wider uppercase mb-1">Templates</h3>
          <div className="w-full h-[1px] bg-border my-4" />
          
          {/* Visual placeholders representing pages and binding margins */}
          <div className="flex flex-col gap-4 w-full mt-4">
            <div className="h-16 rounded-xl bg-background/60 border border-border p-3 flex flex-col justify-between">
              <div className="w-10 h-1.5 bg-muted-foreground/50 rounded" />
              <div className="w-full h-2 bg-muted rounded" />
            </div>
            <div className="h-16 rounded-xl bg-background/60 border border-border p-3 flex flex-col justify-between">
              <div className="w-14 h-1.5 bg-muted-foreground/50 rounded" />
              <div className="w-full h-2 bg-muted rounded" />
            </div>
            <div className="h-16 rounded-xl bg-background/60 border border-border p-3 flex flex-col justify-between opacity-50">
              <div className="w-8 h-1.5 bg-muted-foreground/50 rounded" />
              <div className="w-full h-2 bg-muted rounded" />
            </div>
            <div className="h-16 rounded-xl bg-background/60 border border-border p-3 flex flex-col justify-between opacity-20">
              <div className="w-12 h-1.5 bg-muted-foreground/50 rounded" />
              <div className="w-full h-2 bg-muted rounded" />
            </div>
          </div>
        </Card>

        {/* CENTERED MODAL: The principal interactive Login Panel */}
        <Card className="glass w-full max-w-xl p-8 md:p-10 rounded-[32px] border border-border space-y-8 shadow-2xl relative z-10 mx-auto bg-background/75">
          
          {/* Logo Badge & Headings */}
          <div className="flex flex-col items-center text-center gap-4">
            <div className="relative group">
              {/* Purple glow ring around the logo */}
              <div className="absolute inset-0 bg-primary rounded-2xl blur-xl opacity-60 group-hover:opacity-90 transition-opacity" />
              <Card className="relative bg-gradient-to-tr from-primary to-accent p-4 rounded-2xl text-primary-foreground shadow-xl shadow-primary/20 border border-border flex items-center justify-center">
                <BookOpen className="h-9 w-9 text-primary-foreground" aria-hidden="true" />
              </Card>
            </div>
            <div>
              <h1 className="text-3xl font-extrabold text-foreground tracking-tight m-0 bg-gradient-to-r from-foreground via-foreground/80 to-muted-foreground bg-clip-text">
                Booklet Studio
              </h1>
              
              {/* Features List with customized icons and colors */}
              <div className="mt-6 flex flex-col items-start gap-3 max-w-xs mx-auto text-muted-foreground text-sm font-medium">
                <div className="flex items-center gap-3.5 hover:text-foreground transition-colors">
                  <div className="w-8 h-8 rounded-lg bg-primary/10 border border-primary/25 flex items-center justify-center text-primary shrink-0">
                    <SignatureIcon className="h-4 w-4" />
                  </div>
                  <span>Double-sided imposition</span>
                </div>

                <div className="flex items-center gap-3.5 hover:text-foreground transition-colors">
                  <div className="w-8 h-8 rounded-lg bg-accent/10 border border-accent/25 flex items-center justify-center text-accent shrink-0">
                    <StorageBucketIcon className="h-4 w-4" />
                  </div>
                  <span>Modular S3-compatible storage</span>
                </div>

                <div className="flex items-center gap-3.5 hover:text-foreground transition-colors">
                  <div className="w-8 h-8 rounded-lg bg-background/70 border border-border flex items-center justify-center text-foreground shrink-0">
                    <SearchVectorIcon className="h-4 w-4" />
                  </div>
                  <span>pg_vector searches</span>
                </div>

                <div className="flex items-center gap-3.5 hover:text-foreground transition-colors">
                  <div className="w-8 h-8 rounded-lg bg-background/70 border border-border flex items-center justify-center text-foreground shrink-0">
                    <ObservableGraphIcon className="h-4 w-4" />
                  </div>
                  <span>Prometheus observability</span>
                </div>
              </div>
            </div>
          </div>

          {/* Authentication Options Tab Section */}
          <Tabs defaultValue="oidc" className="w-full">
            
            {/* Custom styled tabs headers matching the pill style */}
            <TabsList className={`grid w-full ${isDev ? "grid-cols-2" : "grid-cols-1"} bg-muted/60 border border-border rounded-2xl p-1 h-auto mb-6`}>
              <TabsTrigger 
                value="oidc" 
                className="flex items-center gap-2 justify-center py-2.5 rounded-xl font-bold text-xs text-muted-foreground data-[state=active]:bg-background data-[state=active]:border-border data-[state=active]:text-foreground transition-all border border-transparent"
              >
                <Shield className="h-4 w-4" aria-hidden="true" />
                OIDC Login
              </TabsTrigger>
              {isDev && (
                <TabsTrigger 
                  value="mock" 
                  className="flex items-center gap-2 justify-center py-2.5 rounded-xl font-bold text-xs text-muted-foreground data-[state=active]:bg-background data-[state=active]:border-border data-[state=active]:text-foreground transition-all border border-transparent"
                >
                  <Key className="h-4 w-4" aria-hidden="true" />
                  Developer Bypass
                </TabsTrigger>
              )}
            </TabsList>

            {/* OIDC Authentication Tab */}
            <TabsContent value="oidc" className="space-y-6 pt-2 focus-visible:outline-none">
              <p className="text-muted-foreground text-xs text-center leading-relaxed max-w-sm mx-auto">
                Login securely using your enterprise OpenID Connect identity provider
              </p>
              
              {/* OIDC Provider Logos List */}
              <div className="flex flex-wrap items-center justify-center gap-3 py-2">
                {[
                  { component: <KeycloakLogo />, label: "Keycloak" },
                  { component: <AuthentikLogo />, label: "Authentik" },
                  { component: <OktaLogo />, label: "Okta" },
                  { component: <ActiveDirectoryLogo />, label: "Active Directory" },
                  { component: <GoogleLogo />, label: "Google" },
                  { component: <GitLabLogo />, label: "GitLab" }
                ].map((provider, i) => (
                  <Button
                    type="button"
                    key={i}
                    variant="ghost"
                    onClick={() => window.location.href = api.loginUrl()}
                    title={`Login with ${provider.label}`}
                    className="w-12 h-12 flex items-center justify-center rounded-xl bg-background/70 border border-border hover:bg-muted/70 hover:border-primary/30 hover:scale-105 active:scale-95 transition-all cursor-pointer shadow-lg p-0"
                  >
                    {provider.component}
                  </Button>
                ))}
                
                {/* Visual extra logo dots */}
                <Button 
                  type="button"
                  variant="ghost"
                  onClick={() => window.location.href = api.loginUrl()}
                  title="More providers"
                  className="w-12 h-12 flex items-center justify-center rounded-xl bg-background/70 border border-border hover:bg-muted/70 hover:border-primary/30 hover:scale-105 active:scale-95 transition-all text-muted-foreground hover:text-foreground font-bold p-0"
                >
                  •••
                </Button>
              </div>

              <Button 
                className="w-full py-6 rounded-2xl font-bold flex items-center justify-center gap-2 bg-gradient-to-r from-primary to-accent hover:from-primary/90 hover:to-accent/90 border border-border text-primary-foreground shadow-lg shadow-primary/15 hover:shadow-primary/25 transition-all"
                onClick={() => window.location.href = api.loginUrl()}
              >
                <Shield className="h-4.5 w-4.5" aria-hidden="true" />
                Authenticate with OIDC
              </Button>
            </TabsContent>

            {/* Developer Local Bypass Tab — only rendered in development mode */}
            {isDev && (
              <TabsContent value="mock" className="space-y-6 pt-2 focus-visible:outline-none">
                <p className="text-zinc-400 text-xs text-center leading-relaxed max-w-sm mx-auto">
                  Bypass authentication locally using a mock profile. This generates a valid JWT session key signed by the backend.
                </p>
                
                <Form {...devForm}>
                  <form onSubmit={handleMockLogin} className="space-y-4 pt-1">
                    <FormField
                      control={devForm.control}
                      name="email"
                      render={({ field }) => (
                        <FormItem className="space-y-1.5">
                          <Label htmlFor="mock-email" className="text-[10px] uppercase font-bold text-muted-foreground tracking-wider">Email Address</Label>
                          <FormControl>
                            <Input
                              id="mock-email"
                              type="email"
                              placeholder="e.g. dev@example.com"
                              required
                              className="bg-background/70 border-border focus:border-primary/50 focus:ring-primary/20 rounded-xl h-11 text-foreground"
                              {...field}
                            />
                          </FormControl>
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={devForm.control}
                      name="name"
                      render={({ field }) => (
                        <FormItem className="space-y-1.5">
                          <Label htmlFor="mock-name" className="text-[10px] uppercase font-bold text-muted-foreground tracking-wider">Display Name</Label>
                          <FormControl>
                            <Input
                              id="mock-name"
                              type="text"
                              placeholder="e.g. Developer User"
                              required
                              className="bg-background/70 border-border focus:border-primary/50 focus:ring-primary/20 rounded-xl h-11 text-foreground"
                              {...field}
                            />
                          </FormControl>
                        </FormItem>
                      )}
                    />

                    <Button
                      type="submit"
                      className="w-full py-6 rounded-2xl font-bold flex items-center justify-center gap-2 mt-4 bg-background border border-border hover:bg-muted/70 hover:border-primary/30 hover:text-foreground transition-all text-foreground"
                    >
                      <Key className="h-4.5 w-4.5" aria-hidden="true" />
                      Start Dev Session
                    </Button>
                  </form>
                </Form>
              </TabsContent>
            )}
          </Tabs>

        </Card>
      </div>
      
      {/* Footer copyright section at bottom */}
      <footer className="absolute bottom-6 left-0 right-0 text-center text-muted-foreground text-[11px] font-medium tracking-wide pointer-events-none select-none z-10">
        &copy; {new Date().getFullYear()} Booklet Studio - Imposition &amp; Management
      </footer>
    </div>
  )
}
