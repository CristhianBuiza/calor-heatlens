// Reproducible sample data: registers the three demo sites, measures real
// element positions on every demo page in Chrome (using the tracker's own
// describe() for labels), uploads full-page screenshots, then simulates
// ~3 weeks of sessions and sends them through the public ingest endpoint
// (admin-authorised backfill so historical timestamps are kept).
//
//   node seed.mjs [--days 21] [--scale 1]
import { api, API, DEMO_PORT, KEY, launch, rng, waitFor } from './lib.mjs';
import { sites as demoSites } from '../tracker/demo/sites.mjs';

const argv = process.argv.slice(2);
const arg = (name, def) => {
  const i = argv.indexOf(`--${name}`);
  return i >= 0 ? Number(argv[i + 1]) : def;
};
const DAYS = arg('days', 30);
const SCALE = arg('scale', 1);
const R = rng(20261109);

const H = (arr) => arr.map((w, h) => [h, w]);
const CFG = {
  'acme-coffee': {
    sessions: 150,
    bounce: 0.34,
    weekday: [1.3, 0.9, 0.9, 0.95, 1, 1.15, 1.45],
    hours: H([2, 1, 1, 1, 2, 4, 8, 13, 14, 10, 8, 8, 9, 8, 7, 7, 8, 10, 12, 14, 13, 10, 6, 3]),
    referrers: { 'https://www.google.com/': 40, 'https://www.instagram.com/': 24, '': 24, 'https://www.pinterest.com/': 6, 'https://mail.google.com/': 6 },
    entries: { '/': 46, '/shop': 16, '/brew-guides': 12, '/subscribe': 8, '/products/ethiopia-guji': 7, '/products/house-espresso': 5, '/products/kenya-nyeri': 3, '/contact': 3 },
    devices: { desktop: 45, mobile: 45, tablet: 10 },
    rules: [[/^Shop coffee$/, 9], [/Start a subscription/, 6], [/Add to cart/, 5], [/^Subscribe$/, 3], [/^Shop$/, 4], [/Subscriptions/, 2], [/Brew guides/, 2], [/Cart/, 3], [/^Choose /, 5], [/Checkout/, 7], [/roast|Decaf|^All$/, 2.5], [/Keep shopping/, 1.5], [/Subscribe & save/, 3], [/^(Whole bean|Drip|Espresso|French press)$/, 2], [/Ethiopia Guji|Colombia Huila|House Espresso|Kenya Nyeri|Guatemala Antigua|Swiss Water Decaf/, 3.5], [/Request a price list/, 4], [/Save 10%/, 2], [/Shop pour-over/, 3], [/you@example/, 2.5]],
    rage: [/Checkout/, 0.3],
    pages: { '/': { clicks: 2.4, exit: 0.35, depth: 62 }, '/shop': { clicks: 3.2, exit: 0.3, depth: 70 }, '/subscribe': { clicks: 1.8, exit: 0.45, depth: 66 }, '/brew-guides': { clicks: 0.9, exit: 0.55, depth: 55 }, '/about': { clicks: 0.6, exit: 0.6, depth: 48 }, '/cart': { clicks: 2.2, exit: 0.5, depth: 90 }, '/products/ethiopia-guji': { clicks: 2.4, exit: 0.4, depth: 64 }, '/products/colombia-huila': { clicks: 2.2, exit: 0.42, depth: 60 }, '/products/house-espresso': { clicks: 2.3, exit: 0.4, depth: 62 }, '/products/kenya-nyeri': { clicks: 2, exit: 0.45, depth: 58 }, '/products/guatemala-antigua': { clicks: 1.9, exit: 0.45, depth: 57 }, '/products/swiss-water-decaf': { clicks: 1.7, exit: 0.5, depth: 55 }, '/contact': { clicks: 1.5, exit: 0.6, depth: 92 } },
  },
  'nimbus-analytics': {
    sessions: 230,
    bounce: 0.4,
    weekday: [0.35, 1.15, 1.25, 1.2, 1.15, 0.95, 0.4],
    hours: H([1, 1, 1, 1, 1, 2, 3, 6, 10, 14, 16, 15, 11, 13, 15, 14, 12, 9, 6, 4, 3, 2, 2, 1]),
    referrers: { 'https://www.google.com/': 34, '': 26, 'https://news.ycombinator.com/': 12, 'https://www.linkedin.com/': 10, 'https://www.g2.com/': 8, 'https://x.com/': 10 },
    entries: { '/': 50, '/pricing': 18, '/docs': 26, '/signup': 6 },
    devices: { desktop: 80, mobile: 15, tablet: 5 },
    rules: [[/Start free trial|Start trial/, 10], [/Book a demo|Contact sales/, 4], [/^Pricing$|See pricing/, 4], [/^Docs$|Read the docs/, 3], [/Live demo/, 3], [/Log in/, 2.5], [/^(Overview|Reports|Alerts)$/, 5], [/Yearly/, 4], [/\?$/, 2.5], [/Create account/, 6], [/Continue with Google/, 4], [/Open report|New alert/, 3], [/Quickstart|Connect a warehouse|first dashboard|Create your workspace/, 2], [/Work email|Company|Password/, 3]],
    rage: [/Continue with Google/, 0.35],
    pages: { '/': { clicks: 2.2, exit: 0.4, depth: 58 }, '/pricing': { clicks: 2.6, exit: 0.35, depth: 72 }, '/docs': { clicks: 1.4, exit: 0.5, depth: 52 }, '/signup': { clicks: 3, exit: 0.55, depth: 95 }, '/app': { clicks: 2.4, exit: 0.4, depth: 80 }, '/app/reports': { clicks: 2, exit: 0.45, depth: 85 }, '/app/alerts': { clicks: 1.6, exit: 0.5, depth: 88 } },
  },
  'fieldnote-journal': {
    sessions: 105,
    bounce: 0.52,
    weekday: [1.7, 0.8, 0.85, 0.85, 0.9, 1, 1.2],
    hours: H([3, 2, 1, 1, 1, 2, 3, 5, 8, 7, 6, 6, 7, 6, 5, 5, 6, 8, 11, 14, 15, 13, 9, 5]),
    referrers: { '': 20, 'https://www.google.com/': 30, 'https://mail.google.com/': 22, 'https://www.pinterest.com/': 10, 'https://www.reddit.com/': 14, 'https://www.instagram.com/': 4 },
    entries: { '/': 30, '/posts/slow-travel-in-portugal': 26, '/posts/kumano-kodo': 20, '/posts/night-trains-europe': 14, '/about': 4, '/newsletter': 6 },
    devices: { desktop: 50, mobile: 40, tablet: 10 },
    rules: [[/Read the latest/, 6], [/Read the story/, 5], [/Three weeks|Kumano|night train/, 5], [/^Subscribe$/, 5], [/Share/, 2], [/Copy link/, 3], [/More stories/, 2], [/^(Portugal|Japan|Italy|Norway|Mexico|Georgia|Vietnam|Scotland)$/, 2.5], [/Get it by email/, 3], [/Your email/, 3]],
    rage: [/Copy link/, 0.3],
    pages: { '/': { clicks: 1.8, exit: 0.35, depth: 64 }, '/posts/slow-travel-in-portugal': { clicks: 1.1, exit: 0.5, depth: 58 }, '/posts/kumano-kodo': { clicks: 1, exit: 0.55, depth: 52 }, '/posts/night-trains-europe': { clicks: 1.2, exit: 0.5, depth: 66 }, '/destinations': { clicks: 1.6, exit: 0.45, depth: 70 }, '/about': { clicks: 0.6, exit: 0.6, depth: 60 }, '/newsletter': { clicks: 2, exit: 0.6, depth: 98 } },
  },
};

