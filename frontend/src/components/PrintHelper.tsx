import React, { useState, useEffect } from "react"
import { api } from "../api"
import { PDFPageRenderer } from "./PDFPageRenderer"
import { Button } from "./ui/button"
import { Card } from "./ui/card"
import { Label } from "./ui/label"
import { ScrollArea } from "./ui/scroll-area"
import { Select } from "./ui/select"
import { Input } from "./ui/input"
import {
  Printer,
  RotateCw,
  AlertTriangle,
  CheckCircle2,
  FileDown,
  HelpCircle,
  RefreshCw,
  Eye,
  BookOpen,
  Download,
  Mail
} from "lucide-react"

interface PrintHelperProps {
  bookletId: string;
  documentId: string;
  totalPages: number; // original PDF pages
  signatureSize: number;
  pages: { page_number: number; text_preview: string }[];
  onBack: () => void;
}

export const PrintHelper: React.FC<PrintHelperProps> = ({ bookletId, documentId, totalPages, signatureSize, pages, onBack }) => {
  const [batchSize, setBatchSize] = useState<number>(10)
  const [completedBatches, setCompletedBatches] = useState<Record<number, boolean>>({})
  const [selectedSheet, setSelectedSheet] = useState<number>(1)
  const [previewSide, setPreviewSide] = useState<"front" | "back">("front")

  const [showEmailModal, setShowEmailModal] = useState(false)
  const [recipientEmail, setRecipientEmail] = useState("")
  const [emailLoading, setEmailLoading] = useState(false)
  const [emailStatus, setEmailStatus] = useState<{ type: "success" | "error"; text: string } | null>(null)

  // Load current user's email for pre-fill
  useEffect(() => {
    api.getMe().then((status) => {
      if (status.authenticated && status.user?.email) {
        setRecipientEmail(status.user.email)
      }
    }).catch(() => {})
  }, [])

  // Total sheets in booklet is Ceil(totalPages / 4).
  // Target pages is Ceil(totalPages / 4) * 4.
  const targetPages = Math.ceil(totalPages / 4) * 4
  const totalSheets = targetPages / 4
  const maxBookletPage = targetPages / 2

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

  const handleDownloadSheet = (type: "fronts" | "backs" | "both") => {
    const downloadUrl = api.getDownloadUrl(bookletId, type === "both" ? undefined : type, String(selectedSheet))
    window.open(downloadUrl, "_blank")
  }

  // Calculate pages for a given physical sheet
  const getPagesForSheet = (sheetIndex: number) => {
    const sheetsPerSignature = signatureSize / 4
    const signatureIndex = Math.floor((sheetIndex - 1) / sheetsPerSignature)
    const s = ((sheetIndex - 1) % sheetsPerSignature) + 1 // 1-indexed sheet within signature
    const offset = signatureIndex * signatureSize

    const frontLeft = offset + (signatureSize - 2 * (s - 1))
    const frontRight = offset + (2 * (s - 1) + 1)
    const backLeft = offset + (2 * (s - 1) + 2)
    const backRight = offset + (signatureSize - 2 * (s - 1) - 1)

    const getSnippet = (num: number) => {
      if (num > totalPages) return null
      const p = pages.find(item => item.page_number === num)
      return p ? p.text_preview : ""
    }

    return {
      front: {
        rawLeft: frontLeft,
        rawRight: frontRight,
        snippetLeft: getSnippet(frontLeft),
        snippetRight: getSnippet(frontRight),
      },
      back: {
        rawLeft: backLeft,
        rawRight: backRight,
        snippetLeft: getSnippet(backLeft),
        snippetRight: getSnippet(backRight),
      }
    }
  }

  const activePages = getPagesForSheet(selectedSheet)

  return (
    <div className="space-y-4">
      {/* Header Row */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 bg-background/30 p-3 rounded-xl border border-border/50">
        <div>
          <h2 className="text-xl font-bold text-foreground flex items-center gap-2">
            <Printer className="h-5 w-5 text-primary" />
            Printing Guide &amp; Helper
          </h2>
          <p className="text-muted-foreground text-xs">
            Booklet ID: <code className="text-primary font-mono text-[11px]">{bookletId.slice(0, 8)}</code> &bull; Signature size: {signatureSize}
          </p>
        </div>
        <div className="flex items-center gap-2 self-start sm:self-auto">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setShowEmailModal(true)}
            className="text-xs border-primary/20 hover:border-primary/40 text-primary bg-primary/5 hover:bg-primary/10 flex items-center gap-1.5 font-bold"
          >
            <Mail className="h-3.5 w-3.5" />
            Email Booklet
          </Button>
          <Button variant="outline" size="sm" onClick={onBack} className="text-xs">
            Back to Dashboard
          </Button>
        </div>
      </div>

      {/* Stats Row */}
      <div className="flex flex-wrap items-center gap-x-6 gap-y-2 glass px-4 py-2 rounded-xl border border-border text-xs">
        <div className="flex items-center gap-1.5">
          <span className="text-muted-foreground font-medium">Original PDF:</span>
          <span className="font-bold text-foreground">{totalPages} pages</span>
        </div>
        <div className="h-3.5 w-px bg-border" />
        <div className="flex items-center gap-1.5">
          <span className="text-muted-foreground font-medium">Layout Sheets:</span>
          <span className="font-bold text-primary">{totalSheets} sheets</span>
        </div>
        <div className="h-3.5 w-px bg-border" />
        <div className="flex items-center gap-1.5">
          <span className="text-muted-foreground font-medium">Total Printable Pages:</span>
          <span className="font-bold text-accent">{targetPages} pages</span>
        </div>
        <div className="h-3.5 w-px bg-border text-muted-foreground/30" />
        <div className="flex items-center gap-1.5">
          <span className="text-muted-foreground font-medium">Total Signatures:</span>
          <span className="font-bold text-foreground">{Math.ceil(totalSheets / (signatureSize / 4))}</span>
        </div>
      </div>

      {/* Main Grid */}
      <div className="grid grid-cols-1 lg:grid-cols-12 gap-4">

        {/* Left Console Column */}
        <div className="lg:col-span-7 space-y-4">
          <div className="glass p-4 rounded-xl border-border space-y-3">
            <div className="flex items-center justify-between border-b border-border/50 pb-2">
              <div className="flex items-center gap-2">
                <BookOpen className="h-4 w-4 text-primary" />
                <h3 className="text-sm font-bold text-foreground">Print Batches</h3>
              </div>
              <div className="flex items-center gap-2">
                <span className="text-muted-foreground text-[11px] shrink-0">Batch size:</span>
                <Select
                  value={batchSize}
                  onChange={(e) => {
                    setBatchSize(parseInt(e.target.value))
                    setCompletedBatches({})
                    setSelectedSheet(1)
                  }}
                  className="h-7 text-xs py-0 pr-8 w-28"
                >
                  <option value={5}>5 Sheets</option>
                  <option value={10}>10 Sheets</option>
                  <option value={20}>20 Sheets</option>
                  <option value={totalSheets}>All ({totalSheets})</option>
                </Select>
              </div>
            </div>

            <ScrollArea className="max-h-[340px]">
              <div className="space-y-2 pr-3.5">
                {batches.map((batch) => {
                  const isDone = completedBatches[batch.id] || false
                  return (
                    <div
                      key={batch.id}
                      className={`p-2.5 rounded-lg border transition-all flex items-center justify-between gap-3 text-xs ${isDone
                        ? "bg-primary/5 border-primary/15 opacity-70"
                        : "bg-background/40 border-border hover:border-primary/20"
                        }`}
                    >
                      <div className="space-y-0.5 min-w-0">
                        <div className="flex items-center gap-2">
                          <span className={`text-[10px] font-bold px-1.5 py-0.2 rounded ${isDone ? "bg-accent/15 text-accent" : "bg-primary/15 text-primary"
                            }`}>
                            Batch {batch.id}
                          </span>
                          <span className="font-bold text-foreground truncate">
                            Sheets {batch.startSheet} &ndash; {batch.endSheet}
                          </span>
                        </div>
                        <div className="flex flex-wrap items-center gap-1.5 text-muted-foreground text-[10px] mt-1">
                          <span>Sheet:</span>
                          <div className="flex flex-wrap items-center gap-1">
                            {Array.from({ length: batch.endSheet - batch.startSheet + 1 }, (_, idx) => {
                              const sNum = batch.startSheet + idx
                              const isSelected = selectedSheet === sNum
                              return (
                                <Button
                                  type="button"
                                  key={sNum}
                                  onClick={(e) => {
                                    e.stopPropagation()
                                    setSelectedSheet(sNum)
                                  }}
                                  variant={isSelected ? "default" : "outline"}
                                  className="h-5 px-1.5 py-0 text-[10px] font-bold"
                                >
                                  {sNum}
                                </Button>
                              )
                            })}
                          </div>
                        </div>
                      </div>

                      <div className="flex items-center gap-1.5 shrink-0">
                        <Button
                          variant="glass"
                          size="xs"
                          className="h-7 text-[11px] px-2 font-semibold"
                          onClick={() => window.open(api.getDownloadUrl(bookletId, "fronts", `${batch.startSheet}-${batch.endSheet}`), "_blank")}
                          disabled={isDone}
                        >
                          <FileDown className="mr-1 h-3 w-3" />
                          Fronts
                        </Button>
                        <Button
                          variant="glass"
                          size="xs"
                          className="h-7 text-[11px] px-2 font-semibold"
                          onClick={() => window.open(api.getDownloadUrl(bookletId, "backs", `${batch.startSheet}-${batch.endSheet}`), "_blank")}
                          disabled={isDone}
                        >
                          <FileDown className="mr-1 h-3 w-3" />
                          Backs
                        </Button>
                        <Button
                          onClick={() => toggleBatchComplete(batch.id)}
                          variant="ghost"
                          className={`h-7 w-7 p-0 rounded border transition-all cursor-pointer ${isDone
                            ? "bg-accent/10 border-accent/25 text-accent hover:bg-accent/20"
                            : "bg-background border-border text-muted-foreground hover:text-foreground hover:bg-muted"
                            }`}
                          aria-label={isDone ? "Mark batch incomplete" : "Mark batch complete"}
                        >
                          <CheckCircle2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    </div>
                  )
                })}
              </div>
            </ScrollArea>
          </div>

          {/* Compact Instructions */}
          <div className="glass p-4 rounded-xl border-border space-y-2">
            <div className="flex items-center gap-1.5 border-b border-border/40 pb-1.5">
              <HelpCircle className="h-4 w-4 text-primary" />
              <h3 className="text-xs font-bold text-foreground">Duplex Printing Instructions</h3>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4 text-[11px] text-muted-foreground leading-relaxed">
              <div className="space-y-1">
                <span className="font-bold text-primary uppercase text-[9px] tracking-wider block">Step 1: Front Side</span>
                <p>Download &amp; print **Fronts**. Settings: **1-sided, Landscape, Actual Size (no scaling)**.</p>
              </div>
              <div className="space-y-1">
                <span className="font-bold text-accent uppercase text-[9px] tracking-wider block">Step 2: Back Side</span>
                <p>Take printed stack without rearranging, flip along **short edge**, re-insert, and print **Backs**.</p>
              </div>
            </div>
          </div>
        </div>

        {/* Right Preview & Recovery Column */}
        <div className="lg:col-span-5 space-y-4">

          {/* Visual Sheet Preview Card */}
          <div className="glass p-4 rounded-xl border-border space-y-3">
            <div className="flex items-center justify-between border-b border-border/50 pb-2">
              <div className="flex items-center gap-2">
                <Eye className="h-4 w-4 text-accent" />
                <h3 className="text-sm font-bold text-foreground">Sheet {selectedSheet} Preview</h3>
              </div>
              <div className="flex bg-muted p-0.5 rounded border border-border text-[9px] font-bold">
                <Button
                  type="button"
                  onClick={() => setPreviewSide("front")}
                  variant="ghost"
                  className={`text-[10px] font-bold px-2 py-1 h-7 rounded-md transition-all ${previewSide === "front"
                    ? "bg-background text-foreground shadow-sm hover:bg-background"
                    : "text-muted-foreground hover:text-foreground hover:bg-transparent"
                    }`}
                >
                  Front (Odds)
                </Button>
                <Button
                  type="button"
                  onClick={() => setPreviewSide("back")}
                  variant="ghost"
                  className={`text-[10px] font-bold px-2 py-1 h-7 rounded-md transition-all ${previewSide === "back"
                    ? "bg-background text-foreground shadow-sm hover:bg-background"
                    : "text-muted-foreground hover:text-foreground hover:bg-transparent"
                    }`}
                >
                  Back (Evens)
                </Button>
              </div>
            </div>

            {/* Simulated Sheet - LOOKS LIKE PAPER: no corner radius, drop shadow */}
            <div className="relative aspect-[1.5/1] w-full bg-white border border-neutral-300 shadow-[0_6px_16px_rgba(0,0,0,0.12)] flex flex-col p-4 overflow-hidden">
              <div className="flex-1 w-full flex items-center justify-center overflow-hidden">
                <PDFPageRenderer
                  url={api.getDownloadUrl(bookletId)}
                  pageNumber={(selectedSheet - 1) * 2 + (previewSide === "front" ? 1 : 2)}
                  className="w-full h-full"
                  rotation={0}
                />
              </div>


            </div>
            <div className="mt-3 flex flex-col gap-3 border-t border-border/30 pt-3 z-10">
              <div className="flex items-center justify-between text-[10px] text-muted-foreground">
                <span>Physical Sheet {selectedSheet} of {totalSheets}</span>
                <span className="font-semibold text-primary">
                  {previewSide === "front" ? "Odds (Front Layout)" : "Evens (Back Layout)"}
                </span>
              </div>
              <div className="flex gap-2">
                <Button variant="outline" size="sm" className="flex-1 text-[10px] h-7" onClick={() => handleDownloadSheet("fronts")}>
                  <Download className="h-3 w-3 mr-1" />
                  Front
                </Button>
                <Button variant="outline" size="sm" className="flex-1 text-[10px] h-7" onClick={() => handleDownloadSheet("backs")}>
                  <Download className="h-3 w-3 mr-1" />
                  Back
                </Button>
                <Button variant="outline" size="sm" className="flex-1 text-[10px] h-7 bg-primary/10 border-primary/20 text-primary hover:bg-primary/20" onClick={() => handleDownloadSheet("both")}>
                  <Download className="h-3 w-3 mr-1" />
                  Both
                </Button>
              </div>
            </div>
          </div>
        </div>
      </div>
      
      {/* Email Booklet Modal */}
      {showEmailModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4 animate-in fade-in duration-200" onClick={() => setShowEmailModal(false)}>
          <div 
            className="bg-card border border-border rounded-2xl max-w-md w-full p-6 shadow-2xl space-y-4 animate-in zoom-in-95 duration-200"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between border-b border-border/40 pb-2">
              <h3 className="text-base font-bold text-foreground flex items-center gap-2 m-0">
                <Mail className="h-5 w-5 text-primary" />
                Email Booklet PDF
              </h3>
              <Button 
                variant="ghost" 
                size="sm" 
                onClick={() => {
                  setShowEmailModal(false)
                  setEmailStatus(null)
                }}
                className="h-7 w-7 p-0 rounded-full"
              >
                &times;
              </Button>
            </div>
            
            <p className="text-xs text-muted-foreground leading-relaxed m-0">
              Send the compiled PDF booklet as an email attachment. This will use the system-wide SMTP settings.
            </p>

            {emailStatus && (
              <div className={`p-3 rounded-lg border text-xs font-medium ${
                emailStatus.type === "success" 
                  ? "bg-green-500/10 border-green-500/30 text-green-400" 
                  : "bg-destructive/10 border-destructive/30 text-destructive"
              }`}>
                {emailStatus.text}
              </div>
            )}

            <div className="space-y-2">
              <Label htmlFor="email-recipient" className="text-xs font-bold text-muted-foreground">Recipient Email Address</Label>
              <Input
                id="email-recipient"
                type="email"
                placeholder="recipient@example.com"
                value={recipientEmail}
                onChange={(e) => setRecipientEmail(e.target.value)}
                className="bg-background/50 border-border focus-visible:ring-primary w-full h-9"
                disabled={emailLoading}
              />
            </div>

            <div className="flex items-center justify-end gap-2 pt-2 border-t border-border/20">
              <Button 
                variant="outline" 
                size="sm" 
                onClick={() => {
                  setShowEmailModal(false)
                  setEmailStatus(null)
                }}
                disabled={emailLoading}
                className="text-xs"
              >
                Cancel
              </Button>
              <Button 
                size="sm" 
                onClick={async () => {
                  if (!recipientEmail) {
                    setEmailStatus({ type: "error", text: "Recipient email is required." })
                    return
                  }
                  setEmailLoading(true)
                  setEmailStatus(null)
                  try {
                    const result = await api.emailBooklet(bookletId, recipientEmail)
                    setEmailStatus({ type: "success", text: result.message || "Booklet emailed successfully!" })
                  } catch (err: any) {
                    setEmailStatus({ type: "error", text: err.message || "Failed to email booklet." })
                  } finally {
                    setEmailLoading(false)
                  }
                }}
                disabled={emailLoading}
                className="bg-primary hover:bg-primary/90 text-primary-foreground font-bold text-xs"
              >
                {emailLoading ? "Sending..." : "Send Email"}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
