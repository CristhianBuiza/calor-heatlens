// End-to-end check of component tagging in the dashboard (needs a seeded
// stack: make start + make seed). Picks an element in the live-page picker,
// checks the live match preview, saves, verifies the component report and the
// component-scoped heatmap, registers a discovered data attribute, then
// removes what it created. Screenshots go to SHOTS_DIR (default output/).
import { mkdirSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { api, DASHBOARD, DEMO_PORT, launch, waitFor } from './lib.mjs';

const OUT = resolve(process.env.SHOTS_DIR || 'output');
mkdirSync(OUT, { recursive: true });
const SITE = 'acme-coffee';
const HOME = `http://acme.localhost:${DEMO_PORT}/`;
const created = [];

function check(ok, msg) {
  if (!ok) throw new Error(`FAIL: ${msg}`);
  console.log(`ok - ${msg}`);
}

async function settle(page) {
  await page.waitForNetworkIdle({ idleTime: 400, timeout: 30000 }).catch(() => {});
  await page.waitForFunction(() => !document.querySelector('.skeleton') && !document.querySelector('.hm-loading'), { timeout: 30000 });
  await new Promise((r) => setTimeout(r, 300));
}

async function open(page, path) {
  await page.goto(DASHBOARD + path, { waitUntil: 'networkidle0' });
  await settle(page);
}

async function shot(page, file, fromSel, toSel = fromSel, pad = 12) {
  const a = await (await page.$(fromSel)).boundingBox();
  const b = await (await page.$(toSel)).boundingBox();
  const y = Math.max(0, a.y - pad);
  await page.screenshot({ path: join(OUT, file), clip: { x: Math.max(0, Math.min(a.x, b.x) - pad), y, width: Math.max(a.x + a.width, b.x + b.width) - Math.min(a.x, b.x) + pad * 2, height: b.y + b.height - y + pad }, captureBeyondViewport: true });
  console.log('saved', file);
}

async function pickerFrame(page, url) {
  await page.select('.picker select', url);
  await page.waitForFunction(() => {
    const f = document.querySelector('.picker-stage iframe');
    return f && f.contentDocument && f.contentDocument.readyState === 'complete' && f.contentDocument.querySelector('.hero');
  }, { timeout: 30000 });
  return page.frames().find((f) => f.url().includes('/api/calor-frame'));
}

async function previewCount(page) {
  await page.waitForFunction(() => document.querySelector('#match-preview .mini-kpis b'), { timeout: 15000 });
  await new Promise((r) => setTimeout(r, 900));
  return page.$eval('#match-preview .mini-kpis b', (b) => Number(b.textContent.replace(/[^\d]/g, '')));
}

async function main() {
  await waitFor(`${DASHBOARD}/sites`);
  const before = await api('GET', `/api/sites/${SITE}/components`);
  check(before.status === 200 && before.json.components.length >= 5, `seeded components present (${before.json.components.length})`);
  const browser = await launch();
  const page = await browser.newPage();
  await page.setViewport({ width: 1440, height: 1000 });
  await page.setCookie({ name: 'calor_tz', value: '0', url: DASHBOARD });

  try {
    // Manage view: list + discovered attribute values.
    await open(page, `/s/${SITE}/components`);
    const rows = await page.$$eval('#components-table tbody tr', (trs) => trs.length);
    check(rows === before.json.components.length, `list shows ${rows} components`);
    const discovered = await page.$$eval('#discovered tbody tr', (trs) => trs.map((tr) => tr.innerText));
    check(discovered.some((t) => t.includes('cart-summary') && t.includes('Register')), 'unregistered data-calor-component value is offered for registration');
    await page.screenshot({ path: join(OUT, 'tagging-manage.png'), fullPage: true });
    console.log('saved tagging-manage.png');

    // Picker: hover + click inside the sanitized live page, choose an ancestor as the root.
    await open(page, `/s/${SITE}/components/new`);
    const frame = await pickerFrame(page, HOME);
    check(!!frame, 'live page loads in the picker frame');
    check((await frame.$$('script')).length === 0, 'picker frame has no scripts');
    await frame.hover('.hero .ctas a.btn.ghost');
    await frame.click('.hero .ctas a.btn.ghost');
    await page.waitForSelector('.pick-result .chip');
    const chips = await page.$$eval('.pick-result .chips-row:first-child .chip', (cs) => cs.map((c) => c.textContent.trim()));
    check(chips[0].startsWith('a.btn') && chips.includes('div.ctas'), `ancestry chips: ${chips.join(' / ')}`);
    await page.evaluate(() => [...document.querySelectorAll('.pick-result .chips-row:first-child .chip')].find((c) => c.textContent.trim() === 'div.ctas').click());
    await page.waitForFunction(() => document.querySelector('input[name=selector]').value === 'div.ctas');
    const n = await previewCount(page);
    check(n > 100, `live preview counts ${n} recorded clicks for div.ctas`);
    const onPage = await page.$eval('#picker-count', (p) => p.textContent);
    check(/^1 element/.test(onPage), `in-page match count: ${onPage}`);
    await page.type('input[name=pagePattern]', '/');
    await page.type('input[name=name]', 'Hero buttons (e2e)');
    await new Promise((r) => setTimeout(r, 800));
    await shot(page, 'tagging-picker.png', '.editor-main', '.picker');
    await Promise.all([page.waitForNavigation({ waitUntil: 'networkidle0' }), page.click('.editor button[type=submit]')]);
    const id = page.url().match(/components\/(cmp_[a-z0-9]+)/)?.[1];
    check(!!id, `saved and redirected to the component report (${id})`);
    created.push(id);
    await settle(page);
    const kpi = await page.$eval('#component-kpis .kpi-value', (v) => Number(v.textContent.replace(/[^\d]/g, '')));
    check(kpi === n, `report shows ${kpi} clicks, same as the preview`);
    check(!!(await page.$('#heatmap canvas')), 'component report embeds a scoped heatmap');

    // Component-scoped heatmap on the page detail view.
    await open(page, `/s/${SITE}/detail?url=${encodeURIComponent(HOME)}&component=${id}&device=desktop`);
    await page.waitForFunction(() => [...document.querySelectorAll('.hm-stage img')].every((i) => i.complete && i.naturalWidth > 0));
    await new Promise((r) => setTimeout(r, 600));
    const meta = await page.$eval('.hm-meta', (m) => m.textContent);
    check(/served from raw/.test(meta), `scoped heatmap bypasses the rollup (${meta.trim().slice(0, 60)}…)`);
    const banner = await page.$eval('#scope-banner', (b) => b.textContent);
    check(banner.includes('Hero buttons (e2e)'), 'scope banner names the component');
    const scoped = await api('GET', `/api/sites/${SITE}/heatmap?url=${encodeURIComponent(HOME)}&component=${id}`);
    const all = await api('GET', `/api/sites/${SITE}/heatmap?url=${encodeURIComponent(HOME)}`);
    check(scoped.json.total > 0 && scoped.json.total < all.json.total, `scoped heatmap ${scoped.json.total} of ${all.json.total} clicks`);
    await shot(page, 'tagging-scoped-heatmap.png', '.page-head', '#heatmap');

    // Register a discovered attribute value from the list.
    await open(page, `/s/${SITE}/components`);
    await Promise.all([page.waitForNavigation({ waitUntil: 'networkidle0' }), page.evaluate(() => [...document.querySelectorAll('#discovered tr')].find((tr) => tr.innerText.includes('cart-summary')).querySelector('a.btn').click())]);
    check((await page.$eval('input[name=attrValue]', (i) => i.value)) === 'cart-summary', 'register link pre-fills attribute mode');
    const nAttr = await previewCount(page);
    check(nAttr > 0, `attribute preview counts ${nAttr} clicks`);
    await Promise.all([page.waitForNavigation({ waitUntil: 'networkidle0' }), page.click('.editor button[type=submit]')]);
    const id2 = page.url().match(/components\/(cmp_[a-z0-9]+)/)?.[1];
    check(!!id2, 'attribute component saved');
    created.push(id2);

    // Validation errors come back from the API.
    await open(page, `/s/${SITE}/components/new`);
    await page.type('input[name=name]', 'bad');
    await page.type('input[name=selector]', 'a:hover');
    await page.waitForFunction(() => /pseudo-class/.test(document.querySelector('#match-preview')?.textContent || ''), { timeout: 10000 });
    check(true, 'invalid selector shows the API validation message in the preview');
  } finally {
    for (const id of created) await api('DELETE', `/api/sites/${SITE}/components/${id}`);
    await browser.close();
  }
  const after = await api('GET', `/api/sites/${SITE}/components`);
  check(after.json.components.length === before.json.components.length, 'cleaned up created components');
}

main().catch((e) => {
  console.error(e);
  for (const id of created) api('DELETE', `/api/sites/${SITE}/components/${id}`);
  process.exitCode = 1;
});
