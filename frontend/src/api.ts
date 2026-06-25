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

export interface DocumentInfo {
  id: string;
  name: string;
  total_pages: number;
  status: "processing" | "ready" | "failed";
  created_at: string;
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

export interface SearchResult {
  document_id: string;
  document_name: string;
  page_number: number;
  text_snippet: string;
  similarity: number;
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
  compileBooklet: (
    docId: string, 
    config: { margin: number; gutter: number; paper_size: string; signature_size: number }
  ) => apiFetch<{ booklet_id: string }>(`/documents/${docId}/booklet/compile`, {
    method: "POST",
    body: JSON.stringify(config),
  }),
  
  getBooklet: (id: string) => apiFetch<BookletInfo>(`/booklets/${id}`),
  
  getDownloadUrl: (bookletId: string, filter?: string, sheets?: string, pages?: string) => {
    let urlStr = `http://localhost:8080/api/booklets/${bookletId}/download`;
    const params = new URLSearchParams();
    if (filter) params.append("filter", filter);
    if (sheets) params.append("sheets", sheets);
    if (pages) params.append("pages", pages);
    const query = params.toString();
    return query ? `${urlStr}?${query}` : urlStr;
  },

  // Search
  search: (query: string, docId?: string) => {
    let path = `/search?q=${encodeURIComponent(query)}`;
    if (docId) path += `&document_id=${docId}`;
    return apiFetch<SearchResult[]>(path);
  },
};
