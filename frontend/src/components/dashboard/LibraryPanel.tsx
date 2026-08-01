import React, { useState } from "react"
import { Button } from "../ui/button"
import { Input } from "../ui/input"
import { ScrollArea } from "../ui/scroll-area"
import { Checkbox } from "../ui/checkbox"
import { Loader2, Search, Trash2 } from "lucide-react"
import type { DocumentInfo } from "../../api"
import type { FailedUpload } from "./useDocumentUploads"
import { FailedDocumentRow } from "./FailedDocumentRow"
import { LibraryDocumentRow } from "./LibraryDocumentRow"
import { DeleteConfirmationDialog } from "./DeleteConfirmationDialog"

export { FailedDocumentRow } from "./FailedDocumentRow"

type LibraryPanelProps = {
  documents: DocumentInfo[]
  filteredDocuments: DocumentInfo[]
  loading: boolean
  searchQuery: string
  onSearchQueryChange: (value: string) => void
  selectedDocId: string | null
  onSelectDocument: (docId: string) => void
  checkedDocIds: string[]
  onToggleChecked: (docId: string, checked: boolean, shiftKey?: boolean) => void
  onSelectAll: (docIds: string[]) => void
  onBulkDelete: (ids: string[]) => void
  isBulkDeleting?: boolean
  failedUploads: FailedUpload[]
  onResume: (docId: string) => void
  onDismissFailure: (id: string) => void
  onOpenLibraryDialog: () => void
  onRename: (docId: string, newName: string) => void
  onDelete: (docId: string) => void
}

export const LibraryPanel: React.FC<LibraryPanelProps> = ({
  documents,
  filteredDocuments,
  loading,
  searchQuery,
  onSearchQueryChange,
  selectedDocId,
  onSelectDocument,
  checkedDocIds,
  onToggleChecked,
  onSelectAll,
  onBulkDelete,
  isBulkDeleting = false,
  failedUploads,
  onResume,
  onDismissFailure,
  onOpenLibraryDialog,
  onRename,
  onDelete,
}) => {
  const [showConfirmDelete, setShowConfirmDelete] = useState(false)

  const readyFilteredIds = filteredDocuments.filter((d) => d.status === "ready").map((d) => d.id)
  const isAllSelected =
    readyFilteredIds.length > 0 && readyFilteredIds.every((id) => checkedDocIds.includes(id))

  const selectedCountInView = readyFilteredIds.filter((id) => checkedDocIds.includes(id)).length

  return (
    <div className="glass p-6 rounded-2xl border-border space-y-4">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-bold text-foreground m-0">Library</h3>
        <Button
          type="button"
          variant="link"
          className="p-0 h-auto text-xs text-primary hover:underline font-semibold"
          onClick={onOpenLibraryDialog}
        >
          See all
        </Button>
      </div>

      {loading ? (
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
              onChange={(e) => onSearchQueryChange(e.target.value)}
            />
          </div>

          {filteredDocuments.length > 0 && (
            <div className="flex items-center justify-between pt-1 pb-1 px-1 border-b border-border/50 text-xs text-muted-foreground">
              <label className="flex items-center gap-2 cursor-pointer select-none">
                <Checkbox
                  checked={isAllSelected}
                  onCheckedChange={() => onSelectAll(readyFilteredIds)}
                  aria-label="Select all documents"
                />
                <span>Select All ({readyFilteredIds.length})</span>
              </label>

              {checkedDocIds.length > 0 && (
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

          {filteredDocuments.length === 0 ? (
            <p className="text-muted-foreground text-xs text-center py-6">No matching documents found.</p>
          ) : (
            <ScrollArea className="max-h-[400px]">
              <div className="space-y-2.5 pr-4">
                {filteredDocuments.map((doc) => {
                  const isSelected = selectedDocId === doc.id
                  const failedUpload = failedUploads.find((item) => item.documentId === doc.id)

                  if (failedUpload || doc.status === "failed") {
                    return (
                      <FailedDocumentRow
                        key={doc.id}
                        doc={doc}
                        layout="row"
                        message={`Upload failed${failedUpload ? `: ${failedUpload.message}` : "."}`}
                        onResume={() => onResume(doc.id)}
                        onDismiss={() => onDismissFailure(failedUpload?.id ?? `doc-${doc.id}`)}
                      />
                    )
                  }

                  return (
                    <LibraryDocumentRow
                      key={doc.id}
                      doc={doc}
                      isSelected={isSelected}
                      isChecked={checkedDocIds.includes(doc.id)}
                      onToggleChecked={onToggleChecked}
                      onSelectDocument={onSelectDocument}
                      onRename={onRename}
                      onDelete={onDelete}
                    />
                  )
                })}
              </div>
            </ScrollArea>
          )}
        </>
      )}

      <DeleteConfirmationDialog
        open={showConfirmDelete}
        onOpenChange={setShowConfirmDelete}
        count={checkedDocIds.length}
        onConfirm={() => onBulkDelete(checkedDocIds)}
        isDeleting={isBulkDeleting}
      />
    </div>
  )
}

