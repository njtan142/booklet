import { CheckCircle2, AlertTriangle } from "lucide-react"

export type Feedback = { type: "success" | "error"; text: string } | null

interface FeedbackBannerProps {
  msg: Feedback
}

export function FeedbackBanner({ msg }: FeedbackBannerProps) {
  if (!msg) return null
  return (
    <div className={`flex items-center gap-3 p-3 rounded-lg border text-xs font-medium ${
      msg.type === "success"
        ? "bg-green-500/10 border-green-500/30 text-green-500"
        : "bg-destructive/10 border-destructive/30 text-destructive"
    }`}>
      {msg.type === "success"
        ? <CheckCircle2 className="h-4 w-4 shrink-0" />
        : <AlertTriangle className="h-4 w-4 shrink-0" />}
      <span>{msg.text}</span>
    </div>
  )
}
