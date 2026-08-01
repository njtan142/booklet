import React from "react"
import { Label } from "../ui/label"
import { Select } from "../ui/select"

type SelectFieldProps = {
  id: string
  label: string
  value: string
  options: { value: string; label: string }[]
  onChange: (value: string) => void
}

export const SelectField: React.FC<SelectFieldProps> = ({
  id,
  label,
  value,
  options,
  onChange,
}) => (
  <div className="space-y-0.5">
    <Label htmlFor={id} className="text-[10px] font-semibold text-muted-foreground uppercase">
      {label}
    </Label>
    <Select
      id={id}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className="h-7 text-xs py-0"
    >
      {options.map((option) => (
        <option key={option.value} value={option.value}>
          {option.label}
        </option>
      ))}
    </Select>
  </div>
)
