import React, { useState } from "react"
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
import { FileText, Loader2, Pencil, Search, Trash2 } from "lucide-react"
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
  onRename: (docId: string, newName: string) => void
  onDelete: (docId: string) => void
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
  onRename,
  onDelete,
}) => {
  const [renamingDocId, setRenamingDocId] = useState<string | null>(null)
  const [renameInput, setRenameInput] = useState<string>("")

  const startRename = (doc: DocumentInfo) => {
    setRenamingDocId(doc.id)
    setRenameInput(doc.name)
  }

  const commitRename = (docId: string) => {
    const trimmed = renameInput.trim()
    if (trimmed && trimmed !== documents.find((d) => d.id === docId)?.name) {
      onRename(docId, trimmed)
    }
    setRenamingDocId(null)
    setRenameInput("")
  }

  const handleRenameKeyDown = (docId: string, e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      e.preventDefault()
      commitRename(docId)
    } else if (e.key === "Escape") {
      e.preventDefault()
      setRenamingDocId(null)
      setRenameInput("")
    }
  }

  const handleDelete = (doc: DocumentInfo) => {
    if (window.confirm(`Delete "${doc.name}"? This cannot be undone.`)) {
      onDelete(doc.id)
    }
  }

  return (
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
                  const isRenaming = renamingDocId === doc.id

                  return (
                    <div key={doc.id} className="relative group">
                      <Button
                        type="button"
                        variant="ghost"
                        onClick={() => {
                          if (doc.status === "ready") {
                            onSelectDocument(doc.id)
                            onOpenChange(false)
                          }
                        }}
                        disabled={doc.status !== "ready"}
                        className={`w-full text-left h-auto p-3.5 rounded-xl border flex flex-col gap-2 cursor-pointer transition-all whitespace-normal ${
                          isSelected
                            ? "bg-primary/10 border-primary/30"
                            : isBusy
                              ? "bg-muted/30 border-border opacity-60 cursor-not-allowed"
                              : "bg-background/60 border-border hover:border-primary/25"
                        }`}
                      >
                        <div className="flex items-center gap-3 min-w-0 w-full">
                          <div className={`p-2 rounded-lg shrink-0 ${isSelected ? "bg-primary/20 text-primary" : "bg-muted text-muted-foreground"}`}>
                            <FileText className="h-4 w-4" aria-hidden="true" />
                          </div>
                          <div className="min-w-0 flex-1">
                            {isRenaming ? (
                              <Input
                                type="text"
                                className="h-5 text-xs"
                                value={renameInput}
                                onChange={(e) => setRenameInput(e.target.value)}
                                onBlur={() => commitRename(doc.id)}
                                onKeyDown={(e) => handleRenameKeyDown(doc.id, e)}
                                autoFocus
                                onClick={(e) => e.stopPropagation()}
                              />
                            ) : (
                              <h4 className="text-xs font-bold text-foreground truncate m-0" title={doc.name}>
                                {doc.name}
                              </h4>
                            )}
                          </div>
                        </div>

                        <p className="text-[10px] text-muted-foreground mt-0.5">
                          {documentProgressLabel(doc, true)}
                        </p>
                      </Button>

                      <div className="absolute top-2 right-2 flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          className="h-6 w-6"
                          onClick={() => startRename(doc)}
                          disabled={isBusy}
                          aria-label={`Rename ${doc.name}`}
                        >
                          <Pencil className="h-3 w-3" />
                        </Button>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          className="h-6 w-6 text-destructive hover:text-destructive hover:bg-destructive/15"
                          onClick={() => handleDelete(doc)}
                          disabled={isBusy}
                          aria-label={`Delete ${doc.name}`}
                        >
                          <Trash2 className="h-3 w-3" />
                        </Button>
                      </div>
                    </div>
                  )
                })}
              </div>
            </ScrollArea>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
