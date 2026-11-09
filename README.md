# Calor

Self-hosted, multi-site click heatmaps and navigation analytics that run comfortably on a
small VPS (~4 GB RAM shared with other things).

- **`api/`**: Go HTTP API on pure-Go SQLite (no CGO). Ingests tracker beacons, stores
  events, and serves aggregations (heatmaps, KPIs, scroll depth, entry/exit, flow, …).
- **`tracker/`**: dependency-free browser snippet (5.2 KB minified, **2.6 KB gzipped**)
  that captures clicks, navigations and scroll depth and ships them in batched beacons.
- **`dashboard/`**: Next.js app (server components, server-rendered SVG charts, lazy
  canvas heatmap) with per-site overview, per-page detail, site comparison and site
  management.
- **`e2e/`**: seed script (3 realistic demo sites, 30 days), live browser e2e with
  wire-level traffic capture, resource measurement and screenshot capture.
- **`deploy/`**: systemd units and Dockerfiles; `docker-compose.yml` at the root.

```
calor/
├── Makefile                 build / run / seed / e2e / measure targets
├── docker-compose.yml       api + dashboard with memory limits (untested here: no Docker)
├── deploy/
│   ├── api.Dockerfile, dashboard.Dockerfile
│   └── systemd/calor-api.service, calor-dashboard.service
├── api/                     Go 1.22 module `calor/api`
│   ├── cmd/calor-api/       main: env config, memory limit, retention, graceful shutdown
│   ├── cmd/calor-loadtest/  ingest + query load generator that samples API RSS
│   └── internal/
│       ├── store/           SQLite schema, migrations, inserts, rollups, all aggregations (+ tests)
│       └── server/          routes, CORS, auth, ingest queue, response cache, handlers (+ tests)
├── tracker/
│   ├── src/calor.js         the tracker (readable source)
│   ├── build.mjs            esbuild → dist/calor.js, dist/calor.min.js (prints raw/gzip/brotli)
│   └── demo/                zero-dependency server for 3 demo sites (*.localhost)
├── dashboard/               Next.js 16 / React 19, `output: 'standalone'`
│   ├── app/                 routes: /s/[site], /s/[site]/pages, /s/[site]/detail, /compare, /sites
│   ├── components/          charts.tsx (SVG), sections.tsx (async sections), heatmap/ (client)
│   ├── lib/                 server-only API client, filters, formatting, types
│   └── proxy.ts             optional HTTP Basic auth for the whole dashboard
└── e2e/                     seed.mjs, live.mjs, measure.mjs, screenshots.mjs (puppeteer-core)
```

## Quick start (everything together)

Requirements: Go ≥ 1.22, Node ≥ 20, and Chrome/Chromium for the seed/e2e scripts.

```bash
make deps        # go mod download + npm ci in tracker/, dashboard/, e2e/
make build       # bin/calor-api, tracker/dist/calor.min.js, dashboard standalone build
make start       # API :8080 + dashboard :3000 + demo sites :5500 (Ctrl-C stops all)

# in another shell, with the stack running:
make seed        # registers 3 demo sites and backfills 30 days of realistic traffic
open http://localhost:3000
```

`make dev` runs the same three processes in development mode (`go run`, `next dev`).
Other targets: `make test` (go vet + tests), `make e2e`, `make measure`, `make loadtest`,
`make screenshots`, `make clean`. The defaults are an admin key of `dev-admin-key` and a
database at `data/calor.db`.

The demo sites are served by Host header on `http://acme.localhost:5500`,
`http://nimbus.localhost:5500` (has an SPA section using `pushState`) and
`http://fieldnote.localhost:5500`. Chrome resolves `*.localhost` to loopback. Click
around and the events show up in the dashboard after a few seconds. Analytics responses
are cached for 15 s.

## Running each piece

### API

```bash
cd api && CALOR_ADMIN_KEY=secret CALOR_DB_PATH=../data/calor.db \
  CALOR_TRACKER_DIR=../tracker/dist go run ./cmd/calor-api
# or the static binary
CGO_ENABLED=0 go build -o ../bin/calor-api ./cmd/calor-api
```

