// Resource report for a running stack: tracker size, dashboard JS per route
// (scripts the browser actually loads, sized from the build output), and the
// RSS of the API and dashboard processes (plus the summed memory of a local
// PostgreSQL server, if one runs on this machine) before/after loading every route.
//   node measure.mjs  -> prints a summary, writes output/measure.json
import { execSync } from 'node:child_process';
import { mkdirSync, readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { brotliCompressSync, constants, gzipSync } from 'node:zlib';
import { DASHBOARD, DEMO_PORT, launch, waitFor } from './lib.mjs';

const root = new URL('..', import.meta.url).pathname;
const kb = (n) => `${(n / 1024).toFixed(1)} KiB`;
const sizes = (buf) => ({ raw: buf.length, gzip: gzipSync(buf, { level: 9 }).length, brotli: brotliCompressSync(buf, { params: { [constants.BROTLI_PARAM_QUALITY]: 11 } }).length });

function pid(pattern, exact) {
  try {
    return Number(execSync(`pgrep ${exact ? '-x' : '-f'} '${pattern}'`).toString().trim().split('\n')[0]);
  } catch {
    return 0;
  }
}
function mem(p) {
  if (!p) return null;
  const s = readFileSync(`/proc/${p}/status`, 'utf8');
  const get = (k) => Math.round(Number(s.match(new RegExp(`${k}:\\s+(\\d+)`))[1]) / 1024);
  return { rssMiB: get('VmRSS'), peakMiB: get('VmHWM') };
}
// Sum over every local postgres process. PSS shares shared_buffers out
// between the processes mapping them; it needs the postgres user or root,
// otherwise RSS (which counts shared memory once per process) is summed.
function pgMem() {
  let pss = 0, rss = 0, pssOk = true, n = 0;
  for (const d of readdirSync('/proc')) {
    if (!/^\d+$/.test(d)) continue;
    try {
      if (readFileSync(`/proc/${d}/comm`, 'utf8').trim() !== 'postgres') continue;
      rss += Number(readFileSync(`/proc/${d}/status`, 'utf8').match(/VmRSS:\s+(\d+)/)[1]);
      n++;
      try {
        pss += Number(readFileSync(`/proc/${d}/smaps_rollup`, 'utf8').match(/Pss:\s+(\d+)/)[1]);
      } catch {
        pssOk = false;
      }
    } catch {}
  }
  if (!n) return null;
  return pssOk ? { pssMiB: Math.round(pss / 1024), processes: n } : { rssSumMiB: Math.round(rss / 1024), processes: n };
}
function walk(dir, out = []) {
  for (const f of readdirSync(dir)) {
    const p = join(dir, f);
    if (statSync(p).isDirectory()) walk(p, out);
    else out.push(p);
  }
  return out;
}

async function main() {
  await waitFor(`${DASHBOARD}/sites`);
  const apiPid = pid('calor-api', true);
  const dashPid = pid('next-server');
  const report = { at: new Date().toISOString() };

  report.tracker = { 'calor.min.js': sizes(readFileSync(join(root, 'tracker/dist/calor.min.js'))), 'calor.js': sizes(readFileSync(join(root, 'tracker/dist/calor.js'))) };

  const staticDir = join(root, 'dashboard/.next/static');
  const files = walk(staticDir);
  const sum = (ext) => files.filter((f) => f.endsWith(ext)).reduce((a, f) => {
    const s = sizes(readFileSync(f));
    return { files: a.files + 1, raw: a.raw + s.raw, gzip: a.gzip + s.gzip };
  }, { files: 0, raw: 0, gzip: 0 });
  report.dashboardStatic = { js: sum('.js'), css: sum('.css') };
  report.standaloneBytes = Number(execSync(`du -sb ${join(root, 'dashboard/.next/standalone')}`).toString().split('\t')[0]);

  report.memBefore = { api: mem(apiPid), dashboard: mem(dashPid), postgres: pgMem() };
  const home = encodeURIComponent(`http://acme.localhost:${DEMO_PORT}/`);
  const routes = {
    overview: '/s/acme-coffee',
    pages: '/s/acme-coffee/pages',
    'page detail + heatmap': `/s/acme-coffee/detail?url=${home}`,
    compare: '/compare?sites=acme-coffee,nimbus-analytics,fieldnote-journal',
    sites: '/sites',
  };
  const browser = await launch();
  report.routes = {};
  for (let round = 0; round < 3; round++) {
    for (const [name, path] of Object.entries(routes)) {
      const page = await browser.newPage();
      const scripts = new Set();
      page.on('response', (r) => {
        const u = new URL(r.url());
        if (u.pathname.startsWith('/_next/static/') && u.pathname.endsWith('.js')) scripts.add(u.pathname.slice('/_next/static/'.length));
      });
      await page.goto(DASHBOARD + path, { waitUntil: 'networkidle0' });
      if (name.includes('heatmap')) await page.waitForSelector('.hm-canvas, canvas', { timeout: 15000 }).catch(() => {});
      await page.close();
      if (round) continue;
      const js = [...scripts].map((s) => sizes(readFileSync(join(staticDir, decodeURIComponent(s)))));
      report.routes[name] = { scripts: js.length, raw: js.reduce((a, s) => a + s.raw, 0), gzip: js.reduce((a, s) => a + s.gzip, 0) };
    }
  }
  await browser.close();
  report.memAfter = { api: mem(apiPid), dashboard: mem(dashPid), postgres: pgMem() };

  mkdirSync('output', { recursive: true });
  writeFileSync('output/measure.json', JSON.stringify(report, null, 2));
  const t = report.tracker['calor.min.js'];
  console.log(`tracker calor.min.js: ${t.raw} B raw, ${t.gzip} B gzip, ${t.brotli} B brotli`);
  console.log(`dashboard static JS: ${report.dashboardStatic.js.files} files, ${kb(report.dashboardStatic.js.raw)} raw / ${kb(report.dashboardStatic.js.gzip)} gzip; CSS ${kb(report.dashboardStatic.css.raw)} / ${kb(report.dashboardStatic.css.gzip)} gzip`);
  console.log(`standalone server bundle: ${(report.standaloneBytes / 1048576).toFixed(1)} MiB`);
  for (const [n, r] of Object.entries(report.routes)) console.log(`  ${n.padEnd(22)} ${r.scripts} scripts  ${kb(r.raw)} raw  ${kb(r.gzip)} gzip`);
  console.log('RSS before:', JSON.stringify(report.memBefore), ' after 3x all routes:', JSON.stringify(report.memAfter));
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
