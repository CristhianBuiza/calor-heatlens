// Zero-dependency demo server: serves the three demo sites by Host header
// (acme.localhost, nimbus.localhost, fieldnote.localhost; Chrome resolves
// *.localhost to loopback) plus /calor.min.js from ../dist.
import { createServer } from 'node:http';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import { render, sites } from './sites.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const port = +(process.env.DEMO_PORT || 5500);
const endpoint = process.env.CALOR_ENDPOINT || 'http://localhost:8080/api/events';

createServer((req, res) => {
  const host = (req.headers.host || '').split(':')[0];
  const path = new URL(req.url, 'http://x').pathname.replace(/\/+$/, '') || '/';
  if (path === '/calor.min.js' || path === '/calor.js') {
    try {
      res.writeHead(200, { 'content-type': 'text/javascript', 'cache-control': 'no-cache' });
      return res.end(readFileSync(join(here, '..', 'dist', path.slice(1))));
    } catch {
      res.writeHead(404);
      return res.end('run `npm run build` in tracker/ first');
    }
  }
  const html = render(host, path, endpoint);
  if (html) {
    res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' });
    return res.end(html);
  }
  if (!sites.some((s) => s.host === host)) {
    res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' });
    return res.end(`<h1>Calor demo</h1><ul>${sites.map((s) => `<li><a href="http://${s.host}:${port}/">${s.name}</a> (site id <code>${s.id}</code>)</li>`).join('')}</ul>`);
  }
  res.writeHead(404, { 'content-type': 'text/html' });
  res.end('<h1>Not found</h1><a href="/">Home</a>');
}).listen(port, () => {
  console.log(`demo sites on :${port} -> ${sites.map((s) => `http://${s.host}:${port}/`).join('  ')}`);
  console.log(`tracker endpoint: ${endpoint}`);
});
