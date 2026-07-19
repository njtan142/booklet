import React, { useState, useEffect, useMemo } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "../api"
import type { DocumentInfo, DocumentDetail, BookletListResponse } from "../api"
import { Button } from "./ui/button"
import { Input } from "./ui/input"
import { Select } from "./ui/select"
import { Label } from "./ui/label"
import { ScrollArea } from "./ui/scroll-area"
import { Card } from "./ui/card"
import { Slider } from "./ui/slider"
import { Checkbox } from "./ui/checkbox"
import { PrintHelper } from "./PrintHelper"
import { PDFPageRenderer } from "./PDFPageRenderer"
import {
  UploadCloud,
  FileText,
  Settings,
  Loader2,
  Printer,
  Download,
  AlertCircle,
  FileCheck,
  X,
  Eye,
  Search
} from "lucide-react"

type PendingUpload = {
  documentId: string
  fileName: string
  startedAt: number
}

type FailedUpload = {
  id: string
  documentId?: string
  fileName: string
  message: string
}

const UPLOAD_FAILURE_TIMEOUT_MS = 60000

export const Dashboard: React.FC = () => {
  const queryClient = useQueryClient()
  const [selectedDocId, setSelectedDocId] = useState<string | null>(null)
  const [activeBookletId, setActiveBookletId] = useState<string | null>(null)

  // Custom booklet parameters
  const [margin, setMargin] = useState<number>(12)
  const [gutter, setGutter] = useState<number>(24)
  const [paperSize, setPaperSize] = useState<string>("a4")
  const [signatureSize, setSignatureSize] = useState<number>(4)
  const [guides, setGuides] = useState<boolean>(true)
  const [dashboardPreviewSide, setDashboardPreviewSide] = useState<"front" | "back">("front")

  const [compiling, setCompiling] = useState<boolean>(false)
  const [compileStatus, setCompileStatus] = useState<string>("")
  const [pollingBookletId, setPollingBookletId] = useState<string | null>(null)
  const [pendingUploads, setPendingUploads] = useState<PendingUpload[]>([])
  const [failedUploads, setFailedUploads] = useState<FailedUpload[]>([])
  const [searchQuery, setSearchQuery] = useState<string>("")

  // 1. Fetch document list
  const { data: rawDocuments, isLoading: loadingDocs, refetch: refetchDocs } = useQuery({
    queryKey: ["documents"],
    queryFn: api.listDocuments,
    refetchInterval: (query) => {
      // Poll if any document is processing or queued
      const hasProcessing = query.state.data?.some(d => d.status === "processing" || d.status === "queued")
      return hasProcessing ? 2000 : false
    }
  })
  const documents = rawDocuments || []

  const filteredDocuments = useMemo(() => {
    if (!searchQuery.trim()) return documents
    const query = searchQuery.toLowerCase()
    return documents.filter((doc) => doc.name.toLowerCase().includes(query))
  }, [documents, searchQuery])

  // 2. Fetch selected document details
  const { data: docDetail, isLoading: loadingDocDetail } = useQuery({
    queryKey: ["document", selectedDocId],
    queryFn: () => api.getDocument(selectedDocId!),
    enabled: !!selectedDocId,
    refetchInterval: (query) => {
      // Poll if this document is processing or queued
      const status = query.state.data?.status
      return (status === "processing" || status === "queued") ? 2000 : false
    }
  })

  // 3. Fetch Recent Print Sessions
  const { data: recentSessions, isLoading: loadingSessions } = useQuery({
    queryKey: ["booklets"],
    queryFn: api.listBooklets,
    refetchInterval: (query) => {
      const hasProcessing = query.state.data?.some(b => b.status === "compiling")
      return hasProcessing ? 2000 : false
    }
  })

  const handleSelectSession = (session: BookletListResponse) => {
    setSelectedDocId(session.document_id)
    setMargin(session.config_margin)
    setGutter(session.config_gutter)
    setPaperSize(session.config_paper_size)
    setSignatureSize(session.config_signature_size)
    setGuides(session.config_guides)
    setActiveBookletId(session.id)
    
    if (session.status === "compiling") {
      setPollingBookletId(session.id)
      setCompiling(true)
      setCompileStatus("Arranging pages & generating canvas...")
    } else {
      setPollingBookletId(null)
      setCompiling(false)
      setCompileStatus("")
    }
  }

  // 4. Upload State and Helpers
  const [inFlightUploads, setInFlightUploads] = useState<{ id: string; fileName: string }[]>([])

  const handleUploadFiles = async (files: FileList) => {
    const fileArray = Array.from(files).filter(f => f.name.toLowerCase().endsWith(".pdf"))
    if (fileArray.length === 0) return

    const newInFlight = fileArray.map(file => ({
      id: `inflight-${Date.now()}-${file.name}-${Math.random().toString(36).substring(2, 9)}`,
      fileName: file.name,
      file,
    }))

    setInFlightUploads((current) => [...current, ...newInFlight.map(item => ({ id: item.id, fileName: item.fileName }))])

    newInFlight.forEach(async (item) => {
      try {
        const data = await api.uploadDocument(item.file)
        setInFlightUploads((current) => current.filter(x => x.id !== item.id))
        setPendingUploads((current) => [
          ...current,
          {
            documentId: data.document_id,
            fileName: item.fileName,
            startedAt: Date.now(),
          },
        ])
        queryClient.invalidateQueries({ queryKey: ["documents"] })
      } catch (err) {
        const message = err instanceof Error ? err.message : "Upload failed"
        setInFlightUploads((current) => current.filter(x => x.id !== item.id))
        setFailedUploads((current) => [
          ...current,
          {
            id: `request-${Date.now()}-${item.fileName}`,
            fileName: item.fileName,
            message,
          },
        ])
      }
    })
  }

  useEffect(() => {
    if (pendingUploads.length === 0) return

    const now = Date.now()
    const resolvedFailures: FailedUpload[] = []

    const nextPendingUploads = pendingUploads.filter((pending) => {
      const document = documents.find((item) => item.id === pending.documentId)

      if (!document) {
        // If the document has not yet been registered in the system and has exceeded the timeout, mark it as failed
        if (now - pending.startedAt > UPLOAD_FAILURE_TIMEOUT_MS) {
          resolvedFailures.push({
            id: `timeout-${pending.documentId}`,
            documentId: pending.documentId,
            fileName: pending.fileName,
            message: "Upload failed to register on the server.",
          })
          return false
        }
        return true
      }

      if (document.status === "ready") {
        return false
      }

      if (document.status === "failed") {
        resolvedFailures.push({
          id: `doc-${pending.documentId}`,
          documentId: pending.documentId,
          fileName: pending.fileName,
          message: "Upload failed while the backend was processing the PDF.",
        })
        return false
      }

      if (document.status === "queued") {
        // Queued timeout is 15 minutes to allow for large queues
        const start = new Date(document.updated_at || document.created_at).getTime()
        if (now - start > 15 * 60 * 1000) {
          resolvedFailures.push({
            id: `timeout-${pending.documentId}`,
            documentId: pending.documentId,
            fileName: pending.fileName,
            message: "Upload stalled in queue. The backend may have crashed or is overloaded.",
          })
          return false
        }
        return true
      }

      if (document.status === "processing") {
        // Processing timeout is UPLOAD_FAILURE_TIMEOUT_MS (60s) of inactivity (no update to updated_at)
        const lastActive = new Date(document.updated_at || document.created_at).getTime()
        if (now - lastActive > UPLOAD_FAILURE_TIMEOUT_MS) {
          resolvedFailures.push({
            id: `timeout-${pending.documentId}`,
            documentId: pending.documentId,
            fileName: pending.fileName,
            message: "Upload stalled while processing. The backend may have crashed.",
          })
          return false
        }
        return true
      }

      if (now - pending.startedAt > UPLOAD_FAILURE_TIMEOUT_MS) {
        resolvedFailures.push({
          id: `timeout-${pending.documentId}`,
          documentId: pending.documentId,
          fileName: pending.fileName,
          message: "Upload stalled while processing. The backend may have crashed.",
        })
        return false
      }

      return true
    })

    if (resolvedFailures.length > 0) {
      setFailedUploads((current) => {
        const next = [...current]
        for (const failure of resolvedFailures) {
          if (!next.some((item) => item.id === failure.id)) {
            next.push(failure)
          }
        }
        return next
      })
    }

    if (nextPendingUploads.length !== pendingUploads.length) {
      setPendingUploads(nextPendingUploads)
    }
  }, [documents, pendingUploads])

  const dismissFailedUpload = async (id: string) => {
    setFailedUploads((current) => current.filter((item) => item.id !== id))

    const uuidMatch = id.match(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i)
    if (uuidMatch) {
      const docId = uuidMatch[0]
      try {
        await api.dismissDocument(docId)
        queryClient.invalidateQueries({ queryKey: ["documents"] })
      } catch (err) {
        console.error("Failed to dismiss document:", err)
      }
    }
  }

  // Handle file drop/selection
  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files
    if (files && files.length > 0) {
      handleUploadFiles(files)
    }
  }

  // Document Resume Mutation
  const resumeMutation = useMutation({
    mutationFn: (docId: string) => api.resumeDocument(docId),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ["documents"] })
      setPendingUploads((current) => {
        if (current.some((x) => x.documentId === data.document_id)) {
          return current
        }
        return [
          ...current,
          {
            id: `resume-${data.document_id}`,
            documentId: data.document_id,
            fileName: "Resumed PDF Document",
            startedAt: Date.now(),
          },
        ]
      })
    },
    onError: (err: any) => {
      alert(`Failed to resume document processing: ${err.message}`)
    }
  })

  // 4. Booklet Compile Mutation
  const compileMutation = useMutation({
    mutationFn: (docId: string) => api.compileBooklet(docId, {
      margin,
      gutter,
      paper_size: paperSize,
      signature_size: signatureSize,
      guides,
    }),
    onSuccess: (data) => {
      setPollingBookletId(data.booklet_id)
      setCompiling(true)
      setCompileStatus("Arranging pages & generating canvas...")
      
      // Asynchronously trigger cleanup of old booklet sessions with this configuration
      api.cleanupBookletSessions(selectedDocId!, {
        margin,
        gutter,
        paper_size: paperSize,
        signature_size: signatureSize,
        guides,
        current_booklet_id: data.booklet_id,
      }).catch(err => console.warn("Failed to clean up old booklet sessions:", err))

      queryClient.invalidateQueries({ queryKey: ["booklets"] })
    },
    onError: (err: any) => {
      setCompileStatus(`Compilation failed: ${err.message}`)
      setCompiling(false)
      queryClient.invalidateQueries({ queryKey: ["booklets"] })
    }
  })

  // Poll booklet status
  useEffect(() => {
    if (!pollingBookletId) return

    const interval = setInterval(async () => {
      try {
        const booklet = await api.getBooklet(pollingBookletId)
        if (booklet.status === "ready") {
          clearInterval(interval)
          setCompiling(false)
          setPollingBookletId(null)
          setActiveBookletId(pollingBookletId)
          setCompileStatus("")
          queryClient.invalidateQueries({ queryKey: ["booklets"] })
        } else if (booklet.status === "failed") {
          clearInterval(interval)
          setCompiling(false)
          setPollingBookletId(null)
          setCompileStatus("Booklet generation failed on backend.")
          queryClient.invalidateQueries({ queryKey: ["booklets"] })
        }
      } catch (err) {
        clearInterval(interval)
        setCompiling(false)
        setPollingBookletId(null)
        setCompileStatus("Error polling booklet compile status.")
        queryClient.invalidateQueries({ queryKey: ["booklets"] })
      }
    }, 2000)

    return () => clearInterval(interval)
  }, [pollingBookletId, queryClient])

  // Reset active booklet mode
  if (activeBookletId && docDetail) {
    return (
      <PrintHelper
        bookletId={activeBookletId}
        documentId={selectedDocId!}
        totalPages={docDetail.total_pages}
        signatureSize={signatureSize}
        pages={docDetail.pages}
        onBack={() => {
          setActiveBookletId(null)
          setCompileStatus("")
          queryClient.invalidateQueries({ queryKey: ["booklets"] })
        }}
      />
    )
  }

  return (
    <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
      {/* Left panel: Upload & List Documents */}
      <div className="lg:col-span-1 space-y-6">
        <div className="glass p-6 rounded-2xl border-border space-y-4">
          <h3 className="text-lg font-bold text-foreground m-0">Upload Document</h3>

          <div className="relative border-2 border-dashed border-border rounded-xl p-8 flex flex-col items-center justify-center gap-2 hover:border-primary/50 transition-all bg-background/40 group">
            <UploadCloud className="h-10 w-10 text-muted-foreground group-hover:text-primary transition-colors" aria-hidden="true" />
            <span className="text-muted-foreground text-xs font-medium">Drag & drop your PDF file(s) or click to browse</span>
            <Input
              id="pdf-file-upload"
              type="file"
              accept=".pdf"
              multiple={true}
              className="absolute inset-0 w-full h-full opacity-0 cursor-pointer"
              onChange={handleFileChange}
              aria-label="Upload PDF documents"
            />
          </div>

          {inFlightUploads.length > 0 && (
            <div className="space-y-2">
              {inFlightUploads.map((upload) => (
                <div key={upload.id} className="flex items-center gap-2 text-xs text-muted-foreground bg-muted/60 p-3 rounded-lg border border-border">
                  <Loader2 className="h-3.5 w-3.5 animate-spin text-primary" aria-hidden="true" />
                  <span className="truncate">Uploading {upload.fileName}...</span>
                </div>
              ))}
            </div>
          )}

          {failedUploads.length > 0 && (
            <div className="space-y-2">
              {failedUploads.map((failure) => (
                <div
                  key={failure.id}
                  className="flex items-start justify-between gap-3 rounded-xl border border-destructive/25 bg-destructive/10 p-3 text-xs text-destructive"
                >
                  <div className="min-w-0 space-y-0.5">
                    <p className="font-semibold truncate m-0">{failure.fileName}</p>
                    <p className="text-destructive/80 leading-relaxed m-0">{failure.message}</p>
                  </div>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    className="shrink-0 h-7 w-7 text-destructive hover:bg-destructive/15 hover:text-destructive"
                    onClick={() => dismissFailedUpload(failure.id)}
                    aria-label={`Dismiss failed upload for ${failure.fileName}`}
                  >
                    <X className="h-4 w-4" aria-hidden="true" />
                  </Button>
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="glass p-6 rounded-2xl border-border space-y-4">
          <h3 className="text-lg font-bold text-foreground m-0">Library</h3>

          {loadingDocs ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 className="h-6 w-6 animate-spin text-primary" aria-hidden="true" />
            </div>
          ) : documents.length === 0 ? (
            <p className="text-muted-foreground text-xs text-center py-6">No documents uploaded yet.</p>
          ) : (
            <>
              <div className="relative">
                <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                <Input
                  type="text"
                  placeholder="Search documents..."
                  className="pl-8"
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                />
              </div>

              {filteredDocuments.length === 0 ? (
                <p className="text-muted-foreground text-xs text-center py-6">No matching documents found.</p>
              ) : (
                <ScrollArea className="max-h-[400px]">
                  <div className="space-y-2.5 pr-4">
                    {filteredDocuments.map((doc) => {
                      const isSelected = selectedDocId === doc.id
                      const failedUpload = failedUploads.find((item) => item.documentId === doc.id)
                      const effectiveStatus = failedUpload ? "failed" : doc.status

                      if (effectiveStatus === "failed") {
                        return (
                          <div
                            key={doc.id}
                            className="w-full text-left h-auto p-3.5 rounded-xl border flex items-center justify-between gap-4 bg-destructive/10 border-destructive/25"
                          >
                            <div className="flex items-center gap-3 min-w-0">
                              <Card className="p-2 rounded-lg bg-destructive/15 text-destructive border-none shadow-none">
                                <FileText className="h-4 w-4" aria-hidden="true" />
                              </Card>
                              <div className="min-w-0">
                                <h4 className="text-xs font-bold text-foreground truncate m-0">{doc.name}</h4>
                                <p className="text-[10px] text-destructive/80 mt-0.5">
                                  Upload failed{failedUpload ? `: ${failedUpload.message}` : "."}
                                </p>
                              </div>
                            </div>

                            <div className="flex items-center gap-1.5 shrink-0">
                              <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                className="h-8 text-[11px]"
                                onClick={() => resumeMutation.mutate(doc.id)}
                              >
                                Resume
                              </Button>
                              <Button
                                type="button"
                                variant="ghost"
                                size="sm"
                                className="h-8 text-[11px] text-destructive hover:text-destructive hover:bg-destructive/15"
                                onClick={() => dismissFailedUpload(failedUpload?.id ?? `doc-${doc.id}`)}
                              >
                                Dismiss
                              </Button>
                            </div>
                          </div>
                        )
                      }

                      return (
                        <Button
                          type="button"
                          key={doc.id}
                          variant="ghost"
                          onClick={() => doc.status === "ready" && setSelectedDocId(doc.id)}
                          disabled={doc.status !== "ready"}
                          className={`w-full text-left h-auto p-3.5 rounded-xl border flex items-center justify-between gap-4 cursor-pointer transition-all whitespace-normal ${isSelected
                              ? "bg-primary/10 border-primary/30"
                              : (effectiveStatus === "processing" || effectiveStatus === "queued")
                                ? "bg-muted/30 border-border opacity-60 cursor-not-allowed"
                                : "bg-background/60 border-border hover:border-primary/25"
                            }`}
                        >
                          <div className="flex items-center gap-3 min-w-0">
                            <div className={`p-2 rounded-lg ${isSelected ? "bg-primary/20 text-primary" : "bg-muted text-muted-foreground"}`}>
                              <FileText className="h-4 w-4" aria-hidden="true" />
                            </div>
                            <div className="min-w-0">
                              <h4 className="text-xs font-bold text-foreground truncate m-0">{doc.name}</h4>
                              <p className="text-[10px] text-muted-foreground mt-0.5">
                                {doc.status === "queued" 
                                  ? "Queued..." 
                                  : doc.status === "processing" 
                                    ? doc.split_pages < doc.total_pages
                                      ? `Splitting (${doc.split_pages}/${doc.total_pages} pages)...`
                                      : `Parsing (${doc.parsed_pages}/${doc.total_pages} pages)...` 
                                    : `${doc.total_pages} pages`}
                              </p>
                            </div>
                          </div>

                          <div>
                            {(effectiveStatus === "processing" || effectiveStatus === "queued") ? (
                              <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" aria-hidden="true" />
                            ) : effectiveStatus === "failed" ? (
                              <AlertCircle className="h-4 w-4 text-destructive" aria-hidden="true" />
                            ) : (
                              <FileCheck className="h-4 w-4 text-emerald-500" aria-hidden="true" />
                            )}
                          </div>
                        </Button>
                      )
                    })}
                  </div>
                </ScrollArea>
              )}
            </>
          )}
        </div>

        <div className="glass p-6 rounded-2xl border-border space-y-4">
          <h3 className="text-lg font-bold text-foreground m-0">Recent Print Sessions</h3>
          {loadingSessions ? (
            <div className="flex items-center justify-center py-6">
              <Loader2 className="h-5 w-5 animate-spin text-primary" aria-hidden="true" />
            </div>
          ) : !recentSessions || recentSessions.length === 0 ? (
            <p className="text-muted-foreground text-xs text-center py-4">No recent print sessions.</p>
          ) : (
            <ScrollArea className="max-h-[300px]">
              <div className="space-y-2.5 pr-4">
                {recentSessions.map((session) => (
                  <Button
                    type="button"
                    key={session.id}
                    variant="ghost"
                    onClick={() => handleSelectSession(session)}
                    className="w-full text-left h-auto p-3.5 rounded-xl border flex items-center justify-between gap-4 cursor-pointer transition-all whitespace-normal bg-background/60 border-border hover:border-primary/25"
                  >
                    <div className="flex items-center gap-3 min-w-0">
                      <Card className="p-2 rounded-lg bg-muted text-muted-foreground border-none shadow-none">
                        <Printer className="h-4 w-4" aria-hidden="true" />
                      </Card>
                      <div className="min-w-0">
                        <h4 className="text-xs font-bold text-foreground truncate m-0">{session.document_name}</h4>
                        <p className="text-[10px] text-muted-foreground mt-0.5">
                          {session.config_paper_size.toUpperCase()} | Mar: {session.config_margin} | Gut: {session.config_gutter} | Sig: {session.config_signature_size}
                        </p>
                      </div>
                    </div>
                    <div>
                      {session.status === "compiling" ? (
                        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" aria-hidden="true" />
                      ) : session.status === "failed" ? (
                        <AlertCircle className="h-4 w-4 text-destructive" aria-hidden="true" />
                      ) : (
                        <FileCheck className="h-4 w-4 text-emerald-500" aria-hidden="true" />
                      )}
                    </div>
                  </Button>
                ))}
              </div>
            </ScrollArea>
          )}
        </div>
      </div>

      {/* Right panel: Compile Booklet parameters & Details */}
      <div className="lg:col-span-2">
        {selectedDocId && docDetail ? (
          <div className="glass p-6 md:p-8 rounded-2xl border-border space-y-6">
            <div>
              <span className="text-[10px] uppercase font-bold text-primary tracking-wider">Document Details</span>
              <h2 className="text-xl font-extrabold text-foreground mt-1">{docDetail.name}</h2>
              <p className="text-muted-foreground text-xs mt-1">Uploaded {new Date(docDetail.created_at).toLocaleDateString()}</p>
            </div>

            <div className="border-t border-border pt-4 space-y-3">
              <div className="flex items-center justify-between border-b border-border/40 pb-2">
                <div className="flex items-center gap-2">
                  <Settings className="h-4 w-4 text-muted-foreground" aria-hidden="true" />
                  <h3 className="text-sm font-bold text-foreground m-0">Booklet Imposition Config</h3>
                </div>
                <div className="flex bg-muted p-0.5 rounded border border-border text-[9px] font-bold">
                  <Button
                    type="button"
                    variant="ghost"
                    onClick={() => setDashboardPreviewSide("front")}
                    className={`px-1.5 py-0.5 h-6 rounded text-[9px] font-bold transition-all ${dashboardPreviewSide === "front" ? "bg-background text-foreground shadow-sm hover:bg-background" : "text-muted-foreground hover:text-foreground hover:bg-transparent"
                      }`}
                  >
                    Front Side
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    onClick={() => setDashboardPreviewSide("back")}
                    className={`px-1.5 py-0.5 h-6 rounded text-[9px] font-bold transition-all ${dashboardPreviewSide === "back" ? "bg-background text-foreground shadow-sm hover:bg-background" : "text-muted-foreground hover:text-foreground hover:bg-transparent"
                      }`}
                  >
                    Back Side
                  </Button>
                </div>
              </div>

              {/* Mock Sheet Container - LOOKS LIKE PAPER: no corner radius, drop shadow. MOVED TO TOP */}
              <Card className="relative aspect-[1.5/1] w-full bg-white border border-neutral-300 shadow-[0_6px_16px_rgba(0,0,0,0.12)] flex items-center justify-center overflow-hidden rounded-none">
                <PDFPageRenderer
                  url={api.getBookletPreviewUrl(selectedDocId!, margin, gutter, paperSize, signatureSize, guides, dashboardPreviewSide)}
                  className="w-full h-full"
                  rotation={0}
                />
              </Card>

              {/* Compact Spacing Sliders and dropdowns inline on a single row */}
              <div className="grid grid-cols-2 lg:grid-cols-4 gap-3 pt-2">
                <div className="space-y-1">
                  <Label htmlFor="margin-input" className="text-[10px] font-semibold text-muted-foreground uppercase">Margins: <span className="text-foreground font-bold">{margin}pt</span></Label>
                  <Slider
                    id="margin-input"
                    min={0}
                    max={72}
                    step={1}
                    value={[margin]}
                    onValueChange={(val) => setMargin(val[0])}
                    className="w-full pt-1.5 cursor-pointer"
                  />
                </div>

                <div className="space-y-1">
                  <Label htmlFor="gutter-input" className="text-[10px] font-semibold text-muted-foreground uppercase">Gutter: <span className="text-foreground font-bold">{gutter}pt</span></Label>
                  <Slider
                    id="gutter-input"
                    min={0}
                    max={100}
                    step={1}
                    value={[gutter]}
                    onValueChange={(val) => setGutter(val[0])}
                    className="w-full pt-1.5 cursor-pointer"
                  />
                </div>

                <div className="space-y-0.5">
                  <Label htmlFor="paper-size-select" className="text-[10px] font-semibold text-muted-foreground uppercase">Paper Format</Label>
                  <Select id="paper-size-select" value={paperSize} onChange={(e) => setPaperSize(e.target.value)} className="h-7 text-xs py-0">
                    <option value="a4">A4 Landscape (11.7×8.3")</option>
                    <option value="letter">Letter Landscape (11×8.5")</option>
                    <option value="folio">Folio Landscape (13×8.5")</option>
                  </Select>
                </div>

                <div className="space-y-0.5">
                  <Label htmlFor="signature-size-select" className="text-[10px] font-semibold text-muted-foreground uppercase">Signature Size</Label>
                  <Select id="signature-size-select" value={signatureSize.toString()} onChange={(e) => setSignatureSize(parseInt(e.target.value))} className="h-7 text-xs py-0">
                    <option value="4">4 Pages (1 sheet)</option>
                    <option value="8">8 Pages (2 sheets)</option>
                    <option value="12">12 Pages (3 sheets)</option>
                    <option value="16">16 Pages (4 sheets)</option>
                  </Select>
                </div>
              </div>

              {/* Bottom Row: Checkbox and Compile Button inline */}
              <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 pt-2 border-t border-border/30">
                <div className="flex items-center gap-2">
                  <Checkbox
                    id="guides-checkbox"
                    checked={guides}
                    onCheckedChange={(checked) => setGuides(checked === true)}
                  />
                  <Label htmlFor="guides-checkbox" className="text-xs font-semibold text-foreground cursor-pointer">
                    Draw Folding &amp; Cutting Guides
                  </Label>
                </div>

                <Button
                  className="sm:w-auto h-8 px-4 font-bold flex items-center justify-center gap-1.5 text-xs shadow-md shadow-primary/10"
                  onClick={() => compileMutation.mutate(selectedDocId!)}
                  disabled={compiling}
                >
                  <Printer className="h-3.5 w-3.5" aria-hidden="true" />
                  Compile &amp; Generate Layout
                </Button>
              </div>

              {compiling && (
                <div className="flex items-center gap-3 bg-background/80 p-3 rounded-xl border border-border">
                  <Loader2 className="h-4 w-4 animate-spin text-primary" aria-hidden="true" />
                  <div className="text-xs">
                    <p className="font-bold text-foreground">Compiling Booklet...</p>
                    <p className="text-muted-foreground mt-0.5">{compileStatus}</p>
                  </div>
                </div>
              )}

              {!compiling && compileStatus && (
                <div className="p-3 bg-destructive/10 border border-destructive/20 text-destructive rounded-xl text-xs flex items-center gap-2">
                  <AlertCircle className="h-3.5 w-3.5" aria-hidden="true" />
                  <span>{compileStatus}</span>
                </div>
              )}
            </div>
          </div>
        ) : (
          <div className="glass h-[400px] rounded-2xl border-border flex flex-col items-center justify-center text-center p-6">
            <FileText className="h-16 w-16 text-muted-foreground animate-pulse" aria-hidden="true" />
            <h3 className="text-base font-bold text-foreground mt-4">No Document Selected</h3>
            <p className="text-muted-foreground text-xs mt-1.5 max-w-xs leading-relaxed">
              Select an uploaded document from the library panel or drop a new PDF file to configure your booklet imposition parameters.
            </p>
          </div>
        )}
      </div>
    </div>
  )
}
