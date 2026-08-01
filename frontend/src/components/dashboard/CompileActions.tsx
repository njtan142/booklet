import React from "react"
import { Button } from "../ui/button"
import { Checkbox } from "../ui/checkbox"
import { Label } from "../ui/label"
import { AlertCircle, Loader2, Printer } from "lucide-react"
import type { BookletConfig } from "./BookletConfigPanel"

type CompileActionsProps = {
  guides: boolean
  compiling: boolean
  compileStatus: string
  onConfigChange: (patch: Partial<BookletConfig>) => void
  onCompile: () => void
}

export const CompileActions: React.FC<CompileActionsProps> = ({
  guides,
  compiling,
  compileStatus,
  onConfigChange,
  onCompile,
}) => (
  <>
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
  </>
)