// Demo component tags: CSS selectors (the primary mechanism) plus
// data-calor-component values present in the demo markup. post-subscribe and
// cart-summary are left unregistered so they show up as discovered values.
const COMPONENTS = {
  'acme-coffee': [
    { name: 'Hero CTAs', selector: '.hero .ctas', pagePattern: '/' },
    { name: 'Add to cart buttons', selector: 'button.add-to-cart' },
    { name: 'Main navigation', selector: 'header.top nav.main' },
    { name: 'Product cards', attrValue: 'product-card' },
    { name: 'Newsletter signup', attrValue: 'newsletter-signup', pagePattern: '/' },
    { name: 'Product options', selector: '.chips', pagePattern: '/products/*' },
  ],
  'nimbus-analytics': [
    { name: 'Hero CTAs', selector: '.hero .ctas', pagePattern: '/' },
    { name: 'Pricing cards', attrValue: 'pricing-card', pagePattern: '/pricing' },
    { name: 'Pricing FAQ', selector: 'details', pagePattern: '/pricing' },
    { name: 'App tabs', selector: '.tabs', pagePattern: '/app*' },
    { name: 'Header CTAs', selector: 'header.top .btn' },
  ],
  'fieldnote-journal': [
    { name: 'Share bar', attrValue: 'share-bar', pagePattern: '/posts/*' },
    { name: 'Story cards', selector: '.grid .card', pagePattern: '/' },
    { name: 'Header subscribe', selector: 'header.top .btn.primary' },
  ],
};

