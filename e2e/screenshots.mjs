// Captures the dashboard views used in the README/report at 1440x900.
//   SHOTS_DIR=output node screenshots.mjs
import { mkdirSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { DASHBOARD, DEMO_PORT, launch, waitFor } from './lib.mjs';

const OUT = resolve(process.env.SHOTS_DIR || 'output');
mkdirSync(OUT, { recursive: true });
const ACME = 'acme-coffee';
const NIMBUS = 'nimbus-analytics';
const FIELD = 'fieldnote-journal';
const acmeHome = `http://acme.localhost:${DEMO_PORT}/`;
const portugal = `http://fieldnote.localhost:${DEMO_PORT}/posts/slow-travel-in-portugal`;

async function settle(page) {
  await page.waitForNetworkIdle({ idleTime: 400, timeout: 30000 }).catch(() => {});
  await page.waitForFunction(() => !document.querySelector('.skeleton') && !document.querySelector('.hm-loading'), { timeout: 30000 });
  await new Promise((r) => setTimeout(r, 300));
}

async function open(page, path) {
  await page.goto(DASHBOARD + path, { waitUntil: 'networkidle0' });
  await settle(page);
}

async function element(page, selector, file, pad = 12) {
  const el = await page.$(selector);
  if (!el) throw new Error(`missing ${selector}`);
  const box = await el.boundingBox();
  await page.screenshot({
    path: join(OUT, file),
    clip: { x: Math.max(0, box.x - pad), y: Math.max(0, box.y - pad), width: box.width + pad * 2, height: box.height + pad * 2 },
    captureBeyondViewport: true,
  });
  console.log('saved', file);
}

async function main() {
  await waitFor(`${DASHBOARD}/sites`);
  const browser = await launch();
  const page = await browser.newPage();
  // Pin the dashboard timezone so hour-of-day charts match the seeded (UTC) schedule.
  await page.setCookie({ name: 'calor_tz', value: '0', url: DASHBOARD });
  await page.emulateTimezone('UTC');

  await open(page, `/s/${ACME}`);
  await page.screenshot({ path: join(OUT, 'dashboard-overview.png'), fullPage: true });
  console.log('saved dashboard-overview.png');
  await element(page, '#top-elements', 'dashboard-top-elements.png');

  await open(page, `/s/${NIMBUS}`);
  await element(page, '#time-of-day', 'dashboard-time-of-day.png');

  await open(page, `/s/${ACME}/pages?sort=clicks&order=desc`);
  await page.screenshot({ path: join(OUT, 'dashboard-page-table.png'), fullPage: true });
  console.log('saved dashboard-page-table.png');

  await open(page, `/s/${ACME}/detail?url=${encodeURIComponent(acmeHome)}&device=desktop`);
  await page.waitForSelector('.hm-stage img');
  await page.waitForFunction(() => [...document.querySelectorAll('.hm-stage img')].every((i) => i.complete && i.naturalWidth > 0));
  await new Promise((r) => setTimeout(r, 500));
  await element(page, '#heatmap', 'dashboard-heatmap.png');

  await open(page, `/s/${FIELD}/detail?url=${encodeURIComponent(portugal)}`);
  await element(page, '#scroll-depth', 'dashboard-scroll-depth.png');

  await open(page, `/s/${ACME}`);
  await page.click('.switcher-btn');
  await page.waitForSelector('.switcher-menu');
  await page.hover('.switcher-item:nth-of-type(2)');
  await page.screenshot({ path: join(OUT, 'dashboard-site-switcher.png'), clip: { x: 0, y: 0, width: 1440, height: 560 } });
  console.log('saved dashboard-site-switcher.png');

  await open(page, `/compare?sites=${ACME},${NIMBUS},${FIELD}`);
  await page.screenshot({ path: join(OUT, 'dashboard-site-compare.png'), fullPage: true });
  console.log('saved dashboard-site-compare.png');

  await browser.close();
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
