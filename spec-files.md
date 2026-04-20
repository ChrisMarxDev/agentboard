# AgentBoard — Files Feature Spec

> **Status**: Draft. Builds on v2 in `spec.md`. Additive: existing projects keep working with zero changes.

---

## 1. Motivation

Agents regularly produce binary artifacts — screenshots, reports (PDF), exports (CSV/XLSX), generated images, log bundles. Today the data store holds JSON only, so agents have no native way to say "here's the latest chart render" or "attach this PDF to the dashboard". Every such artifact has to be hosted elsewhere.

We add a first-class file layer: upload arbitrary bytes via REST or MCP, reference them from MDX pages with two new components (`<Image>` for inline display, `<File>` for downloadable attachments). Files live on disk next to pages and components, consistent with the existing folder-driven model.

---

## 2. Design Principles

1. **Files are files.** Store them on disk as-is. Don't encode into SQLite, don't chunk, don't compress. `ls files/` reveals what's there.
2. **Filesystem-first.** Users can drop a PNG into `<project>/files/` with Finder; agents can `PUT /api/files/foo.png`. Both paths end at the same bytes.
3. **Same patterns as pages/components.** REST verbs mirror those surfaces. MCP tools mirror page tools. Filename is the key.
4. **Inline vs. download is a rendering concern, not a server concern.** The server serves bytes with the right MIME; the page's component chooses how to present them.
5. **Stable URLs.** `/api/files/<name>` resolves to the bytes forever (until the file is deleted). Pages can hardcode the URL OR reference via data key; both work.

---

## 3. Non-Goals

- **No transforms.** No resizing, format conversion, thumbnail generation. That's a component's job (render `<img srcset>`) or a user-component's job.
- **No CDN / remote storage.** Files live in the project folder. If you want S3, run a connector; the server never calls out.
- **No versioning.** Overwriting by name replaces the bytes. Use history-via-git on the project folder if you care.
- **No streaming uploads.** Max file is 50 MB; the whole body is read at once.
- **No ACLs.** Local mode, no auth. Hosted mode (future) will gate via the same auth layer that covers data and pages.

---

## 4. Storage

### On disk

```
my-project/
├── index.md
├── pages/
├── components/
├── files/              ← new folder
│   ├── hero.png
│   ├── q4-report.pdf
│   └── export.csv
├── agentboard.yaml
└── .agentboard/
    └── ...
```

- **Visible at project root** (not hidden under `.agentboard/`). Rationale: users can add files manually via Finder or a drop-zone; consistent with `components/` and `pages/`.
- Auto-created on first upload or first-run if missing.
- Added to default `.gitignore` template? **No** — users likely want small images in git. They can ignore manually.

### Metadata

Metadata (mtime, size, MIME) is read from the filesystem on demand. No separate index. A file watcher (fsnotify on `files/`) broadcasts `file-updated` SSE events so pages using `<Image source="...">` can hot-reload.

---

## 5. REST API

All endpoints live under `/api/files`.

### `PUT /api/files/:name`

Create or replace a file. Raw body is the file bytes.

**Request:**
- Method: `PUT`
- Headers: `Content-Type: <correct MIME>` (optional — server sniffs if absent)
- Body: raw bytes

