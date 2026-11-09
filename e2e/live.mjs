// End-to-end check with the real tracker: drives Chrome through the demo
// sites served by a private demo server (LIVE_PORT, default 5501) whose
// tracker endpoint is a recording proxy in front of the API, so every beacon
// is captured byte-for-byte on the wire, including the ones sent while a page
// unloads (DevTools drops most of those). Then verifies the events reached
// the API and measures typical dashboard query response sizes.
// Run it before `seed.mjs`: seeding re-registers the sites and wipes these
// test events.
//   node live.mjs  -> prints a summary, writes output/traffic.json
import { mkdirSync, writeFileSync } from 'node:fs';
import http from 'node:http';
import { spawn } from 'node:child_process';
import { api, API, auth, DEMO_PORT as SEEDED_PORT, launch, waitFor } from './lib.mjs';

const DEMO_PORT = +(process.env.LIVE_PORT || 5501);
const PROXY_PORT = +(process.env.PROXY_PORT || 8099);

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const captured = [];

// Records raw request/response bytes for /api/events and forwards everything to the API.
function recordingProxy() {
  const target = new URL(API);
  return http.createServer((req, res) => {
    const chunks = [];
    req.on('data', (c) => chunks.push(c));
    req.on('end', () => {
      const body = Buffer.concat(chunks);
      const up = http.request({ host: target.hostname, port: target.port, method: req.method, path: req.url, headers: req.headers }, (ur) => {
        const out = [];
        ur.on('data', (c) => out.push(c));
        ur.on('end', () => {
          const resBody = Buffer.concat(out);
          if (req.url.startsWith('/api/events') && req.method === 'POST') {
            const raw = (pairs) => pairs.reduce((n, v) => n + Buffer.byteLength(v) + 2, 0) // "name: value\r\n";
            captured.push({
              at: Date.now(),
              body: body.toString(),
              reqHeaders: req.headers,
              requestHeaderBytes: Buffer.byteLength(`${req.method} ${req.url} HTTP/${req.httpVersion}\r\n`) + raw(req.rawHeaders) + 2,
              status: ur.statusCode,
              resHeaders: ur.headers,
              responseHeaderBytes: Buffer.byteLength(`HTTP/1.1 ${ur.statusCode} ${ur.statusMessage}\r\n`) + raw(ur.rawHeaders) + 2,
              responseBody: resBody.toString(),
            });
          }
          res.writeHead(ur.statusCode, ur.rawHeaders);
          res.end(resBody);
        });
      });
      up.on('error', () => (res.writeHead(502), res.end()));
      up.end(body);
    });
  });
}

async function tracked(browser) {
  const ctx = await browser.createBrowserContext();
  const page = await ctx.newPage();
  await page.setViewport({ width: 1440, height: 900 });
  return { ctx, page };
}

async function scrollTo(page, frac, steps = 6) {
  for (let i = 1; i <= steps; i++) {
    await page.evaluate((f) => window.scrollTo(0, (document.documentElement.scrollHeight - innerHeight) * f), (frac * i) / steps);
    await sleep(320);
  }
}

async function clickText(page, selector, text) {
  const handles = await page.$$(selector);
  for (const h of handles) {
    const t = await h.evaluate((el) => (el.getAttribute('aria-label') || el.textContent || '').trim());
    if (t.includes(text)) {
      await h.scrollIntoView();
      await h.click();
      return;
    }
  }
  throw new Error(`no ${selector} containing "${text}"`);
}

async function counts(site) {
  const k = (await api('GET', `/api/sites/${site}/kpis`)).json;
  return { clicks: k.clicks, pageviews: k.pageviews, visitors: k.uniqueVisitors, rage: k.rageClicks, navigations: k.navigations };
}

