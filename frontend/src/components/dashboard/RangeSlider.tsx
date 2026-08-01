import React from "react"
import { Label } from "../ui/label"
import { Slider } from "../ui/slider"

type RangeSliderProps = {
  id: string
  label: string
  unit: string
  value: number
  min: number
  max: number
  step: number
  onValueChange: (value: number) => void
}

export const RangeSlider: React.FC<RangeSliderProps> = ({
  id,
  label,
  unit,
  value,
  min,
  max,
  step,
  onValueChange,
}) => (
  <div className="space-y-1">
    <Label htmlFor={id} className="text-[10px] font-semibold text-muted-foreground uppercase">
      {label}: <span className="text-foreground font-bold">{value}{unit}</span>
    </Label>
    <Slider
      id={id}
      min={min}
      max={max}
      step={step}
      value={[value]}
      onValueChange={(val) => onValueChange(val[0])}
      className="w-full pt-1.5 cursor-pointer"
    />
  </div>
)