const DEVICE_VIEW = { desktop: [1440, 900, 1], mobile: [390, 844, 2.4], tablet: [820, 1180, 1.35] };

async function measure(browser, site, path, withComponents) {
  const page = await browser.newPage();
  // Component ids change on every re-seed; never reuse a cached tracker config.
  await page.setCacheEnabled(false);
  // Request interception never sees sendBeacon (ping) requests, so silence the tracker in-page.
  await page.evaluateOnNewDocument(() => {
    navigator.sendBeacon = () => true;
    const f = window.fetch;
    window.fetch = (u, o) => (String(u).includes('/api/events') ? Promise.resolve(new Response(null, { status: 202 })) : f(u, o));
  });
  const url = `http://${site.host}:${DEMO_PORT}${path}`;
  await page.goto(url, { waitUntil: 'networkidle0' });
  await page.waitForFunction(() => window.calor && window.calor.describe);
  // The tracker fetches the site's component rules once per session.
  if (withComponents) await page.waitForFunction(() => sessionStorage.getItem('calor_cfg'));
  const data = await page.evaluate(() => {
    const INTERACTIVE = 'a[href],button,input,select,textarea,summary,[role=tab]';
    const sel = `${INTERACTIVE},h1,h2,h3,.thumb,.logos span,.kpi,.cover,.quote,.price,article p`;
    const out = [];
    for (const el of document.querySelectorAll(sel)) {
      const r = el.getBoundingClientRect();
      if (r.width < 4 || r.height < 4) continue;
      const interactive = el.matches(INTERACTIVE);
      if (!interactive && el.closest(INTERACTIVE)) continue;
      const desc = window.calor.describe(el);
      const a = el.closest('a[href]');
      const cmp = window.calor.match(el);
      out.push({
        cmp: cmp.length ? cmp : undefined,
        interactive,
        header: !!el.closest('header.top'),
        route: !!el.closest('[data-route]'),
        href: a ? a.href : '',
        box: { x: r.left + scrollX, y: r.top + scrollY, w: r.width, h: r.height },
        desc,
      });
    }
    const e = document.documentElement;
    return { elements: out, docW: Math.max(e.scrollWidth, document.body.scrollWidth), docH: Math.max(e.scrollHeight, document.body.scrollHeight) };
  });
  const shot = await page.screenshot({ fullPage: true, type: 'jpeg', quality: 82 });
  await page.close();
  return { url: `http://${site.host}:${DEMO_PORT}${path}`, path, ...data, shot };
}

function weightOf(cfg, el) {
  const label = el.desc.aria || el.desc.txt || '';
  for (const [re, w] of cfg.rules) if (re.test(label)) return w;
  if (el.interactive) return el.header ? 1.2 : 1;
  if (el.desc.tag === 'div' && /thumb|cover/.test(el.desc.cls || '')) return 0.7;
  return 0.25;
}

