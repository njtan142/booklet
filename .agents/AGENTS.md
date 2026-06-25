# Booklet Project Rules

## Frontend UI Development
1. **Shadcn UI Library Enforcement**: 
   - All interactive and presentation components (buttons, dialogs, inputs, tables, select boxes, alerts) must be built using Shadcn UI.
   - You must run `npx shadcn-doctor` to verify the configuration and health of the UI components and configuration files when creating or modifying frontend styles/components.
   - Maintain the `shadcn-doctor` dependency as a `devDependency` in the frontend `package.json`.

## Separation of Concerns (APIs)
1. **Stateless Operations**:
   - The Go backend must not hold state in memory. All states (uploads, processing stages, compiled booklet locations) must be written to PostgreSQL and MinIO.
2. **Modular Booklet Flows**:
   - Booklet compilation and manual duplex print layout must be separate API actions from document uploading.
   - Do not generate booklet pages during the initial document upload phase; only split the document into single-page PDFs, extract text, and index embeddings. Booklet rendering is done on-demand using page-selection layout coordinates.