**Name rules** (applied after URL-decoding):
- Regex: `^[A-Za-z0-9][A-Za-z0-9._ -]{0,127}$` — must start with alphanumeric, at most 128 chars total, allowed: letters, digits, `.`, `_`, `-`, space.
- **Blocked:** leading `.` (prevents dotfiles), `..`, `/`, `\`, null bytes.
- Extension comes from the name (e.g. `hero.png` → detected as PNG via both ext and content-sniff).

**Size cap:** 50 MB default. Configurable via `agentboard.yaml: max_file_size_mb: 50`. Hard cap 500 MB.

**Response:**
```json
{
  "ok": true,
  "name": "hero.png",
  "size": 48213,
  "content_type": "image/png",
  "url": "/api/files/hero.png"
}
```

**Errors:**
- `400 INVALID_KEY` — bad filename
- `413 VALUE_TOO_LARGE` — body exceeds `max_file_size_mb`
- `415 UNSUPPORTED_MEDIA_TYPE` — reserved for future deny-list (SVG in hosted mode, etc.)

### `GET /api/files`

List all files with metadata.

**Response:**
```json
[
  { "name": "hero.png",      "size": 48213,  "content_type": "image/png",        "modified_at": "2026-04-19T12:00:00Z" },
  { "name": "q4-report.pdf", "size": 248192, "content_type": "application/pdf",  "modified_at": "2026-04-18T09:30:00Z" }
]
```

Query params:
- `?prefix=exports/` — reserved for future nested folders. Phase 1 is flat.

### `GET /api/files/:name`

Serve the file bytes.

Headers returned:
- `Content-Type: <detected MIME>`
- `Content-Length: <size>`
- `X-Content-Type-Options: nosniff` — prevents browsers from re-interpreting the file
- `Content-Disposition: inline; filename="<name>"` — most types
- `Content-Disposition: attachment; filename="<name>"` — for `application/octet-stream`, `.zip`, `.tar.gz`, `.bin`, and anything ambiguous

404 if not found.

### `DELETE /api/files/:name`

Remove the file from disk. Broadcasts `file-updated` SSE event with `{name, deleted: true}`.

### MIME detection

Use `http.DetectContentType` (Go stdlib) on the first 512 bytes. If that returns `application/octet-stream` or the generic text type, fall back to the file-extension → MIME map (mime.TypeByExtension). Record the winner in the response.

---

## 6. MCP Tools

Three new tools, always advertised (no flag gating in Phase 1 — files are safer than JSX components since they never execute as code in the trusted local context).

### `agentboard_write_file`

```json
{
  "name": "hero.png",
  "content_base64": "iVBORw0KGgoAAAANSUhEUgAA..."
}
```

Returns the same shape as the REST endpoint. Base64 is the path of least resistance for MCP — tool-use protocols don't handle raw bytes cleanly.

### `agentboard_list_files`

No args. Returns the same array as `GET /api/files`.

### `agentboard_delete_file`

```json
{ "name": "hero.png" }
```

### Existing tool count impact

Takes the MCP surface from **13 → 16 tools**. `CORE_GUIDELINES.md` §2.3 is updated alongside the implementation.

---

## 7. Components

Two new built-ins join the catalog (making **19** total).

### `<Image>`

Inline image. Resolves `source` → URL, renders `<img>`.

**Props:**
- `source` (string, optional) — data key whose value is a filename OR `{file, alt?, width?, height?}` object
- `src` (string, optional) — direct URL. Pass-through for remote (`https://...`) or pre-resolved paths. Takes precedence over `source`.
- `alt` (string)
- `width` (number, CSS px)
- `height` (number, CSS px)
- `fit` (string) — `contain` (default) | `cover`

**URL resolution** (used by both `<Image>` and `<File>`):
1. If the string starts with `http://` or `https://` → use as-is
2. If it starts with `/` → use as-is
3. Otherwise prefix with `/api/files/` and URL-encode

**Data shape:**
- Plain string: `"hero.png"` → resolves to `/api/files/hero.png`
- Object: `{ "file": "hero.png", "alt": "Company logo", "width": 200 }`

**Example:**
```mdx
<Image source="welcome.hero" alt="Logo" />
<Image src="/api/files/banner.jpg" fit="cover" width={800} height={240} />
```

### `<File>`

Downloadable attachment card.

**Props:**
- `source` or `src` — same resolution as `<Image>`
- `label` (string) — override filename shown on the card

**Rendering:**
A small Card-like box with:
- File-type icon (📄 generic / 📊 spreadsheet / 📑 PDF / 🗄 archive — icon set chosen by extension)
- Filename (or `label`)
- Size (fetched from `/api/files` on mount, or from data-key object shape)
- "Download" button (native `<a href download>`)

**Data shape:**
- String: `"q4-report.pdf"` → resolves to `/api/files/q4-report.pdf`
- Object: `{ "file": "q4-report.pdf", "label": "Q4 financials" }`

**Example:**
```mdx
<File source="reports.latest" />
<File src="/api/files/export.csv" label="Latest export" />
```

### Live updates

When `file-updated` SSE fires with a matching name, both components re-render. For `<Image>`, re-render forces the browser to refetch (we append `?v=<timestamp>` to bust cache).

---

## 8. Config

New fields in `agentboard.yaml`:

```yaml
max_file_size_mb: 50     # per-file upload cap (default 50, hard cap 500)
```

All fields optional. Existing projects work without changes.

---

## 9. Security

### Local mode (default)

- Filename validation blocks path traversal (see §14 for nested-path rules).
- `X-Content-Type-Options: nosniff` on every served file.
- Size cap prevents accidental disk exhaustion.
- No auth — same posture as data/pages/components. Anything on localhost can upload and serve.

### Known risks (tracked in `seams_to_watch.md`)

Each of the following is **accepted for Phase 1** and logged in `seams_to_watch.md` so we revisit before hosted mode or when the surface grows:

1. **SVG XSS.** Uploaded SVGs are served with `image/svg+xml` and can execute embedded JS.
2. **HTML phishing.** An uploaded `login.html` at `/api/files/login.html` could mimic trusted UIs.
3. **Disk DoS.** No per-project total quota.

---

## 10. Bruno tests

New test folder: `bruno/tests/06-files/`.

