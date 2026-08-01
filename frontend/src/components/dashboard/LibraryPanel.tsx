import React from "react"
import { Button } from "../ui/button"
import { Card } from "../ui/card"
import { Checkbox } from "../ui/checkbox"
import { Input } from "../ui/input"
import { ScrollArea } from "../ui/scroll-area"
import { FileText, Loader2, Search } from "lucide-react"
import type { DocumentInfo } from "../../api"
import type { FailedUpload } from "./useDocumentUploads"
import { DocumentStatusIcon, documentProgressLabel } from "./documentStatus"

type FailedDocumentRowProps = {
  doc: DocumentInfo
  message: string
  layout: "row" | "card"
  onResume: () => void
  onDismiss: () => void
}

// A document that failed offers Resume and Dismiss instead of selection: it has
// no pages to act on, so neither the booklet panel nor a tool can use it.
export const FailedDocumentRow: React.FC<FailedDocumentRowProps> = ({
  doc,
  message,
  layout,
  onResume,
  onDismiss,
}) => (
  <div
    className={
      layout === "row"
        ? "w-full text-left h-auto p-3.5 rounded-xl border flex items-center justify-between gap-4 bg-destructive/10 border-destructive/25"
        : "w-full text-left p-3.5 rounded-xl border flex flex-col justify-between gap-3 bg-destructive/10 border-destructive/25"
    }
  >
    <div className={`flex gap-3 min-w-0 ${layout === "row" ? "items-center" : "items-start"}`}>
      <Card className="p-2 rounded-lg bg-destructive/15 text-destructive border-none shadow-none shrink-0">
        <FileText className="h-4 w-4" aria-hidden="true" />
      </Card>
      <div className="min-w-0">
        <h4 className="text-xs font-bold text-foreground truncate m-0" title={doc.name}>
          {doc.name}
        </h4>
        <p className="text-[10px] text-destructive/80 mt-0.5 leading-normal">{message}</p>
      </div>
    </div>

    <div className={`flex items-center gap-1.5 shrink-0 ${layout === "card" ? "self-end" : ""}`}>
      <Button type="button" variant="outline" size="sm" className="h-8 text-[11px]" onClick={onResume}>
        Resume
      </Button>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="h-8 text-[11px] text-destructive hover:text-destructive hover:bg-destructive/15"
        onClick={onDismiss}
      >
        Dismiss
      </Button>
    </div>
  </div>
)

type LibraryPanelProps = {
  documents: DocumentInfo[]
  filteredDocuments: DocumentInfo[]
  loading: boolean
  searchQuery: string
  onSearchQueryChange: (value: string) => void
  selectedDocId: string | null
  onSelectDocument: (docId: string) => void
  checkedDocIds: string[]
  onToggleChecked: (docId: string, checked: boolean) => void
  failedUploads: FailedUpload[]
  onResume: (docId: string) => void
  onDismissFailure: (id: string) => void
  onOpenLibraryDialog: () => void
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
  failedUploads,
  onResume,
  onDismissFailure,
  onOpenLibraryDialog,
}) => (
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

                const isBusy = doc.status === "processing" || doc.status === "queued"

                return (
                  <div key={doc.id} className="flex items-center gap-2">
                    {/* Sibling of the row button, not a child: nesting an
                        interactive control inside a button is invalid and the
                        click would be swallowed by the row. */}
                    <Checkbox
                      id={`select-${doc.id}`}
                      checked={checkedDocIds.includes(doc.id)}
                      onCheckedChange={(checked) => onToggleChecked(doc.id, checked === true)}
                      disabled={doc.status !== "ready"}
                      aria-label={`Select ${doc.name} for a tool`}
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      onClick={() => doc.status === "ready" && onSelectDocument(doc.id)}
                      disabled={doc.status !== "ready"}
                      className={`flex-1 min-w-0 text-left h-auto p-3.5 rounded-xl border flex items-center justify-between gap-4 cursor-pointer transition-all whitespace-normal ${
                        isSelected
                          ? "bg-primary/10 border-primary/30"
                          : isBusy
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
                            {documentProgressLabel(doc)}
                          </p>
                        </div>
                      </div>

                      <div>
                        <DocumentStatusIcon status={doc.status} />
                      </div>
                    </Button>
                  </div>
                )
              })}
            </div>
          </ScrollArea>
        )}
      </>
    )}
  </div>
)
