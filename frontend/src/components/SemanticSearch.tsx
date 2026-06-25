import React, { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { api } from "../api"
import { Button } from "./ui/button"
import { Input } from "./ui/input"
import { Select } from "./ui/select"
import { Search, Loader2, Sparkles, FileText, ChevronRight } from "lucide-react"

export const SemanticSearch: React.FC = () => {
  const [query, setQuery] = useState<string>("")
  const [triggerQuery, setTriggerQuery] = useState<string>("")
  const [docFilter, setDocFilter] = useState<string>("")

  // Fetch documents for the dropdown filter
  const { data: documents = [] } = useQuery({
    queryKey: ["documents"],
    queryFn: api.listDocuments,
  })

  // Fetch search results
  const { data: results = [], isLoading, isError } = useQuery({
    queryKey: ["search", triggerQuery, docFilter],
    queryFn: () => api.search(triggerQuery, docFilter),
    enabled: !!triggerQuery,
  })

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault()
    if (query.trim()) {
      setTriggerQuery(query.trim())
    }
  }

  // Highlight query words in text snippet
  const highlightText = (text: string, searchWord: string) => {
    if (!searchWord) return text
    const words = searchWord.split(/\s+/).filter(w => w.length > 2)
    if (words.length === 0) return text

    // Create regex matching any of the query words
    const pattern = `(${words.map(w => w.replace(/[-\/\\^$*+?.()|[\]{}]/g, '\\$&')).join("|")})`
    const regex = new RegExp(pattern, "gi")

    const parts = text.split(regex)
    return (
      <>
        {parts.map((part, i) => 
          regex.test(part) 
            ? <mark key={i} className="bg-primary/30 text-white rounded px-0.5 border border-primary/20">{part}</mark> 
            : part
        )}
      </>
    )
  }

  return (
    <div className="space-y-6 max-w-4xl mx-auto">
      <div>
        <h2 className="text-2xl font-bold text-white m-0">Semantic Search</h2>
        <p className="text-zinc-400 text-sm mt-1">Ask questions or search topics across your library using self-hosted vector embeddings.</p>
      </div>

      {/* Search Input Bar */}
      <form onSubmit={handleSearch} className="flex flex-col md:flex-row gap-3">
        <div className="flex-1 relative">
          <Input 
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search details, e.g. 'What is the binding margin recommended?'..."
            className="pl-10 py-5 bg-zinc-950/40 border-zinc-800"
          />
          <Search className="absolute left-3.5 top-3.5 h-4.5 w-4.5 text-zinc-500" />
        </div>

        <div className="w-full md:w-56">
          <Select value={docFilter} onChange={(e) => setDocFilter(e.target.value)}>
            <option value="">All Documents</option>
            {documents.filter(d => d.status === "ready").map(doc => (
              <option key={doc.id} value={doc.id}>{doc.name}</option>
            ))}
          </Select>
        </div>

        <Button type="submit" disabled={isLoading} className="py-5 px-6 font-bold flex items-center gap-2">
          {isLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Sparkles className="h-4 w-4" />}
          Semantic Search
        </Button>
      </form>

      {/* Results Container */}
      <div className="space-y-4">
        {isLoading && (
          <div className="flex flex-col items-center justify-center py-20 gap-4">
            <Loader2 className="h-8 w-8 animate-spin text-primary" />
            <p className="text-zinc-500 text-xs animate-pulse">Running embedding lookup & pg_vector cosine similarity search...</p>
          </div>
        )}

        {!isLoading && triggerQuery && results.length === 0 && (
          <div className="glass p-12 text-center rounded-2xl border-zinc-800">
            <p className="text-zinc-500 text-sm">No semantically matching pages found in the library.</p>
          </div>
        )}

        {!isLoading && results.length > 0 && (
          <div className="space-y-4">
            <h3 className="text-xs font-bold text-zinc-400 uppercase tracking-wider">Top Semantic Matches</h3>
            <div className="space-y-3">
              {results.map((result, idx) => {
                const similarityPercentage = Math.round(result.similarity * 100)
                return (
                  <div key={idx} className="glass p-5 rounded-2xl border-zinc-800/80 hover:border-zinc-700/80 transition-all flex flex-col md:flex-row md:items-start gap-4">
                    {/* Score column */}
                    <div className="flex md:flex-col items-center justify-between md:justify-start gap-2 bg-zinc-950/50 border border-zinc-900 px-3 py-2 rounded-xl min-w-28 text-center">
                      <span className="text-[10px] uppercase font-bold text-zinc-500 tracking-wider">Match Score</span>
                      <span className="text-lg font-black text-violet-400">{similarityPercentage}%</span>
                    </div>

                    {/* Content column */}
                    <div className="flex-1 min-w-0 space-y-2.5">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="flex items-center gap-1 text-[11px] text-zinc-400 font-semibold bg-zinc-900 px-2 py-0.5 rounded border border-zinc-800/60">
                          <FileText className="h-3 w-3 text-zinc-400" />
                          {result.document_name}
                        </span>
                        <ChevronRight className="h-3 w-3 text-zinc-600" />
                        <span className="text-[11px] text-primary font-bold bg-primary/10 px-2 py-0.5 rounded border border-primary/10">
                          Page {result.page_number}
                        </span>
                      </div>

                      <p className="text-zinc-300 text-xs leading-relaxed italic bg-zinc-950/20 p-3 rounded-lg border border-zinc-900/50">
                        {highlightText(result.text_snippet, triggerQuery)}
                      </p>
                    </div>
                  </div>
                )
              })}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
