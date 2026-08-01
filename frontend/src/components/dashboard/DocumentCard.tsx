import React, { useState } from "react"
import { Button } from "../ui/button"
import { Checkbox } from "../ui/checkbox"
import { Input } from "../ui/input"
import { FileText, Pencil, Trash2 } from "lucide-react"
import type { DocumentInfo } from "../../api"
import { documentProgressLabel } from "./documentStatus"

type DocumentCardProps = {
  doc: DocumentInfo
  isSelected: boolean
  isChecked?: boolean
  onToggleChecked?: (docId: string, checked: boolean, shiftKey?: boolean) => void
  onSelectDocument: (docId: string) => void
  onOpenChange: (open: boolean) => void
  onRename: (docId: string, newName: string) => void
  onDelete: (docId: string) => void
}

export const DocumentCard: React.FC<DocumentCardProps> = ({
  doc,
  isSelected,
  isChecked = false,
  onToggleChecked,
  onSelectDocument,
  onOpenChange,
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

  const handleCardClick = (e: React.MouseEvent) => {
    if (doc.status !== "ready") return
    if (e.shiftKey && onToggleChecked) {
      onToggleChecked(doc.id, true, true)
    } else {
      onSelectDocument(doc.id)
      onOpenChange(false)
    }
  }

  return (
    <div className="relative group">
      <Button
        type="button"
        variant="ghost"
        onClick={handleCardClick}
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
          {onToggleChecked && (
            <Checkbox
              id={`select-card-${doc.id}`}
              checked={isChecked}
              onCheckedChange={(checked) => onToggleChecked(doc.id, checked === true)}
              onClick={(e) => {
                e.stopPropagation()
                if (e.shiftKey) {
                  onToggleChecked(doc.id, true, true)
                }
              }}
              disabled={doc.status !== "ready"}
              aria-label={`Select ${doc.name}`}
            />
          )}
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
                onBlur={commitRename}
                onKeyDown={handleRenameKeyDown}
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
          onClick={startRename}
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
          onClick={handleDelete}
          disabled={isBusy}
          aria-label={`Delete ${doc.name}`}
        >
          <Trash2 className="h-3 w-3" />
        </Button>
      </div>
    </div>
  )
}
