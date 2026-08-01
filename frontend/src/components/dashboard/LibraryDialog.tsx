import React, { useState } from "react"
import { Button } from "../ui/button"
import { Checkbox } from "../ui/checkbox"
import { Input } from "../ui/input"
import { ScrollArea } from "../ui/scroll-area"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "../ui/dialog"
import { Loader2, Search, Trash2 } from "lucide-react"
import type { DocumentInfo } from "../../api"
import type { FailedUpload } from "./useDocumentUploads"
import { FailedDocumentRow } from "./FailedDocumentRow"
import { DocumentCard } from "./DocumentCard"
import { DeleteConfirmationDialog } from "./DeleteConfirmationDialog"

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
  checkedDocIds?: string[]
  onToggleChecked?: (docId: string, checked: boolean, shiftKey?: boolean) => void
  onSelectAll?: (docIds: string[]) => void
  onBulkDelete?: (ids: string[]) => void
  isBulkDeleting?: boolean
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
  checkedDocIds = [],
  onToggleChecked,
  onSelectAll,
  onBulkDelete,
  isBulkDeleting = false,
  failedUploads,
  onResume,
  onDismissFailure,
  onRename,
  onDelete,
}) => {
  const [showConfirmDelete, setShowConfirmDelete] = useState(false)

  const readyFilteredIds = filteredDocuments.filter((d) => d.status === "ready").map((d) => d.id)
  const isAllSelected =
    readyFilteredIds.length > 0 && readyFilteredIds.every((id) => checkedDocIds.includes(id))

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl glass">
        <DialogHeader>
          <DialogTitle>Document Library</DialogTitle>
          <DialogDescription>
            Select an uploaded document to configure booklet imposition parameters or select multiple to manage.
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

          {filteredDocuments.length > 0 && onSelectAll && (
            <div className="flex items-center justify-between pt-1 pb-1 px-1 border-b border-border/50 text-xs text-muted-foreground">
              <label className="flex items-center gap-2 cursor-pointer select-none">
                <Checkbox
                  checked={isAllSelected}
                  onCheckedChange={() => onSelectAll(readyFilteredIds)}
                  aria-label="Select all documents"
                />
                <span>Select All ({readyFilteredIds.length})</span>
              </label>

              {checkedDocIds.length > 0 && onBulkDelete && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => setShowConfirmDelete(true)}
                  className="h-7 px-2 text-xs text-destructive hover:text-destructive hover:bg-destructive/15 gap-1 font-semibold"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  Delete ({checkedDocIds.length})
                </Button>
              )}
            </div>
          )}

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

                  return (
                    <DocumentCard
                      key={doc.id}
                      doc={doc}
                      isSelected={isSelected}
                      isChecked={checkedDocIds.includes(doc.id)}
                      onToggleChecked={onToggleChecked}
                      onSelectDocument={onSelectDocument}
                      onOpenChange={onOpenChange}
                      onRename={onRename}
                      onDelete={onDelete}
                    />
                  )
                })}
              </div>
            </ScrollArea>
          )}
        </div>

        {onBulkDelete && (
          <DeleteConfirmationDialog
            open={showConfirmDelete}
            onOpenChange={setShowConfirmDelete}
            count={checkedDocIds.length}
            onConfirm={() => onBulkDelete(checkedDocIds)}
            isDeleting={isBulkDeleting}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}