| Variable | Default | Meaning |
|---|---|---|
| `CALOR_ADDR` | `:8080` | listen address |
| `CALOR_DB_PATH` | `calor.db` | SQLite file (WAL mode) |
| `CALOR_ADMIN_KEY` | *(empty)* | key for all read/admin endpoints (`Authorization: Bearer …` or `X-API-Key`). Empty = unauthenticated (logged as a warning) |
| `CALOR_TRACKER_DIR` | *(unset)* | if set, serves `calor.min.js` at `/tracker/` |
| `CALOR_STRICT_ORIGIN` | `true` | reject ingest whose `Origin` (or page URL host) is not the site's domain or a subdomain |
| `CALOR_MAX_BODY_KB` | `256` | max ingest body (413 above) |
| `CALOR_MAX_BATCH` | `200` | max events per request (413 above) |
| `CALOR_QUEUE_SIZE` | `2048` | ingest queue length in requests; when full, ingest returns 503 + `Retry-After` |
| `CALOR_CACHE_TTL_SEC` | `15` | analytics response cache TTL (also invalidated per site on ingest) |
| `CALOR_RETENTION_DAYS` | `0` (keep) | hourly pruning of raw events older than N days (heatmap rollups are kept) |
| `CALOR_MEMORY_LIMIT_MB` | `192` | Go soft memory limit when `GOMEMLIMIT` is not set |
| `CALOR_SQLITE_HEAP_MB` | `64` | SQLite `soft_heap_limit` (process-wide; SQLite memory is invisible to `GOMEMLIMIT`) |
| `CALOR_SQLITE_CACHE_KB` | `4096` | page cache per connection |
| `CALOR_DB_READERS` | `2` | read-only connections (each can hold cache and sorter memory) |
| `CALOR_SQLITE_MMAP_MB` | `0` | memory-mapped I/O; off by default so file pages don't inflate RSS |

### Tracker

```bash
cd tracker && npm ci && npm run build   # dist/calor.js + dist/calor.min.js
npm run demo                            # demo sites on :5500 (CALOR_ENDPOINT, DEMO_PORT)
```

### Dashboard

```bash
cd dashboard && npm ci
npm run dev                              # development
npm run build && npm start               # standalone server, node --max-old-space-size=192
```

| Variable | Default | Meaning |
|---|---|---|
| `CALOR_API_URL` | `http://127.0.0.1:8080` | where the dashboard server reaches the API |
| `CALOR_ADMIN_KEY` | *(empty)* | sent to the API. It stays on the server and never reaches the browser |
| `CALOR_PUBLIC_API_URL` | `http://localhost:8080` | public API URL used in the embed snippet |
| `DASHBOARD_USER` / `DASHBOARD_PASSWORD` | *(unset)* | if both set, HTTP Basic auth on every dashboard route |
| `PORT` / `HOSTNAME` | `3000` / `0.0.0.0` | standard Next.js standalone settings |

The browser never talks to the API directly. Server components fetch with the admin
key. The heatmap client component goes through `app/api/calor/[...path]`, an allow-listed
proxy that forwards only heatmap, element, screenshot reads and screenshot uploads.

## Embedding the tracker

Register the site in the dashboard (**Sites & snippet**) or via the API, then paste the
snippet it shows before `</body>`:

```html
<script src="https://calor.example.com/tracker/calor.min.js"
        data-site-id="site_k3j2m9x1q8w7e6r5" data-endpoint="https://calor.example.com/api/events" defer></script>
```

| Attribute | Default | |
|---|---|---|
| `data-site-id` | *(required)* | site ID from `/api/sites` |
| `data-endpoint` | script origin + `/api/events` | ingest URL |
| `data-flush-interval` | `5000` | ms between periodic flushes |
| `data-max-batch` | `25` | flush as soon as this many events are queued |
| `data-respect-dnt` | *(off)* | present → do nothing when Do Not Track is on |

`window.CalorConfig = { siteId, endpoint, flushInterval, maxBatch, respectDnt }` works
too. Add `data-calor-ignore` to any element to exclude clicks inside it. The tracker
exposes `window.calor.flush()`.

**Captured:** clicks (page-relative x/y, viewport, document size, element: CSS selector,
tag, id, up to 3 classes, trimmed text or the `aria-label`/`title`/`alt` attribute,
rage flag (3 clicks within 1 s and 30 px), dead flag (not inside an interactive element));
navigations (link clicks with target, `pushState`/`replaceState`/`popstate` for SPAs,
unloads); pageviews with referrer and redirect count; per-pageview max scroll depth and
visible (engaged) time. The session ID lives in `sessionStorage`, so there are no cookies.
Query strings and fragments are stripped from URLs before sending.

## Multi-site

- **API.** `POST /api/sites {name, domain[, id]}` generates `site_` plus 16 base32 chars
  unless an ID is supplied. `GET/PATCH/DELETE /api/sites/{site}` manage a site. Every
  analytics route is `/api/sites/{site}/…`, and every query filters by `site_id`.
  `events`, `heatmap_rollup` and `screenshots` all carry `site_id` with indexes that
  lead with it. Ingest looks the site up in an in-memory registry and rejects unknown IDs
  with 403. With `CALOR_STRICT_ORIGIN` it also rejects requests whose `Origin`, or page
  URL host, is not the site's domain or a subdomain of it (`www.` and ports ignored).
  Deleting a site removes its data in bounded chunks.