function simulateSession(site, cfg, layouts, start) {
  const sid = `seed${start.toString(36)}${R.int(1e9).toString(36)}`;
  const device = R.pick(cfg.devices);
  const [vw, vh, hMul] = DEVICE_VIEW[device];
  const events = [];
  let t = start;
  let path = R.pick(cfg.entries);
  let ref = R.pick(cfg.referrers);
  let kind = 'load';
  const origin = `http://${site.host}:${DEMO_PORT}`;
  for (let step = 0; step < 8; step++) {
    const L = layouts[path];
    const P = cfg.pages[path] || { clicks: 1, exit: 0.5, depth: 60 };
    if (!L) break;
    const url = origin + path;
    const pv = `pv${R.int(2 ** 31).toString(36)}${step}`;
    const dw = device === 'desktop' ? L.docW : vw;
    const dh = Math.round(L.docH * hMul);
    const fold = Math.min(100, Math.round((vh * 100) / dh));
    const depth = Math.max(fold, Math.min(100, Math.round(R.normal(P.depth, 24))));
    const pos = (box) => {
      const rx = (box.x + box.w / 2 + R.normal(0, box.w / 5)) / L.docW;
      const ry = (box.y + box.h / 2 + R.normal(0, box.h / 4)) / L.docH;
      return [Math.round(Math.max(0, Math.min(1, rx)) * dw), Math.round(Math.max(0, Math.min(1, ry)) * dh)];
    };
    events.push({ t: 'p', ts: t, u: url, p: pv, k: kind, ref: ref || undefined, sd: fold, vw, vh });
    const reachable = L.elements.filter((e) => (e.box.y + e.box.h / 2) / L.docH <= depth / 100 + 0.02);
    const weights = reachable.map((e, i) => [i, weightOf(cfg, e)]);
    const bounced = step === 0 && R.chance(cfg.bounce);
    const nClicks = bounced ? 0 : Math.min(12, R.poisson(P.clicks));
    let next = null;
    let leftSite = false;
    let clicks = 0;
    for (let c = 0; c < nClicks && weights.length; c++) {
      t += Math.round(R.range(1500, 14000));
      const el = reachable[R.pick(weights)];
      const [x, y] = pos(el.box);
      const label = el.desc.aria || el.desc.txt || '';
      const burst = cfg.rage && cfg.rage[0].test(label) && R.chance(cfg.rage[1]) ? 3 + R.int(3) : 1;
      for (let b = 0; b < burst; b++) {
        events.push({ t: 'c', ts: t + b * 180, u: url, p: pv, x: x + R.int(9) - 4, y: y + R.int(9) - 4, dw, dh, vw, vh, el: el.desc, cmp: el.cmp, rage: b >= 2 ? 1 : undefined, dead: el.interactive ? undefined : 1 });
      }
      clicks++;
      t += burst * 180;
      if (el.href) {
        const target = new URL(el.href);
        const internal = target.host === new URL(origin).host;
        const toPath = target.pathname.replace(/\/$/, '') || '/';
        if (internal && toPath === path) continue;
        const spa = el.route;
        events.push({ t: 'n', ts: t + 5, u: url, p: pv, k: spa ? 'pushstate' : 'link', to: target.origin + toPath, x, y, dw, dh, vw, vh });
        if (internal && layouts[toPath]) next = { path: toPath, kind: spa ? 'pushstate' : 'load' };
        else leftSite = true;
        break;
      }
    }
    const dur = Math.round(Math.max(1500, R.normal(9000 + depth * 450 + clicks * 2500, 6000)));
    events.push({ t: 's', ts: t + 400, u: url, p: pv, sd: depth, ms: dur, vw, vh });
    t += Math.round(R.range(600, 2500));
    if (!next && !leftSite && !bounced && !R.chance(P.exit)) {
      const nav = L.elements.filter((e) => e.header && e.href && layouts[new URL(e.href).pathname.replace(/\/$/, '') || '/'] && new URL(e.href).pathname !== path);
      if (nav.length) {
        const el = nav[R.int(nav.length)];
        const [x, y] = pos(el.box);
        const toPath = new URL(el.href).pathname.replace(/\/$/, '') || '/';
        events.push({ t: 'c', ts: t, u: url, p: pv, x, y, dw, dh, vw, vh, el: el.desc, cmp: el.cmp });
        events.push({ t: 'n', ts: t + 5, u: url, p: pv, k: el.route ? 'pushstate' : 'link', to: origin + toPath, x, y, dw, dh, vw, vh });
        next = { path: toPath, kind: el.route ? 'pushstate' : 'load' };
      }
    }
    if (!next) {
      if (!leftSite) events.push({ t: 'n', ts: t, u: url, p: pv, k: 'unload', vw, vh });
      break;
    }
    ref = url;
    kind = next.kind;
    path = next.path;
    t += Math.round(R.range(300, 1800));
  }
  return { sid, events, end: t };
}

