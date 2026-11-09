import dns from 'node:dns';
import http from 'node:http';
import https from 'node:https';
import type { LookupFunction } from 'node:net';
import { NextRequest } from 'next/server';
import { apiGet, ApiError } from '@/lib/api';
import type { Site } from '@/lib/types';

// Serves a tracked page from the dashboard's own origin, with scripts and
// inline handlers removed, so the component picker can inspect its DOM
// inside a sandboxed same-origin iframe. Only pages on the site's registered
// domain are fetched, which keeps this from being a general-purpose proxy.

const MAX_BYTES = 3 << 20;

const lookup: LookupFunction = (host, opts, cb) => {
  // *.localhost resolves to loopback (RFC 6761), but not through getaddrinfo on every OS.
  if (host === 'localhost' || host.endsWith('.localhost')) {
    if (opts.all) return (cb as unknown as (e: null, a: { address: string; family: number }[]) => void)(null, [{ address: '127.0.0.1', family: 4 }]);
    return cb(null, '127.0.0.1', 4);
  }
  dns.lookup(host, opts, cb);
};

function domainMatches(host: string, domain: string) {
  host = host.toLowerCase();
  return host === domain || host.endsWith(`.${domain}`);
}

function fetchPage(url: URL): Promise<{ status: number; type: string; body: string }> {
  return new Promise((resolve, reject) => {
    const mod = url.protocol === 'https:' ? https : http;
    const req = mod.get(url, { lookup, timeout: 6000, headers: { 'User-Agent': 'CalorPicker/1.0', Accept: 'text/html' } }, (res) => {
      const chunks: Buffer[] = [];
      let size = 0;
      res.on('data', (c: Buffer) => {
        size += c.length;
        if (size > MAX_BYTES) {
          req.destroy(new Error('page larger than 3 MiB'));
          return;
        }
        chunks.push(c);
      });
      res.on('end', () => resolve({ status: res.statusCode || 0, type: String(res.headers['content-type'] || ''), body: Buffer.concat(chunks).toString('utf8') }));
      res.on('error', reject);
    });
    req.on('timeout', () => req.destroy(new Error('timed out')));
    req.on('error', reject);
  });
}

function sanitize(html: string, base: string) {
  const out = html
    .replace(/<script\b[^>]*>[\s\S]*?<\/script\s*>/gi, '')
    .replace(/<script\b[^>]*\/?>/gi, '')
    .replace(/<(iframe|object|embed|frame|frameset)\b[\s\S]*?(<\/\1\s*>|\/?>)/gi, '')
    .replace(/<meta[^>]+http-equiv[^>]*>/gi, '')
    .replace(/\s(on[a-z]+)\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)/gi, '')
    .replace(/(href|src|action|formaction)\s*=\s*(["']?)\s*javascript:[^"'\s>]*\2/gi, '$1="#"');
  const baseTag = `<base href="${base.replace(/"/g, '&quot;')}">`;
  return /<head[^>]*>/i.test(out) ? out.replace(/<head([^>]*)>/i, `<head$1>${baseTag}`) : baseTag + out;
}

const err = (status: number, msg: string) =>
  new Response(`<!doctype html><meta charset="utf-8"><body style="font:14px system-ui;color:#64748b;padding:24px">${msg.replace(/</g, '&lt;')}</body>`, {
    status,
    headers: { 'Content-Type': 'text/html; charset=utf-8', 'Content-Security-Policy': "default-src 'none'; style-src 'unsafe-inline'" },
  });

export async function GET(req: NextRequest) {
  const siteId = req.nextUrl.searchParams.get('site') || '';
  const raw = req.nextUrl.searchParams.get('url') || '';
  let site: Site;
  try {
    site = await apiGet<Site>(`/api/sites/${encodeURIComponent(siteId)}`);
  } catch (e) {
    return err(e instanceof ApiError && e.status === 404 ? 404 : 502, 'Unknown site.');
  }
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    return err(400, 'Invalid page URL.');
  }
  if (!/^https?:$/.test(url.protocol) || !domainMatches(url.hostname, site.domain)) {
    return err(400, `Only pages on ${site.domain} can be loaded.`);
  }
  try {
    const page = await fetchPage(url);
    if (page.status >= 300 && page.status < 400) return err(502, `The page redirected (HTTP ${page.status}). Pick a page that loads directly.`);
    if (page.status !== 200) return err(502, `The page returned HTTP ${page.status}.`);
    if (!/text\/html|application\/xhtml/i.test(page.type)) return err(502, 'The URL is not an HTML page.');
    return new Response(sanitize(page.body, url.href), {
      headers: {
        'Content-Type': 'text/html; charset=utf-8',
        'Cache-Control': 'private, max-age=60',
        'Content-Security-Policy': "default-src 'none'; script-src 'none'; style-src * 'unsafe-inline'; img-src * data: blob:; font-src * data:; media-src *; frame-ancestors 'self'; form-action 'none'; base-uri *",
        'X-Content-Type-Options': 'nosniff',
      },
    });
  } catch (e) {
    return err(502, `Could not load the page: ${(e as Error).message}`);
  }
}
