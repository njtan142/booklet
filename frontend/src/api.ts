const API_BASE = "http://localhost:8080/api";

export interface User {
  id: string;
  email: string;
  name: string;
}

export interface AuthStatus {
  authenticated: boolean;
  user?: User;
}

// Only 'pdf' documents are split and embedded. 'source' is a non-PDF upload
// awaiting conversion, 'export' is a download-only artifact with no pages.
export type DocumentKind = "pdf" | "source" | "export";

export interface DocumentInfo {
  id: string;
  name: string;
  total_pages: number;
  split_pages: number;
  parsed_pages: number;
  status: "queued" | "processing" | "ready" | "failed";
  kind: DocumentKind;
  mime_type: string;
  created_at: string;
  updated_at: string;
}

export interface PageDetail {
  page_number: number;
  text_preview: string;
  width: number;
  height: number;
}

export interface DocumentDetail extends DocumentInfo {
  pages: PageDetail[];
}

export interface BookletInfo {
  id: string;
  document_id: string;
  status: "compiling" | "ready" | "failed";
  created_at: string;
}

export interface BookletProgress {
  booklet_id: string;
  batch_size: number;
  completed_sheets: Record<number, boolean>;
  completed_batches?: Record<number, boolean>;
}

export interface BookletListResponse {
  id: string;
  document_id: string;
  document_name: string;
  total_pages: number;
  status: "compiling" | "ready" | "failed";
  config_margin: number;
  config_gutter: number;
  config_paper_size: string;
  config_signature_size: number;
  config_guides: boolean;
  created_at: string;
}

export interface SearchResult {
  document_id: string;
  document_name: string;
  page_number: number;
  text_snippet: string;
  similarity: number;
}

// Mirrors backend/tools.ParamType.
export type ToolParamType =
  | "string"
  | "int"
  | "bool"
  | "enum"
  | "page_range"
  | "password";

export interface ToolParam {
  name: string;
  label: string;
  type: ToolParamType;
  required: boolean;
  default?: unknown;
  options?: string[];
  min?: number;
  max?: number;
  help?: string;
}

// Mirrors backend/tools.Tool. The catalog is data-driven so a tool registered
// in the backend appears in the menu without a frontend change.
export interface Tool {
  slug: string;
  label: string;
  description: string;
  icon: string;
  params: ToolParam[];
  // max_inputs of 0 means unbounded (Merge).
  min_inputs: number;
  max_inputs: number;
  input_kinds: DocumentKind[];
  preserves_text: boolean;
}

export type JobStatus = "queued" | "running" | "completed" | "failed";

export interface Job {
  id: string;
  tool_slug: string;
  status: JobStatus;
  params: Record<string, unknown>;
  progress_current: number;
  progress_total: number;
  progress_step?: string;
  error?: string;
  attempt: number;
  max_attempts: number;
  created_at: string;
  started_at?: string;
  completed_at?: string;
  input_document_ids: string[];
  output_document_ids: string[];
}

export interface DocumentPermissions {
  document_id: string;
  owner_id: string;
  owner_email?: string;
  group_id?: string;
  group_name?: string;
  // Nine rwx bits, e.g. 420 = 0o644.
  mode: number;
}

export interface Group {
  id: string;
  name: string;
  is_personal: boolean;
  created_at: string;
  member_count?: number;
}

// selectionAllowsTool reports whether a tool can run on the current selection.
// The API enforces arity and kind independently; this only decides whether the
// menu offers the tool at all.
export function selectionAllowsTool(tool: Tool, selection: DocumentInfo[]): boolean {
  if (selection.length < tool.min_inputs) return false;
  if (tool.max_inputs > 0 && selection.length > tool.max_inputs) return false;
  return selection.every((doc) => tool.input_kinds.includes(doc.kind));
}

// Fetch helper with credentials
async function apiFetch<T>(path: string, options?: RequestInit): Promise<T> {
  const url = `${API_BASE}${path}`;
  const response = await fetch(url, {
    ...options,
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...(options?.headers || {}),
    },
  });

  if (!response.ok) {
    if (response.status === 401) {
      // Session expired, redirect to login if not already there
      if (!window.location.pathname.endsWith("/login")) {
        window.location.href = "/login";
      }
    }
    const errText = await response.text();
    throw new Error(errText || `API Error: ${response.status}`);
  }

  if (response.status === 204) {
    return null as any;
  }

  const text = await response.text();
  if (!text) {
    return null as any;
  }

  return JSON.parse(text) as T;
}

