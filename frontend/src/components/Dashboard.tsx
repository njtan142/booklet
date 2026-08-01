import React, { useState, useEffect, useMemo } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "../api"
import type { BookletListResponse } from "../api"
import { PrintHelper } from "./PrintHelper"
import { ToolActionBar } from "./ToolActionBar"
import { UploadPanel } from "./dashboard/UploadPanel"
import { LibraryPanel } from "./dashboard/LibraryPanel"
import { LibraryDialog } from "./dashboard/LibraryDialog"
import { RecentSessionsPanel } from "./dashboard/RecentSessionsPanel"
import { BookletConfigPanel } from "./dashboard/BookletConfigPanel"
import type { BookletConfig } from "./dashboard/BookletConfigPanel"
import { useDocumentUploads } from "./dashboard/useDocumentUploads"

const DEFAULT_BOOKLET_CONFIG: BookletConfig = {
  margin: 12,
  gutter: 24,
  paperSize: "a4",
  signatureSize: 4,
  guides: true,
}

export const Dashboard: React.FC = () => {
  const queryClient = useQueryClient()
  const [selectedDocId, setSelectedDocId] = useState<string | null>(null)
  const [activeBookletId, setActiveBookletId] = useState<string | null>(null)

  const [config, setConfig] = useState<BookletConfig>(DEFAULT_BOOKLET_CONFIG)
  const [previewSide, setPreviewSide] = useState<"front" | "back">("front")

  const [compiling, setCompiling] = useState<boolean>(false)
  const [compileStatus, setCompileStatus] = useState<string>("")
  const [pollingBookletId, setPollingBookletId] = useState<string | null>(null)

  const [searchQuery, setSearchQuery] = useState<string>("")
  const [isLibraryModalOpen, setIsLibraryModalOpen] = useState<boolean>(false)
  const [modalSearchQuery, setModalSearchQuery] = useState<string>("")

  // Documents ticked for a tool run. Kept separate from selectedDocId, which
  // drives the booklet panel and is a single document by nature.
  const [checkedDocIds, setCheckedDocIds] = useState<string[]>([])

  const { data: rawDocuments, isLoading: loadingDocs } = useQuery({
    queryKey: ["documents"],
    queryFn: api.listDocuments,
    refetchInterval: (query) => {
      const hasProcessing = query.state.data?.some(
        (d) => d.status === "processing" || d.status === "queued"
      )
      return hasProcessing ? 2000 : false
    },
  })
  const documents = rawDocuments || []

  const {
    inFlightUploads,
    failedUploads,
    uploadFiles,
    dismissFailedUpload,
    trackResumedDocument,
  } = useDocumentUploads(documents)

  const filteredDocuments = useMemo(() => {
    if (!searchQuery.trim()) return documents
    const query = searchQuery.toLowerCase()
    return documents.filter((doc) => doc.name.toLowerCase().includes(query))
  }, [documents, searchQuery])

  const filteredModalDocuments = useMemo(() => {
    if (!modalSearchQuery.trim()) return documents
    const query = modalSearchQuery.toLowerCase()
    return documents.filter((doc) => doc.name.toLowerCase().includes(query))
  }, [documents, modalSearchQuery])

  // Resolve ids against the live list so a document that was dismissed or
  // failed elsewhere cannot linger in the selection the tool menu acts on.
  const checkedDocuments = useMemo(
    () => documents.filter((doc) => checkedDocIds.includes(doc.id)),
    [documents, checkedDocIds]
  )

  const toggleChecked = (docId: string, checked: boolean) => {
    setCheckedDocIds((current) =>
      checked ? [...current, docId] : current.filter((id) => id !== docId)
    )
  }

  const { data: docDetail } = useQuery({
    queryKey: ["document", selectedDocId],
    queryFn: () => api.getDocument(selectedDocId!),
    enabled: !!selectedDocId,
    refetchInterval: (query) => {
      const status = query.state.data?.status
      return status === "processing" || status === "queued" ? 2000 : false
    },
  })

  const { data: recentSessions, isLoading: loadingSessions } = useQuery({
    queryKey: ["booklets"],
    queryFn: api.listBooklets,
    refetchInterval: (query) => {
      const hasProcessing = query.state.data?.some((b) => b.status === "compiling")
      return hasProcessing ? 2000 : false
    },
  })

  const handleSelectSession = (session: BookletListResponse) => {
    setSelectedDocId(session.document_id)
    setConfig({
      margin: session.config_margin,
      gutter: session.config_gutter,
      paperSize: session.config_paper_size,
      signatureSize: session.config_signature_size,
      guides: session.config_guides,
    })
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

  // Auto-select a session when arriving with ?session_id=
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const sessionId = params.get("session_id")
    if (sessionId && recentSessions && recentSessions.length > 0) {
      const session = recentSessions.find((s) => s.id === sessionId)
      if (session) {
        handleSelectSession(session)
        window.history.replaceState({}, document.title, window.location.pathname)
      }
    }
  }, [recentSessions])

  const resumeMutation = useMutation({
    mutationFn: (docId: string) => api.resumeDocument(docId),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ["documents"] })
      trackResumedDocument(data.document_id)
    },
    onError: (err: any) => {
      alert(`Failed to resume document processing: ${err.message}`)
    },
  })

  const renameMutation = useMutation({
    mutationFn: ({ docId, name }: { docId: string; name: string }) => api.renameDocument(docId, name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["documents"] })
    },
    onError: (err: any) => {
      alert(`Failed to rename document: ${err.message}`)
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (docId: string) => api.deleteDocument(docId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["documents"] })
      queryClient.invalidateQueries({ queryKey: ["booklets"] })
      setSelectedDocId(null)
      setActiveBookletId(null)
    },
    onError: (err: any) => {
      alert(`Failed to delete document: ${err.message}`)
    },
  })

  const compileMutation = useMutation({
    mutationFn: (docId: string) =>
      api.compileBooklet(docId, {
        margin: config.margin,
        gutter: config.gutter,
        paper_size: config.paperSize,
        signature_size: config.signatureSize,
        guides: config.guides,
      }),
    onSuccess: (data) => {
      setPollingBookletId(data.booklet_id)
      setCompiling(true)
      setCompileStatus("Arranging pages & generating canvas...")

      // Retire older sessions built from the same configuration.
      api
        .cleanupBookletSessions(selectedDocId!, {
          margin: config.margin,
          gutter: config.gutter,
          paper_size: config.paperSize,
          signature_size: config.signatureSize,
          guides: config.guides,
          current_booklet_id: data.booklet_id,
        })
        .catch((err) => console.warn("Failed to clean up old booklet sessions:", err))

      queryClient.invalidateQueries({ queryKey: ["booklets"] })
    },
    onError: (err: any) => {
      setCompileStatus(`Compilation failed: ${err.message}`)
      setCompiling(false)
      queryClient.invalidateQueries({ queryKey: ["booklets"] })
    },
  })

  // Poll the compiling booklet until it settles.
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

  // A compiled booklet takes over the whole view.
  if (activeBookletId && docDetail) {
    return (
      <PrintHelper
        bookletId={activeBookletId}
        documentId={selectedDocId!}
        totalPages={docDetail.total_pages}
        signatureSize={config.signatureSize}
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
      <div className="lg:col-span-1 space-y-6">
        <UploadPanel
          inFlightUploads={inFlightUploads}
          failedUploads={failedUploads}
          onFilesSelected={uploadFiles}
          onDismissFailure={dismissFailedUpload}
        />

        <LibraryPanel
          documents={documents}
          filteredDocuments={filteredDocuments}
          loading={loadingDocs}
          searchQuery={searchQuery}
          onSearchQueryChange={setSearchQuery}
          selectedDocId={selectedDocId}
          onSelectDocument={setSelectedDocId}
          checkedDocIds={checkedDocIds}
          onToggleChecked={toggleChecked}
          failedUploads={failedUploads}
          onResume={(docId) => resumeMutation.mutate(docId)}
          onDismissFailure={dismissFailedUpload}
          onOpenLibraryDialog={() => setIsLibraryModalOpen(true)}
          onRename={(docId, name) => renameMutation.mutate({ docId, name })}
          onDelete={(docId) => deleteMutation.mutate(docId)}
        />

        <RecentSessionsPanel
          sessions={recentSessions}
          loading={loadingSessions}
          onSelectSession={handleSelectSession}
        />
      </div>

      <div className="lg:col-span-2">
        <BookletConfigPanel
          documentId={selectedDocId}
          docDetail={docDetail}
          config={config}
          onConfigChange={(patch) => setConfig((current) => ({ ...current, ...patch }))}
          previewSide={previewSide}
          onPreviewSideChange={setPreviewSide}
          compiling={compiling}
          compileStatus={compileStatus}
          onCompile={() => compileMutation.mutate(selectedDocId!)}
        />
      </div>

      <LibraryDialog
        open={isLibraryModalOpen}
        onOpenChange={setIsLibraryModalOpen}
        documents={documents}
        filteredDocuments={filteredModalDocuments}
        loading={loadingDocs}
        searchQuery={modalSearchQuery}
        onSearchQueryChange={setModalSearchQuery}
        selectedDocId={selectedDocId}
        onSelectDocument={setSelectedDocId}
        failedUploads={failedUploads}
        onResume={(docId) => resumeMutation.mutate(docId)}
        onDismissFailure={dismissFailedUpload}
        onRename={(docId, name) => renameMutation.mutate({ docId, name })}
        onDelete={(docId) => deleteMutation.mutate(docId)}
      />

      <ToolActionBar selection={checkedDocuments} onClear={() => setCheckedDocIds([])} />
    </div>
  )
}
