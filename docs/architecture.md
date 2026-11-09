# Calor architecture

## Data flow

```
 visitor's browser                      calor-api (Go, one static binary)                 SQLite (WAL)
┌──────────────────────┐  sendBeacon   ┌───────────────────────────────────────────┐     ┌────────────────────┐
│ calor.min.js         │ ────────────▶ │ POST /api/events                          │     │ sites              │
│  clicks, navigations │  text/plain   │  validate · site + Origin check           │     │ events             │
│  scroll, pageviews   │  batched      │  bounded queue ──▶ single writer          │ ──▶ │ heatmap_rollup     │
└──────────────────────┘               │   (coalesces batches into one tx)         │     │ screenshots        │
                                       │                                           │     │ components *       │
 dashboard server (Next.js)            │ GET /api/sites/{site}/…  (admin key)      │ ◀── │ event_components * │
┌──────────────────────┐  HTTP + key   │  2 read-only connections · response cache │     └────────────────────┘
│ server components    │ ◀───────────▶ │  (TTL + LRU, gzip, per-site invalidation) │
│ SVG charts, heatmap  │               └───────────────────────────────────────────┘     * component tagging
└──────────▲───────────┘
           │ HTML/RSC + one lazy canvas chunk
       admin's browser
```

- The **tracker** runs on tracked sites. It only talks to `POST /api/events` (and, with
  component tagging, `GET …/tracker-config`).
- The **dashboard** server holds the admin key and does all API reads from server
  components.
  - The admin's browser never sees the key.
  - Client-side code reaches the API only through `dashboard/app/api/calor/[...path]`,
    an allow-listed proxy: heatmap, elements, screenshots, screenshot upload.
- The **API** is the only process that touches SQLite.

## API internals (`api/internal`)

### `store/`: schema, writes and every aggregation

- **Database handles.**
  - Two `*sql.DB` handles on one file: a single writer connection, and a small pool of
    `query_only` readers (default 2).
  - Pragmas: `journal_mode=WAL`, `synchronous=NORMAL`, `busy_timeout=5000`,
    `foreign_keys=ON`, `cache_size` (default 4 MiB per connection), `mmap_size` (default
    0), `journal_size_limit=64 MiB`, `temp_store=FILE`, and `soft_heap_limit` (default
    64 MiB).
- **Migrations** are numbered and tracked in `PRAGMA user_version`.
- **Tables:**
  - `events`: one row per click, navigation, pageview or scroll summary. Indexed on
    (site, type, ts), (site, page, type, ts), (site, session, ts) and (pageview).
  - `heatmap_rollup`: per site, page, type, device and hour, counts on a fixed 200×800
    grid of document-relative cells. A heatmap reads a few thousand cells instead of
    every click.
  - `screenshots`: one image per site and page.
- **Aggregations** are plain SQL: window functions for entry/exit and bounce, per
  pageview max scroll depth, and gap-filled daily series. Labels and element grouping
  are assembled in Go.
- **Deletes** (site deletion, retention pruning) run in 5,000-row transactions followed
  by `wal_checkpoint(TRUNCATE)`, so the WAL never grows to the size of one big delete.

### `server/`: HTTP

- **Routing:** stdlib `ServeMux` with method and path patterns (Go 1.22).
- **Ingest** decodes the compact wire format, validates every event and normalizes
  URLs. Then it enqueues the batch on a bounded queue (`CALOR_QUEUE_SIZE` requests).
  - When the queue is full it returns 503 with `Retry-After`, rather than buffering
    without limit.
  - A single writer goroutine flushes every 200 ms or at 5,000 events per transaction.
- **Site registry:** in memory, so ingest never queries SQLite to validate a site.
- **Response cache:** TTL + LRU. Keys include a per-site generation number that is
  bumped on ingest and on site or component changes. A gzip copy is built lazily and
  kept.

### `cmd/`

- **`calor-api`:**
  - Reads environment configuration and sets `debug.SetMemoryLimit` (192 MiB) when
    `GOMEMLIMIT` is unset.
  - Runs the optional hourly retention job, sets HTTP timeouts, and shuts down
    gracefully.
