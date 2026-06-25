import React, { useState, useEffect } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "../api"
import type { DocumentInfo, DocumentDetail } from "../api"
import { Button } from "./ui/button"
import { Input } from "./ui/input"
import { Select } from "./ui/select"
import { Label } from "./ui/label"
import { PrintHelper } from "./PrintHelper"
import { 
  UploadCloud, 
  FileText, 
  Settings, 
  Loader2, 
  Printer, 
  Download, 
  AlertCircle,
  FileCheck
} from "lucide-react"

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
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["documents"] })
      setUploadProgress("")
    },
    onError: (err: any) => {
      setUploadProgress(`Error: ${err.message}`)
    }
  })

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
        <div className="glass p-6 rounded-2xl border-zinc-800 space-y-4">
          <h3 className="text-lg font-bold text-white m-0">Upload Document</h3>
          
          <div className="relative border-2 border-dashed border-zinc-800 rounded-xl p-8 flex flex-col items-center justify-center gap-2 hover:border-primary/50 transition-all bg-zinc-950/20 group">
            <UploadCloud className="h-10 w-10 text-zinc-400 group-hover:text-primary transition-colors" aria-hidden="true" />
            <span className="text-zinc-400 text-xs font-medium">Drag & drop your PDF file or click to browse</span>
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
            <div className="flex items-center gap-2 text-xs text-zinc-400 bg-zinc-900/60 p-3 rounded-lg border border-zinc-800/80">
              <Loader2 className="h-3.5 w-3.5 animate-spin text-primary" aria-hidden="true" />
              <span>{uploadProgress}</span>
            </div>
          )}
        </div>

        <div className="glass p-6 rounded-2xl border-zinc-800 space-y-4">
          <h3 className="text-lg font-bold text-white m-0">Library</h3>

          {loadingDocs ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 className="h-6 w-6 animate-spin text-primary" aria-hidden="true" />
            </div>
          ) : documents.length === 0 ? (
            <p className="text-zinc-400 text-xs text-center py-6">No documents uploaded yet.</p>
          ) : (
            <div className="space-y-2.5 max-h-[400px] overflow-y-auto pr-2">
              {documents.map((doc) => {
                const isSelected = selectedDocId === doc.id
                return (
                  <button 
                    type="button"
                    key={doc.id} 
                    onClick={() => doc.status === "ready" && setSelectedDocId(doc.id)}
                    disabled={doc.status !== "ready"}
                    className={`w-full text-left p-3.5 rounded-xl border flex items-center justify-between gap-4 cursor-pointer transition-all ${
                      isSelected 
                        ? "bg-primary/10 border-primary/30" 
                        : doc.status === "processing" 
                          ? "bg-zinc-950/20 border-zinc-900 opacity-60 cursor-not-allowed"
                          : "bg-zinc-950/40 border-zinc-800/60 hover:border-zinc-700/80"
                    }`}
                  >
                    <div className="flex items-center gap-3 min-w-0">
                      <div className={`p-2 rounded-lg ${isSelected ? "bg-primary/20 text-primary" : "bg-zinc-900 text-zinc-400"}`}>
                        <FileText className="h-4 w-4" aria-hidden="true" />
                      </div>
                      <div className="min-w-0">
                        <h4 className="text-xs font-bold text-white truncate m-0">{doc.name}</h4>
                        <p className="text-[10px] text-zinc-400 mt-0.5">{doc.total_pages} pages</p>
                      </div>
                    </div>

                    <div>
                      {doc.status === "processing" ? (
                        <Loader2 className="h-4 w-4 animate-spin text-zinc-400" aria-hidden="true" />
                      ) : doc.status === "failed" ? (
                        <AlertCircle className="h-4 w-4 text-rose-550" aria-hidden="true" />
                      ) : (
                        <FileCheck className="h-4 w-4 text-emerald-500" aria-hidden="true" />
                      )}
                    </div>
                  </button>
                )
              })}
            </div>
          )}
        </div>
      </div>

      {/* Right panel: Compile Booklet parameters & Details */}
      <div className="lg:col-span-2">
        {selectedDocId && docDetail ? (
          <div className="glass p-6 md:p-8 rounded-2xl border-zinc-800 space-y-6">
            <div>
              <span className="text-[10px] uppercase font-bold text-primary tracking-wider">Document Details</span>
              <h2 className="text-xl font-extrabold text-white mt-1">{docDetail.name}</h2>
              <p className="text-zinc-400 text-xs mt-1">Uploaded {new Date(docDetail.created_at).toLocaleDateString()}</p>
            </div>

            <div className="border-t border-zinc-800/80 pt-6 space-y-4">
              <div className="flex items-center gap-2">
                <Settings className="h-5 w-5 text-zinc-400" aria-hidden="true" />
                <h3 className="text-base font-bold text-white m-0">Booklet Imposition Config</h3>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div className="space-y-1.5">
                  <Label htmlFor="margin-input" className="text-xs font-semibold text-zinc-450">Outer Margins (Points)</Label>
                  <Input 
                    id="margin-input"
                    type="number" 
                    value={margin} 
                    onChange={(e) => setMargin(parseFloat(e.target.value) || 0)} 
                  />
                  <p className="text-[10px] text-zinc-400">Spacing around page edges. 72pt = 1 inch.</p>
                </div>

                <div className="space-y-1.5">
                  <Label htmlFor="gutter-input" className="text-xs font-semibold text-zinc-455">Inner Gutter (Points)</Label>
                  <Input 
                    id="gutter-input"
                    type="number" 
                    value={gutter} 
                    onChange={(e) => setGutter(parseFloat(e.target.value) || 0)} 
                  />
                  <p className="text-[10px] text-zinc-400">Spacing between side-by-side pages.</p>
                </div>

                <div className="space-y-1.5">
                  <Label htmlFor="paper-size-select" className="text-xs font-semibold text-zinc-460">Paper Format (Landscape)</Label>
                  <Select id="paper-size-select" value={paperSize} onChange={(e) => setPaperSize(e.target.value)}>
                    <option value="a4">A4 Landscape</option>
                    <option value="letter">Letter Landscape</option>
                  </Select>
                  <p className="text-[10px] text-zinc-400">Dimensions of final booklet sheet.</p>
                </div>

                <div className="space-y-1.5">
                  <Label htmlFor="signature-size-select" className="text-xs font-semibold text-zinc-465">Signature Size</Label>
                  <Select id="signature-size-select" value={signatureSize.toString()} onChange={(e) => setSignatureSize(parseInt(e.target.value))}>
                    <option value="4">4 Pages (1 sheet)</option>
                    <option value="8">8 Pages (2 sheets)</option>
                    <option value="12">12 Pages (3 sheets)</option>
                    <option value="16">16 Pages (4 sheets)</option>
                  </Select>
                  <p className="text-[10px] text-zinc-400">Grouping count for folding/binding.</p>
                </div>
              </div>

              {compiling && (
                <div className="flex items-center gap-3 bg-zinc-950/80 p-4 rounded-xl border border-zinc-800">
                  <Loader2 className="h-5 w-5 animate-spin text-primary" aria-hidden="true" />
                  <div className="text-xs">
                    <p className="font-bold text-white">Compiling Booklet...</p>
                    <p className="text-zinc-400 mt-0.5">{compileStatus}</p>
                  </div>
                </div>
              )}

              {!compiling && compileStatus && (
                <div className="p-4 bg-rose-500/10 border border-rose-500/20 text-rose-400 rounded-xl text-xs flex items-center gap-2">
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
          <div className="glass h-[400px] rounded-2xl border-zinc-800 flex flex-col items-center justify-center text-center p-6">
            <FileText className="h-16 w-16 text-zinc-400 animate-pulse" aria-hidden="true" />
            <h3 className="text-base font-bold text-white mt-4">No Document Selected</h3>
            <p className="text-zinc-400 text-xs mt-1.5 max-w-xs leading-relaxed">
              Select an uploaded document from the library panel or drop a new PDF file to configure your booklet imposition parameters.
            </p>
          </div>
        )}
      </div>
    </div>
  )
}
