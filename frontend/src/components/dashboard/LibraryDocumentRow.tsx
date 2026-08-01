import React, { useState } from "react"
import { Button } from "../ui/button"
import { Checkbox } from "../ui/checkbox"
import { Input } from "../ui/input"
import { FileText, Pencil, Trash2 } from "lucide-react"
import type { DocumentInfo } from "../../api"
import { DocumentStatusIcon, documentProgressLabel } from "./documentStatus"

export type LibraryDocumentRowProps = {
  doc: DocumentInfo
  isSelected: boolean
  isChecked: boolean
  onToggleChecked: (docId: string, checked: boolean) => void
  onSelectDocument: (docId: string) => void
  onRename: (docId: string, newName: string) => void
  onDelete: (docId: string) => void
}

export const LibraryDocumentRow: React.FC<LibraryDocumentRowProps> = ({
  doc,
  isSelected,
  isChecked,
  onToggleChecked,
  onSelectDocument,
  onRename,
  onDelete,
}) => {
  const [isRenaming, setIsRenaming] = useState(false)
  const [renameInput, setRenameInput] = useState("")

  const startRename = () => {
    setIsRenaming(true)
    setRenameInput(doc.name)
  }

  const commitRename = () => {
    const trimmed = renameInput.trim()
    if (trimmed && trimmed !== doc.name) {
      onRename(doc.id, trimmed)
    }
    setIsRenaming(false)
    setRenameInput("")
  }

  const handleRenameKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      e.preventDefault()
      commitRename()
    } else if (e.key === "Escape") {
      e.preventDefault()
      setIsRenaming(false)
      setRenameInput("")
    }
  }

  const handleDelete = () => {
    if (window.confirm(`Delete "${doc.name}"? This cannot be undone.`)) {
      onDelete(doc.id)
    }
  }

  const isBusy = doc.status === "processing" || doc.status === "queued"

  return (
    <div className="flex items-center gap-2 group">
      {/* Sibling of the row button, not a child: nesting an
          interactive control inside a button is invalid and the
          click would be swallowed by the row. */}
      <Checkbox
        id={`select-${doc.id}`}
        checked={isChecked}
        onCheckedChange={(checked) => onToggleChecked(doc.id, checked === true)}
        disabled={doc.status !== "ready"}
        aria-label={`Select ${doc.name} for a tool`}
      />
      <Button
        type="button"
        variant="ghost"
        onClick={() => doc.status === "ready" && onSelectDocument(doc.id)}
        disabled={doc.status !== "ready"}
        className={`flex-1 min-w-0 text-left h-auto p-3.5 rounded-xl border flex items-center justify-between gap-4 transition-all whitespace-normal ${
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
            {isRenaming ? (
              <Input
                type="text"
                className="h-5 text-xs"
                value={renameInput}
                onChange={(e) => setRenameInput(e.target.value)}
                onBlur={commitRename}
                onKeyDown={handleRenameKeyDown}
                autoFocus
                onClick={(e) => e.stopPropagation()}
              />
            ) : (
              <>
                <h4 className="text-xs font-bold text-foreground truncate m-0" title={doc.name}>
                  {doc.name}
                </h4>
                <p className="text-[10px] text-muted-foreground mt-0.5">
                  {documentProgressLabel(doc)}
                </p>
              </>
            )}
          </div>
        </div>

        <div>
          <DocumentStatusIcon status={doc.status} />
        </div>
      </Button>

      <div className="flex items-center gap-1 shrink-0 opacity-0 group-hover:opacity-100 transition-opacity">
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="h-7 w-7"
          onClick={startRename}
          disabled={isBusy}
          aria-label={`Rename ${doc.name}`}
        >
          <Pencil className="h-3.5 w-3.5" />
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="h-7 w-7 text-destructive hover:text-destructive hover:bg-destructive/15"
          onClick={handleDelete}
          disabled={isBusy}
          aria-label={`Delete ${doc.name}`}
        >
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
      </div>
    </div>
  )
}
