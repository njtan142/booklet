import React from "react"
import type { BookletConfig } from "./BookletConfigPanel"
import { RangeSlider } from "./RangeSlider"
import { SelectField } from "./SelectField"

type ImpositionControlsProps = {
  margin: number
  gutter: number
  paperSize: string
  signatureSize: number
  onConfigChange: (patch: Partial<BookletConfig>) => void
}

const paperSizeOptions = [
  { value: "a4", label: 'A4 Landscape (11.7×8.3")' },
  { value: "letter", label: 'Letter Landscape (11×8.5")' },
  { value: "folio", label: 'Folio Landscape (13×8.5")' },
]

const signatureSizeOptions = [
  { value: "4", label: "4 Pages (1 sheet)" },
  { value: "8", label: "8 Pages (2 sheets)" },
  { value: "12", label: "12 Pages (3 sheets)" },
  { value: "16", label: "16 Pages (4 sheets)" },
]

export const ImpositionControls: React.FC<ImpositionControlsProps> = ({
  margin,
  gutter,
  paperSize,
  signatureSize,
  onConfigChange,
}) => (
  <div className="grid grid-cols-2 lg:grid-cols-4 gap-3 pt-2">
    <RangeSlider
      id="margin-input"
      label="Margins"
      unit="pt"
      value={margin}
      min={0}
      max={72}
      step={1}
      onValueChange={(val) => onConfigChange({ margin: val })}
    />
    <RangeSlider
      id="gutter-input"
      label="Gutter"
      unit="pt"
      value={gutter}
      min={0}
      max={100}
      step={1}
      onValueChange={(val) => onConfigChange({ gutter: val })}
    />
    <SelectField
      id="paper-size-select"
      label="Paper Format"
      value={paperSize}
      options={paperSizeOptions}
      onChange={(val) => onConfigChange({ paperSize: val })}
    />
    <SelectField
      id="signature-size-select"
      label="Signature Size"
      value={signatureSize.toString()}
      options={signatureSizeOptions}
      onChange={(val) => onConfigChange({ signatureSize: parseInt(val) })}
    />
  </div>
)