- **Tracker.** `data-site-id` is required and is sent as `site` in every batch envelope.
- **Dashboard.** The site switcher in the top bar remembers the last site in a cookie and
  shows per-site pageviews. Every route is `/s/{site}/…`. **Sites & snippet** registers
  sites and shows a copy-ready snippet per site. **Compare** (`/compare?sites=a,b,c`, 2–6
  sites) shows a KPI table with the best value per row highlighted, daily visitors and
  hour-of-day overlays, and a column per site with KPIs, pageviews, scroll reach, devices
  and top pages.

## API reference (read endpoints need the admin key)

| Method & path | |
|---|---|
| `POST /api/events` | public ingest, CORS `*`, accepts `text/plain` (beacons) or JSON → `202 {"accepted":n,"rejected":m}` |
| `GET /healthz` | `{"ok":true,"queued":n}` |
| `GET/POST /api/sites`, `GET/PATCH/DELETE /api/sites/{site}` | site management (list includes pageviews and last event) |
| `GET /api/sites/{site}/kpis` | visitors, pageviews, clicks, avg scroll, avg time, bounce, navigations, rage/dead |
| `…/daily`, `…/time-of-day`, `…/devices`, `…/referrers` | trend (gap-filled), hour × weekday matrix, device split, external referrers |
| `…/pages?sort=&order=&limit=&offset=&q=` | per-page table: clicks, unique visitors, pageviews, avg scroll, avg time, entries, exits, exit rate, bounce, rage |
| `…/elements?url=&limit=` | most-clicked elements with readable labels, visitors, rage/dead counts |
| `…/scroll?url=` | reach at 25/50/75/100 %, distribution of max depth, avg time |
| `…/entry-exit` | top entry pages, exit pages and exit points (last click before leaving) |
| `…/flow?url=` | where visitors came from and went next for one page |
| `…/heatmap?url=&type=click\|navigation\|all&cols=&rows=` | binned grid plus metadata. Served from the hourly rollup when the range is hour-aligned |
| `GET/PUT/DELETE …/screenshot?url=`, `…/screenshot/meta` | page screenshot (png/jpeg/webp/gif ≤ 8 MiB) |
| `GET /api/compare?sites=a,b` | side-by-side data for 2–6 sites |

Common filters: `from`, `to` (`YYYY-MM-DD` in the `tz` offset, inclusive, or unix ms or
RFC 3339), `tz` (minutes east of UTC), `device` (`mobile`/`tablet`/`desktop`), and `url`.

## Tracker → API traffic (measured)

All numbers below were captured on the wire by `make e2e` (`e2e/live.mjs`). It drives
Chrome through the three demo sites and records each `/api/events` request through a
recording proxy, which also catches beacons sent while a page unloads.

| Request (real capture) | Events | Body | Request headers | Response |
|---|---|---|---|---|
| pageview on load (flushed by the 5 s timer) | 1 | 209 B | ~660 B | 202, 27 B body + 146 B headers |
| single click (timer flush) | 1 | 345 B | ~660 B | 202, 27 B + 146 B |
| link click → navigation (click + nav + scroll summary) | 3 | 494–497 B | ~660 B | 202, 27 B + 146 B |
| page exit beacon (pageview + click + unload + scroll) | 4 | 513 B | ~660 B | 202, 27 B + 146 B |
| typical batch (pageview + 5 clicks incl. rage) | 6 | 1,388 B | ~660 B | 202, 27 B + 146 B |
| SPA session (3 `pushState` + `popstate` + link) | 16 | 2,351 B | ~660 B | 202, 28 B + 146 B |

The full scripted run sent 12 requests, 50 events and 8.1 KB of bodies. Request headers
are Chrome's own (UA, client hints, `sec-fetch-*`, referer, origin) and can't be trimmed.

**Envelope** (sent once per batch): `v` version, `site` site ID, `sid` session ID, `ts`
send time (ms), `u` page URL, `p` pageview ID, `vw`/`vh` viewport. **Event**: `t` type
(`c` click, `n` navigation, `p` pageview, `s` scroll summary) and `d` (ms offset from
`ts`). Then, by type:

