import React from "react"
import type { DocumentDetail } from "../../api"
import { NoDocumentSelected } from "./NoDocumentSelected"
import { DocumentDetailsHeader } from "./DocumentDetailsHeader"
import { BookletPreview } from "./BookletPreview"
import { ImpositionControls } from "./ImpositionControls"
import { CompileActions } from "./CompileActions"

export type BookletConfig = {
  margin: number
  gutter: number
  paperSize: string
  signatureSize: number
  guides: boolean
}

type BookletConfigPanelProps = {
  documentId: string | null
  docDetail: DocumentDetail | undefined
  config: BookletConfig
  onConfigChange: (patch: Partial<BookletConfig>) => void
  previewSide: "front" | "back"
  onPreviewSideChange: (side: "front" | "back") => void
  compiling: boolean
  compileStatus: string
  onCompile: () => void
}

export const BookletConfigPanel: React.FC<BookletConfigPanelProps> = ({
  documentId,
  docDetail,
  config,
  onConfigChange,
  previewSide,
  onPreviewSideChange,
  compiling,
  compileStatus,
  onCompile,
}) => {
  if (!documentId || !docDetail) {
    return <NoDocumentSelected />
  }

  const { margin, gutter, paperSize, signatureSize, guides } = config

  return (
    <div className="glass p-6 md:p-8 rounded-2xl border-border space-y-6">
      <DocumentDetailsHeader docDetail={docDetail} />

      <div className="border-t border-border pt-4 space-y-3">
        <BookletPreview
          documentId={documentId}
          margin={margin}
          gutter={gutter}
          paperSize={paperSize}
          signatureSize={signatureSize}
          guides={guides}
          previewSide={previewSide}
          onPreviewSideChange={onPreviewSideChange}
        />

        <ImpositionControls
          margin={margin}
          gutter={gutter}
          paperSize={paperSize}
          signatureSize={signatureSize}
          onConfigChange={onConfigChange}
        />

        <CompileActions
          guides={guides}
          compiling={compiling}
          compileStatus={compileStatus}
          onConfigChange={onConfigChange}
          onCompile={onCompile}
        />
      </div>
    </div>
  )
}
