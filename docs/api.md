# Calor API reference

Base URL: wherever `calor-api` listens (default `:8080`). All responses are JSON unless
noted. Responses of 1 KB or more are gzipped when the client sends
`Accept-Encoding: gzip`.

## Authentication

- **Public, no key:** `POST /api/events`, `OPTIONS /api/events`, `GET /healthz`,
  `GET /tracker/*`, and `GET /api/sites/{site}/tracker-config`.
- **Everything else** needs the admin key (`CALOR_ADMIN_KEY`), sent as
  `Authorization: Bearer <key>` or `X-API-Key: <key>`. The key is compared in constant
  time.
- If `CALOR_ADMIN_KEY` is empty, the read and admin endpoints are open. The API logs a
  warning at startup; never deploy like that.

CORS is `Access-Control-Allow-Origin: *`. The preflight-only headers (`Allow-Methods`,
`Allow-Headers`, `Max-Age`) are sent only on `OPTIONS` responses.

## Ingest

### `POST /api/events`

Accepts `text/plain` (what `navigator.sendBeacon` sends, which avoids a CORS preflight)
or `application/json`. The body is one batch:

```json
{"v":1,"site":"acme-coffee","sid":"y61n1teccj9eu31bmw8yrdlv","ts":1794210768563,
 "u":"https://acme.example/","p":"s0m1x9bcb2ylfw87mw8yrdlv","vw":1440,"vh":900,
 "e":[{"t":"c","x":540,"y":247,"dw":1440,"dh":2005,
       "el":{"sel":"body>div.wrap.hero>h1","tag":"h1","txt":"Small-batch coffee for people who care about the cup."},
       "dead":1,"d":-3395}]}
```

**Envelope:**

| Key | Meaning |
|---|---|
| `v` | wire version |
| `site` | site ID |
| `sid` | session ID |
| `ts` | send time (unix ms) |
| `u` | page URL, the default for events |
| `p` | pageview ID, the default for events |
| `vw` / `vh` | viewport, the default for events |
| `e` | events (1–200) |

**Event:** every event has `t` and `d`. The other keys depend on the type.

| Key | Meaning |
|---|---|
| `t` | type: `c` click, `n` navigation, `p` pageview, `s` scroll summary (long names `click`, `navigation`, `pageview`, `scroll` are also accepted) |
| `d` | ms offset from the envelope `ts`. The server re-anchors to its own clock, which fixes client clock skew |
| `u`, `p`, `vw`, `vh` | only present when they differ from the envelope |
| `x`, `y`, `dw`, `dh` | click position relative to the document, and document size (clicks; also link navigations) |
| `el` | clicked element: `sel` (CSS path, at most 5 levels, up to 2 classes per ancestor), `tag`, `id`, `cls` (up to 6 classes), `txt` (≤ 80 chars), `aria` (aria-label/title/alt), `ca` (closest `data-calor-component` value, ≤ 60 chars) |
| `cmp` | IDs of the site's components that contain the click, matched in the browser (clicks). The server keeps at most 20, and only IDs of the site's own components |
| `rage`, `dead` | click flags |
| `k` | kind. Navigation: `link`, `pushstate`, `replacestate`, `popstate`, `unload`. Pageview: `load` or the SPA kinds |
| `to` | navigation target URL |
| `ref` | referrer (pageviews) |
| `rc` | redirect count (pageviews) |
| `sd` | scroll depth in % (pageview: initial; scroll summary: max) |
| `ms` | visible time in ms (scroll summary) |

**Responses:**

| Status | When |
|---|---|
| `202` | `{"accepted":n,"rejected":m}`. Invalid events are counted in `rejected`; the rest are stored |
| `400` | malformed JSON, empty `e`, or invalid `sid` |
| `403` | unknown site, or `Origin` / page host not matching the site's domain (`CALOR_STRICT_ORIGIN`) |
| `413` | body over `CALOR_MAX_BODY_KB` or more than `CALOR_MAX_BATCH` events |
| `503` | ingest queue full; comes with a `Retry-After` header. Clients should back off |

Admin-only backfill: with the admin key plus `X-Calor-Backfill: 1`, client timestamps are
trusted, up to 35 days old. The seed script uses this.

## Sites