- click: `x`, `y`, `dw`, `dh`, `el{sel,tag,id,cls,txt,aria}`, `rage`, `dead`
- navigation: `k` (`link`/`pushstate`/`replacestate`/`popstate`/`unload`), `to`
- pageview: `k`, `ref`, `rc`, `sd`
- scroll: `sd` (max depth %), `ms` (visible time)

Events repeat `u`/`p`/`vw`/`vh` only when they differ from the envelope.

```json
{"v":1,"site":"acme-coffee","sid":"y61n1teccj9eu31bmw8yrdlv","ts":1794210768563,
 "u":"http://acme.localhost:5501/","p":"s0m1x9bcb2ylfw87mw8yrdlv","vw":1440,"vh":900,
 "e":[{"t":"c","x":540,"y":247,"dw":1440,"dh":2005,
       "el":{"sel":"body>div.wrap.hero>h1","tag":"h1","txt":"Small-batch coffee for people who care about the cup."},
       "dead":1,"d":-3395}]}
```

**When the tracker sends:**

- Events are batched, never one request per click.
- A batch is flushed every 5 s if anything is queued, immediately at 25 events, on a
  same-tab link click (with the page's scroll summary, so the following `pagehide` has
  nothing left to send), on `visibilitychange` to hidden, and on `pagehide`.
- It uses `navigator.sendBeacon` with a `text/plain` body (a CORS-simple request, so no
  preflight). If the beacon is refused, it falls back to `fetch(..., {keepalive: true})`.
- Scroll handling is passive and throttled to one measurement per 250 ms. Depth is sent
  once per pageview, as a summary event, not per scroll.

**Ingest response:** `202 Accepted` with `{"accepted":n,"rejected":m}` (27–28 B).
Preflight-only CORS headers are sent only on `OPTIONS`, so a response totals about 173 B
on the wire.

**Typical dashboard query sizes** (seeded Acme data, raw / gzip):

| Endpoint | Raw | Gzip |
|---|---|---|
| kpis | 287 B | (raw, < 1 KB) |
| scroll | 288 B | (raw, < 1 KB) |
| devices | 239 B | (raw, < 1 KB) |
| time-of-day | 830 B | (raw, < 1 KB) |
| daily (30 d) | 1,984 B | 447 B |
| entry-exit | 2,275 B | 483 B |
| elements (10) | 3,468 B | 771 B |
| flow | 4,014 B | 918 B |
| pages (25 rows) | 5,219 B | 1,395 B |
| heatmap 100×139 | 13,918 B | 2,777 B |
| compare (3 sites) | 13,774 B | 3,843 B |

**Payload minimization:**

- Short keys, with page, pageview and viewport sent once per batch.
- Relative timestamps (`d`), re-anchored to server time on ingest, which also fixes
  client clock skew.
- Element text clipped to 80 chars, at most 3 classes, selectors at most 5 levels deep.
- Query strings dropped, and one scroll summary per pageview.
- Beacon bodies are not compressed: `CompressionStream` is async and would not finish
  during unload. Responses ≥ 1 KB are gzipped at `BestSpeed`, and cached responses
  keep their gzip.

## Low-resource deployment

Measured on this build with 155k seeded events (3 sites, 30 days, 79 MB database), all
three services running on one machine:

| | Measured |
|---|---|
| API RSS idle | **8.7 MiB** |
| API RSS after 3 passes over every dashboard route | 69 MiB (peak 80 MiB) |
| API RSS under load test (16 ingest + 4 query workers, 30 s) | peak **147.5 MiB**, 107 MiB 20 s later |
| Load-test throughput | 9,054 events/s accepted (0 errors, 503 backpressure when the queue fills), queries p50 0.6 ms / p95 43 ms |
| Dashboard (Next standalone) RSS idle / after browsing | **81 MiB** / 127 MiB (peak 133 MiB) |
| Tracker | 5,246 B minified, **2,584 B gzip**, 2,316 B brotli |
| Dashboard first-load JS per route | 137–141 KiB gzip (460–471 KiB raw). The heatmap chunk (+2.6 KiB gz) loads only on page detail |
| Dashboard static total | 15 JS files, 184 KiB gzip; CSS 3.2 KiB gzip |
| Binaries | `calor-api` 9.6 MB static; standalone dashboard 64 MB on disk |

What keeps it small:

- **API memory.**
  - `GOMEMLIMIT` defaults to 192 MiB. SQLite memory is capped separately by
    `soft_heap_limit` (64 MiB), with a 4 MiB page cache per connection, 1 writer plus
    2 readers, mmap off and `temp_store=FILE`.
  - A bounded ingest queue feeds one writer goroutine that coalesces batches into one
    transaction. When the queue is full, ingest returns 503 rather than buffering.
  - Body limit 256 KB, at most 200 events per request.
  - The response cache holds at most 256 entries / 16 MB.
