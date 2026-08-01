import { useState, useEffect, useCallback } from "react"
import { adminApi, type DocumentInfo } from "../api"
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "./ui/card"
import { Button } from "./ui/button"
import {
  Wrench, FileText, RefreshCw, Loader2, Play,
  CheckCircle2, Clock, Activity, AlertTriangle
} from "lucide-react"
import { FeedbackBanner, type Feedback } from "./FeedbackBanner"
import { StatCard } from "./StatCard"

interface MaintenanceTabProps {
  apiKey: string
}

export function MaintenanceTab({ apiKey }: MaintenanceTabProps) {
  const [docs, setDocs] = useState<DocumentInfo[]>([])
  const [loadingDocs, setLoadingDocs] = useState(false)
  const [cleaning, setCleaning] = useState(false)
  const [cleanMsg, setCleanMsg] = useState<Feedback>(null)
  const [resumingId, setResumingId] = useState<string | null>(null)
  const [resumeMsg, setResumeMsg] = useState<Feedback>(null)

  const fetchDocs = useCallback(async () => {
    try {
      setDocs(await adminApi.listDocuments())
    } catch {
      setDocs([])
    }
  }, [])

  const loadDocs = useCallback(async () => {
    setLoadingDocs(true)
    await fetchDocs()
    setLoadingDocs(false)
  }, [fetchDocs])

  useEffect(() => {
    loadDocs()
  }, [loadDocs])

  useEffect(() => {
    const timer = setInterval(() => {
      fetchDocs()
    }, 500)
    return () => clearInterval(timer)
  }, [fetchDocs])

  const counts = {
    queued: docs.filter(d => d.status === "queued").length,
    processing: docs.filter(d => d.status === "processing").length,
    ready: docs.filter(d => d.status === "ready").length,
    failed: docs.filter(d => d.status === "failed").length,
  }

  const handleClean = async () => {
    if (!apiKey) {
      setCleanMsg({ type: "error", text: "Admin API Key required." })
      return
    }
    setCleaning(true)
    setCleanMsg(null)
    try {
      const res = await adminApi.cleanStaleProcesses(apiKey)
      setCleanMsg({ type: "success", text: res.message || "Stale processes cleaned." })
      await loadDocs()
    } catch (err: unknown) {
      setCleanMsg({ type: "error", text: err instanceof Error ? err.message : "Cleanup failed." })
    } finally {
      setCleaning(false)
    }
  }

  const handleResume = async (id: string) => {
    setResumingId(id)
    setResumeMsg(null)
    try {
      const res = await adminApi.resumeDocument(id)
      setResumeMsg({ type: "success", text: res.message || `Document ${id} resumed.` })
      await loadDocs()
    } catch (err: unknown) {
      setResumeMsg({ type: "error", text: err instanceof Error ? err.message : "Resume failed." })
    } finally {
      setResumingId(null)
    }
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
          <Button
            onClick={handleClean}
            disabled={cleaning || !apiKey}
            className="bg-primary hover:bg-primary/90 text-primary-foreground font-bold gap-1.5"
          >
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
                <div
                  key={doc.id}
                  className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 p-3 rounded-lg border border-border/50 bg-muted/30 hover:bg-muted/50 transition-colors"
                >
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
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => handleResume(doc.id)}
                      disabled={resumingId === doc.id}
                      className="shrink-0 gap-1.5 text-xs border-primary/40 text-primary hover:bg-primary/10"
                    >
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
