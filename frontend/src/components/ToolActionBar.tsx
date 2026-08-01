import React, { useEffect, useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { api, selectionAllowsTool } from "../api"
import type { DocumentInfo, Job, Tool } from "../api"
import { Button } from "./ui/button"
import { Card } from "./ui/card"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "./ui/dropdown-menu"
import { AlertCircle, Loader2, Wrench, X } from "lucide-react"

type ToolActionBarProps = {
  selection: DocumentInfo[]
  onClear: () => void
}

type FinishedJob = {
  id: string
  toolSlug: string
  status: "completed" | "failed"
  error?: string
}

// requiresConfiguration reports whether a tool needs parameters the action bar
// cannot supply. Stage 1.7 only enqueues zero-config tools; the per-schema
// ToolDialog that collects rotate angles, page ranges and passwords arrives
// with the Stage 2 tools that actually need them.
function requiresConfiguration(tool: Tool): boolean {
  return tool.params.some((p) => p.required)
}

export const ToolActionBar: React.FC<ToolActionBarProps> = ({ selection, onClear }) => {
  const queryClient = useQueryClient()
  const [activeJobIds, setActiveJobIds] = useState<string[]>([])
  const [finishedJobs, setFinishedJobs] = useState<FinishedJob[]>([])
  const [submitError, setSubmitError] = useState<string>("")

  // The catalog only lists tools that are implemented and whose engine is
  // reachable, so it is safe to offer everything it returns.
  const { data: tools, isLoading: loadingTools } = useQuery({
    queryKey: ["tools"],
    queryFn: api.listTools,
    staleTime: 5 * 60 * 1000,
  })

  // One poll for every in-flight job rather than a query per job, so the
  // interval does not multiply with the number of running jobs.
  const { data: activeJobs } = useQuery({
    queryKey: ["toolJobs", "active", activeJobIds],
    queryFn: () => Promise.all(activeJobIds.map((id) => api.getToolJob(id))),
    enabled: activeJobIds.length > 0,
    refetchInterval: 2000,
  })

  useEffect(() => {
    if (!activeJobs || activeJobs.length === 0) return

    const settled = activeJobs.filter(
      (job): job is Job & { status: "completed" | "failed" } =>
        job.status === "completed" || job.status === "failed"
    )
    if (settled.length === 0) return

    setActiveJobIds((current) => current.filter((id) => !settled.some((job) => job.id === id)))
    setFinishedJobs((current) => [
      ...current,
      ...settled.map((job) => ({
        id: job.id,
        toolSlug: job.tool_slug,
        status: job.status,
        error: job.error,
      })),
    ])

    // A completed tool job inserts a derived document, so the library is stale.
    if (settled.some((job) => job.status === "completed")) {
      queryClient.invalidateQueries({ queryKey: ["documents"] })
    }
  }, [activeJobs, queryClient])

  const runTool = async (tool: Tool) => {
    setSubmitError("")
    try {
      const { job_id } = await api.createToolJob(
        tool.slug,
        selection.map((doc) => doc.id)
      )
      setActiveJobIds((current) => [...current, job_id])
      onClear()
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : "Failed to start the tool")
    }
  }

  const runningCount = activeJobIds.length
  const hasNothingToShow =
    selection.length === 0 && runningCount === 0 && finishedJobs.length === 0 && !submitError
  if (hasNothingToShow) return null

  const availableTools = tools ?? []

  return (
    <div className="fixed bottom-6 left-1/2 z-40 w-[min(38rem,calc(100vw-2rem))] -translate-x-1/2 space-y-2">
      {submitError && (
        <div className="glass flex items-start gap-2 rounded-xl border border-destructive/25 bg-destructive/10 p-3 text-xs text-destructive">
          <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden="true" />
          <span className="min-w-0 flex-1">{submitError}</span>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="h-6 w-6 shrink-0 text-destructive hover:bg-destructive/15 hover:text-destructive"
            onClick={() => setSubmitError("")}
            aria-label="Dismiss error"
          >
            <X className="h-3.5 w-3.5" aria-hidden="true" />
          </Button>
        </div>
      )}

      {finishedJobs.map((job) => (
        <div
          key={job.id}
          className={`glass flex items-start gap-2 rounded-xl border p-3 text-xs ${
            job.status === "completed"
              ? "border-emerald-500/25 bg-emerald-500/10 text-emerald-600"
              : "border-destructive/25 bg-destructive/10 text-destructive"
          }`}
        >
          <span className="min-w-0 flex-1">
            {job.status === "completed"
              ? `${job.toolSlug} finished. The result is in your library.`
              : `${job.toolSlug} failed: ${job.error || "unknown error"}`}
          </span>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="h-6 w-6 shrink-0"
            onClick={() => setFinishedJobs((current) => current.filter((j) => j.id !== job.id))}
            aria-label="Dismiss job result"
          >
            <X className="h-3.5 w-3.5" aria-hidden="true" />
          </Button>
        </div>
      ))}

      {runningCount > 0 && (
        <div className="glass flex items-center gap-2 rounded-xl border border-border p-3 text-xs text-muted-foreground">
          <Loader2 className="h-3.5 w-3.5 animate-spin text-primary" aria-hidden="true" />
          <span>
            Running {runningCount} tool job{runningCount === 1 ? "" : "s"}...
          </span>
        </div>
      )}

      {selection.length > 0 && (
        <Card className="glass flex flex-row items-center justify-between gap-3 rounded-2xl border-border p-3 shadow-lg">
          <span className="pl-1 text-xs font-bold text-foreground">
            {selection.length} document{selection.length === 1 ? "" : "s"} selected
          </span>

          <div className="flex items-center gap-1.5">
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button type="button" size="sm" className="h-8 gap-1.5 text-xs font-bold">
                  <Wrench className="h-3.5 w-3.5" aria-hidden="true" />
                  Tools
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-64">
                <DropdownMenuLabel className="text-xs">
                  Apply to {selection.length} document{selection.length === 1 ? "" : "s"}
                </DropdownMenuLabel>
                <DropdownMenuSeparator />

                {loadingTools ? (
                  <div className="flex items-center gap-2 px-2 py-3 text-xs text-muted-foreground">
                    <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
                    Loading tools...
                  </div>
                ) : availableTools.length === 0 ? (
                  <p className="px-2 py-3 text-xs text-muted-foreground">
                    No tools are available yet.
                  </p>
                ) : (
                  availableTools.map((tool) => {
                    const fitsSelection = selectionAllowsTool(tool, selection)
                    const needsConfig = requiresConfiguration(tool)
                    return (
                      <DropdownMenuItem
                        key={tool.slug}
                        disabled={!fitsSelection || needsConfig}
                        onSelect={() => runTool(tool)}
                        className="flex-col items-start gap-0.5"
                      >
                        <span className="text-xs font-semibold">{tool.label}</span>
                        <span className="text-[10px] text-muted-foreground">
                          {!fitsSelection
                            ? describeMismatch(tool, selection)
                            : needsConfig
                              ? "Needs configuration"
                              : tool.description}
                        </span>
                      </DropdownMenuItem>
                    )
                  })
                )}
              </DropdownMenuContent>
            </DropdownMenu>

            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-8 text-xs"
              onClick={onClear}
            >
              Clear
            </Button>
          </div>
        </Card>
      )}
    </div>
  )
}

// describeMismatch explains why a tool is greyed out, so the menu does not
// present an unexplained disabled row.
function describeMismatch(tool: Tool, selection: DocumentInfo[]): string {
  if (selection.length < tool.min_inputs) {
    return `Needs at least ${tool.min_inputs} document${tool.min_inputs === 1 ? "" : "s"}`
  }
  if (tool.max_inputs > 0 && selection.length > tool.max_inputs) {
    return `Accepts at most ${tool.max_inputs} document${tool.max_inputs === 1 ? "" : "s"}`
  }
  return `Only accepts: ${tool.input_kinds.join(", ")}`
}