async function main() {
  await waitFor(`${API}/healthz`);
  const sites = ['acme-coffee', 'nimbus-analytics', 'fieldnote-journal'];
  const proxy = recordingProxy().listen(PROXY_PORT);
  const demo = spawn(process.execPath, ['../tracker/demo/serve.mjs'], { env: { ...process.env, DEMO_PORT: String(DEMO_PORT), CALOR_ENDPOINT: `http://localhost:${PROXY_PORT}/api/events` }, stdio: 'ignore' });
  process.on('exit', () => demo.kill());
  await waitFor(`http://127.0.0.1:${DEMO_PORT}/calor.min.js`);
  const before = Object.fromEntries(await Promise.all(sites.map(async (s) => [s, await counts(s)])));
  const browser = await launch();

  // Acme: CTA -> shop, single dead click flushed by the 5 s timer, rage clicks, cart, exit.
  {
    const { ctx, page } = await tracked(browser);
    await page.goto(`http://acme.localhost:${DEMO_PORT}/`, { waitUntil: 'networkidle0' });
    await sleep(5600);
    await page.click('h1');
    await sleep(5600);
    await scrollTo(page, 0.7);
    await clickText(page, 'a.btn', 'Shop coffee');
    await page.waitForNavigation({ waitUntil: 'networkidle0' }).catch(() => {});
    await clickText(page, 'button.chip', 'Light roast');
    await sleep(400);
    const add = (await page.$$('button.add-to-cart'))[0];
    await add.click();
    for (let i = 0; i < 3; i++) await add.click({ delay: 20 });
    await sleep(5600);
    await clickText(page, 'header a', 'Cart');
    await page.waitForNavigation({ waitUntil: 'networkidle0' }).catch(() => {});
    await page.click('button.checkout');
    await sleep(800);
    await page.goto('about:blank');
    await sleep(800);
    await ctx.close();
  }
  // Nimbus: SPA routes (pushState + popstate), then pricing via link.
  {
    const { ctx, page } = await tracked(browser);
    await page.goto(`http://nimbus.localhost:${DEMO_PORT}/`, { waitUntil: 'networkidle0' });
    await clickText(page, 'nav.main a', 'Live demo');
    await page.waitForNavigation({ waitUntil: 'networkidle0' }).catch(() => {});
    await clickText(page, '.tabs a', 'Reports');
    await sleep(300);
    await clickText(page, 'button', 'Open report');
    await clickText(page, '.tabs a', 'Alerts');
    await sleep(300);
    await page.goBack();
    await sleep(500);
    await clickText(page, 'nav.main a', 'Pricing');
    await page.waitForNavigation({ waitUntil: 'networkidle0' }).catch(() => {});
    await clickText(page, 'button.chip', 'Yearly');
    await scrollTo(page, 1);
    await sleep(600);
    await page.goto('about:blank');
    await sleep(800);
    await ctx.close();
  }
  // Fieldnote: read an article to the bottom, then leave.
  {
    const { ctx, page } = await tracked(browser);
    await page.goto(`http://fieldnote.localhost:${DEMO_PORT}/`, { waitUntil: 'networkidle0' });
    await clickText(page, 'a.read-more', 'Read the story');
    await page.waitForNavigation({ waitUntil: 'networkidle0' }).catch(() => {});
    await scrollTo(page, 1, 10);
    await sleep(1200);
    await clickText(page, 'button', 'Copy link');
    await sleep(300);
    await page.goto('about:blank');
    await sleep(800);
    await ctx.close();
  }
  await browser.close();
  await sleep(1500);
  demo.kill();
  proxy.close();

  const after = Object.fromEntries(await Promise.all(sites.map(async (s) => [s, await counts(s)])));
  const delta = Object.fromEntries(sites.map((s) => [s, Object.fromEntries(Object.keys(after[s]).map((k) => [k, after[s][k] - before[s][k]]))]));

  const reqs = captured
    .sort((a, b) => a.at - b.at)
    .map((r) => {
      const body = JSON.parse(r.body);
      const h = r.reqHeaders;
      return {
        transport: h['sec-fetch-mode'] === 'no-cors' ? 'sendBeacon' : 'fetch keepalive',
        page: body.u,
        events: body.e.length,
        types: body.e.map((e) => e.t + (e.k ? `:${e.k}` : '')),
        bodyBytes: Buffer.byteLength(r.body),
        requestHeaderBytes: r.requestHeaderBytes,
        requestHeaders: Object.keys(h),
        contentType: h['content-type'],
        origin: h.origin,
        status: r.status,
        responseHeaderBytes: r.responseHeaderBytes,
        responseBodyBytes: Buffer.byteLength(r.responseBody),
        responseBody: r.responseBody,
        body,
      };
    });

  const single = reqs.find((r) => r.events === 1 && r.types[0] === 'c');
  const exit = reqs.find((r) => r.types.includes('n:unload'));
  const linkNav = reqs.find((r) => r.types.includes('n:link'));
  // Timer-flushed batch (no navigation in it) with the most events.
  const typical = reqs.filter((r) => !r.types.some((t) => t.startsWith('n'))).sort((a, b) => b.events - a.events)[0];

  // Typical dashboard query sizes (identity vs gzip).
  const now = new Date();
  const to = now.toISOString().slice(0, 10);
  const from = new Date(now - 29 * 86_400_000).toISOString().slice(0, 10);
  const home = encodeURIComponent(`http://acme.localhost:${SEEDED_PORT}/`);
  const q = `from=${from}&to=${to}&tz=0`;
  const endpoints = {
    kpis: `/api/sites/acme-coffee/kpis?${q}`,
    daily: `/api/sites/acme-coffee/daily?${q}`,
    'time-of-day': `/api/sites/acme-coffee/time-of-day?${q}`,
    scroll: `/api/sites/acme-coffee/scroll?${q}`,
    devices: `/api/sites/acme-coffee/devices?${q}`,
    'pages (limit 25)': `/api/sites/acme-coffee/pages?${q}&limit=25`,
    'elements (limit 10)': `/api/sites/acme-coffee/elements?${q}&limit=10`,
    'entry-exit': `/api/sites/acme-coffee/entry-exit?${q}&limit=8`,
    referrers: `/api/sites/acme-coffee/referrers?${q}`,
    flow: `/api/sites/acme-coffee/flow?${q}&url=${home}`,
    'heatmap 100x139': `/api/sites/acme-coffee/heatmap?${q}&url=${home}&cols=100&rows=139`,
    'compare (3 sites)': `/api/compare?${q}&sites=${sites.join(',')}`,
  };
  const rawGet = (path, enc) =>
    new Promise((resolve, reject) => {
      const t0 = performance.now();
      http.get(API + path, { headers: { ...auth, 'Accept-Encoding': enc } }, (res) => {
        let n = 0;
        res.on('data', (c) => (n += c.length));
        res.on('end', () => resolve({ status: res.statusCode, bytes: n, enc: res.headers['content-encoding'], cache: res.headers['x-cache'], ms: Math.round((performance.now() - t0) * 10) / 10 }));
      }).on('error', reject);
    });
  const sizes = {};
  for (const [name, path] of Object.entries(endpoints)) {
    const plain = await rawGet(path, 'identity');
    const gz = await rawGet(path, 'gzip');
    sizes[name] = { status: gz.status, rawBytes: plain.bytes, gzipBytes: gz.enc === 'gzip' ? gz.bytes : null, cache: gz.cache, ms: gz.ms };
  }

  const report = { capturedRequests: reqs.length, delta, examples: { singleClick: single, typicalBatch: typical, linkNavigation: linkNav, exitBeacon: exit }, requests: reqs.map(({ body, ...r }) => r), apiResponseSizes: sizes };
  mkdirSync('output', { recursive: true });
  writeFileSync('output/traffic.json', JSON.stringify(report, null, 2));

  console.log(`captured ${reqs.length} tracker requests`);
  for (const r of reqs) console.log(`  ${r.transport.padEnd(15)} ${String(r.bodyBytes).padStart(5)} B body  ${String(r.requestHeaderBytes).padStart(4)} B hdr  -> ${r.status} ${r.responseBody} (${r.responseBodyBytes} B body, ${r.responseHeaderBytes} B hdr)  [${r.types.join(' ')}]`);
  console.log('events landed in the API (after - before):', JSON.stringify(delta));
  console.log('API response sizes:', JSON.stringify(sizes, null, 1));
  const ok = sites.every((s) => delta[s].pageviews > 0) && delta['acme-coffee'].clicks > 0 && delta['acme-coffee'].rage > 0 && reqs.every((r) => r.status === 202);
  if (!ok) {
    console.error('E2E FAILED');
    process.exit(1);
  }
  console.log('E2E OK');
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