Coverage:
- `01-put-image.yml` — upload a small PNG (1×1 pixel) via axios with raw body; assert 200 + response shape
- `02-get-image.yml` — fetch it back, assert `Content-Type: image/png`, assert body length matches
- `03-list.yml` — assert our uploaded file appears in the list with correct size + MIME
- `04-put-pdf.yml` — upload a tiny fake PDF, assert `application/pdf` detected
- `05-delete.yml` — DELETE, assert 200, assert subsequent GET returns 404
- `06-reject-bad-name.yml` — PUT `/api/files/..%2Fetc%2Fpasswd`, assert 400
- `07-reject-oversized.yml` — PUT a 60 MB body, assert 413

Showcase addition:
- `bruno/showcase/04-seed-files.yml` — upload one PNG + one PDF + one CSV
- `02-create-page.yml` — add an "Attachments" Deck using `<Image>` and `<File>` against `showcase.hero` / `showcase.report` / `showcase.export` data keys (each key stores `{file: "..."}`)

Task integration: `task test:bruno` picks up the new folder automatically (iterates every subfolder).

---

## 11. Implementation Phases

**All of this is Phase 1 (MVP)** — it's small enough to land together. But if it needs staging:

### Step 1: REST foundation
- `internal/files/manager.go` — Manager with Write/Read/List/Delete + MIME sniffing + filename validation (allow `/` in middle, reject `..`/leading dot/backslashes) + size cap + ETag computation
- `internal/server/handlers_files.go` — 4 handlers (PUT/GET-list/GET/DELETE). GET honors `If-None-Match` → 304.
- `internal/server/server.go` — mount `/api/files/*` wildcard, wire Manager into Server struct + ServerConfig
- `internal/project/project.go` — `FilesDir()` method returning `<project>/files/`, added to `EnsureDirs()`
- `internal/project/config.go` — `MaxFileSizeMB int` field (default 50)

### Step 2: File watcher + SSE
- Reuse the `fsnotify` pattern from `components/manager.go`. On write/delete, broadcast `file-updated` SSE event.

### Step 3: MCP tools
- Extend `internal/mcp/tools.go` with three tools
- Extend `internal/mcp/server.go` dispatch

### Step 4: Frontend components
- `frontend/src/lib/fileUrl.ts` — shared URL resolver (importable by Image + File)
- `frontend/src/components/builtin/Image.tsx`
- `frontend/src/components/builtin/File.tsx`
- Register both in `componentRegistry.ts` and `internal/components/manager.go` catalog

### Step 5: Tests + docs
- Bruno `tests/06-files/` folder (7 requests + 1 for ETag 304 round-trip = 8)
- Showcase update (add 4th seed request + extend showcase page with `<Image>` and `<File>` sections)
- Update `bruno/README.md` with the new folder
- Update `CORE_GUIDELINES.md` §2.3 (13 → 16 MCP tools) and §2.4 (17 → 19 built-ins)
- Update `seams_to_watch.md` with the SVG / HTML-phishing / disk-quota entries (or confirm they're already there)

---

## 12. Resolved Questions (decisions locked in)

| Q | Decision |
| --- | --- |
| Q1 — visibility | **Project root** — `<project>/files/`. Users can drop files via Finder. |
| Q2 — SVG policy | **Allow for now.** Logged in `seams_to_watch.md` to revisit for hosted mode. |
| Q3 — cache busting | **Add ETag.** Use `sha1(mtime-size-name)` or similar cheap fingerprint. Browser sends `If-None-Match`, server returns `304` on match. No `?v=` query string needed. |
| Q4 — nested folders | **Supported in Phase 1.** Route mounted as `/api/files/*`; path validation allows `/` in the middle and rejects `..`, leading `/`, leading `.`, backslashes, null bytes. |
| Q5 — static serving shortcut | **Skip.** Go through the Manager so MIME sniffing, validation, SSE broadcast, and size cap all fire uniformly.  |

### Still open

- **Image/File `source` vs `src`.** Spec supports both (`source` = data key indirection, `src` = direct passthrough). If in practice agents only ever use one, we can deprecate the other later. No decision needed to ship.

---

## 13. What this adds

- **2 components** (Image, File) → catalog grows 17 → 19
- **4 REST endpoints** (PUT/GET-list/GET-one/DELETE)
- **3 MCP tools** → surface grows 13 → 16
- **1 config field** (`max_file_size_mb`)
- **1 new SSE event type** (`file-updated`)
- **7 Bruno tests** + showcase integration
- **No new runtime deps** (uses Go stdlib + fsnotify we already have)

Bundle size impact on the binary: ~zero (no new Go deps). Frontend impact: ~0.5 KB for the two new components.

---

**End of spec.**
