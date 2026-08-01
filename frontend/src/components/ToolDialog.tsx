import React, { useEffect, useState } from "react"
import { Button } from "./ui/button"
import { Checkbox } from "./ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "./ui/dialog"
import { Input } from "./ui/input"
import { Label } from "./ui/label"
import { Select } from "./ui/select"
import type { DocumentInfo, Tool, ToolParam } from "../api"

type ToolDialogProps = {
  tool: Tool
  selection: DocumentInfo[]
  open: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (params: Record<string, unknown>) => void
}

// initialValues seeds state from the catalog's declared defaults.
//
// Seeding matters: a select rendered from `param.default` would otherwise look
// filled in while state stayed empty, so the required check would reject a form
// the user can see is complete.
function initialValues(tool: Tool): Record<string, unknown> {
  const seeded: Record<string, unknown> = {}
  for (const param of tool.params) {
    seeded[param.name] = param.default ?? (param.type === "bool" ? false : "")
  }
  return seeded
}

export const ToolDialog: React.FC<ToolDialogProps> = ({
  tool,
  selection,
  open,
  onOpenChange,
  onSubmit,
}) => {
  const [values, setValues] = useState<Record<string, unknown>>(() => initialValues(tool))
  const [errors, setErrors] = useState<Record<string, string>>({})

  // Reopening the dialog, or switching tools without unmounting, must not carry
  // the previous run's values over.
  useEffect(() => {
    if (open) {
      setValues(initialValues(tool))
      setErrors({})
    }
  }, [open, tool])

  const setValue = (name: string, value: unknown) => {
    setValues((current) => ({ ...current, [name]: value }))
    setErrors((current) => {
      if (!current[name]) return current
      const next = { ...current }
      delete next[name]
      return next
    })
  }

  const handleSubmit = () => {
    const found: Record<string, string> = {}
    for (const param of tool.params) {
      const value = values[param.name]
      if (param.required && (value === undefined || value === null || value === "")) {
        found[param.name] = `${param.label} is required`
      }
    }
    if (Object.keys(found).length > 0) {
      setErrors(found)
      return
    }

    // Empty optionals are dropped rather than sent as "", which a backend
    // validator would have to special-case as "absent" on every tool.
    const params: Record<string, unknown> = {}
    for (const param of tool.params) {
      const value = values[param.name]
      if (value === undefined || value === null || value === "") continue
      params[param.name] = value
    }

    onSubmit(params)
    onOpenChange(false)
  }

  const renderParam = (param: ToolParam) => {
    const value = values[param.name] ?? ""
    const error = errors[param.name]

    const label = (
      <Label htmlFor={param.name} className="text-xs">
        {param.label}
        {param.required && (
          <span className="ml-1 text-destructive" aria-hidden="true">
            *
          </span>
        )}
      </Label>
    )

    const footnote = (
      <>
        {param.help && !error && (
          <p className="text-[10px] text-muted-foreground">{param.help}</p>
        )}
        {error && (
          <p className="text-[10px] text-destructive" role="alert">
            {error}
          </p>
        )}
      </>
    )

    if (param.type === "bool") {
      return (
        <div key={param.name} className="space-y-1.5">
          <div className="flex items-center gap-2">
            <Checkbox
              id={param.name}
              checked={value === true}
              onCheckedChange={(checked) => setValue(param.name, checked === true)}
            />
            <Label htmlFor={param.name} className="text-xs font-normal">
              {param.label}
            </Label>
          </div>
          {footnote}
        </div>
      )
    }

    if (param.type === "enum") {
      return (
        <div key={param.name} className="space-y-1.5">
          {label}
          <Select
            id={param.name}
            value={String(value)}
            onChange={(event) => setValue(param.name, event.target.value)}
            aria-invalid={!!error}
          >
            <option value="" disabled>
              Select {param.label.toLowerCase()}
            </option>
            {param.options?.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </Select>
          {footnote}
        </div>
      )
    }

    if (param.type === "int") {
      return (
        <div key={param.name} className="space-y-1.5">
          {label}
          <Input
            id={param.name}
            type="number"
            min={param.min}
            max={param.max}
            value={String(value)}
            onChange={(event) =>
              setValue(
                param.name,
                event.target.value === "" ? "" : Number(event.target.value)
              )
            }
            aria-invalid={!!error}
          />
          {footnote}
        </div>
      )
    }

    return (
      <div key={param.name} className="space-y-1.5">
        {label}
        <Input
          id={param.name}
          type={param.type === "password" ? "password" : "text"}
          value={String(value)}
          onChange={(event) => setValue(param.name, event.target.value)}
          placeholder={param.type === "page_range" ? "All pages" : undefined}
          aria-invalid={!!error}
        />
        {footnote}
      </div>
    )
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="text-base">{tool.label}</DialogTitle>
          <DialogDescription className="text-xs">
            {tool.description} Applying to {selection.length} document
            {selection.length === 1 ? "" : "s"}.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">{tool.params.map(renderParam)}</div>

        <DialogFooter>
          <Button type="button" variant="outline" size="sm" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button type="button" size="sm" onClick={handleSubmit}>
            Run {tool.label}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
