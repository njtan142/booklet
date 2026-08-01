import React from "react"
import { Label } from "../ui/label"
import { Select } from "../ui/select"
import { Slider } from "../ui/slider"
import type { BookletConfig } from "./BookletConfigPanel"

type ImpositionControlsProps = {
  margin: number
  gutter: number
  paperSize: string
  signatureSize: number
  onConfigChange: (patch: Partial<BookletConfig>) => void
}

export const ImpositionControls: React.FC<ImpositionControlsProps> = ({
  margin,
  gutter,
  paperSize,
  signatureSize,
  onConfigChange,
}) => (
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
)
