import React from "react"
import { Button } from "../ui/button"
import { Input } from "../ui/input"
import { ScrollArea } from "../ui/scroll-area"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "../ui/dialog"
import { FileText, Loader2, Search } from "lucide-react"
import type { DocumentInfo } from "../../api"
import type { FailedUpload } from "./useDocumentUploads"
import { DocumentStatusIcon, documentProgressLabel } from "./documentStatus"
import { FailedDocumentRow } from "./LibraryPanel"

type LibraryDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  documents: DocumentInfo[]
  filteredDocuments: DocumentInfo[]
  loading: boolean
  searchQuery: string
  onSearchQueryChange: (value: string) => void
  selectedDocId: string | null
  onSelectDocument: (docId: string) => void
  failedUploads: FailedUpload[]
  onResume: (docId: string) => void
  onDismissFailure: (id: string) => void
}

export const LibraryDialog: React.FC<LibraryDialogProps> = ({
  open,
  onOpenChange,
  documents,
  filteredDocuments,
  loading,
  searchQuery,
  onSearchQueryChange,
  selectedDocId,
  onSelectDocument,
  failedUploads,
  onResume,
  onDismissFailure,
}) => (
  <Dialog open={open} onOpenChange={onOpenChange}>
    <DialogContent className="max-w-2xl glass">
      <DialogHeader>
        <DialogTitle>Document Library</DialogTitle>
        <DialogDescription>
          Select an uploaded document to configure booklet imposition parameters.
        </DialogDescription>
      </DialogHeader>

      <div className="space-y-4 mt-2">
        <div className="relative">
          <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            type="text"
            placeholder="Search documents by name..."
            className="pl-8"
            value={searchQuery}
            onChange={(e) => onSearchQueryChange(e.target.value)}
          />
        </div>

        {loading ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 className="h-8 w-8 animate-spin text-primary" aria-hidden="true" />
          </div>
        ) : documents.length === 0 ? (
          <p className="text-muted-foreground text-xs text-center py-10">No documents uploaded yet.</p>
        ) : filteredDocuments.length === 0 ? (
          <p className="text-muted-foreground text-xs text-center py-10">No matching documents found.</p>
        ) : (
          <ScrollArea className="max-h-[380px] pr-2">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 pb-2">
              {filteredDocuments.map((doc) => {
                const isSelected = selectedDocId === doc.id
                const failedUpload = failedUploads.find((item) => item.documentId === doc.id)

                if (failedUpload || doc.status === "failed") {
                  return (
                    <FailedDocumentRow
                      key={doc.id}
                      doc={doc}
                      layout="card"
                      message={failedUpload ? failedUpload.message : "Processing failed."}
                      onResume={() => onResume(doc.id)}
                      onDismiss={() => onDismissFailure(failedUpload?.id ?? `doc-${doc.id}`)}
                    />
                  )
                }

                const isBusy = doc.status === "processing" || doc.status === "queued"

                return (
                  <Button
                    type="button"
                    key={doc.id}
                    variant="ghost"
                    onClick={() => {
                      if (doc.status === "ready") {
                        onSelectDocument(doc.id)
                        onOpenChange(false)
                      }
                    }}
                    disabled={doc.status !== "ready"}
                    className={`w-full text-left h-auto p-3.5 rounded-xl border flex items-center justify-between gap-3 cursor-pointer transition-all whitespace-normal ${
                      isSelected
                        ? "bg-primary/10 border-primary/30"
                        : isBusy
                          ? "bg-muted/30 border-border opacity-60 cursor-not-allowed"
                          : "bg-background/60 border-border hover:border-primary/25"
                    }`}
                  >
                    <div className="flex items-center gap-3 min-w-0">
                      <div className={`p-2 rounded-lg shrink-0 ${isSelected ? "bg-primary/20 text-primary" : "bg-muted text-muted-foreground"}`}>
                        <FileText className="h-4 w-4" aria-hidden="true" />
                      </div>
                      <div className="min-w-0">
                        <h4 className="text-xs font-bold text-foreground truncate" title={doc.name}>
                          {doc.name}
                        </h4>
                        <p className="text-[10px] text-muted-foreground mt-0.5">
                          {documentProgressLabel(doc, true)}
                        </p>
                      </div>
                    </div>

                    <div className="shrink-0">
                      <DocumentStatusIcon status={doc.status} />
                    </div>
                  </Button>
                )
              })}
            </div>
          </ScrollArea>
        )}
      </div>
    </DialogContent>
  </Dialog>
)
