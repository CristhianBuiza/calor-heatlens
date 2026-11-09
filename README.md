# Calor

Calor is a self-hosted, privacy-friendly heatmap and click analytics tool for one or
many websites. A 2.6 KB tracker records clicks, navigations and scroll depth. A single Go
binary stores them in SQLite. A Next.js dashboard shows click heatmaps over page
screenshots, most-clicked elements, time-of-day patterns, scroll reach, entry and exit
pages, and side-by-side site comparisons.

It is built to run on a small VPS: in measured runs the whole stack stays under
~300 MiB of RAM.

- No cookies: the session ID lives in `sessionStorage`, and query strings are stripped
  from URLs.
- No CGO, no external database, no chart libraries.
- Multi-site from the ground up: sites have generated IDs, every query is scoped by site,
  and ingest checks that the request comes from the site's domain.

## Contents

- [Architecture](#architecture)
- [Quick start](#quick-start)
- [Running each piece](#running-each-piece)
- [Configuration](#configuration)
- [Embedding the tracker](#embedding-the-tracker)
- [Dashboard features](#dashboard-features)
- [Multi-site support](#multi-site-support)
- [Component tagging (in progress)](#component-tagging-in-progress)
- [API summary](#api-summary)
- [Resource usage (measured)](#resource-usage-measured)
- [Deployment](#deployment)
- [Setup caveats and known limits](#setup-caveats-and-known-limits)
- More detail: [`docs/architecture.md`](docs/architecture.md) and
  [`docs/api.md`](docs/api.md) (full API reference, wire format, measured traffic)

## Architecture

```
  tracked website                     calor-api  (Go, static binary)              SQLite file (WAL)
 ┌──────────────────┐  sendBeacon    ┌──────────────────────────────────────┐    ┌────────────────┐
 │ calor.min.js     │ ─────────────▶ │ POST /api/events  (public, CORS *)   │    │ sites          │
 │ clicks · navs ·  │  batched JSON  │  validate → bounded queue → 1 writer │ ─▶ │ events         │
 │ scroll · SPA     │  (text/plain)  │                                      │    │ heatmap_rollup │
 └──────────────────┘                │ GET /api/sites/{site}/…  (admin key) │ ◀─ │ screenshots    │
                                     │  2 readers · response cache · gzip   │    └────────────────┘
  dashboard (Next.js standalone)     └──────────────────────────────────────┘
 ┌──────────────────┐   HTTP + admin key        ▲
 │ server components│ ──────────────────────────┘
 │ SVG charts       │
 │ canvas heatmap   │ ◀── admin's browser (never sees the admin key)
 └──────────────────┘
```

| Directory | What it is |
|---|---|
| `api/` | Go 1.22 module. `cmd/calor-api` is the server, `cmd/calor-loadtest` a load generator. `internal/store` holds the SQLite schema, migrations and aggregations; `internal/server` holds HTTP, ingest, auth and cache. Pure-Go SQLite ([modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)) is the only dependency |
| `tracker/` | `src/calor.js`, the tracker, which has no dependencies. `build.mjs` (esbuild) produces `dist/calor.min.js`. `demo/` is a zero-dependency server for three demo sites |
| `dashboard/` | Next.js 16 / React 19 App Router, `output: 'standalone'`. Runtime dependencies: `next`, `react`, `react-dom`, `server-only` |
| `e2e/` | puppeteer-core scripts: realistic seed data, live browser test with wire-level traffic capture, resource measurement, screenshots |
| `deploy/` | systemd units and Dockerfiles. `docker-compose.yml` is at the root |
| `docs/` | architecture and API reference |

Data flow:

1. The tracker batches events and sends them with `sendBeacon`.
2. The API checks the site and origin, then queues the batch.
3. A single writer stores each batch in one transaction and updates an hourly heatmap
   rollup.
4. The dashboard server queries the read endpoints with the admin key and renders
   everything server-side, except the canvas heatmap.

Details are in [`docs/architecture.md`](docs/architecture.md).

## Quick start

Requirements: **Go ≥ 1.22**, **Node ≥ 20.9** (Next.js 16), GNU make, and Chrome or
Chromium for the seed, e2e and screenshot scripts.

```bash
git clone https://github.com/CristhianBuiza/calor-heatlens.git calor && cd calor
make deps        # go mod download + npm ci in tracker/, dashboard/, e2e/
make build       # bin/calor-api, tracker/dist/calor.min.js, dashboard standalone build
make start       # API :8080 + dashboard :3000 + demo sites :5500 (Ctrl-C stops all)

# in a second shell, while the stack runs:
make seed        # registers 3 demo sites and backfills 30 days of realistic traffic
open http://localhost:3000
```

The demo sites run on `http://acme.localhost:5500` (shop), `http://nimbus.localhost:5500`
(SaaS with an SPA section) and `http://fieldnote.localhost:5500` (blog). Chrome resolves
`*.localhost` to loopback, so clicking around produces real events. They appear after a
few seconds; analytics responses are cached for 15 s.

| Target | |
|---|---|
| `make dev` | the same three processes in development mode (`go run`, `next dev`) |
| `make test` | `go vet` + Go tests |
| `make e2e` | real Chrome sessions on the demo sites, capturing every tracker request. Writes `e2e/output/traffic.json`. Run it before `make seed`, which re-registers the demo sites |
| `make measure` | tracker size, dashboard JS per route, and API and dashboard RSS. Writes `e2e/output/measure.json` |
| `make loadtest` | 30 s of ingest plus queries against a running API, with RSS sampling |
| `make screenshots` | dashboard screenshots into `e2e/output/` (or `SHOTS_DIR`) |
| `make clean` | removes build output and the local database |

## Running each piece

```bash
# API
cd api && CALOR_ADMIN_KEY=change-me CALOR_DB_PATH=../data/calor.db \
  CALOR_TRACKER_DIR=../tracker/dist go run ./cmd/calor-api
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ../bin/calor-api ./cmd/calor-api

# Tracker
cd tracker && npm ci && npm run build       # dist/calor.js + dist/calor.min.js (prints sizes)
npm run demo                                # demo sites on :5500 (env DEMO_PORT, CALOR_ENDPOINT)

# Dashboard
cd dashboard && npm ci
npm run dev                                 # development
npm run build && npm start                  # standalone, node --max-old-space-size=192
```

## Configuration

### API (`calor-api`)

| Variable | Default | Meaning |
|---|---|---|
| `CALOR_ADDR` | `:8080` | listen address |
| `CALOR_DB_PATH` | `calor.db` | SQLite file (WAL mode) |
| `CALOR_ADMIN_KEY` | *(empty)* | key for every read and admin endpoint (`Authorization: Bearer …` or `X-API-Key`). **Empty means unauthenticated.** The Makefile uses `dev-admin-key` for local runs; change it anywhere else |
| `CALOR_TRACKER_DIR` | *(unset)* | if set, serves the tracker at `/tracker/calor.min.js` |
| `CALOR_STRICT_ORIGIN` | `true` | reject ingest whose `Origin`, or page URL host, isn't the site's domain or a subdomain |
| `CALOR_MAX_BODY_KB` / `CALOR_MAX_BATCH` | `256` / `200` | ingest limits (413 above them) |
| `CALOR_QUEUE_SIZE` | `2048` | ingest queue length in requests. When it is full, ingest returns 503 with `Retry-After` |
| `CALOR_CACHE_TTL_SEC` | `15` | analytics response cache TTL |
| `CALOR_RETENTION_DAYS` | `0` (keep all) | prune raw events older than N days, hourly. Heatmap rollups are kept |
| `GOMEMLIMIT` / `CALOR_MEMORY_LIMIT_MB` | unset / `192` | Go soft memory limit. The second applies when the first is unset |
| `CALOR_SQLITE_HEAP_MB` | `64` | SQLite `soft_heap_limit`, process-wide. `GOMEMLIMIT` can't see SQLite's memory |
| `CALOR_SQLITE_CACHE_KB` | `4096` | page cache per connection |
| `CALOR_DB_READERS` | `2` | read-only connections |
| `CALOR_SQLITE_MMAP_MB` | `0` | memory-mapped I/O; off so file pages don't count toward RSS |

### Dashboard

| Variable | Default | Meaning |
|---|---|---|
| `CALOR_API_URL` | `http://127.0.0.1:8080` | how the dashboard server reaches the API |
| `CALOR_ADMIN_KEY` | *(empty)* | sent to the API. It stays on the server |
| `CALOR_PUBLIC_API_URL` | `http://localhost:8080` | public API URL, used in the embed snippet |
| `DASHBOARD_USER` / `DASHBOARD_PASSWORD` | *(unset)* | when both are set, HTTP Basic auth on every dashboard route |
| `PORT` / `HOSTNAME` | `3000` / `0.0.0.0` | standard Next.js standalone settings |

## Embedding the tracker

Register the site in the dashboard (**Sites & snippet**) or with `POST /api/sites`. Then
paste the snippet it shows before `</body>`:

```html
<script src="https://calor.example.com/tracker/calor.min.js"
        data-site-id="site_k3j2m9x1q8w7e6r5"
        data-endpoint="https://calor.example.com/api/events" defer></script>
```

| Attribute | Default | |
|---|---|---|
| `data-site-id` | *(required)* | the site ID |
| `data-endpoint` | script origin + `/api/events` | ingest URL |
| `data-flush-interval` | `5000` | ms between periodic flushes |
| `data-max-batch` | `25` | flush as soon as this many events are queued |
| `data-respect-dnt` | *(off)* | if present, do nothing when Do Not Track is on |

- `window.CalorConfig = { siteId, endpoint, flushInterval, maxBatch, respectDnt }`
  works too.
- Add `data-calor-ignore` to an element to exclude clicks inside it.
- `window.calor.flush()` sends the queued events immediately.

**What is captured:**

- **Clicks:** page-relative position, viewport and document size, and the element (CSS
  path, tag, id, classes, trimmed text or aria-label). Rage clicks (3 within 1 s and
  30 px) and dead clicks (outside any interactive element) are flagged.
- **Navigations:** link clicks, SPA `pushState`/`replaceState`/`popstate`, and unloads.
- **Pageviews:** referrer and redirect count.
- **Per pageview:** max scroll depth and visible (engaged) time.

The tracker is 5,246 B minified and **2,584 B gzipped**. See
[`docs/api.md`](docs/api.md#tracker--api-traffic-measured) for the measured request
sizes and when it sends.

## Dashboard features

- **Global**
  - Site switcher showing pageviews per site.
  - Date presets (7, 14, 30, 90 days), custom from/to dates, and a device filter.
  - Local-timezone charts.
  - Optional HTTP Basic auth.
- **Overview** (`/s/{site}`)
  - 8 KPI cards: visitors, pageviews, clicks, avg scroll, avg visible time, bounce rate,
    navigations, rage/dead clicks.
  - Daily trend. Today is drawn dashed because it is partial.
  - Top pages.
  - Clicks by hour, by weekday, and a weekday × hour grid.
  - Scroll reach (25/50/75/100%) and where visitors stopped scrolling.
  - Devices.
  - Top entry pages, exit pages and exit points.
  - Referrers.
  - Most-clicked elements with readable labels and rage/dead badges.
- **Pages** (`/s/{site}/pages`)
  - Per-page table sortable by every column: clicks, unique visitors, pageviews, avg
    scroll, avg time, entries, exits, exit rate, bounce, rage.
  - Search and pagination.
- **Page detail** (`/s/{site}/detail?url=…`)
  - Canvas heatmap with three modes (clicks, link exits, or both) and three backgrounds:
    the page screenshot, a live sandboxed iframe, or a grid.
  - Radius and opacity sliders, numbered pins on the top elements, and screenshot
    upload.
  - Page-level elements, scroll, devices, time of day, navigation flow (came from /
    went to), and trend.
- **Compare** (`/compare?sites=a,b,c`)
  - KPI table for 2–6 sites, with the best value per row highlighted.
  - Overlaid daily-visitors and hour-of-day charts.
  - One column per site.
- **Sites & snippet** (`/sites`): register sites and copy each site's embed snippet.

## Multi-site support

- **API**
  - `POST /api/sites` generates IDs (`site_` + 16 base32 characters) unless one is
    given.
  - Every analytics route is `/api/sites/{site}/…`, and every query filters by
    `site_id`.
  - The response cache is invalidated per site.
- **Ingest**
  - Rejects unknown site IDs with 403.
  - With `CALOR_STRICT_ORIGIN` (the default), the request's `Origin` and the page URL's
    host must match the site's domain or one of its subdomains. `www.` and ports are
    ignored.
- **Tracker:** `data-site-id` is required and is sent with every batch.
- **Dashboard:** a site switcher, per-site routes, site management with snippets, and
  cross-site comparison.

## Component tagging (in progress)

> **Status:** under active development and not finished in this revision. The design
> below is what is being built. Parts may be missing or change before it is marked
> complete.

Component tagging lets you name parts of your pages, then filter heatmaps and stats down
to clicks inside them. Examples: "Hero CTAs", "Pricing cards", "Newsletter form".

**Two ways to tag:**

1. **CSS selectors (primary).** Define a component in the dashboard with a selector
   (`.hero .ctas`, `div.grid > div.card`, `#signup`). The selector can be scoped to pages
   with a path pattern (`/products/*`).
   - You can type the selector, or pick an element on a live preview of the page and
     have the selector generated.
   - A live preview shows how many recorded clicks match before you save.
2. **HTML data attributes.** Mark elements in your own markup, for example
   `<section data-calor-component="newsletter">`. The tracker always records the closest
   `data-calor-component` value, so no configuration is needed. Register the value in the
   dashboard to give it a name. Other attribute names (`data-x="y"`) also work through
   the tracker config.

**How matching works** (it stays within the small-machine memory budget):

- **In the tracker:** it fetches the site's selectors once per session from a small
  cached endpoint and tags each click with the components that contain it.
- **At ingest:** the server also matches each click's recorded ancestry against the
  selectors, for clients whose config hasn't loaded yet.
- **For past data:** creating or editing a component tags historical clicks by matching
  each distinct recorded element once, not each click.

**Planned dashboard pieces:**

- Component list with stats.
- Create, edit and delete, with the element picker, manual selector entry, attribute
  entry, and live match preview.
- A component filter that scopes the heatmap and click stats.
- A per-component analytics page.

See [`docs/architecture.md`](docs/architecture.md#component-tagging-in-progress) for the
storage and matching design, and [`docs/api.md`](docs/api.md#component-tagging-in-progress)
for the planned endpoints.

## API summary

| Endpoint | |
|---|---|
| `POST /api/events` | public ingest (CORS `*`, `text/plain` beacons) → `202 {"accepted":n,"rejected":m}` |
| `GET /healthz` | liveness and current queue length |
| `GET /api/sites`, `POST /api/sites`, `GET/PATCH/DELETE /api/sites/{site}` | site management |
| `GET /api/sites/{site}/kpis`, `daily`, `time-of-day`, `scroll`, `devices`, `pages`, `elements`, `entry-exit`, `referrers`, `flow`, `heatmap` | analytics, filtered by `from`, `to`, `tz`, `device`, `url` |
| `GET/PUT/DELETE /api/sites/{site}/screenshot?url=` | page screenshots used as heatmap backgrounds |
| `GET /api/compare?sites=a,b` | cross-site comparison |
| `/api/sites/{site}/components…` | component tagging (in progress) |

Everything except ingest, health and the tracker files needs the admin key. The full
reference, with parameters, status codes and the wire format, is in
[`docs/api.md`](docs/api.md).

## Resource usage (measured)

Measured with 155k seeded events (3 sites, 30 days, a 79 MB database), with the API, the
dashboard and the demo server on one machine:

| | |
|---|---|
| API RSS, idle | **8.7 MiB** |
| API RSS, after browsing every dashboard route 3 times | 69 MiB (peak 80 MiB) |
| API RSS, 30 s load test (16 ingest + 4 query workers) | peak **147.5 MiB** |
| Load-test throughput | 9,054 events/s accepted, 0 errors (503 backpressure when the queue fills); queries p50 0.6 ms, p95 43 ms |
| Ingest only | ~15k events/s |
| Dashboard RSS, idle / after browsing | **81 MiB** / 127 MiB (peak 133 MiB) |
| Tracker | 5,246 B minified, **2,584 B gzip** |
| Dashboard first-load JS per route | 137–141 KiB gzip. The heatmap chunk (+2.6 KiB) loads only on page detail |
| Binaries | `calor-api` 9.6 MB static; standalone dashboard 64 MB on disk |

How memory stays bounded:

- Go's soft memory limit (`GOMEMLIMIT`), plus a separate cap on SQLite's own memory
  (`soft_heap_limit`), small page caches, two readers, and mmap off.
- A bounded ingest queue that returns 503 instead of buffering.
- A capped response cache.
- An hourly rollup, so heatmaps don't scan raw clicks.
- A V8 heap cap on the dashboard.

Reproduce the numbers with `make measure` and `make loadtest`.

## Deployment

### systemd

Units are in `deploy/systemd/`. They set `MemoryHigh=256M` / `MemoryMax=320M` and
filesystem hardening, and their syntax checks with `systemd-analyze verify`.

```bash
sudo useradd --system --home /opt/calor calor
sudo mkdir -p /opt/calor/bin /opt/calor/tracker /opt/calor/dashboard/.next /etc/calor
sudo cp bin/calor-api /opt/calor/bin/
sudo cp -r tracker/dist /opt/calor/tracker/
sudo cp -r dashboard/.next/standalone /opt/calor/dashboard/.next/
echo 'CALOR_ADMIN_KEY=<a long random value>' | sudo tee /etc/calor/calor.env && sudo chmod 600 /etc/calor/calor.env
sudo cp deploy/systemd/*.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now calor-api calor-dashboard
```

Put a TLS reverse proxy (Caddy or nginx) in front:

- Expose `/api/events` and `/tracker/` publicly.
- Keep the dashboard behind auth.
- The admin read endpoints don't need to be public at all: the dashboard reaches the API
  over localhost.

### Docker Compose

```bash
CALOR_ADMIN_KEY=<a long random value> CALOR_PUBLIC_API_URL=https://calor.example.com \
  docker compose up -d --build
```

Each service has `mem_limit: 320m`, and data lives in the `calor-data` volume. The
Dockerfiles mirror the tested `make start` setup. **They have not yet been run**, because
Docker wasn't available in the environment where this was built.

### Tuning

| Goal | Change |
|---|---|
| less memory | lower `GOMEMLIMIT` and `CALOR_SQLITE_HEAP_MB`, set `CALOR_DB_READERS=1`, lower `CALOR_QUEUE_SIZE`, use `--max-old-space-size=128` |
| faster heavy queries on big sites | raise `CALOR_SQLITE_CACHE_KB` and `CALOR_DB_READERS` |
| bounded disk use | `CALOR_RETENTION_DAYS` |

## Setup caveats and known limits

- **Change the admin key.**
  - The Makefile, the seed script and the examples use `dev-admin-key` for local
    development.
  - An empty `CALOR_ADMIN_KEY` leaves every read endpoint open; the API logs a warning.
  - Use a long random value in production, and keep the dashboard behind
    `DASHBOARD_USER`/`DASHBOARD_PASSWORD` or a proxy that requires sign-in.
- **Domains and origins.**
  - A site's domain must match where the tracker runs. Subdomains are allowed; `www.`
    and ports are ignored.
  - Requests from other origins get 403 while `CALOR_STRICT_ORIGIN=true`.
  - The ingest endpoint is intentionally CORS `*`. The site and origin checks are what
    keep foreign traffic out.
- **Versions.**
  - Go 1.22 is enough. `modernc.org/sqlite` is pinned to v1.34.5 because newer releases
    need a newer Go toolchain; with a newer Go you can upgrade it.
  - Next.js 16 needs Node ≥ 20.9.
- **Screenshots** are not taken automatically.
  - Upload one per page from the page-detail view, or with `PUT
    /api/sites/{site}/screenshot?url=…`. The seed script does this for the demo sites.
  - Without a screenshot, the heatmap falls back to a live iframe (only if the site
    allows framing) or a grid.
  - Heatmap positions are relative to document size, so pages that change layout a lot
    between devices are best viewed with the device filter.
- **Large sites.** On a single site with about 1.3M events in the selected range, the
  heavy aggregations (KPIs, elements, pages, entry/exit) take 2–4 s each with pure-Go
  SQLite.
  - Results are cached for 15 s, and heatmaps use the rollup and stay fast.
  - Shorter ranges, `CALOR_RETENTION_DAYS`, or a larger cache help.
- **Deleting a site** removes its data in bounded transactions. For very large sites it
  can take a while, and the database file doesn't shrink until you run `VACUUM`.
- **Docker** files are untested (see above). systemd and `make start` are the tested
  paths.
- **Component tagging** is in progress (see above).
