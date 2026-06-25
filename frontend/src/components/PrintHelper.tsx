import React, { useState } from "react"
import { api } from "../api"
import { Button } from "./ui/button"
import { Select } from "./ui/select"
import { Input } from "./ui/input"
import { Label } from "./ui/label"
import { 
  Printer, 
  RotateCw, 
  AlertTriangle, 
  CheckCircle2, 
  FileDown, 
  HelpCircle,
  RefreshCw
} from "lucide-react"

interface PrintHelperProps {
  bookletId: string;
  totalPages: number; // original PDF pages
  onBack: () => void;
}

export const PrintHelper: React.FC<PrintHelperProps> = ({ bookletId, totalPages, onBack }) => {
  const [batchSize, setBatchSize] = useState<number>(10)
  const [completedBatches, setCompletedBatches] = useState<Record<number, boolean>>({})
  
  // Recovery states
  const [ruinedStart, setRuinedStart] = useState<string>("")
  const [ruinedEnd, setRuinedEnd] = useState<string>("")
  const [recoveryError, setRecoveryError] = useState<string>("")

  // Total sheets in booklet is Ceil(totalPages / 4) * 2 pages per sheet?
  // Let's calculate: signature size is 4. Target pages is Ceil(totalPages / 4) * 4.
	// Since each sheet of paper has 2 booklet pages (front and back),
	// Total sheets = Target Pages / 2.
  const targetPages = Math.ceil(totalPages / 4) * 4
  const totalSheets = targetPages / 2

  // Generate batches
  const numBatches = Math.ceil(totalSheets / batchSize)
  const batches = Array.from({ length: numBatches }, (_, i) => {
    const startSheet = i * batchSize + 1
    const endSheet = Math.min((i + 1) * batchSize, totalSheets)
    return {
      id: i + 1,
      startSheet,
      endSheet,
    }
  })

  const toggleBatchComplete = (batchId: number) => {
    setCompletedBatches(prev => ({
      ...prev,
      [batchId]: !prev[batchId]
    }))
  }

  const handleDownloadRecovery = (type: "fronts" | "backs" | "both") => {
    setRecoveryError("")
    const start = parseInt(ruinedStart)
    const end = ruinedEnd ? parseInt(ruinedEnd) : start

    if (isNaN(start) || start < 1 || start > targetPages) {
      setRecoveryError(`Please enter a valid starting page number between 1 and ${targetPages}`)
      return
    }

    if (isNaN(end) || end < start || end > targetPages) {
      setRecoveryError(`Please enter a valid ending page number between ${start} and ${targetPages}`)
      return
    }

    const rangeStr = `${start}-${end}`
    const downloadUrl = api.getDownloadUrl(bookletId, type === "both" ? undefined : type, undefined, rangeStr)
    
    // Open in a new tab to trigger download
    window.open(downloadUrl, "_blank")
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-2xl font-bold text-white m-0">Printing Guide & Helper</h2>
          <p className="text-zinc-400 text-sm mt-1">Manual duplex optimization & recovery wizard for booklet ID: {bookletId.slice(0, 8)}...</p>
        </div>
        <Button variant="outline" size="sm" onClick={onBack}>
          Back to Dashboard
        </Button>
      </div>

      {/* Overview stats */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <div className="glass p-4 rounded-xl border-zinc-800">
          <p className="text-zinc-500 text-xs font-semibold uppercase tracking-wider">Original Pages</p>
          <p className="text-2xl font-extrabold text-white mt-1">{totalPages}</p>
        </div>
        <div className="glass p-4 rounded-xl border-zinc-800">
          <p className="text-zinc-500 text-xs font-semibold uppercase tracking-wider">Padded Booklet Pages</p>
          <p className="text-2xl font-extrabold text-violet-400 mt-1">{targetPages}</p>
        </div>
        <div className="glass p-4 rounded-xl border-zinc-800">
          <p className="text-zinc-500 text-xs font-semibold uppercase tracking-wider">Total Sheets of Paper</p>
          <p className="text-2xl font-extrabold text-blue-400 mt-1">{totalSheets}</p>
        </div>
        <div className="glass p-4 rounded-xl border-zinc-800">
          <p className="text-zinc-500 text-xs font-semibold uppercase tracking-wider">Estimated Signatures</p>
          <p className="text-2xl font-extrabold text-emerald-400 mt-1">{targetPages / 4}</p>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Step-by-step Batch Printing Column */}
        <div className="lg:col-span-2 space-y-6">
          <div className="glass p-6 rounded-2xl border-zinc-800 space-y-4">
            <div className="flex items-center justify-between border-b border-zinc-800/80 pb-4">
              <div className="flex items-center gap-2">
                <Printer className="h-5 w-5 text-primary" />
                <h3 className="text-lg font-bold text-white m-0">Batch Printing Console</h3>
              </div>
              <div className="flex items-center gap-3">
                <span className="text-zinc-400 text-xs font-medium">Batch size:</span>
                <div className="w-24">
                  <Select 
                    value={batchSize.toString()} 
                    onChange={(e) => {
                      setBatchSize(parseInt(e.target.value))
                      setCompletedBatches({})
                    }}
                  >
                    <option value="5">5 Sheets</option>
                    <option value="10">10 Sheets</option>
                    <option value="20">20 Sheets</option>
                    <option value="50">50 Sheets</option>
                    <option value={totalSheets.toString()}>All ({totalSheets})</option>
                  </Select>
                </div>
              </div>
            </div>

            <p className="text-zinc-300 text-xs leading-relaxed">
              We recommend printing in batches of 10 or 20 sheets. That way, if a paper jam or double-feed occurs during the back-side print, you only waste a few sheets instead of the entire document.
            </p>

            <div className="space-y-3 max-h-[450px] overflow-y-auto pr-2">
              {batches.map((batch) => {
                const isDone = completedBatches[batch.id] || false;
                return (
                  <div 
                    key={batch.id} 
                    className={`p-4 rounded-xl border transition-all flex flex-col md:flex-row md:items-center justify-between gap-4 ${
                      isDone 
                        ? "bg-emerald-950/10 border-emerald-800/30" 
                        : "bg-zinc-950/40 border-zinc-800/60 hover:border-zinc-700/80"
                    }`}
                  >
                    <div className="space-y-1">
                      <div className="flex items-center gap-2">
                        <span className={`text-xs font-bold px-2 py-0.5 rounded-full ${
                          isDone ? "bg-emerald-500/20 text-emerald-400" : "bg-primary/20 text-primary"
                        }`}>
                          Batch {batch.id}
                        </span>
                        <h4 className="text-sm font-bold text-white m-0">
                          Sheets {batch.startSheet} &ndash; {batch.endSheet}
                        </h4>
                      </div>
                      <p className="text-zinc-500 text-xs">
                        Prints booklet pages {2 * batch.startSheet - 1} to {2 * batch.endSheet}
                      </p>
                    </div>

                    <div className="flex flex-wrap items-center gap-2">
                      <Button 
                        variant="glass" 
                        size="sm" 
                        onClick={() => window.open(api.getDownloadUrl(bookletId, "fronts", `${batch.startSheet}-${batch.endSheet}`), "_blank")}
                        disabled={isDone}
                      >
                        <FileDown className="mr-1.5 h-3.5 w-3.5" />
                        1. Fronts (Odds)
                      </Button>
                      <Button 
                        variant="glass" 
                        size="sm" 
                        onClick={() => window.open(api.getDownloadUrl(bookletId, "backs", `${batch.startSheet}-${batch.endSheet}`), "_blank")}
                        disabled={isDone}
                      >
                        <FileDown className="mr-1.5 h-3.5 w-3.5" />
                        2. Backs (Evens)
                      </Button>
                      <Button 
                        onClick={() => toggleBatchComplete(batch.id)}
                        variant="ghost"
                        className={`p-2 rounded-lg border transition-all cursor-pointer ${
                          isDone 
                            ? "bg-emerald-500/10 border-emerald-500/30 text-emerald-400 hover:bg-emerald-500/20" 
                            : "bg-zinc-900 border-zinc-800 text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800"
                        }`}
                        title={isDone ? "Mark Incomplete" : "Mark Complete"}
                      >
                        <CheckCircle2 className="h-4 w-4" />
                      </Button>
                    </div>
                  </div>
                )
              })}
            </div>
          </div>

          {/* Guide Card */}
          <div className="glass p-6 rounded-2xl border-zinc-800 space-y-4">
            <div className="flex items-center gap-2 border-b border-zinc-800/80 pb-4">
              <HelpCircle className="h-5 w-5 text-blue-400" />
              <h3 className="text-lg font-bold text-white m-0">How to Manual Duplex Print</h3>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-6 text-xs text-zinc-300">
              <div className="space-y-2">
                <h4 className="font-bold text-white uppercase tracking-wider text-[10px] text-blue-400">Step 1: Front Side</h4>
                <p>1. Download the **Fronts (Odds)** PDF for your active batch.</p>
                <p>2. Send to printer. Make sure printer settings are: **1-sided, Landscape, Actual Size (no scaling)**.</p>
                <p>3. Let all pages print out. They will print on one side of each sheet.</p>
              </div>
              <div className="space-y-2">
                <h4 className="font-bold text-white uppercase tracking-wider text-[10px] text-violet-400">Step 2: Back Side</h4>
                <p>1. Take the printed stack out of the output tray without shuffling or rearranging them.</p>
                <p>2. Flip the stack over so you print on the blank side. **Orientation rule**: typically, flip along the short edge (bottom to top) and re-insert into the input tray.</p>
                <p>3. Download the **Backs (Evens)** PDF and print it. The backs will print in register with the fronts!</p>
              </div>
            </div>
          </div>
        </div>

        {/* Reprint Recovery Column */}
        <div className="space-y-6">
          <div className="glass p-6 rounded-2xl border-zinc-800 space-y-4">
            <div className="flex items-center gap-2 border-b border-zinc-800/80 pb-4">
              <AlertTriangle className="h-5 w-5 text-rose-500" />
              <h3 className="text-lg font-bold text-white m-0">Ruined Print Recovery</h3>
            </div>
            <p className="text-zinc-400 text-xs leading-relaxed">
              Did the printer double-feed, jam, or skip a page? Don't panic and don't throw away the successful sheets!
            </p>
            <p className="text-zinc-400 text-xs leading-relaxed">
              Enter the booklet page numbers that were ruined below. We will automatically calculate which physical sheets contain those pages and download just the required fronts and backs.
            </p>

            <div className="space-y-3 pt-2">
              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-1">
                  <Label className="text-[10px] uppercase font-bold text-zinc-500 tracking-wider">Start Page #</Label>
                  <Input 
                    type="number" 
                    min="1" 
                    max={targetPages}
                    placeholder="e.g. 13"
                    value={ruinedStart}
                    onChange={(e) => setRuinedStart(e.target.value)}
                  />
                </div>
                <div className="space-y-1">
                  <Label className="text-[10px] uppercase font-bold text-zinc-500 tracking-wider">End Page # (Opt)</Label>
                  <Input 
                    type="number" 
                    min="1" 
                    max={targetPages}
                    placeholder="e.g. 16"
                    value={ruinedEnd}
                    onChange={(e) => setRuinedEnd(e.target.value)}
                  />
                </div>
              </div>

              {recoveryError && (
                <p className="text-rose-400 text-[11px] font-medium animate-pulse">{recoveryError}</p>
              )}

              <div className="flex flex-col gap-2 pt-2">
                <Button 
                  variant="destructive" 
                  className="w-full flex items-center justify-center gap-2"
                  onClick={() => handleDownloadRecovery("fronts")}
                >
                  <RotateCw className="h-4 w-4" />
                  Reprint Ruined Fronts
                </Button>
                <Button 
                  variant="destructive" 
                  className="w-full flex items-center justify-center gap-2"
                  onClick={() => handleDownloadRecovery("backs")}
                >
                  <RotateCw className="h-4 w-4" />
                  Reprint Ruined Backs
                </Button>
                <Button 
                  variant="outline" 
                  className="w-full text-zinc-300 border-zinc-800 hover:bg-zinc-900"
                  onClick={() => handleDownloadRecovery("both")}
                >
                  <RefreshCw className="h-4 w-4" />
                  Reprint Both (Full Sheets)
                </Button>
              </div>
            </div>
          </div>

          {/* Visual Flipper Hint */}
          <div className="glass p-6 rounded-2xl border-zinc-800 space-y-3">
            <h4 className="text-xs font-bold text-zinc-400 uppercase tracking-wider m-0">Printer Feed Tips</h4>
            <div className="space-y-3 text-xs text-zinc-400 leading-relaxed">
              <div className="p-3 bg-zinc-950/50 rounded-lg border border-zinc-900">
                <span className="font-bold text-white block mb-1">Testing Flip Direction:</span>
                Draw a small arrow pointing **UP** on the top page of the tray. Print a single test sheet. See where the arrow ends up. This tells you if your printer feeds head-first, face-up, or face-down!
              </div>
              <div className="p-3 bg-zinc-950/50 rounded-lg border border-zinc-900">
                <span className="font-bold text-white block mb-1">Page Orientation:</span>
                For landscape booklets, printing pages double-sided requires flipping along the **short edge** to prevent the back page from printing upside down!
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
