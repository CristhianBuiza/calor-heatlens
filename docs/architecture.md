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
    an allow-listed proxy: heatmap, elements, screenshots, screenshot upload, and the
    component match preview.
  - The element picker loads the tracked page through `dashboard/app/api/calor-frame`.
    It fetches only pages on the site's own domain, strips scripts and inline handlers,
    adds a `<base>` tag, sends `Content-Security-Policy: script-src 'none'`, and caps the
    size at 3 MiB. The result is shown in a sandboxed same-origin iframe, so the picker
    can read its DOM without running the site's code.
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
  - `components` and `event_components` (component tagging, below).
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

## Component tagging

Goal: name parts of a page ("Hero CTAs", "Pricing cards", "Newsletter form") and get
heatmaps and stats scoped to clicks inside them, including clicks recorded before the
component existed.

**Defining a component:**

- A component is defined per site by a CSS selector (primary), an HTML data attribute
  such as `data-calor-component="newsletter"`, or both. A click matches when any rule
  matches.
- It can be limited to pages by a path pattern with `*` wildcards (`/products/*`).

**Storage** (schema version 2, applied as one transaction; adding a column with a constant
default is instant even on large databases):

- `components(id, site_id, name, selector, attr_name, attr_value, page_pattern,
  created_at, updated_at)`, with a foreign key to `sites` (`ON DELETE CASCADE`).
- `event_components(component_id, ts, event_id)`, a `WITHOUT ROWID` junction whose
  primary key starts with the component and time, so a component-scoped query is a range
  scan. It has no foreign keys; component and site deletes remove its rows in bounded
  chunks.
- `events.el_component` holds the closest `data-calor-component` value of each click.

**Matching runs in three places, and the results are unioned:**

1. **In the browser** (most precise: the full live DOM). The tracker fetches
   `GET /api/sites/{site}/tracker-config` once per session and caches it in
   `sessionStorage` for 5 minutes. The rules can also be embedded in the snippet
   (`data-components`). On click it runs `target.closest(selector)` for each rule whose
   page pattern matches the current path, and sends the matching IDs as `cmp`. It always
   sends the closest `data-calor-component` value as `el.ca`. The ingest handler keeps at
   most 20 IDs per click and drops any that aren't the site's components.
2. **On the server at ingest.** Each click's recorded path (`el.sel`, plus the element's
   id and classes) is matched against the site's compiled rules by a small CSS-subset
   matcher, evaluated right to left like a browser. An attribute rule for
   `data-calor-component` matches `el_component`. This tags clicks sent before the config
   reached the browser. The compiled rules are cached in memory per site and rebuilt when
   a component changes.
3. **Retroactively,** when a component is created or a rule changes. Pending ingest is
   flushed first. The server pages through the site's past clicks by id in batches of
   5,000. It matches each distinct (path, element, attribute value, page) combination
   once, using a memo capped at 100,000 entries, and inserts junction rows one batch per
   transaction. The match preview runs the same matcher over clicks grouped in SQL by
   element, so it writes nothing and costs one pass over the range.

Memory use stays flat: every pass streams fixed-size batches, and the memo is capped. On
the seeded demo data (155k events), creating a component on the largest demo site,
including the retroactive pass, takes about 270 ms. A preview takes 20–150 ms.

**Queries.** `store.Filter.Component` adds
`(type <> 'click' OR id IN (SELECT event_id FROM event_components WHERE component_id = ? AND ts …))`
to every aggregation. Click metrics are scoped; pageviews, scroll and navigation stay
page-wide. Component-scoped heatmaps bypass the hourly rollup, which has no component
dimension, and count clicks only. The response cache needs no special handling, because
component changes bump the site's cache generation.

**Why this design.** Matching distinct element structures (hashing each click's path into
an `elements` table) was the alternative. It needs a new wire format and an extra table
before it pays off. The per-click junction reuses the path the tracker already records,
gives exact per-click attribution for browser-side matches, which see the whole DOM, and
keeps queries as an indexed range scan.

**Limits:**

- The server only sees what the tracker recorded: the element plus up to 4 ancestors,
  6 classes on the element and 2 on each ancestor. Retroactive and preview counts can be
  lower than what the tracker tags on live traffic. They are not expected to be higher.
- Attribute tests other than a lone `[data-calor-component=…]` are browser-only, so they
  tag new traffic only (`evaluable: false`).