function toBatches(site, sess) {
  const out = [];
  for (let i = 0; i < sess.events.length; i += 200) {
    const chunk = sess.events.slice(i, i + 200);
    const ts = chunk[chunk.length - 1].ts + 1000;
    out.push(
      JSON.stringify({
        v: 1,
        site: site.id,
        sid: sess.sid,
        ts,
        e: chunk.map(({ ts: et, ...e }) => ({ ...e, d: et - ts })),
      }),
    );
  }
  return out;
}

async function main() {
  await waitFor(`${API}/healthz`);
  await waitFor(`http://127.0.0.1:${DEMO_PORT}/`);
  const browser = await launch();
  const now = Date.now();
  const startDay = Math.floor(now / 86_400_000) - (DAYS - 1);
  let totalEvents = 0;
  let totalSessions = 0;
  const queue = [];

  for (const site of demoSites) {
    const cfg = CFG[site.id];
    await api('DELETE', `/api/sites/${site.id}`);
    const reg = await api('POST', '/api/sites', { id: site.id, name: site.name, domain: site.host });
    if (reg.status !== 201) throw new Error(`register ${site.id}: ${reg.status} ${reg.text}`);
    const comps = COMPONENTS[site.id] || [];
    for (const c of comps) {
      const res = await api('POST', `/api/sites/${site.id}/components`, c);
      if (res.status !== 201) throw new Error(`component ${site.id}/${c.name}: ${res.status} ${res.text}`);
    }
    const layouts = {};
    for (const path of Object.keys(cfg.pages)) {
      const L = await measure(browser, site, path, comps.length > 0);
      layouts[path] = L;
      const up = await api('PUT', `/api/sites/${site.id}/screenshot?url=${encodeURIComponent(L.url)}`, new Uint8Array(L.shot), { 'Content-Type': 'image/jpeg' });
      if (up.status !== 200) throw new Error(`screenshot ${L.url}: ${up.status} ${up.text}`);
      console.log(`  ${site.id}${path}: ${L.elements.length} elements, doc ${L.docW}x${L.docH}, screenshot ${up.json.width}x${up.json.height} (${Math.round(up.json.bytes / 1024)} KiB)`);
    }
    let n = 0;
    for (let d = 0; d < DAYS; d++) {
      const day = startDay + d;
      const dow = (day + 4) % 7;
      const growth = 0.82 + (0.36 * d) / Math.max(1, DAYS - 1);
      const count = Math.round(cfg.sessions * SCALE * cfg.weekday[dow] * growth * R.range(0.88, 1.12));
      for (let i = 0; i < count; i++) {
        let hour = +R.pick(cfg.hours);
        if (site.id === 'fieldnote-journal' && dow === 0 && R.chance(0.3)) hour = 8 + R.int(2);
        const start = day * 86_400_000 + hour * 3_600_000 + R.int(3_600_000);
        const sess = simulateSession(site, cfg, layouts, start);
        // Future timestamps get clamped by the API, which would reorder the session.
        if (sess.end + 5_000 > now) continue;
        n++;
        totalEvents += sess.events.length;
        for (const b of toBatches(site, sess)) queue.push(b);
      }
    }
    totalSessions += n;
    console.log(`${site.name}: ${n} sessions`);
  }
  await browser.close();

  let sent = 0;
  let rejected = 0;
  const started = Date.now();
  async function worker() {
    while (queue.length) {
      const body = queue.pop();
      const res = await fetch(`${API}/api/events`, { method: 'POST', body, headers: { 'Content-Type': 'text/plain', 'X-Calor-Backfill': '1', Authorization: `Bearer ${KEY}` } });
      const j = await res.json();
      if (res.status === 503) {
        queue.push(body);
        await new Promise((r) => setTimeout(r, 250));
        continue;
      }
      if (res.status !== 202) throw new Error(`ingest ${res.status}: ${JSON.stringify(j)}`);
      sent += j.accepted;
      rejected += j.rejected;
    }
  }
  await Promise.all(Array.from({ length: 8 }, worker));
  await new Promise((r) => setTimeout(r, 1500));
  for (const site of demoSites) {
    const res = await api('GET', `/api/sites/${site.id}/components`);
    for (const c of res.json.components) console.log(`  ${site.id} component "${c.name}": ${c.clicks} clicks, ${c.visitors} visitors`);
  }
  console.log(`seeded ${totalSessions} sessions / ${sent} events (${rejected} rejected) across ${demoSites.length} sites in ${((Date.now() - started) / 1000).toFixed(1)}s`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
