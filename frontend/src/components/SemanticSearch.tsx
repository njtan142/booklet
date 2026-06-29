import React, { useState, useEffect, useMemo } from "react"
import { useForm } from "react-hook-form"
import { useQuery } from "@tanstack/react-query"
import { api, type SearchResult } from "../api"
import { Button } from "./ui/button"
import { Input } from "./ui/input"
import { Select } from "./ui/select"
import { Form, FormField, FormItem, FormControl } from "./ui/form"
import { ScrollArea } from "./ui/scroll-area"
import { Card } from "./ui/card"
import { Search, Loader2, Sparkles, FileText, ChevronRight } from "lucide-react"

interface GroupedDoc {
  id: string;
  name: string;
  matches: SearchResult[];
}

export const SemanticSearch: React.FC = () => {
  const [triggerQuery, setTriggerQuery] = useState<string>("")
  const [docFilter, setDocFilter] = useState<string>("")
  const [selectedDocId, setSelectedDocId] = useState<string>("")

  const searchForm = useForm({
    defaultValues: { query: "" },
  })

  // Fetch documents for the dropdown filter
  const { data: documents = [] } = useQuery({
    queryKey: ["documents"],
    queryFn: api.listDocuments,
  })

  // Fetch search results
  const { data: rawResults, isLoading, isError } = useQuery({
    queryKey: ["search", triggerQuery, docFilter],
    queryFn: () => api.search(triggerQuery, docFilter),
    enabled: !!triggerQuery,
  })
  const results = rawResults || []

  // Group matches by document
  const groupedDocs = useMemo(() => {
    const groups: { [key: string]: GroupedDoc } = {}
    results.forEach((r) => {
      if (!groups[r.document_id]) {
        groups[r.document_id] = {
          id: r.document_id,
          name: r.document_name,
          matches: [],
        }
      }
      groups[r.document_id].matches.push(r)
    })
    return Object.values(groups)
  }, [results])

  // Automatically select the first book when search results change
  useEffect(() => {
    if (groupedDocs.length > 0) {
      setSelectedDocId(groupedDocs[0].id)
    } else {
      setSelectedDocId("")
    }
  }, [groupedDocs])

  const handleSearch = searchForm.handleSubmit((values) => {
    if (values.query.trim()) {
      setTriggerQuery(values.query.trim())
    }
  })

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
            ? <mark key={i} className="bg-primary/30 text-primary-foreground rounded px-0.5 border border-primary/20">{part}</mark> 
            : part
        )}
      </>
    )
  }

  return (
    <div className="space-y-6 max-w-6xl mx-auto">
      <div>
        <h2 className="text-2xl font-bold text-foreground m-0">Semantic Search</h2>
        <p className="text-muted-foreground text-sm mt-1">Ask questions or search topics across your library using self-hosted vector embeddings.</p>
      </div>

      {/* Search Input Bar */}
      <Form {...searchForm}>
        <form onSubmit={handleSearch} className="flex flex-col md:flex-row gap-3">
          <FormField
            control={searchForm.control}
            name="query"
            render={({ field }) => (
              <FormItem className="flex-1 relative">
                <FormControl>
                  <Input
                    id="search-query-input"
                    placeholder="Type a search query..."
                    className="pl-10 py-5 bg-background/70 border-border"
                    aria-label="Search query"
                    {...field}
                  />
                </FormControl>
                <Search className="absolute left-3.5 top-3.5 h-4.5 w-4.5 text-muted-foreground" aria-hidden="true" />
              </FormItem>
            )}
          />

          <div className="w-full md:w-56">
            <Select
              id="doc-filter-select"
              value={docFilter}
              onChange={(e) => setDocFilter(e.target.value)}
              aria-label="Filter search by document"
            >
              <option value="">All Documents</option>
              {documents.filter(d => d.status === "ready").map(doc => (
                <option key={doc.id} value={doc.id}>{doc.name}</option>
              ))}
            </Select>
          </div>

          <Button type="submit" disabled={isLoading} className="py-5 px-6 font-bold flex items-center gap-2">
            {isLoading ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" /> : <Sparkles className="h-4 w-4" aria-hidden="true" />}
            Semantic Search
          </Button>
        </form>
      </Form>

      {/* Results Container */}
      <div className="space-y-4">
        {isLoading && (
          <div className="flex flex-col items-center justify-center py-20 gap-4">
            <Loader2 className="h-8 w-8 animate-spin text-primary" aria-hidden="true" />
            <p className="text-muted-foreground text-xs animate-pulse">Running embedding lookup & pg_vector cosine similarity search...</p>
          </div>
        )}

        {!isLoading && triggerQuery && results.length === 0 && (
          <div className="glass p-12 text-center rounded-2xl border-border">
            <p className="text-muted-foreground text-sm">No semantically matching pages found in the library.</p>
          </div>
        )}

        {!isLoading && results.length > 0 && (
          <div className="space-y-4">
            <h3 className="text-xs font-bold text-muted-foreground uppercase tracking-wider">Top Semantic Matches</h3>
            
            <div className="grid grid-cols-1 md:grid-cols-12 gap-6">
              {/* Sidebar: Grouped Books List */}
              <ScrollArea className="col-span-12 md:col-span-4 h-[600px] pr-2">
                <div className="space-y-3">
                  {groupedDocs.map((doc) => (
                    <Button
                      type="button"
                      key={doc.id}
                      onClick={() => setSelectedDocId(doc.id)}
                      variant="ghost"
                      className={`w-full text-left p-4 h-auto rounded-xl border transition-all flex flex-col items-stretch whitespace-normal ${
                        selectedDocId === doc.id
                          ? "bg-primary/10 border-primary shadow-sm hover:bg-primary/10"
                          : "bg-background/40 border-border hover:border-primary/20 hover:bg-background/60"
                      }`}
                    >
                      <div className="flex items-start gap-2.5">
                        <FileText className="h-4.5 w-4.5 text-muted-foreground mt-0.5 flex-shrink-0" />
                        <div className="min-w-0">
                          <span className="font-bold text-xs text-foreground block truncate">{doc.name}</span>
                          <span className="text-[10px] text-muted-foreground block mt-0.5">{doc.matches.length} matching pages</span>
                        </div>
                      </div>

                      {/* Expandable Snippets under selected book */}
                      {selectedDocId === doc.id && (
                        <div className="mt-3.5 space-y-2 pt-3.5 border-t border-primary/20 w-full text-left">
                          {doc.matches.map((m, i) => (
                            <Card key={i} className="text-[11px] bg-background/60 p-2.5 rounded-lg border border-border/60 hover:border-primary/30 transition-colors shadow-none">
                              <span className="text-primary font-bold">Page {m.page_number}</span>
                              <p className="text-foreground/80 leading-relaxed italic mt-1 font-serif">
                                {highlightText(m.text_snippet, triggerQuery)}
                              </p>
                            </Card>
                          ))}
                        </div>
                      )}
                    </Button>
                  ))}
                </div>
              </ScrollArea>

              {/* Preview Pane: Merged PDF inside Iframe */}
              <div className="col-span-12 md:col-span-8 h-[600px] bg-background/40 border border-border rounded-2xl overflow-hidden glass flex flex-col">
                {selectedDocId ? (
                  <iframe
                    src={`${api.getSearchPreviewUrl(selectedDocId, triggerQuery)}#search=${encodeURIComponent(triggerQuery)}`}
                    className="w-full h-full border-none"
                    title="Search Match Preview"
                  />
                ) : (
                  <div className="flex flex-col items-center justify-center h-full text-muted-foreground gap-2">
                    <FileText className="h-8 w-8 text-muted-foreground/50" />
                    <p className="text-sm">Select a book to preview matched pages.</p>
                  </div>
                )}
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
