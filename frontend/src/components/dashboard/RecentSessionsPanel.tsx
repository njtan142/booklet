import React from "react"
import { Link } from "@tanstack/react-router"
import { Button } from "../ui/button"
import { Card } from "../ui/card"
import { ScrollArea } from "../ui/scroll-area"
import { Loader2, Printer } from "lucide-react"
import type { BookletListResponse } from "../../api"
import { DocumentStatusIcon } from "./documentStatus"

type RecentSessionsPanelProps = {
  sessions: BookletListResponse[] | undefined
  loading: boolean
  onSelectSession: (session: BookletListResponse) => void
}

export const RecentSessionsPanel: React.FC<RecentSessionsPanelProps> = ({
  sessions,
  loading,
  onSelectSession,
}) => (
  <div className="glass p-6 rounded-2xl border-border space-y-4">
    <div className="flex items-center justify-between">
      <h3 className="text-lg font-bold text-foreground m-0">Recent Print Sessions</h3>
      <Link to="/sessions" className="text-xs text-primary hover:underline font-semibold">
        See all
      </Link>
    </div>

    {loading ? (
      <div className="flex items-center justify-center py-6">
        <Loader2 className="h-5 w-5 animate-spin text-primary" aria-hidden="true" />
      </div>
    ) : !sessions || sessions.length === 0 ? (
      <p className="text-muted-foreground text-xs text-center py-4">No recent print sessions.</p>
    ) : (
      <ScrollArea className="max-h-[300px]">
        <div className="space-y-2.5 pr-4">
          {sessions.map((session) => (
            <Button
              type="button"
              key={session.id}
              variant="ghost"
              onClick={() => onSelectSession(session)}
              className="w-full text-left h-auto p-3.5 rounded-xl border flex items-center justify-between gap-4 cursor-pointer transition-all whitespace-normal bg-background/60 border-border hover:border-primary/25"
            >
              <div className="flex items-center gap-3 min-w-0">
                <Card className="p-2 rounded-lg bg-muted text-muted-foreground border-none shadow-none">
                  <Printer className="h-4 w-4" aria-hidden="true" />
                </Card>
                <div className="min-w-0">
                  <h4 className="text-xs font-bold text-foreground truncate m-0">{session.document_name}</h4>
                  <p className="text-[10px] text-muted-foreground mt-0.5">
                    {session.config_paper_size.toUpperCase()} | Mar: {session.config_margin} | Gut:{" "}
                    {session.config_gutter} | Sig: {session.config_signature_size}
                  </p>
                </div>
              </div>
              <div>
                {/* A booklet is 'compiling' where a document would be
                    'processing'; both render as the same spinner. */}
                <DocumentStatusIcon
                  status={
                    session.status === "compiling"
                      ? "processing"
                      : session.status === "failed"
                        ? "failed"
                        : "ready"
                  }
                />
              </div>
            </Button>
          ))}
        </div>
      </ScrollArea>
    )}
  </div>
)
