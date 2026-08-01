import { useEffect, useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { api } from "../../api"
import type { DocumentInfo } from "../../api"

export type PendingUpload = {
  documentId: string
  fileName: string
  startedAt: number
}

export type FailedUpload = {
  id: string
  documentId?: string
  fileName: string
  message: string
}

type InFlightUpload = {
  id: string
  fileName: string
}

const UPLOAD_FAILURE_TIMEOUT_MS = 60000

// Queued documents get a far longer grace period than processing ones: sitting
// in a backlog is normal, whereas a processing document that stops touching
// updated_at has almost certainly died with the backend.
const QUEUE_TIMEOUT_MS = 15 * 60 * 1000

// useDocumentUploads owns everything about an upload between "the user picked a
// file" and "the document is ready or has visibly failed".
//
// The backend never reports a crashed upload as failed — a process that dies
// mid-split simply stops updating the row — so the timeouts here are what turn
// a stalled document into a visible failure the user can resume or dismiss.
export function useDocumentUploads(documents: DocumentInfo[]) {
  const queryClient = useQueryClient()
  const [inFlightUploads, setInFlightUploads] = useState<InFlightUpload[]>([])
  const [pendingUploads, setPendingUploads] = useState<PendingUpload[]>([])
  const [failedUploads, setFailedUploads] = useState<FailedUpload[]>([])

  const uploadFiles = async (files: FileList) => {
    const fileArray = Array.from(files).filter((f) => f.name.toLowerCase().endsWith(".pdf"))
    if (fileArray.length === 0) return

    const newInFlight = fileArray.map((file) => ({
      id: `inflight-${Date.now()}-${file.name}-${Math.random().toString(36).substring(2, 9)}`,
      fileName: file.name,
      file,
    }))

    setInFlightUploads((current) => [
      ...current,
      ...newInFlight.map((item) => ({ id: item.id, fileName: item.fileName })),
    ])

    newInFlight.forEach(async (item) => {
      try {
        const data = await api.uploadDocument(item.file)
        setInFlightUploads((current) => current.filter((x) => x.id !== item.id))
        setPendingUploads((current) => [
          ...current,
          {
            documentId: data.document_id,
            fileName: item.fileName,
            startedAt: Date.now(),
          },
        ])
        queryClient.invalidateQueries({ queryKey: ["documents"] })
      } catch (err) {
        const message = err instanceof Error ? err.message : "Upload failed"
        setInFlightUploads((current) => current.filter((x) => x.id !== item.id))
        setFailedUploads((current) => [
          ...current,
          {
            id: `request-${Date.now()}-${item.fileName}`,
            fileName: item.fileName,
            message,
          },
        ])
      }
    })
  }

  // Reconcile pending uploads against the polled document list, promoting the
  // stalled ones to failures.
  useEffect(() => {
    if (pendingUploads.length === 0) return

    const now = Date.now()
    const resolvedFailures: FailedUpload[] = []

    const nextPendingUploads = pendingUploads.filter((pending) => {
      const document = documents.find((item) => item.id === pending.documentId)

      if (!document) {
        // The upload returned an id but the row never appeared in the listing.
        if (now - pending.startedAt > UPLOAD_FAILURE_TIMEOUT_MS) {
          resolvedFailures.push({
            id: `timeout-${pending.documentId}`,
            documentId: pending.documentId,
            fileName: pending.fileName,
            message: "Upload failed to register on the server.",
          })
          return false
        }
        return true
      }

      if (document.status === "ready") {
        return false
      }

      if (document.status === "failed") {
        resolvedFailures.push({
          id: `doc-${pending.documentId}`,
          documentId: pending.documentId,
          fileName: pending.fileName,
          message: "Upload failed while the backend was processing the PDF.",
        })
        return false
      }

      if (document.status === "queued") {
        const start = new Date(document.updated_at || document.created_at).getTime()
        if (now - start > QUEUE_TIMEOUT_MS) {
          resolvedFailures.push({
            id: `timeout-${pending.documentId}`,
            documentId: pending.documentId,
            fileName: pending.fileName,
            message: "Upload stalled in queue. The backend may have crashed or is overloaded.",
          })
          return false
        }
        return true
      }

      if (document.status === "processing") {
        // Measured from the last activity, not from the upload: a large
        // document legitimately processes for far longer than the timeout.
        const lastActive = new Date(document.updated_at || document.created_at).getTime()
        if (now - lastActive > UPLOAD_FAILURE_TIMEOUT_MS) {
          resolvedFailures.push({
            id: `timeout-${pending.documentId}`,
            documentId: pending.documentId,
            fileName: pending.fileName,
            message: "Upload stalled while processing. The backend may have crashed.",
          })
          return false
        }
        return true
      }

      if (now - pending.startedAt > UPLOAD_FAILURE_TIMEOUT_MS) {
        resolvedFailures.push({
          id: `timeout-${pending.documentId}`,
          documentId: pending.documentId,
          fileName: pending.fileName,
          message: "Upload stalled while processing. The backend may have crashed.",
        })
        return false
      }

      return true
    })

    if (resolvedFailures.length > 0) {
      setFailedUploads((current) => {
        const next = [...current]
        for (const failure of resolvedFailures) {
          if (!next.some((item) => item.id === failure.id)) {
            next.push(failure)
          }
        }
        return next
      })
    }

    if (nextPendingUploads.length !== pendingUploads.length) {
      setPendingUploads(nextPendingUploads)
    }
  }, [documents, pendingUploads])

  // dismissFailedUpload clears the banner and, when the failure belongs to a
  // real document row, dismisses that row server-side so it does not come back
  // on the next poll.
  const dismissFailedUpload = async (id: string) => {
    setFailedUploads((current) => current.filter((item) => item.id !== id))

    const uuidMatch = id.match(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i)
    if (uuidMatch) {
      try {
        await api.dismissDocument(uuidMatch[0])
        queryClient.invalidateQueries({ queryKey: ["documents"] })
      } catch (err) {
        console.error("Failed to dismiss document:", err)
      }
    }
  }

  const trackResumedDocument = (documentId: string) => {
    setPendingUploads((current) => {
      if (current.some((x) => x.documentId === documentId)) {
        return current
      }
      return [
        ...current,
        {
          documentId,
          fileName: "Resumed PDF Document",
          startedAt: Date.now(),
        },
      ]
    })
  }

  return {
    inFlightUploads,
    failedUploads,
    uploadFiles,
    dismissFailedUpload,
    trackResumedDocument,
  }
}
