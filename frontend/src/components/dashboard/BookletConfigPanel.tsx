import React from "react"
import { api } from "../../api"
import type { DocumentDetail } from "../../api"
import { Button } from "../ui/button"
import { Card } from "../ui/card"
import { Checkbox } from "../ui/checkbox"
import { Label } from "../ui/label"
import { Select } from "../ui/select"
import { Slider } from "../ui/slider"
import { PDFPageRenderer } from "../PDFPageRenderer"
import { AlertCircle, FileText, Loader2, Printer, Settings } from "lucide-react"

// BookletConfig is the imposition parameter set shared by the preview URL, the
// compile call and the cleanup call, so the three can never drift apart.
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
    return (
      <div className="glass h-[400px] rounded-2xl border-border flex flex-col items-center justify-center text-center p-6">
        <FileText className="h-16 w-16 text-muted-foreground animate-pulse" aria-hidden="true" />
        <h3 className="text-base font-bold text-foreground mt-4">No Document Selected</h3>
        <p className="text-muted-foreground text-xs mt-1.5 max-w-xs leading-relaxed">
          Select an uploaded document from the library panel or drop a new PDF file to configure your
          booklet imposition parameters.
        </p>
      </div>
    )
  }

  const { margin, gutter, paperSize, signatureSize, guides } = config

  return (
    <div className="glass p-6 md:p-8 rounded-2xl border-border space-y-6">
      <div>
        <span className="text-[10px] uppercase font-bold text-primary tracking-wider">Document Details</span>
        <h2 className="text-xl font-extrabold text-foreground mt-1">{docDetail.name}</h2>
        <p className="text-muted-foreground text-xs mt-1">
          Uploaded {new Date(docDetail.created_at).toLocaleDateString()}
        </p>
      </div>

      <div className="border-t border-border pt-4 space-y-3">
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

        <div className="grid grid-cols-2 lg:grid-cols-4 gap-3 pt-2">
          <div className="space-y-1">
            <Label htmlFor="margin-input" className="text-[10px] font-semibold text-muted-foreground uppercase">
              Margins: <span className="text-foreground font-bold">{margin}pt</span>
            </Label>
            <Slider
              id="margin-input"
              min={0}
              max={72}
              step={1}
              value={[margin]}
              onValueChange={(val) => onConfigChange({ margin: val[0] })}
              className="w-full pt-1.5 cursor-pointer"
            />
          </div>

          <div className="space-y-1">
            <Label htmlFor="gutter-input" className="text-[10px] font-semibold text-muted-foreground uppercase">
              Gutter: <span className="text-foreground font-bold">{gutter}pt</span>
            </Label>
            <Slider
              id="gutter-input"
              min={0}
              max={100}
              step={1}
              value={[gutter]}
              onValueChange={(val) => onConfigChange({ gutter: val[0] })}
              className="w-full pt-1.5 cursor-pointer"
            />
          </div>

          <div className="space-y-0.5">
            <Label htmlFor="paper-size-select" className="text-[10px] font-semibold text-muted-foreground uppercase">
              Paper Format
            </Label>
            <Select
              id="paper-size-select"
              value={paperSize}
              onChange={(e) => onConfigChange({ paperSize: e.target.value })}
              className="h-7 text-xs py-0"
            >
              <option value="a4">A4 Landscape (11.7×8.3")</option>
              <option value="letter">Letter Landscape (11×8.5")</option>
              <option value="folio">Folio Landscape (13×8.5")</option>
            </Select>
          </div>

          <div className="space-y-0.5">
            <Label htmlFor="signature-size-select" className="text-[10px] font-semibold text-muted-foreground uppercase">
              Signature Size
            </Label>
            <Select
              id="signature-size-select"
              value={signatureSize.toString()}
              onChange={(e) => onConfigChange({ signatureSize: parseInt(e.target.value) })}
              className="h-7 text-xs py-0"
            >
              <option value="4">4 Pages (1 sheet)</option>
              <option value="8">8 Pages (2 sheets)</option>
              <option value="12">12 Pages (3 sheets)</option>
              <option value="16">16 Pages (4 sheets)</option>
            </Select>
          </div>
        </div>

        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 pt-2 border-t border-border/30">
          <div className="flex items-center gap-2">
            <Checkbox
              id="guides-checkbox"
              checked={guides}
              onCheckedChange={(checked) => onConfigChange({ guides: checked === true })}
            />
            <Label htmlFor="guides-checkbox" className="text-xs font-semibold text-foreground cursor-pointer">
              Draw Folding &amp; Cutting Guides
            </Label>
          </div>

          <Button
            className="sm:w-auto h-8 px-4 font-bold flex items-center justify-center gap-1.5 text-xs shadow-md shadow-primary/10"
            onClick={onCompile}
            disabled={compiling}
          >
            <Printer className="h-3.5 w-3.5" aria-hidden="true" />
            Compile &amp; Generate Layout
          </Button>
        </div>

        {compiling && (
          <div className="flex items-center gap-3 bg-background/80 p-3 rounded-xl border border-border">
            <Loader2 className="h-4 w-4 animate-spin text-primary" aria-hidden="true" />
            <div className="text-xs">
              <p className="font-bold text-foreground">Compiling Booklet...</p>
              <p className="text-muted-foreground mt-0.5">{compileStatus}</p>
            </div>
          </div>
        )}

        {!compiling && compileStatus && (
          <div className="p-3 bg-destructive/10 border border-destructive/20 text-destructive rounded-xl text-xs flex items-center gap-2">
            <AlertCircle className="h-3.5 w-3.5" aria-hidden="true" />
            <span>{compileStatus}</span>
          </div>
        )}
      </div>
    </div>
  )
}