export const api = {
  // Auth
  getMe: () => apiFetch<AuthStatus>("/auth/me"),
  logoutUrl: () => `http://localhost:8080/api/auth/logout`,
  loginUrl: () => `http://localhost:8080/api/auth/login`,
  devLoginUrl: () => `http://localhost:8080/api/auth/dev/login`,

  // Documents
  listDocuments: () => apiFetch<DocumentInfo[]>("/documents"),
  getDocument: (id: string) => apiFetch<DocumentDetail>(`/documents/${id}`),
  dismissDocument: (id: string) => apiFetch<void>(`/documents/${id}/dismiss`, { method: "POST" }),
  
  renameDocument: (id: string, name: string) =>
    apiFetch<{ id: string; name: string }>(`/documents/${id}/rename`, {
      method: "POST",
      body: JSON.stringify({ name }),
    }),

  deleteDocument: (id: string) =>
    apiFetch<void>(`/documents/${id}/delete`, { method: "POST" }),
  
  uploadDocument: async (file: File): Promise<{ document_id: string }> => {
    const formData = new FormData();
    formData.append("file", file);
    
    const response = await fetch(`${API_BASE}/documents/upload`, {
      method: "POST",
      body: formData,
      credentials: "include",
      // Note: do not set Content-Type header when uploading FormData, 
      // the browser will automatically set it with boundary parameters.
    });

    if (!response.ok) {
      const errText = await response.text();
      throw new Error(errText || `Upload failed: ${response.status}`);
    }

    return response.json();
  },

  // Booklet
  listBooklets: () => apiFetch<BookletListResponse[]>("/booklets"),
  
  compileBooklet: (
    docId: string, 
    config: { margin: number; gutter: number; paper_size: string; signature_size: number; guides: boolean }
  ) => apiFetch<{ booklet_id: string }>(`/documents/${docId}/booklet/compile`, {
    method: "POST",
    body: JSON.stringify(config),
  }),
  
  resumeDocument: (id: string) => apiFetch<{ message: string; document_id: string }>(`/documents/${id}/resume`, {
    method: "POST",
  }),

  cleanupBookletSessions: (
    docId: string,
    config: {
      margin: number;
      gutter: number;
      paper_size: string;
      signature_size: number;
      guides: boolean;
      current_booklet_id: string;
    }
  ) => apiFetch<{ message: string }>(`/documents/${docId}/booklet/cleanup`, {
    method: "POST",
    body: JSON.stringify(config),
  }),
  
  getBooklet: (id: string) => apiFetch<BookletInfo>(`/booklets/${id}`),
  
  getDownloadUrl: (bookletId: string, filter?: string, sheets?: string, pages?: string) => {
    let urlStr = `${API_BASE}/booklets/${bookletId}/download`;
    const params = new URLSearchParams();
    if (filter) params.append("filter", filter);
    if (sheets) params.append("sheets", sheets);
    if (pages) params.append("pages", pages);
    const query = params.toString();
    return query ? `${urlStr}?${query}` : urlStr;
  },

  getPagePdfUrl: (docId: string, pageNum: number) => {
    return `${API_BASE}/documents/${docId}/pages/${pageNum}/pdf`;
  },

  getBookletPreviewUrl: (docId: string, margin: number, gutter: number, paperSize: string, sigSize: number, guides: boolean, side: string) => {
    return `${API_BASE}/documents/${docId}/booklet/preview?margin=${margin}&gutter=${gutter}&paper_size=${paperSize}&signature_size=${sigSize}&guides=${guides}&side=${side}`;
  },

  // Search
  search: (query: string, docId?: string) => {
    let path = `/search?q=${encodeURIComponent(query)}`;
    if (docId) path += `&document_id=${docId}`;
    return apiFetch<SearchResult[]>(path);
  },
  
  getSearchPreviewUrl: (docId: string, query: string) => {
    return `${API_BASE}/documents/${docId}/search-preview?q=${encodeURIComponent(query)}`;
  },
  
  emailBooklet: (bookletId: string, email: string) => apiFetch<{ status: string; message: string }>(`/booklets/${bookletId}/email`, {
    method: "POST",
    body: JSON.stringify({ email }),
  }),
  
  getBookletProgress: (bookletId: string) => apiFetch<BookletProgress>(`/booklets/${bookletId}/progress`),
  
  updateBookletProgress: (
    bookletId: string,
    batchSize: number,
    completedSheets: Record<number, boolean>
  ) => apiFetch<{ message: string }>(`/booklets/${bookletId}/progress`, {
    method: "POST",
    body: JSON.stringify({ batch_size: batchSize, completed_sheets: completedSheets }),
  }),

  // Tools & jobs
  listTools: () => apiFetch<Tool[]>("/tools"),

  createToolJob: (
    toolSlug: string,
    inputDocumentIds: string[],
    params: Record<string, unknown> = {}
  ) => apiFetch<{ job_id: string }>("/tools/jobs", {
    method: "POST",
    body: JSON.stringify({
      tool_slug: toolSlug,
      input_document_ids: inputDocumentIds,
      params,
    }),
  }),

  getToolJob: (jobId: string) => apiFetch<Job>(`/tools/jobs/${jobId}`),

  listToolJobs: (limit?: number) =>
    apiFetch<Job[]>(limit ? `/tools/jobs?limit=${limit}` : "/tools/jobs"),

  // Permissions & groups
  getDocumentPermissions: (docId: string) =>
    apiFetch<DocumentPermissions>(`/documents/${docId}/permissions`),

  updateDocumentPermissions: (
    docId: string,
    update: { owner_id?: string; group_id?: string; mode?: number }
  ) => apiFetch<DocumentPermissions>(`/documents/${docId}/permissions`, {
    method: "PUT",
    body: JSON.stringify(update),
  }),

  listGroups: () => apiFetch<Group[]>("/groups"),
};
