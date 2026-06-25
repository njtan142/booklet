import React, { useState } from "react"
import { api } from "../api"
import { Button } from "./ui/button"
import { Input } from "./ui/input"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "./ui/tabs"
import { BookOpen, Shield, Key } from "lucide-react"

export const Login: React.FC = () => {
  const [email, setEmail] = useState<string>("dev@example.com")
  const [name, setName] = useState<string>("Developer User")

  const handleMockLogin = (e: React.FormEvent) => {
    e.preventDefault()
    if (email) {
      const loginUrl = `${api.loginUrl()}?email=${encodeURIComponent(email)}&name=${encodeURIComponent(name)}`
      window.location.href = loginUrl
    }
  }

  return (
    <div className="min-h-[80svh] flex items-center justify-center">
      <div className="glass w-full max-w-md p-8 rounded-3xl border-zinc-800 space-y-6 shadow-2xl shadow-primary/10">
        <div className="flex flex-col items-center text-center gap-3">
          <div className="bg-primary p-3 rounded-2xl text-white shadow-xl shadow-primary/20 animate-bounce">
            <BookOpen className="h-8 w-8" />
          </div>
          <div>
            <h2 className="text-2xl font-extrabold text-white tracking-tight m-0">Booklet Studio</h2>
            <p className="text-zinc-500 text-xs mt-1.5 max-w-xs">Double-sided imposition, modular S3-compatible storage, pg_vector searches, and Prometheus observability.</p>
          </div>
        </div>

        <Tabs defaultValue="oidc" className="w-full">
          <TabsList className="grid w-full grid-cols-2">
            <TabsTrigger value="oidc" className="flex items-center gap-1.5 justify-center py-2.5">
              <Shield className="h-4 w-4" />
              OIDC Login
            </TabsTrigger>
            <TabsTrigger value="mock" className="flex items-center gap-1.5 justify-center py-2.5">
              <Key className="h-4 w-4" />
              Developer Bypass
            </TabsTrigger>
          </TabsList>

          <TabsContent value="oidc" className="space-y-4 pt-4">
            <p className="text-zinc-400 text-xs text-center leading-relaxed">
              Login securely using your enterprise OpenID Connect identity provider (Keycloak, Authentik, Okta, etc.).
            </p>
            <Button 
              className="w-full py-6 font-bold flex items-center justify-center gap-2 mt-2"
              onClick={() => window.location.href = api.loginUrl()}
            >
              <Shield className="h-4 w-4" />
              Authenticate with OIDC
            </Button>
          </TabsContent>

          <TabsContent value="mock" className="space-y-4 pt-4">
            <p className="text-zinc-400 text-xs text-center leading-relaxed">
              Bypass authentication locally using a mock profile. This generates a valid JWT session key signed by the backend.
            </p>
            
            <form onSubmit={handleMockLogin} className="space-y-3.5 pt-2">
              <div className="space-y-1">
                <label className="text-[10px] uppercase font-bold text-zinc-500 tracking-wider">Email Address</label>
                <Input 
                  type="email" 
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder="e.g. dev@example.com"
                  required
                />
              </div>

              <div className="space-y-1">
                <label className="text-[10px] uppercase font-bold text-zinc-500 tracking-wider">Display Name</label>
                <Input 
                  type="text" 
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="e.g. Developer User"
                  required
                />
              </div>

              <Button type="submit" variant="secondary" className="w-full py-6 font-bold flex items-center justify-center gap-2 mt-2 bg-zinc-900 border border-zinc-800 hover:bg-zinc-850 hover:border-zinc-700">
                <Key className="h-4 w-4" />
                Spawn Mock Session
              </Button>
            </form>
          </TabsContent>
        </Tabs>
      </div>
    </div>
  )
}
