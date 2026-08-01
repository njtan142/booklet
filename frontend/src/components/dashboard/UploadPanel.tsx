import React from "react"
import { Button } from "../ui/button"
import { Input } from "../ui/input"
import { UploadCloud, Loader2, X } from "lucide-react"
import type { FailedUpload } from "./useDocumentUploads"

type UploadPanelProps = {
  inFlightUploads: { id: string; fileName: string }[]
  failedUploads: FailedUpload[]
  onFilesSelected: (files: FileList) => void
  onDismissFailure: (id: string) => void
}

export const UploadPanel: React.FC<UploadPanelProps> = ({
  inFlightUploads,
  failedUploads,
  onFilesSelected,
  onDismissFailure,
}) => {
  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files
    if (files && files.length > 0) {
      onFilesSelected(files)
    }
  }

  return (
    <div className="glass p-6 rounded-2xl border-border space-y-4">
      <h3 className="text-lg font-bold text-foreground m-0">Upload Document</h3>

      <div className="relative border-2 border-dashed border-border rounded-xl p-8 flex flex-col items-center justify-center gap-2 hover:border-primary/50 transition-all bg-background/40 group">
        <UploadCloud className="h-10 w-10 text-muted-foreground group-hover:text-primary transition-colors" aria-hidden="true" />
        <span className="text-muted-foreground text-xs font-medium">Drag &amp; drop your PDF file(s) or click to browse</span>
        <Input
          id="pdf-file-upload"
          type="file"
          accept=".pdf"
          multiple={true}
          className="absolute inset-0 w-full h-full opacity-0 cursor-pointer"
          onChange={handleFileChange}
          aria-label="Upload PDF documents"
        />
      </div>

      {inFlightUploads.length > 0 && (
        <div className="space-y-2">
          {inFlightUploads.map((upload) => (
            <div key={upload.id} className="flex items-center gap-2 text-xs text-muted-foreground bg-muted/60 p-3 rounded-lg border border-border">
              <Loader2 className="h-3.5 w-3.5 animate-spin text-primary" aria-hidden="true" />
              <span className="truncate">Uploading {upload.fileName}...</span>
            </div>
          ))}
        </div>
      )}

      {failedUploads.length > 0 && (
        <div className="space-y-2">
          {failedUploads.map((failure) => (
            <div
              key={failure.id}
              className="flex items-start justify-between gap-3 rounded-xl border border-destructive/25 bg-destructive/10 p-3 text-xs text-destructive"
            >
              <div className="min-w-0 space-y-0.5">
                <p className="font-semibold truncate m-0">{failure.fileName}</p>
                <p className="text-destructive/80 leading-relaxed m-0">{failure.message}</p>
              </div>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="shrink-0 h-7 w-7 text-destructive hover:bg-destructive/15 hover:text-destructive"
                onClick={() => onDismissFailure(failure.id)}
                aria-label={`Dismiss failed upload for ${failure.fileName}`}
              >
                <X className="h-4 w-4" aria-hidden="true" />
              </Button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