- **API storage and queries.**
  - WAL mode with `journal_size_limit`. Retention pruning and site deletion run in
    5,000-row transactions followed by `wal_checkpoint(TRUNCATE)`, so the WAL can't grow
    to the size of a big delete.
  - Composite indexes on (site, type, ts), (site, page, type, ts) and (site, session, ts).
  - An hourly 200×800 heatmap rollup means a heatmap reads a few thousand cells, not raw
    clicks.
- **Dashboard.**
  - Server components with per-section `Suspense` streaming. Charts are server-rendered
    SVG, with no chart library.
  - The only client JS beyond the Next and React runtime is the filter bar, site switcher,
    copy button and the heatmap, which is loaded via `next/dynamic` with `ssr: false`.
  - Tables are paginated and sorted by the API.
  - It runs as `output: 'standalone'` under `node --max-old-space-size=192`.
- **Dependencies.** The API depends only on `modernc.org/sqlite` and uses the stdlib
  router. Dashboard runtime dependencies are `next`, `react`, `react-dom` and
  `server-only`. The tracker has none (esbuild is build-only).

### systemd (tested unit syntax with `systemd-analyze verify`)

```bash
sudo useradd --system --home /opt/calor calor
sudo mkdir -p /opt/calor/bin /opt/calor/tracker /opt/calor/dashboard/.next /etc/calor
sudo cp bin/calor-api /opt/calor/bin/
sudo cp -r tracker/dist /opt/calor/tracker/
sudo cp -r dashboard/.next/standalone /opt/calor/dashboard/.next/
echo 'CALOR_ADMIN_KEY=change-me' | sudo tee /etc/calor/calor.env && sudo chmod 600 /etc/calor/calor.env
sudo cp deploy/systemd/*.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now calor-api calor-dashboard
```

Both units set `MemoryHigh=256M` / `MemoryMax=320M`, so the stack fits within 640 MiB at
the hard limits. That cgroup limit also counts the SQLite file pages the kernel caches,
but those are reclaimable. Put a reverse proxy (Caddy or nginx) in front for TLS,
exposing `/api/events` and `/tracker/` publicly and the dashboard behind auth.

### Docker Compose

```bash
CALOR_ADMIN_KEY=change-me CALOR_PUBLIC_API_URL=https://calor.example.com docker compose up -d --build
```

Each service has `mem_limit: 320m`. Note: Docker was not available where this was built,
so the Dockerfiles and compose file are written to match the tested `make start` setup
but have not been run.

### Tuning

- To use less memory, lower `GOMEMLIMIT`, `CALOR_SQLITE_HEAP_MB`, `CALOR_DB_READERS=1`,
  `CALOR_QUEUE_SIZE` and `--max-old-space-size=128`.
- For faster heavy queries on big sites, raise `CALOR_SQLITE_CACHE_KB` and
  `CALOR_DB_READERS`.
- Set `CALOR_RETENTION_DAYS` to bound disk use. Heatmap rollups stay, so long-range
  heatmaps survive.

## Seed data, e2e, screenshots

- **`make seed` (`e2e/seed.mjs`)**
  - Registers Acme Coffee Roasters, Nimbus (SaaS with an SPA section) and Fieldnote
    (travel blog).
  - Loads every demo page in Chrome to measure real element positions and labels, and
    uploads full-page screenshots.
  - Simulates 30 days of sessions with per-site traffic shapes: weekday/hour curves,
    devices, referrers, entry pages, bounce rates, click weights per element, rage-prone
    elements, exit and scroll behavior.
  - Sends them through the normal ingest path with the admin-only
    `X-Calor-Backfill: 1` header, which trusts client timestamps up to 35 days old.
  - Options: `--days N`, `--scale X`.
- **`make e2e` (`e2e/live.mjs`)**
  - Real browser sessions on all three sites: CTA clicks, rage and dead clicks, scrolling,
    link navigation, SPA `pushState`/`popstate`, exits.
  - Captures each tracker request byte for byte, checks that every one got 202 and that
    the events show up in the API, and measures query response sizes.
  - Writes `e2e/output/traffic.json`. Run it before `make seed`: seeding re-registers the
    demo sites, which wipes the live-test events.
- **`make measure` (`e2e/measure.mjs`)** reports tracker size, dashboard JS per route and
  API/dashboard RSS, and writes `e2e/output/measure.json`.
- **`make screenshots` (`e2e/screenshots.mjs`)** writes `dashboard-*.png` (1440×900
  viewport) into `SHOTS_DIR`.
