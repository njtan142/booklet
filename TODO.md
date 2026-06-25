# PDF Booklet Maker & Semantic Search - Project Checklist

This checklist tracks the status of all components and features as defined in the implementation plan and project rules.

---

## 1. Project Initialization & Setup
- [ ] Initialize Git repository (`git init`)
- [ ] Initialize Go backend module (`go mod init booklet`)
- [ ] Initialize Vite React frontend with TypeScript (`npx create-vite`)
- [ ] Add project rules `.agents/AGENTS.md`
- [ ] Establish approved implementation plan `implementation_plan.md`
- [ ] Maintain `shadcn-doctor` dependency as a `devDependency` in frontend `package.json`

---

## 2. Database Layer (`PostgreSQL` + `pg_vector`)
- [ ] Enable `pg_vector` extension on startup (`CREATE EXTENSION IF NOT EXISTS vector`)
- [ ] Create `users` table:
  - [ ] `id` (text primary key)
  - [ ] `email` (text unique not null)
  - [ ] `name` (text)
  - [ ] `created_at` / `updated_at` (timestamps)
- [ ] Create `documents` table:
  - [ ] `id` (uuid primary key)
  - [ ] `name` (text not null)
  - [ ] `total_pages` (int not null)
  - [ ] `status` (text not null)
  - [ ] `created_at` / `updated_at` (timestamps)
- [ ] Create `document_pages` table:
  - [ ] `id` (uuid primary key)
  - [ ] `document_id` (uuid reference cascade)
  - [ ] `page_number` (int not null)
  - [ ] `text_content` (text not null)
  - [ ] `embedding` (vector type matching configured dimensions)
  - [ ] `storage_path` (text path in object store)
  - [ ] `width` / `height` (double precision dimensions)
- [ ] Create unique composite constraint index on `document_pages(document_id, page_number)`
- [ ] Create HNSW vector search index using cosine distance operators (`vector_cosine_ops`)
- [ ] Create `compiled_booklets` table:
  - [ ] `id` (uuid primary key)
  - [ ] `document_id` (uuid reference cascade)
  - [ ] `status` (text not null)
  - [ ] `storage_path` (text)
  - [ ] `config_margin` (double precision margins)
  - [ ] `config_gutter` (double precision gutter space)
  - [ ] `config_paper_size` (text paper format)
  - [ ] `config_signature_size` (int signature page count)
- [ ] Implement database connection retry loop on boot (10 attempts, 3-second delays)

---

## 3. Authentication & Session Middleware
- [ ] Implement OIDC provider redirection (generating random anti-CSRF state tokens)
- [ ] Implement OIDC redirect login callback code exchange
- [ ] Implement JWT token generation signing session claims (user ID, email, name, expirations)
- [ ] Protect handler routes with JWT verification cookie parser middleware (`RequireAuth`)
- [ ] Implement session cookie clearance upon session logout or validation timeouts
- [ ] Create Developer Bypass (Mock Auth) mode returning signed JWT tokens on form inputs

---

## 4. Backend Processing & Storage Services
- [ ] Split multi-page PDF files into single-page PDFs in a temporary workspace using `pdfcpu`
- [ ] Extract plain text from split PDF pages using `github.com/dslipak/pdf`
- [ ] Connect MinIO S3-compatible storage driver
- [ ] Create MinIO bucket auto-provisioner checking/making buckets on startup
- [ ] Implement helper to upload single pages and compiled booklet PDFs to MinIO
- [ ] Integrate Ollama embedding API connector (`all-minilm` 384-dimension embeddings)
- [ ] Implement hash-based deterministic Bag-of-Words Mock embedding fallback

---

## 5. Booklet Canvas Compiler & Filter Endpoints
- [ ] Implement booklet sequence layout calculations (`CalculateBookletLayout`)
- [ ] Programmatically draw page slots side-by-side on landscape PDF canvas using `gopdf`
  - [ ] Apply margins and gutter padding metrics
  - [ ] Scale page sizes maintaining aspect ratios
  - [ ] Support blank/padded sheet slots when page counts are not multiples of signature size