- **`calor-loadtest`:**
  - Concurrent ingest and cache-busting query workers against a fresh `loadtest` site,
    which is deleted afterwards.
  - Reports latency percentiles and samples the API's RSS from `/proc`.

## Tracker (`tracker/src/calor.js`)

- **Structure:** an IIFE with no dependencies.
  - Configured by `data-*` attributes on its `<script>` tag or `window.CalorConfig`.
  - The session ID lives in `sessionStorage`, so no cookies are used.
- **Clicks** use a capture-phase listener.
  - The element description is computed once per click.
  - Rage detection: 3 clicks within 1 s and 30 px. A click is dead when it lands
    outside any interactive element.
- **Link navigations** use a window bubble-phase listener. It runs after page handlers,
  so SPA routers that call `preventDefault()` are recorded as `pushState` navigations
  instead.
- **SPA routing:** `pushState` and `replaceState` are wrapped and `popstate` is observed.
  Each route change closes the current pageview (scroll summary) and opens a new one.
- **Transport:** `sendBeacon` with a `text/plain` body, falling back to `fetch` with
  `keepalive`.

## Dashboard (`dashboard/`)

- **Platform:** Next.js App Router with `output: 'standalone'` and `proxy.ts` (optional
  HTTP Basic auth).
- **Rendering:**
  - Every chart is server-rendered SVG (`components/charts.tsx`); there is no chart
    library.
  - Sections are async server components wrapped in `Suspense`, so a slow query never
    blocks the page.
- **Client JS** beyond the Next and React runtime is the filter bar, the site switcher,
  the copy button, and the heatmap. The heatmap is loaded via `next/dynamic` with
  `ssr: false` and only on page detail.
- **Filters** live in the URL: range, from/to and device. The browser timezone offset is
  stored in a cookie, so days and hours are local.

## Component tagging (in progress)

Goal: name parts of a page ("Hero CTAs", "Pricing cards", "Newsletter form") and get
heatmaps and stats scoped to clicks inside them.

**Defining a component:**

- A component is defined per site by a CSS selector (primary) and/or an HTML data
  attribute such as `data-calor-component="newsletter"`.
- It can optionally be scoped to pages by a path pattern with `*` wildcards
  (`/products/*`).

**Storage:**

- `components(id, site_id, name, selector, attr_name, attr_value, page_pattern,
  created_at, updated_at)`.
- `event_components(component_id, ts, event_id)`, a `WITHOUT ROWID` junction keyed for
  range scans per component.
- A new `events.el_component` column holds the value of the closest
  `data-calor-component` ancestor.

**Matching runs in three places, and the results are unioned:**

1. **Tracker side** (most precise, full DOM). The tracker fetches a small cached config
   from `GET /api/sites/{site}/tracker-config`, a few hundred bytes cached in
   `sessionStorage`. It can also be embedded in the snippet. On click it runs
   `target.closest(selector)` for each configured component that applies to the current
   path, and sends the matching IDs as `cmp`. It always records the closest
   `data-calor-component` value as `el.ca`, so attribute tagging needs no configuration.
2. **Server side at ingest.** Each click's recorded ancestry (`el.sel`, plus the target's
   id and classes) is matched against the site's selectors using a small CSS-subset
   matcher: tag, `#id`, `.class`, `[attr]`, `[attr=value]`, `:nth-of-type()`, and the
   descendant and child combinators. Attribute components match `el_component`. This
   tags clicks from clients whose config hasn't loaded yet.
3. **Retroactively,** when a component is created or its rules change. The server groups
   past clicks by distinct (selector, page, attribute value), matches each group once,
   and inserts junction rows in bounded transactions. The same grouped scan powers the
   live match preview. Memory scales with the number of distinct groups, not clicks,
   which keeps it inside the low-resource budget.

The server matcher only sees what the tracker recorded: up to 5 ancestor levels, with at
most 2 classes per ancestor. So retroactive and preview counts can be lower than what
the tracker tags on live traffic. They are not expected to be higher.
