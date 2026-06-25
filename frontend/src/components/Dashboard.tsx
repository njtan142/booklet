import React, { useState, useEffect } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "../api"
import type { DocumentInfo, DocumentDetail } from "../api"
import { Button } from "./ui/button"
import { Input } from "./ui/input"
import { Select } from "./ui/select"
import { Label } from "./ui/label"
import { ScrollArea } from "./ui/scroll-area"
import { PrintHelper } from "./PrintHelper"
import { 
  UploadCloud, 
  FileText, 
  Settings, 
  Loader2, 
  Printer, 
  Download, 
  AlertCircle,
  FileCheck,
  X
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
  
  const [compiling, setCompiling] = useState<boolean>(false)
  const [compileStatus, setCompileStatus] = useState<string>("")
  const [pollingBookletId, setPollingBookletId] = useState<string | null>(null)
  const [pendingUploads, setPendingUploads] = useState<PendingUpload[]>([])
  const [failedUploads, setFailedUploads] = useState<FailedUpload[]>([])

  // 1. Fetch document list
  const { data: rawDocuments, isLoading: loadingDocs, refetch: refetchDocs } = useQuery({
    queryKey: ["documents"],
    queryFn: api.listDocuments,
    refetchInterval: (query) => {
      // Poll if any document is processing
      const hasProcessing = query.state.data?.some(d => d.status === "processing")
      return hasProcessing ? 2000 : false
    }
  })
  const documents = rawDocuments || []

  // 2. Fetch selected document details
  const { data: docDetail, isLoading: loadingDocDetail } = useQuery({
    queryKey: ["document", selectedDocId],
    queryFn: () => api.getDocument(selectedDocId!),
    enabled: !!selectedDocId,
    refetchInterval: (query) => {
      // Poll if this document is processing
      return query.state.data?.status === "processing" ? 2000 : false
    }
  })

  // 3. Upload Mutation
  const [uploadProgress, setUploadProgress] = useState<string>("")
  const uploadMutation = useMutation({
    mutationFn: api.uploadDocument,
    onSuccess: (data, file) => {
      queryClient.invalidateQueries({ queryKey: ["documents"] })
      setUploadProgress("")
      setPendingUploads((current) => [
        ...current,
        {
          documentId: data.document_id,
          fileName: file.name,
          startedAt: Date.now(),
        },
      ])
    },
    onError: (err: unknown, file) => {
      const message = err instanceof Error ? err.message : "Upload failed"
      setUploadProgress("")
      setFailedUploads((current) => [
        ...current,
        {
          id: `request-${Date.now()}-${file.name}`,
          fileName: file.name,
          message,
        },
      ])
    }
  })

  useEffect(() => {
    if (pendingUploads.length === 0) return

    const now = Date.now()
    const resolvedFailures: FailedUpload[] = []

    const nextPendingUploads = pendingUploads.filter((pending) => {
      const document = documents.find((item) => item.id === pending.documentId)

      if (!document) {
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
    const file = e.target.files?.[0]
    if (file) {
      setUploadProgress("Uploading and parsing PDF...")
      uploadMutation.mutate(file)
    }
  }

  // 4. Booklet Compile Mutation
  const compileMutation = useMutation({
    mutationFn: (docId: string) => api.compileBooklet(docId, {
      margin,
      gutter,
      paper_size: paperSize,
      signature_size: signatureSize,
    }),
    onSuccess: (data) => {
      setPollingBookletId(data.booklet_id)
      setCompiling(true)
      setCompileStatus("Arranging pages & generating canvas...")
    },
    onError: (err: any) => {
      setCompileStatus(`Compilation failed: ${err.message}`)
      setCompiling(false)
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
        } else if (booklet.status === "failed") {
          clearInterval(interval)
          setCompiling(false)
          setPollingBookletId(null)
          setCompileStatus("Booklet generation failed on backend.")
        }
      } catch (err) {
        clearInterval(interval)
        setCompiling(false)
        setPollingBookletId(null)
        setCompileStatus("Error polling booklet compile status.")
      }
    }, 2000)

    return () => clearInterval(interval)
  }, [pollingBookletId])

  // Reset active booklet mode
  if (activeBookletId && docDetail) {
    return (
      <PrintHelper 
        bookletId={activeBookletId} 
        totalPages={docDetail.total_pages}
        onBack={() => setActiveBookletId(null)}
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
            <span className="text-muted-foreground text-xs font-medium">Drag & drop your PDF file or click to browse</span>
            <Input 
              id="pdf-file-upload"
              type="file" 
              accept=".pdf" 
              className="absolute inset-0 w-full h-full opacity-0 cursor-pointer"
              onChange={handleFileChange}
              disabled={uploadMutation.isPending}
              aria-label="Upload PDF document"
            />
          </div>

          {uploadProgress && (
            <div className="flex items-center gap-2 text-xs text-muted-foreground bg-muted/60 p-3 rounded-lg border border-border">
              <Loader2 className="h-3.5 w-3.5 animate-spin text-primary" aria-hidden="true" />
              <span>{uploadProgress}</span>
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
            <ScrollArea className="max-h-[400px]">
              <div className="space-y-2.5 pr-4">
                {documents.map((doc) => {
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
                          <div className="p-2 rounded-lg bg-destructive/15 text-destructive">
                            <FileText className="h-4 w-4" aria-hidden="true" />
                          </div>
                          <div className="min-w-0">
                            <h4 className="text-xs font-bold text-foreground truncate m-0">{doc.name}</h4>
                            <p className="text-[10px] text-destructive/80 mt-0.5">
                              Upload failed{failedUpload ? `: ${failedUpload.message}` : "."}
                            </p>
                          </div>
                        </div>

                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          className="shrink-0 text-destructive hover:text-destructive hover:bg-destructive/15"
                          onClick={() => dismissFailedUpload(failedUpload?.id ?? `doc-${doc.id}`)}
                        >
                          Dismiss
                        </Button>
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
                      className={`w-full text-left h-auto p-3.5 rounded-xl border flex items-center justify-between gap-4 cursor-pointer transition-all whitespace-normal ${
                        isSelected
                          ? "bg-primary/10 border-primary/30"
                          : effectiveStatus === "processing"
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
                          <p className="text-[10px] text-muted-foreground mt-0.5">{doc.total_pages} pages</p>
                        </div>
                      </div>

                      <div>
                        {effectiveStatus === "processing" ? (
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

            <div className="border-t border-border pt-6 space-y-4">
              <div className="flex items-center gap-2">
                <Settings className="h-5 w-5 text-muted-foreground" aria-hidden="true" />
                <h3 className="text-base font-bold text-foreground m-0">Booklet Imposition Config</h3>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-1.5">
                  <Label htmlFor="margin-input" className="text-xs font-semibold text-muted-foreground">Outer Margins (Points)</Label>
                  <Input 
                    id="margin-input"
                    type="number" 
                    value={margin} 
                    onChange={(e) => setMargin(parseFloat(e.target.value) || 0)} 
                  />
                  <p className="text-[10px] text-muted-foreground">Spacing around page edges. 72pt = 1 inch.</p>
                </div>

                <div className="space-y-1.5">
                  <Label htmlFor="gutter-input" className="text-xs font-semibold text-muted-foreground">Inner Gutter (Points)</Label>
                  <Input 
                    id="gutter-input"
                    type="number" 
                    value={gutter} 
                    onChange={(e) => setGutter(parseFloat(e.target.value) || 0)} 
                  />
                  <p className="text-[10px] text-muted-foreground">Spacing between side-by-side pages.</p>
                </div>

                <div className="space-y-1.5">
                  <Label htmlFor="paper-size-select" className="text-xs font-semibold text-muted-foreground">Paper Format (Landscape)</Label>
                  <Select id="paper-size-select" value={paperSize} onChange={(e) => setPaperSize(e.target.value)}>
                    <option value="a4">A4 Landscape</option>
                    <option value="letter">Letter Landscape</option>
                  </Select>
                  <p className="text-[10px] text-muted-foreground">Dimensions of final booklet sheet.</p>
                </div>

                <div className="space-y-1.5">
                  <Label htmlFor="signature-size-select" className="text-xs font-semibold text-muted-foreground">Signature Size</Label>
                  <Select id="signature-size-select" value={signatureSize.toString()} onChange={(e) => setSignatureSize(parseInt(e.target.value))}>
                    <option value="4">4 Pages (1 sheet)</option>
                    <option value="8">8 Pages (2 sheets)</option>
                    <option value="12">12 Pages (3 sheets)</option>
                    <option value="16">16 Pages (4 sheets)</option>
                  </Select>
                  <p className="text-[10px] text-muted-foreground">Grouping count for folding/binding.</p>
                </div>
              </div>

              {compiling && (
                <div className="flex items-center gap-3 bg-background/80 p-4 rounded-xl border border-border">
                  <Loader2 className="h-5 w-5 animate-spin text-primary" aria-hidden="true" />
                  <div className="text-xs">
                    <p className="font-bold text-foreground">Compiling Booklet...</p>
                    <p className="text-muted-foreground mt-0.5">{compileStatus}</p>
                  </div>
                </div>
              )}

              {!compiling && compileStatus && (
                <div className="p-4 bg-destructive/10 border border-destructive/20 text-destructive rounded-xl text-xs flex items-center gap-2">
                  <AlertCircle className="h-4 w-4" aria-hidden="true" />
                  <span>{compileStatus}</span>
                </div>
              )}

              <div className="pt-2">
                <Button 
                  className="w-full py-5 font-bold flex items-center justify-center gap-2 shadow-lg shadow-primary/20"
                  onClick={() => compileMutation.mutate(selectedDocId!)}
                  disabled={compiling}
                >
                  <Printer className="h-4 w-4" aria-hidden="true" />
                  Compile &amp; Generate Booklet Layout
                </Button>
              </div>
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