- [ ] Write booklet page filtering function (`FilterBookletPages`) supporting:
  - [ ] `filter=fronts` (odd booklet pages)
  - [ ] `filter=backs` (even booklet pages)
  - [ ] `sheets=Start-End` (custom sheet range)
- [ ] Write booklet-page-to-physical-sheet translator (`MapPagesToSheets`)
- [ ] Expose REST endpoints:
  - [ ] `GET /api/documents` (list library)
  - [ ] `GET /api/documents/{id}` (fetch details and page dimension metadata)
  - [ ] `POST /api/documents/upload` (process uploaded PDF)
  - [ ] `POST /api/documents/{id}/booklet/compile` (trigger compilation)
  - [ ] `GET /api/booklets/{id}` (poll status)
  - [ ] `GET /api/booklets/{id}/download` (download with filter/sheet/page parameters)
  - [ ] `GET /api/search` (vector search queries)

---

## 6. React Frontend Application (Vite SPA)
- [ ] Configure Tailwind CSS v4 and PostCSS plugins
- [ ] Resolve bundler typescript warning limits using `import type`
- [ ] Install Shadcn components (`button`, `input`, `select`, `label`, `tabs`, `card`)
- [ ] Verify styling compliance by running `npx shadcn-doctor`
- [ ] Setup TanStack Router routing paths
- [ ] Setup TanStack Query hooks, query cache invalidations, and mutations
- [ ] Build **Login UI**:
  - [ ] OIDC standard SSO redirect buttons
  - [ ] Developer Bypass form tab (email, name text boxes)
- [ ] Build **Dashboard UI**:
  - [ ] Drag-and-drop file uploader (UploadCloud icon triggers, progress spinners)
  - [ ] Library list (statuses: ready checks, processing loaders, failed alerts)
  - [ ] Imposition settings forms (margin, gutter inputs, paper size select, signature select)
- [ ] Build **Semantic Search UI**:
  - [ ] Query text inputs and document filter dropdowns
  - [ ] Search score indicators (cosine similarity percentage cards)
  - [ ] Highlighted keyword text snippet renders
- [ ] Build **Printing Helper Wizard**:
  - [ ] Batch download lists (fronts, backs, and sheet ranges in groups of 5, 10, 20 sheets)
  - [ ] Batch progress toggles (marking completed sheets)
  - [ ] Ruined booklet page input recovery console
  - [ ] Visual printer warnings (feed arrow testing tips, short-edge landscape flip guides)

---

## 7. SRE Observability & Docker Compose
- [ ] Write multi-stage Go backend `Dockerfile` compiling Go in a builder container
- [ ] Write Nginx frontend `Dockerfile` copying Vite assets and template SPA `nginx.conf`
- [ ] Instrument backend handlers with Prometheus counters and histograms
- [ ] Configure scraping jobs targeting `backend:8080` in `prometheus/prometheus.yml`
- [ ] Auto-provision Prometheus datasource in `grafana/provisioning/datasources/datasource.yml`
- [ ] Auto-provision dashboards in `grafana/provisioning/dashboards/dashboards.yml`
- [ ] Design custom Grafana SRE Dashboard (`booklet_dashboard.json`) containing panels:
  - [ ] HTTP Request rates
  - [ ] HTTP 95th-percentile Latency
  - [ ] Document Upload rates
  - [ ] Booklet Compilation 90th-percentile durations
  - [ ] Vector Search 90th-percentile durations
- [ ] Write `docker-compose.yml` linking `db`, `minio`, `ollama`, `prometheus`, `grafana`, `backend`, and `frontend`

---

## 8. Verification & Test Suite
- [ ] Write Go unit tests verifying signature pagination sequence math (`pdf_test.go`)
- [ ] Write Go unit tests verifying booklet-page-to-sheet translation mapping math (`pdf_test.go`)
- [ ] Write Playwright configuration (`playwright.config.ts`)
- [ ] Write Playwright E2E browser tests (`booklet.spec.ts`) covering:
  - [ ] Developer mock bypass session logins
  - [ ] Library document uploads
  - [ ] Custom compilation triggers
  - [ ] Sliced front/back and reprint downloads
  - [ ] Semantic query vector search matches
- [ ] Execute test suites inside containerized environments
