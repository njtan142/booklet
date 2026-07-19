import React, { useState, useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import { api } from "../api"
import type { BookletListResponse } from "../api"
import { Button } from "./ui/button"
import { Input } from "./ui/input"
import { ScrollArea } from "./ui/scroll-area"
import { Card } from "./ui/card"
import {
  Printer,
  Download,
  Search,
  ArrowLeft,
  Loader2,
  AlertCircle,
  FileCheck,
  Calendar,
  ExternalLink,
  Sliders,
  Maximize2
} from "lucide-react"

export const PrintSessions: React.FC = () => {
  const navigate = useNavigate()
  const [searchQuery, setSearchQuery] = useState("")
  const [statusFilter, setStatusFilter] = useState<"all" | "ready" | "compiling" | "failed">("all")

  // Fetch all print sessions
  const { data: recentSessions, isLoading, refetch } = useQuery({
    queryKey: ["booklets"],
    queryFn: api.listBooklets,
    refetchInterval: (query) => {
      const hasProcessing = query.state.data?.some(b => b.status === "compiling")
      return hasProcessing ? 2000 : false
    }
  })

  const sessions = recentSessions || []

  // Filter sessions by search query and status filter
  const filteredSessions = useMemo(() => {
    return sessions.filter((session) => {
      const matchesSearch = session.document_name.toLowerCase().includes(searchQuery.toLowerCase())
      const matchesStatus = statusFilter === "all" || session.status === statusFilter
      return matchesSearch && matchesStatus
    })
  }, [sessions, searchQuery, statusFilter])

  const handleOpenInDashboard = (sessionId: string) => {
    navigate({
      to: "/",
      search: { session_id: sessionId } as any
    })
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
        <div>
          <div className="flex items-center gap-2 mb-2">
            <Button
              variant="outline"
              size="sm"
              className="h-8 px-2 flex items-center gap-1 text-xs"
              onClick={() => navigate({ to: "/" })}
            >
              <ArrowLeft className="h-3.5 w-3.5" />
              Back to Dashboard
            </Button>
          </div>
          <h1 className="text-2xl font-extrabold tracking-tight text-foreground m-0">
            Recent Print Sessions
          </h1>
          <p className="text-sm text-muted-foreground mt-1">
            Browse and manage all previously compiled booklets and printing wizard sessions.
          </p>
        </div>

        <Button
          variant="outline"
          size="sm"
          className="self-start md:self-auto h-9"
          onClick={() => refetch()}
        >
          Refresh Sessions
        </Button>
      </div>

      {/* Filters and Search */}
      <div className="glass p-4 rounded-xl border-border flex flex-col md:flex-row gap-4 items-center justify-between">
        <div className="relative w-full md:max-w-md">
          <Search className="absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            type="text"
            placeholder="Search by document name..."
            className="pl-9 h-9"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
          />
        </div>

        <div className="flex flex-wrap gap-2 w-full md:w-auto">
          {(["all", "ready", "compiling", "failed"] as const).map((filter) => (
            <Button
              key={filter}
              variant={statusFilter === filter ? "default" : "outline"}
              size="sm"
              className="h-8 capitalize text-xs shrink-0"
              onClick={() => setStatusFilter(filter)}
            >
              {filter}
            </Button>
          ))}
        </div>
      </div>

      {/* Main List */}
      {isLoading ? (
        <div className="flex flex-col items-center justify-center py-20 gap-3">
          <Loader2 className="h-10 w-10 animate-spin text-primary" />
          <p className="text-sm text-muted-foreground animate-pulse">Loading print sessions...</p>
        </div>
      ) : filteredSessions.length === 0 ? (
        <Card className="glass p-12 text-center rounded-2xl flex flex-col items-center justify-center">
          <Printer className="h-12 w-12 text-muted-foreground mb-4" />
          <h3 className="text-base font-bold text-foreground">No sessions found</h3>
          <p className="text-sm text-muted-foreground mt-1 max-w-sm">
            {searchQuery || statusFilter !== "all"
              ? "Try adjusting your search query or filters to find what you are looking for."
              : "Generate a booklet layout from the dashboard to start your first print session."}
          </p>
        </Card>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {filteredSessions.map((session) => (
            <Card
              key={session.id}
              className="glass p-5 rounded-2xl border-border flex flex-col justify-between hover:border-primary/30 transition-all group"
            >
              <div className="space-y-4">
                {/* Session Header */}
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0 flex-1">
                    <h3 className="text-sm font-bold text-foreground line-clamp-2 leading-snug group-hover:text-primary transition-colors" title={session.document_name}>
                      {session.document_name}
                    </h3>
                    <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground mt-1.5">
                      <Calendar className="h-3 w-3" />
                      <span>{new Date(session.created_at).toLocaleString()}</span>
                    </div>
                  </div>

                  <div className="shrink-0">
                    {session.status === "compiling" ? (
                      <span className="flex items-center gap-1 text-[10px] font-bold text-amber-500 bg-amber-500/10 px-2 py-0.5 rounded-full border border-amber-500/20">
                        <Loader2 className="h-2.5 w-2.5 animate-spin" />
                        Compiling
                      </span>
                    ) : session.status === "failed" ? (
                      <span className="flex items-center gap-1 text-[10px] font-bold text-destructive bg-destructive/10 px-2 py-0.5 rounded-full border border-destructive/20">
                        <AlertCircle className="h-2.5 w-2.5" />
                        Failed
                      </span>
                    ) : (
                      <span className="flex items-center gap-1 text-[10px] font-bold text-emerald-500 bg-emerald-500/10 px-2 py-0.5 rounded-full border border-emerald-500/20">
                        <FileCheck className="h-2.5 w-2.5" />
                        Ready
                      </span>
                    )}
                  </div>
                </div>

                {/* Configuration Specs */}
                <div className="bg-muted/40 border border-border/40 rounded-xl p-3.5 space-y-2 text-xs">
                  <div className="flex items-center gap-1.5 text-muted-foreground font-semibold uppercase text-[9px] tracking-wider mb-1">
                    <Sliders className="h-3 w-3" />
                    <span>Configuration Parameters</span>
                  </div>
                  <div className="grid grid-cols-2 gap-y-1.5 gap-x-4 text-foreground/90 font-medium">
                    <div className="flex justify-between border-b border-border/20 pb-1">
                      <span className="text-muted-foreground text-[10px]">Paper Size</span>
                      <span className="uppercase">{session.config_paper_size}</span>
                    </div>
                    <div className="flex justify-between border-b border-border/20 pb-1">
                      <span className="text-muted-foreground text-[10px]">Signature</span>
                      <span>{session.config_signature_size} pages</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-muted-foreground text-[10px]">Margins</span>
                      <span>{session.config_margin}pt</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-muted-foreground text-[10px]">Gutter</span>
                      <span>{session.config_gutter}pt</span>
                    </div>
                  </div>
                  <div className="flex justify-between items-center pt-1.5 border-t border-border/20 text-[10px]">
                    <span className="text-muted-foreground">Guides & Markers</span>
                    <span className={session.config_guides ? "text-emerald-500 font-bold" : "text-muted-foreground font-bold"}>
                      {session.config_guides ? "Enabled" : "Disabled"}
                    </span>
                  </div>
                </div>
              </div>

              {/* Actions Footer */}
              <div className="mt-5 pt-4 border-t border-border/30 flex flex-col gap-2">
                <Button
                  size="sm"
                  className="w-full text-xs font-bold flex items-center justify-center gap-1.5 h-8.5"
                  onClick={() => handleOpenInDashboard(session.id)}
                  disabled={session.status === "failed"}
                >
                  <ExternalLink className="h-3.5 w-3.5" />
                  Open in Dashboard
                </Button>

                {session.status === "ready" && (
                  <div className="grid grid-cols-2 gap-2 mt-1">
                    <a
                      href={api.getDownloadUrl(session.id, "fronts")}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex h-8 items-center justify-center rounded-md border border-input bg-background px-3 text-[11px] font-semibold text-foreground shadow-sm hover:bg-accent hover:text-accent-foreground transition-all gap-1"
                    >
                      <Download className="h-3 w-3" />
                      Fronts PDF
                    </a>
                    <a
                      href={api.getDownloadUrl(session.id, "backs")}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex h-8 items-center justify-center rounded-md border border-input bg-background px-3 text-[11px] font-semibold text-foreground shadow-sm hover:bg-accent hover:text-accent-foreground transition-all gap-1"
                    >
                      <Download className="h-3 w-3" />
                      Backs PDF
                    </a>
                  </div>
                )}
              </div>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}