| Method & path | |
|---|---|
| `GET /api/sites` | list, with `pageviews` and `lastEventAt` |
| `POST /api/sites` | `{"name","domain","id"?}` → 201. The ID is generated (`site_` + 16 chars) unless given (`^[a-z0-9][a-z0-9_-]{2,63}$`). The domain is normalized (scheme, port and `www.` stripped) |
| `GET /api/sites/{site}` | one site |
| `PATCH /api/sites/{site}` | `{"name"?, "domain"?}` |
| `DELETE /api/sites/{site}` | deletes the site and all its data, in bounded transactions → 204 |

## Analytics

All analytics routes are scoped by site: `/api/sites/{site}/…`.

**Common query parameters:**

| Parameter | Meaning |
|---|---|
| `from`, `to` | `YYYY-MM-DD` (interpreted in the `tz` offset; `to` is inclusive), unix ms, or RFC 3339 |
| `tz` | minutes east of UTC |
| `device` | `mobile` (< 768 px), `tablet` (< 1024 px) or `desktop` |
| `url` | restrict to one page (normalized to scheme://host/path) |
| `component` | restrict click-based metrics to clicks inside this component (see below). Pageviews, scroll and navigation data stay unscoped. Unknown IDs return 404 |

**Endpoints:**

| Path | Returns |
|---|---|
| `kpis` | unique visitors, sessions, pageviews, clicks, avg scroll depth, avg visible time, bounce rate, navigations, rage/dead clicks |
| `daily` | gap-filled daily clicks, pageviews and visitors |
| `time-of-day` | clicks per hour, per weekday, the 7×24 matrix, and the peak |
| `scroll` | reach at 25/50/75/100%, a 10-bucket max-depth distribution, avg time |
| `devices` | visitors and share per device class |
| `pages?sort=&order=&limit=&offset=&q=` | per-page clicks, unique visitors, pageviews, avg scroll, avg time, entries, exits, exit rate, bounce, rage; sorted and paginated |
| `elements?limit=` | most-clicked elements: label (aria > text > #id > tag), selector, page, clicks, visitors, rage, dead, average relative position |
| `entry-exit?limit=` | top entry pages, exit pages, and exit points (last click of the final pageview) |
| `referrers` | external referrer hosts (internal ones excluded) |
| `flow?url=` | for one page: where visitors came from and where they went next |
| `heatmap?url=&type=click\|navigation\|all&cols=&rows=` | binned grid `{cols, rows, cells:[{x,y,n}], …meta}`. Served from the hourly rollup when the range is hour-aligned and no `component` is set; raw events otherwise (`source` says which). With `component`, only clicks are counted |
| `screenshot?url=` (GET, PUT or POST, DELETE) | page screenshot: png/jpeg/webp/gif, ≤ 8 MiB, raw body or multipart. GET supports ETag |
| `screenshot/meta?url=` | screenshot width, height and updatedAt |

**Cross-site:**

| Path | Returns |
|---|---|
| `GET /api/compare?sites=a,b[,…]` | 2–6 sites: KPIs, daily, hours, weekdays, scroll reach, devices, top 5 pages per site |

Responses are cached for `CALOR_CACHE_TTL_SEC` (default 15 s, at most 256 entries / 16
MB). The cache is invalidated per site on ingest, so a response carries `X-Cache: hit`
or `miss`.

## Component tagging

A component has a `name` and one or both rules: a CSS `selector`, and an attribute
(`attrName`, default `data-calor-component`, plus `attrValue`). `pagePattern` optionally
limits it to paths (`/`, `/products/*`; `*` matches anything, the query string is
ignored). See the README section "Component tagging" for the model and limits.

| Method & path | |
|---|---|
| `GET /api/sites/{site}/components?from&to&tz&device` | `{"components":[Component], "limit":200}` with `clicks`, `visitors` and `lastSeen` for the range |
| `POST /api/sites/{site}/components` | `{"name", "selector"?, "attrName"?, "attrValue"?, "pagePattern"?}` → `201 {"component", "tagged"}`. Past clicks are tagged before it returns; `tagged` is how many |
| `GET /api/sites/{site}/components/{id}` | Component with range stats |
| `PATCH /api/sites/{site}/components/{id}` | any subset of the create fields → `{"component", "retagged", "tagged"?}`. Past clicks are re-tagged only when a rule changes |
| `DELETE /api/sites/{site}/components/{id}` | → 204. Its tags are removed in bounded transactions |
| `GET /api/sites/{site}/components/{id}/stats?from&to&tz&device` | `{"component", "stats"}`: clicks, visitors, rage/dead clicks, `clickShare` (% of all clicks), `exposedPageviews`, `clickedPageviews`, `clickRate`, first/last seen, top pages |
| `POST /api/sites/{site}/components/preview?from&to&tz&device` | body like create (`name` optional) → `{"preview":{matchedClicks, totalClicks, share, pages, elements, evaluable}, "browserSelector", "attrName"}`. Nothing is written |
| `GET /api/sites/{site}/components/discovered?from&to` | `{"attribute":"data-calor-component", "values":[{value, clicks, pages, componentId?}]}`: values seen on clicks, with the component that claims each |
| `GET /api/sites/{site}/tracker-config` | public, CORS `*`, `Cache-Control: max-age=300`. `{"c":[[id, browserSelector, pagePattern], …]}` |

The trend, elements, time-of-day and heatmap views of a component use the regular
analytics routes with `component=<id>`.

**Component object:**

```json
{"id":"cmp_cated4z2hwf3q","siteId":"acme-coffee","name":"Add to cart buttons",
 "selector":"button.add-to-cart","attrName":"","attrValue":"","pagePattern":"",
 "createdAt":1794225160824,"updatedAt":1794225160824,
 "browserSelector":"button.add-to-cart","evaluable":true,
 "clicks":2294,"visitors":1393,"lastSeen":1794225757685}
```

- `browserSelector` is what the tracker runs: the selector, the attribute rule as
  `[name="value"]`, or both joined with a comma.
- `evaluable` is false when the server can't check the rule against recorded clicks
  (attribute tests the tracker doesn't record). Such components are tagged only in the
  browser, from new traffic.

**Validation** (400 with `{"error"}`):

- A name (≤ 80 chars) and at least one rule are required.
- Selectors: at most 300 characters, 10 comma alternatives and 8 compounds each. Supported:
  tag, `*`, `#id`, `.class`, `[attr]`, `[attr=value]` (quoted or not), `:nth-of-type(n)`,
  descendant and `>` combinators. Sibling combinators, other pseudo-classes and
  pseudo-elements are rejected with a message that says why.
- `attrName` must be a lowercase attribute name. `data-calor-component` values are at
  most 60 characters (the tracker records 60), other values at most 100.
- `pagePattern` must start with `/`.

**Conflicts** (409 with `{"error", "conflictsWith"?}`): a name already used in the site
(case-insensitive), an attribute value already used by another component, or the limit of
200 components per site.

## Tracker → API traffic (measured)

Captured on the wire by `make e2e` (`e2e/live.mjs`). It drives Chrome through the demo
sites, and a recording proxy in front of the API records every request, including
beacons sent while a page unloads.

| Request | Events | Body | Request headers | Response |
|---|---|---|---|---|
| pageview on load (5 s timer flush) | 1 | 209 B | ~660 B | 202, 27 B body + 146 B headers |
| single click (timer flush) | 1 | 345 B | ~660 B | 202 |
| link click → navigation (click + nav + scroll summary) | 3 | 494–497 B | ~660 B | 202 |
| page exit (pageview + click + unload + scroll) | 4 | 513 B | ~660 B | 202 |
| typical batch (pageview + 5 clicks, incl. rage) | 6 | 1,388 B | ~660 B | 202 |
| SPA session (3 `pushState` + `popstate` + link) | 16 | 2,351 B | ~660 B | 202 |

Request headers come from the browser and can't be trimmed.

**When the tracker sends:**

- Events are batched, never one request per click.
- A batch is flushed every 5 s when anything is queued, immediately at 25 events, on a
  same-tab link click (the page's scroll summary rides along), on `visibilitychange` to
  hidden, and on `pagehide`.
- It uses `sendBeacon` and falls back to `fetch(…, {keepalive: true})`.
- Scroll handling is passive and throttled to 250 ms, with one summary per pageview.

**Typical query response sizes** (seeded demo data):

| Endpoint | Raw | Gzip |
|---|---|---|
| kpis | 287 B | — |
| scroll | 288 B | — |
| devices | 239 B | — |
| time-of-day | 830 B | — |
| daily (30 d) | 1,984 B | 447 B |
| entry-exit | 2,275 B | 483 B |
| elements (10) | 3,468 B | 771 B |
| flow | 4,014 B | 918 B |
| pages (25) | 5,219 B | 1,395 B |
| heatmap 100×139 | 13,918 B | 2,777 B |
| compare (3 sites) | 13,774 B | 3,843 B |

Responses under 1 KB are sent uncompressed.
