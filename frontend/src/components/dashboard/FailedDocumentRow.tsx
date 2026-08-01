import React from "react"
import { Button } from "../ui/button"
import { Card } from "../ui/card"
import { FileText } from "lucide-react"
import type { DocumentInfo } from "../../api"

export type FailedDocumentRowProps = {
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
