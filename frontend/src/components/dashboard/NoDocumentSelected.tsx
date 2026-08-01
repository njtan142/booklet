import React from "react"
import { FileText } from "lucide-react"

export const NoDocumentSelected: React.FC = () => (
  <div className="glass h-[400px] rounded-2xl border-border flex flex-col items-center justify-center text-center p-6">
    <FileText className="h-16 w-16 text-muted-foreground animate-pulse" aria-hidden="true" />
    <h3 className="text-base font-bold text-foreground mt-4">No Document Selected</h3>
    <p className="text-muted-foreground text-xs mt-1.5 max-w-xs leading-relaxed">
      Select an uploaded document from the library panel or drop a new PDF file to configure your
      booklet imposition parameters.
    </p>
  </div>
)
