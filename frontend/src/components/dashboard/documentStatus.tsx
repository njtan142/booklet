import { Loader2, AlertCircle, FileCheck } from "lucide-react"
import type { DocumentInfo } from "../../api"

// Status of a library row as the dashboard sees it. A row can look failed
// because of a client-side upload failure that the server does not know about
// yet, so this is deliberately not DocumentInfo["status"].
export type EffectiveStatus = DocumentInfo["status"]

export const DocumentStatusIcon: React.FC<{ status: EffectiveStatus }> = ({ status }) => {
  if (status === "processing" || status === "queued") {
    return <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" aria-hidden="true" />
  }
  if (status === "failed") {
    return <AlertCircle className="h-4 w-4 text-destructive" aria-hidden="true" />
  }
  return <FileCheck className="h-4 w-4 text-emerald-500" aria-hidden="true" />
}

// documentProgressLabel describes where a document is in the pipeline.
//
// compact omits the "pages" unit inside the counter, which is what the library
// modal needs: its cards are half the width of the sidebar rows.
export function documentProgressLabel(doc: DocumentInfo, compact = false): string {
  if (doc.status === "queued") return "Queued..."

  if (doc.status === "processing") {
    const unit = compact ? "" : " pages"
    return doc.split_pages < doc.total_pages
      ? `Splitting (${doc.split_pages}/${doc.total_pages}${unit})...`
      : `Parsing (${doc.parsed_pages}/${doc.total_pages}${unit})...`
  }

  return `${doc.total_pages} pages`
}
