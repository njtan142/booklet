# Booklet Studio

Booklet Studio is a full-featured web application designed to convert uploaded PDF documents into custom imposition layouts for double-sided booklet printing (duplex layouts) while enabling semantic vector search over the parsed text content of the uploaded documents. 

The system is designed with a modern stateless Go backend, two independent React frontends (a User Dashboard and an Admin Control Panel), a PostgreSQL database utilizing the `pgvector` extension, MinIO object storage, and a complete SRE observability suite (Prometheus, Grafana, and Dozzle).

---

## Table of Contents

- [System Architecture](#system-architecture)
- [Key Workflows & Design Rules](#key-workflows--design-rules)
  - [1. Separation of Concerns & Stateless Backend](#1-separation-of-concerns--stateless-backend)
  - [2. Document Upload & Page-Splitting Pipeline](#2-document-upload--page-splitting-pipeline)
  - [3. Booklet Imposition Mathematics](#3-booklet-imposition-mathematics)
  - [4. Duplex Printing & Ruined Booklet Recovery Flow](#4-duplex-printing--ruined-booklet-recovery-flow)
  - [5. SMTP Configuration & Booklet Emailing](#5-smtp-configuration--booklet-emailing)
  - [6. Background Maintenance & Process Cleanup](#6-background-maintenance--process-cleanup)
- [Database Schema](#database-schema)
- [REST API Endpoints](#rest-api-endpoints)
- [Configuration & Environment Variables](#configuration--environment-variables)
- [Local Development & Docker Compose](#local-development--docker-compose)
  - [Prerequisites](#prerequisites)
  - [Docker Compose Quickstart](#docker-compose-quickstart)
  - [Running Services Locally (Without Docker)](#running-services-locally-without-docker)
  - [Developer Login & Mock Auth bypass](#developer-login--mock-auth-bypass)
- [Testing Suite](#testing-suite)
- [SRE Observability](#sre-observability)
- [UI Standards & Styling](#ui-standards--styling)

---

## System Architecture

The application comprises multiple Docker containers defined in the root [docker-compose.yml](docker-compose.yml):

```mermaid
graph TD
    UserClient[React Frontend - Port 80/5173] -->|API Requests| GoBackend[Go REST Backend - Port 8080]
    AdminClient[Admin Control Panel - Port 8082] -->|Admin API Requests| GoBackend
    GoBackend -->|Relational Data & Vectors| Postgres[PostgreSQL + pgvector - Port 5432]
    GoBackend -->|Blobs: Split PDFs & booklets| MinIO[MinIO Object Storage - Port 9000]
    GoBackend -->|Embedding Generation| Ollama[Ollama LLM Engine - Port 11434]
    Prometheus[Prometheus - Port 9090] -->|Scrapes /metrics| GoBackend
    Grafana[Grafana - Port 3002] -->|Visualizes dashboards| Prometheus
    Dozzle[Dozzle Log Viewer - Port 8081] -->|Docker Logs| DockerDaemon[Docker Daemon]
```

### Components

1. **User Frontend** ([frontend/](frontend)): A Vite-based Single Page Application (SPA). It is built using React 19, TypeScript, Tailwind CSS, Radix primitives, and Shadcn UI. Routing is managed by TanStack Router and state management by TanStack Query. It communicates with the backend via [frontend/src/api.ts](frontend/src/api.ts).
2. **Admin Frontend** ([admin/](admin)): A separate administration panel. It allows system administrators to adjust global SMTP options, send test emails, trigger background database cleanups, and monitor Prometheus metrics. Configured using React 19, TypeScript, Tailwind CSS, and Shadcn UI components.
3. **Go Backend** ([backend/](backend)): A REST service written in Go (v1.22+). It exposes endpoints for authentication, document management, booklet layout calculations, vector searches, and email dispatch.
4. **PostgreSQL Database**: Configured with the `pgvector` extension to allow indexing and cosine distance searches on document page embedding vectors.
5. **MinIO Object Storage**: An S3-compliant object store used to store the original documents, single-page PDF splits, and compiled booklet outputs.
6. **Ollama / Embedding Provider**: Running `all-minilm` (384 dimensions) for text embeddings. Supports a lightweight Bag-of-Words Mock fallback for offline run configurations.
7. **Observability Stack**: Prometheus for metric scraping, Grafana for dashboard visualizations, and Dozzle for container log viewing.

---

## Key Workflows & Design Rules

To ensure performance, data integrity, and compliance with the project guidelines, Booklet Studio follows strict design principles:

### 1. Separation of Concerns & Stateless Backend
- The Go backend does **not** store any session or application state in memory. 
- All intermediate states (document upload progress, booklet compilation status, and compiled booklet files) are persisted immediately to PostgreSQL and MinIO.
- Booklet compilation and duplex page mapping are decoupled from the document upload pipeline. The initial document upload only splits pages, extracts text, and computes vector embeddings. Canvas drawing is done strictly on-demand.

### 2. Document Upload & Page-Splitting Pipeline
When a document is uploaded via [HandleUploadDocument](backend/handlers/handlers.go#L600):
1. A new document record is inserted into the `documents` table in PostgreSQL with a status of `processing` and `total_pages` extracted from the header.
2. The uploaded file is saved to MinIO in the bucket designated by the `MINIO_BUCKET` environment variable.
3. A background worker triggers the splitting process.
4. The backend uses the `pdfcpu` API to split the PDF document into individual single-page PDFs (split files are cached locally in a temp workspace before uploading to MinIO).
5. For each split page:
   - The page PDF is uploaded to MinIO.
   - Plain text content is extracted using `github.com/dslipak/pdf`.
   - The extracted text is sent to the Ollama embedding engine (or the Mock fallback) to generate a vector representation of the configured dimensions (typically 384 or 768).
   - A row is inserted in `document_pages` including page dimensions, text, embedding vector, and MinIO storage path.
   - Database counters (`split_pages`, `parsed_pages`) are updated incrementally.
6. Once all pages are successfully processed, the document status is updated to `ready`. If an error occurs, the status is set to `failed` (allowing the user to restart via the `/api/documents/{id}/resume` route).

### 3. Booklet Imposition Mathematics
Booklet layout calculations are defined in [CalculateBookletLayout](backend/pdf/pdf.go#L765).

#### Signatures
Booklet printing is structured in **signatures**. A signature is a booklet block of folded pages. A single folded sheet of paper contains **4 pages** (Front-Left, Front-Right, Back-Left, Back-Right). The signature size $S$ must therefore be a multiple of 4 (e.g. 4, 8, 12, 16). 

If a document has $N$ pages and a signature size $S$, the total booklet page count $M$ is rounded up to the nearest multiple of $S$:
$$M = \lceil N / S \rceil \times S$$
Any padded pages beyond $N$ ($idx > N$) are rendered as blank pages ($0$ index).

#### Imposition Algorithm
For each signature $sig$ (from $0$ to $M/S - 1$) and sheet $k$ within the signature (from $0$ to $S/4 - 1$):
- $base = sig \times S$
- $p_1 = base + 2k + 1$
- $p_2 = base + 2k + 2$
- $p_3 = base + S - 2k - 1$
- $p_4 = base + S - 2k$

For each physical sheet, the algorithm maps pages to sides as follows:
- **Front Side**: Left Page = $p_4$, Right Page = $p_1$
- **Back Side**: Left Page = $p_2$, Right Page = $p_3$

*Example: $N=4, S=4$ (1 physical sheet)*
- Front Side: Left = Page 4, Right = Page 1
- Back Side: Left = Page 2, Right = Page 3
*When folded down the center, Page 1 forms the front cover, Page 2 and 3 form the inside pages, and Page 4 forms the back cover.*

*Example: $N=6, S=8$ (2 physical sheets, padded to 8 pages)*
- Sheet 1 Front: Left = Page 8 (blank), Right = Page 1
- Sheet 1 Back: Left = Page 2, Right = Page 7 (blank)
- Sheet 2 Front: Left = Page 6, Right = Page 3
- Sheet 2 Back: Left = Page 4, Right = Page 5

#### Canvas Rendering
The backend positions these pages side-by-side onto landscape canvas sheets (A4 Landscape or Letter Landscape) using the `gopdf` library. It scales pages while maintaining their aspect ratio, centers them, applies margins and gutters, and optionally draws a dashed folding guideline down the center split of each sheet if `guides` is enabled in the configuration.

### 4. Duplex Printing & Ruined Booklet Recovery Flow
When downloading compiled booklets, users must follow a specific printing flow using the **Printing Helper Wizard**:

#### Printing Process
1. **First Pass (Fronts)**: Download the fronts-only PDF (`/api/booklets/{id}/download?filter=fronts`). Print this file.
2. **Flip & Feed**: Re-insert the printed sheets back into the printer input tray. Depending on the printer:
   - For long-edge binding landscape layouts, flip the sheets along the short-edge of the paper.
   - Pay attention to the feeding direction (an arrow test page can be printed first using the wizard's feed arrow guides).
3. **Second Pass (Backs)**: Download the backs-only PDF (`/api/booklets/{id}/download?filter=backs`). Print this file.
4. **Fold & Bind**: Fold the printed sheet stack down the middle and staple/bind.

```
       Front Pass (filter=fronts)                     Back Pass (filter=backs)
      +------------+------------+                  +------------+------------+
      |            |            |                  |            |            |
      |   Page 4   |   Page 1   |                  |   Page 2   |   Page 3   |
      |            |            |                  |            |            |
      +------------+------------+                  +------------+------------+
                     \                                    /
                      \                                  /
                       +--------------------------------+
                       |             Fold               |
                       |       (Center Line)            |
                       |               |                |
                       |               v                |
```

#### Ruined Page Recovery
If a printing jam occurs on booklet page $P$ during the duplex process, the user does not need to reprint the entire booklet. 
1. The user inputs the ruined page number into the **Ruined Booklet Page Recovery Console** in the UI.
2. The UI maps the page to its corresponding physical sheet $S$ using the formula:
   $$Sheet = \lfloor (P + 1) / 2 \rfloor$$
3. The backend compiles and serves a customized PDF containing only that sheet or sheet range (e.g. `/api/booklets/{id}/download?sheets=3-3`) so the user can replace only the ruined papers.

### 5. SMTP Configuration & Booklet Emailing
- The system includes a database-driven SMTP config layer managed by [smtp/smtp.go](backend/smtp/smtp.go).
- Administrators can configure host, port, username, password, encryption methods (`none`, `ssl`, `starttls`), and sender details dynamically.
- **Fallback Hierarchy**: The backend first queries Postgres for configuration. If no record is found in the database, it automatically falls back to standard environment variables (`SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_ENCRYPTION`, `SMTP_FROM_EMAIL`, `SMTP_FROM_NAME`).
- **Security & Password Masking**: When retrieval endpoints are called, the SMTP password is returned as a masked string `********`. When saving, if the backend receives `********` for the password field, it preserves the existing password in the database instead of overwriting it, protecting secret credentials.
- **Email Sending**: Dispatched via [HandleEmailBooklet](backend/handlers/handlers.go#L1250) as an asynchronous task, generating the PDF attachment and sending a MIME multipart message to the requested email.

### 6. Background Maintenance & Process Cleanup
To prevent background processes from hanging indefinitely due to service failures, the database includes a cleanup function [FailStaleProcessingDocuments](backend/db/db.go#L201). 
- It updates any document records stuck in `queued` or `processing` status for more than 15 minutes, marking them as `failed`.
- It updates any compiled booklet records stuck in `compiling` status for more than 15 minutes, marking them as `failed`.
- This utility is exposed via a secure administrative API endpoint `/api/admin/clean-stale-processes` requiring `X-API-Key` headers, which can be hooked into an external cron scheduler or triggered directly from the Admin Maintenance tab.

---

## Database Schema

Database migrations and initial schema provisioning are handled programmatically on boot in [db/db.go](backend/db/db.go). 

```sql
-- Enables the pgvector extension for high-performance vector searches
CREATE EXTENSION IF NOT EXISTS vector;

-- 1. Users Table
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    email TEXT UNIQUE NOT NULL,
    name TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 2. Documents Table
CREATE TABLE IF NOT EXISTS documents (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    total_pages INT NOT NULL,
    split_pages INT DEFAULT 0,
    parsed_pages INT DEFAULT 0,
    status TEXT NOT NULL, -- 'queued', 'processing', 'ready', 'failed'
    is_dismissed BOOLEAN DEFAULT FALSE,
    original_storage_path TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 3. Document Pages Table
CREATE TABLE IF NOT EXISTS document_pages (
    id UUID PRIMARY KEY,
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    page_number INT NOT NULL,
    text_content TEXT NOT NULL,
    embedding vector(384), -- Dimension matches config (e.g., 384 for all-minilm)
    storage_path TEXT NOT NULL,
    width DOUBLE PRECISION NOT NULL,
    height DOUBLE PRECISION NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Indices for Document Pages
CREATE UNIQUE INDEX IF NOT EXISTS idx_doc_pages_num ON document_pages(document_id, page_number);
CREATE INDEX IF NOT EXISTS idx_doc_pages_embedding ON document_pages USING hnsw (embedding vector_cosine_ops);

-- 4. Compiled Booklets Table
CREATE TABLE IF NOT EXISTS compiled_booklets (
    id UUID PRIMARY KEY,
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    status TEXT NOT NULL, -- 'compiling', 'ready', 'failed'
    storage_path TEXT,
    config_margin DOUBLE PRECISION NOT NULL,
    config_gutter DOUBLE PRECISION NOT NULL,
    config_paper_size TEXT NOT NULL,
    config_signature_size INT NOT NULL,
    config_guides BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 5. SMTP Configurations Table
CREATE TABLE IF NOT EXISTS smtp_config (
    id TEXT PRIMARY KEY DEFAULT 'global',
    host TEXT NOT NULL,
    port INT NOT NULL,
    username TEXT NOT NULL,
    password TEXT NOT NULL,
    encryption TEXT NOT NULL, -- 'none', 'ssl', 'starttls'
    from_email TEXT NOT NULL,
    from_name TEXT,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
```

---

## REST API Endpoints

### Authentication

- `GET /api/auth/login`
  Redirects the user to the OIDC identity provider (SSO login flow).
- `GET /api/auth/callback`
  Callback path for OIDC code exchange, issuing the signed JWT token.
- `GET /api/auth/logout`
  Clears authentication cookies and invalidates the session.
- `GET /api/auth/me`
  Returns details of the currently authenticated user session.
- `POST /api/auth/dev/login` *(Enabled only when `APP_ENV=development`)*
  Mock authentication form handler bypassing OIDC, returning a developer bypass JWT token.

### Document Management

- `GET /api/documents`
  Lists all documents in the library (excludes dismissed items).
- `GET /api/documents/{id}`
  Retrieves metadata and processing counters for a specific document.
- `POST /api/documents/upload`
  Accepts a multipart file upload (`file`) and initiates the page-splitting background pipeline.
- `POST /api/documents/{id}/dismiss`
  Soft-deletes a document by setting `is_dismissed = true` (removed from default dashboard lists).
- `POST /api/documents/{id}/resume`
  Resets a failed document's processing state and restarts the split/parse/embed background routine.
- `GET /api/documents/{id}/pages/{page_number}/pdf`
  Downloads the single-page PDF slice corresponding to a specific page number.

### Booklet Imposition

- `GET /api/documents/{id}/booklet/preview`
  Generates a real-time on-demand dynamic PDF preview of the first physical sheet of the booklet for the current settings.
- `POST /api/documents/{id}/booklet/compile`
  Initiates booklet rendering on the backend. Parameters can be passed in a JSON payload:
  ```json
  {
    "margin": 20,
    "gutter": 10,
    "paper_size": "A4",
    "signature_size": 8,
    "guides": true
  }
  ```
- `POST /api/documents/{id}/booklet/cleanup`
  Removes compiled booklets associated with a document to free up space.
- `GET /api/booklets`
  Lists compiled booklet files.
- `GET /api/booklets/{id}`
  Retrieves status details of a compiled booklet.
- `GET /api/booklets/{id}/download`
  Downloads the compiled booklet PDF. Supports query parameters:
    - `filter` (string): `fronts`, `backs`.
    - `sheets` (string): e.g., `1-5`, `3`.

### Semantic Search

- `GET /api/search`
  Performs a global search across all documents. Query parameters:
    - `q` (string): search query.
    - `document_id` (string, optional): restricts search to a specific document.
  Returns page matches, raw text snippets, and cosine similarity scores.
- `GET /api/documents/{id}/search-preview`
  Generates a modified version of the document's PDF with matching search terms highlighted.

### Administrative Control Panel

These routes require authentication using the `X-API-Key` HTTP Header.

- `POST /api/admin/clean-stale-processes`
  Runs a database cleanup, marking processing/compiling tasks older than 15 minutes as `failed`.
- `GET /api/admin/settings/smtp`
  Retrieves current global SMTP server details (masks password).
- `POST /api/admin/settings/smtp`
  Saves global SMTP server configurations.
- `POST /api/admin/settings/smtp/test`
  Dispatches a mock test email to verify SMTP host, credentials, and encryption capabilities.

### Metrics

- `GET /metrics`
  Exposes system metrics scraped by Prometheus (counters, histograms, heap summaries).

---

## Configuration & Environment Variables

| Variable | Description | Default |
|---|---|---|
| `PORT` | Local port the Go backend listens on. | `8080` |
| `APP_ENV` | Mode of operation (`development` or `production`). | `development` |
| `DATABASE_URL` | Connection string for PostgreSQL database. | `postgres://postgres:postgres@localhost:5432/booklet?sslmode=disable` |
| `MINIO_ENDPOINT` | MinIO connection endpoint. | `localhost:9000` |
| `MINIO_ACCESS_KEY` | Access key credentials for MinIO. | `minioadmin` |
| `MINIO_SECRET_KEY` | Secret key credentials for MinIO. | `minioadmin` |
| `MINIO_USE_SSL` | Enable SSL for MinIO connection (`true`/`false`). | `false` |
| `MINIO_BUCKET` | Destination bucket name in MinIO. | `booklet` |
| `EMBEDDING_PROVIDER` | Engine to calculate vector embeddings (`ollama` or `mock`). | `mock` |
| `EMBEDDING_DIMENSION` | Dimensions of vectors generated by embeddings provider. | `384` |
| `OLLAMA_HOST` | Endpoint of the Ollama server. | `http://localhost:11434` |
| `FRONTEND_URL` | Canonical URL of the user frontend. | `http://localhost:5173` |
| `ALLOWED_ORIGINS` | Comma-separated list of origins permitted by CORS middleware. | `http://localhost:5173` |
| `MAX_PARALLEL_DOCUMENTS`| Max number of files processed concurrently. | `5` |
| `SPLIT_CHUNK_SIZE` | Chunk size of pages extracted per batch to manage memory. | `100` |
| `PAGE_PARSING_TIMEOUT_SECONDS` | Timeout duration allowed for parsing text on a single page. | `5` |
| `ADMIN_API_KEY` | API secret key protecting routes under `/api/admin/*`. | `dev-admin-key` |
| `SMTP_HOST` | Host address of default SMTP configuration. | *empty* |
| `SMTP_PORT` | Port of default SMTP configuration. | `587` |
| `SMTP_USERNAME` | Username credentials of default SMTP configuration. | *empty* |
| `SMTP_PASSWORD` | Password credentials of default SMTP configuration. | *empty* |
| `SMTP_ENCRYPTION` | Encryption layer (`none`, `ssl`, `starttls`). | `none` |
| `SMTP_FROM_EMAIL` | Sender address of default SMTP configuration. | `noreply@booklet.local` |
| `SMTP_FROM_NAME` | Sender display name of default SMTP configuration. | `Booklet Studio` |

---

## Local Development & Docker Compose

### Prerequisites
- **Docker & Docker Compose** (Recommended to run all infrastructure components easily)
- **Go (v1.22+)** (If executing the backend locally)
- **Node.js (v20+)** (If executing the React frontends locally)

---

### Docker Compose Quickstart
To bootstrap all dependencies, frontends, and backend services:

```bash
# Build and run all services in the foreground
docker-compose up --build

# Run in background (detached mode)
docker-compose up -d --build

# View container logs
docker-compose logs -f
```

#### Default Services Ports
- **User Dashboard**: [http://localhost:80](http://localhost:80)
- **Admin Dashboard**: [http://localhost:8082](http://localhost:8082)
- **Go REST Backend**: [http://localhost:8080](http://localhost:8080)
- **MinIO S3 Panel**: [http://localhost:9001](http://localhost:9001) (User: `minioadmin`, Password: `minioadmin`)
- **Grafana Dashboard**: [http://localhost:3002](http://localhost:3002) (User: `admin`, Password: `admin`)
- **Prometheus Scraper**: [http://localhost:9090](http://localhost:9090)
- **Dozzle Log Viewer**: [http://localhost:8081](http://localhost:8081)

---

### Running Services Locally (Without Docker)

If you prefer to run the codebase locally (for instance, to debug backend code or compile frontends directly):

#### 1. Setup Local Infrastructure
Run only PostgreSQL and MinIO using Docker:
```bash
docker run -d --name local-db -p 5432:5432 -e POSTGRES_DB=booklet -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=postgres pgvector/pgvector:pg16
docker run -d --name local-minio -p 9000:9000 -p 9001:9001 -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin minio/minio:latest server /data --console-address ":9001"
```

#### 2. Run the Backend
Set the appropriate environment variables and compile the Go backend:
```bash
cd backend
$env:DATABASE_URL="postgres://postgres:postgres@localhost:5432/booklet?sslmode=disable"
$env:MINIO_ENDPOINT="localhost:9000"
$env:MINIO_ACCESS_KEY="minioadmin"
$env:MINIO_SECRET_KEY="minioadmin"
$env:EMBEDDING_PROVIDER="mock" # Uses lightweight hash-based vectors
$env:APP_ENV="development"
go run main.go
```

#### 3. Run the User Frontend
```bash
cd frontend
npm install
npm run dev
```
Open [http://localhost:5173](http://localhost:5173).

#### 4. Run the Admin Frontend
```bash
cd admin
npm install
npm run dev
```
Open [http://localhost:5173](http://localhost:5173) (or configured port).

---

### Developer Login & Mock Auth bypass
In local development, the system runs with `APP_ENV=development` and OIDC is typically disabled. 
1. Visit the login page.
2. Select the **Developer Bypass** tab.
3. Input any email and name to sign a mock JWT token.
4. The system registers the developer locally and redirects you directly to the dashboard.

For real OIDC authentication, configure the environment variables:
- `OIDC_ISSUER_URL`
- `OIDC_CLIENT_ID`
- `OIDC_CLIENT_SECRET`
- `OIDC_REDIRECT_URL`

---

## Testing Suite

### 1. Backend Unit Tests
Unit tests cover signature pagination sequences, coordinate math, PDF processors, and email line-wrapping encodings.
To run backend unit tests:

```bash
cd backend
go test -v ./...
```

The core tests reside in [backend/pdf/pdf_test.go](backend/pdf/pdf_test.go), [backend/smtp/smtp_test.go](backend/smtp/smtp_test.go), and [backend/handlers/handlers_test.go](backend/handlers/handlers_test.go).

### 2. End-to-End (E2E) UI Tests
E2E browser testing is conducted using Playwright inside the [e2e/](e2e) directory.
To run the E2E tests:

```bash
cd e2e
npm install
npx playwright test
```

These tests spin up Headless Chromium browsers to verify:
- Developer mock login access.
- Library document uploads.
- Custom compilation requests (validating layouts).
- Sliced front/back and ruined sheet PDF downloads.
- Semantic vector searches matching text snippets.

---

## SRE Observability

Booklet Studio is built with professional production telemetry standards:

### Prometheus Metrics
Exposed at `http://localhost:8080/metrics`. Custom metrics recorded in [backend/metrics/metrics.go](backend/metrics/metrics.go) are:
- `http_requests_total`: Counter tracking total HTTP traffic, labeled by `method`, `status`, and `path`.
- `http_request_duration_seconds`: Histogram checking latency distribution, labeled by `method` and `path`.
- `document_uploads_total`: Ingestion counter tracking uploads, labeled by `status`.
- `booklet_compilation_duration_seconds`: Histogram measuring layout compiler rendering durations.
- `vector_search_duration_seconds`: Histogram measuring Ollama vector lookups and pgvector index scanning.

*Diagnostic Optimization*: Paths like `/metrics` bypass standard request logger middlewares entirely to prevent inflating RAM allocation sizes due to continuous scraping queries.

### Grafana Dashboard
Auto-provisioned under the Grafana instance on port `3002`. Features pre-built panels for:
- Request rates (QPS).
- HTTP p95 latency.
- Ingestion rates.
- Booklet compilation Durations.
- Vector search latency.

---

## UI Standards & Styling

Both frontends utilize Tailwind CSS combined with Radix UI components, configured using **Shadcn UI**. 

### Style Guidelines
- High accessibility colors (curated custom themes rather than raw defaults).
- Responsive layouts with dark/light mode toggles.
- Verification checks: Any styling modifications or addition of UI elements should be verified by running `shadcn-doctor`:

```bash
# From frontend/ or admin/ folders
npx shadcn-doctor
```
This utility scans `components.json`, Tailwind config files, and TS parameters to ensure compatibility. Maintain the `shadcn-doctor` dependency as a `devDependency` in the frontend `package.json`.
