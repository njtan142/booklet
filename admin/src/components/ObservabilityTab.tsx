
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "./ui/card"
import {
  BarChart3, ExternalLink, Database, Activity, TrendingUp,
  Zap, FileText, BookOpen, Eye
} from "lucide-react"

export function ObservabilityTab() {
  const metrics = [
    { icon: TrendingUp, title: "HTTP Request Rate", query: "sum(rate(http_requests_total[5m])) by (method, status, path)", desc: "Requests per second grouped by method, path, and HTTP status code." },
    { icon: Zap, title: "HTTP Latency (p95)", query: "histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket[5m])) by (le, path))", desc: "95th-percentile response latency per API endpoint." },
    { icon: FileText, title: "Document Upload Rate", query: "sum(rate(document_uploads_total[5m])) by (status)", desc: "PDF upload throughput split by success/failure status." },
    { icon: BookOpen, title: "Booklet Compilation (p90)", query: "histogram_quantile(0.90, sum(rate(booklet_compilation_duration_seconds_bucket[5m])) by (le))", desc: "90th-percentile PDF imposition compilation duration." },
    { icon: Eye, title: "Vector Search (p90)", query: "histogram_quantile(0.90, sum(rate(vector_search_duration_seconds_bucket[5m])) by (le))", desc: "90th-percentile semantic vector search query latency." },
  ]

  return (
    <div className="space-y-5">
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <a
          href="http://localhost:3002"
          target="_blank"
          rel="noopener noreferrer"
          className="block glass-interactive rounded-xl p-5 group"
        >
          <div className="flex items-start gap-4">
            <div className="p-3 rounded-xl bg-primary/10 text-primary">
              <BarChart3 className="h-6 w-6" />
            </div>
            <div className="flex-1">
              <div className="flex items-center gap-2">
                <h3 className="font-bold text-foreground">Grafana Dashboard</h3>
                <ExternalLink className="h-3.5 w-3.5 text-muted-foreground group-hover:text-primary transition-colors" />
              </div>
              <p className="text-xs text-muted-foreground mt-1">Pre-provisioned SRE dashboard with time-series panels for all instrumented metrics. Running on port <strong>3002</strong>.</p>
              <p className="text-xs font-mono text-primary/80 mt-2">http://localhost:3002</p>
            </div>
          </div>
        </a>

        <a
          href="http://localhost:9090"
          target="_blank"
          rel="noopener noreferrer"
          className="block glass-interactive rounded-xl p-5 group"
        >
          <div className="flex items-start gap-4">
            <div className="p-3 rounded-xl bg-accent/10 text-accent">
              <Database className="h-6 w-6" />
            </div>
            <div className="flex-1">
              <div className="flex items-center gap-2">
                <h3 className="font-bold text-foreground">Prometheus Console</h3>
                <ExternalLink className="h-3.5 w-3.5 text-muted-foreground group-hover:text-accent transition-colors" />
              </div>
              <p className="text-xs text-muted-foreground mt-1">Raw metric scraper console. Explore and run PromQL queries directly against the backend. Running on port <strong>9090</strong>.</p>
              <p className="text-xs font-mono text-accent/80 mt-2">http://localhost:9090</p>
            </div>
          </div>
        </a>
      </div>

      <Card className="glass border-border">
        <CardHeader className="pb-3">
          <div className="flex items-center gap-2">
            <Activity className="h-5 w-5 text-primary" />
            <CardTitle className="text-base font-bold">Instrumented Metrics</CardTitle>
          </div>
          <CardDescription className="text-xs">All metrics are exposed at <code className="font-mono bg-muted px-1 py-0.5 rounded">/metrics</code> and scraped by Prometheus every 15 seconds.</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-3">
            {metrics.map(({ icon: Icon, title, query, desc }) => (
              <div key={title} className="p-3 rounded-lg border border-border/50 bg-muted/20 space-y-1.5">
                <div className="flex items-center gap-2">
                  <Icon className="h-4 w-4 text-primary shrink-0" />
                  <span className="text-sm font-semibold text-foreground">{title}</span>
                </div>
                <p className="text-xs text-muted-foreground">{desc}</p>
                <code className="block text-[10px] font-mono bg-muted/60 text-muted-foreground px-2 py-1.5 rounded border border-border/40 break-all">{query}</code>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
