import React from "react"
import { api } from "../../api"
import { Button } from "../ui/button"
import { Card } from "../ui/card"
import { PDFPageRenderer } from "../PDFPageRenderer"
import { Settings } from "lucide-react"

type BookletPreviewProps = {
  documentId: string
  margin: number
  gutter: number
  paperSize: string
  signatureSize: number
  guides: boolean
  previewSide: "front" | "back"
  onPreviewSideChange: (side: "front" | "back") => void
}

export const BookletPreview: React.FC<BookletPreviewProps> = ({
  documentId,
  margin,
  gutter,
  paperSize,
  signatureSize,
  guides,
  previewSide,
  onPreviewSideChange,
}) => (
  <>
    <div className="flex items-center justify-between border-b border-border/40 pb-2">
      <div className="flex items-center gap-2">
        <Settings className="h-4 w-4 text-muted-foreground" aria-hidden="true" />
        <h3 className="text-sm font-bold text-foreground m-0">Booklet Imposition Config</h3>
      </div>
      <div className="flex bg-muted p-0.5 rounded border border-border text-[9px] font-bold">
        {(["front", "back"] as const).map((side) => (
          <Button
            key={side}
            type="button"
            variant="ghost"
            onClick={() => onPreviewSideChange(side)}
            className={`px-1.5 py-0.5 h-6 rounded text-[9px] font-bold transition-all ${
              previewSide === side
                ? "bg-background text-foreground shadow-sm hover:bg-background"
                : "text-muted-foreground hover:text-foreground hover:bg-transparent"
            }`}
          >
            {side === "front" ? "Front Side" : "Back Side"}
          </Button>
        ))}
      </div>
    </div>

    {/* Looks like paper: square corners and a drop shadow. */}
    <Card className="relative aspect-[1.5/1] w-full bg-white border border-neutral-300 shadow-[0_6px_16px_rgba(0,0,0,0.12)] flex items-center justify-center overflow-hidden rounded-none">
      <PDFPageRenderer
        url={api.getBookletPreviewUrl(
          documentId,
          margin,
          gutter,
          paperSize,
          signatureSize,
          guides,
          previewSide
        )}
        className="w-full h-full"
        rotation={0}
      />
    </Card>
  </>
)
