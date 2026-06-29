const API_BASE = "http://localhost:8080/api"

export interface DocumentInfo {
  id: string
  name: string
  total_pages: number
  split_pages: number
  parsed_pages: number
  status: "queued" | "processing" | "ready" | "failed"
  created_at: string
  updated_at: string
}

export interface SMTPConfig {
  host: string
  port: number
  username: string
  password: string
  encryption: string
  from_email: string
  from_name: string
}

async function adminFetch<T>(path: string, adminApiKey: string, options?: RequestInit): Promise<T> {
  const url = `${API_BASE}${path}`
  const response = await fetch(url, {
    ...options,
    headers: {
      "Content-Type": "application/json",
      "X-API-Key": adminApiKey,
      ...(options?.headers || {}),
    },
  })

  if (!response.ok) {
    const errText = await response.text()
    throw new Error(errText || `API Error: ${response.status}`)
  }

  if (response.status === 204) return null as T

  const text = await response.text()
  if (!text) return null as T
  return JSON.parse(text) as T
}

async function apiFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const url = `${API_BASE}${path}`
  const response = await fetch(url, {
    ...options,
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...(options?.headers || {}),
    },
  })

  if (!response.ok) {
    const errText = await response.text()
    throw new Error(errText || `API Error: ${response.status}`)
  }

  if (response.status === 204) return null as T
  const text = await response.text()
  if (!text) return null as T
  return JSON.parse(text) as T
}

export const adminApi = {
  // SMTP
  getSMTPConfig: (key: string) =>
    adminFetch<SMTPConfig>("/admin/settings/smtp", key),

  saveSMTPConfig: (key: string, config: SMTPConfig) =>
    adminFetch<{ status: string; message: string }>("/admin/settings/smtp", key, {
      method: "POST",
      body: JSON.stringify(config),
    }),

  testSMTPConfig: (key: string, config: SMTPConfig, to: string) =>
    adminFetch<{ status: string; message: string }>("/admin/settings/smtp/test", key, {
      method: "POST",
      body: JSON.stringify({ config, to }),
    }),

  // Maintenance
  cleanStaleProcesses: (key: string) =>
    adminFetch<{ status: string; message: string }>("/admin/clean-stale-processes", key, {
      method: "POST",
    }),

  // Documents (uses session auth from cookie — admin is expected to be logged in)
  listDocuments: () =>
    apiFetch<DocumentInfo[]>("/documents"),

  resumeDocument: (id: string) =>
    apiFetch<{ message: string; document_id: string }>(`/documents/${id}/resume`, {
      method: "POST",
    }),
}
