import { NextRequest } from 'next/server';
import { API_URL, authHeaders } from '@/lib/api';

// Browser-facing proxy for the few API calls client components make. Only
// allow-listed, site-scoped read paths (plus screenshot upload) are forwarded,
// and the admin key is added server-side.
const READ = /^sites\/[A-Za-z0-9_-]{1,64}\/(heatmap|elements|screenshot|screenshot\/meta)$/;
const WRITE = /^sites\/[A-Za-z0-9_-]{1,64}\/screenshot$/;

type Ctx = { params: Promise<{ path: string[] }> };

async function forward(req: NextRequest, ctx: Ctx, allow: RegExp, init: RequestInit = {}) {
  const path = (await ctx.params).path.join('/');
  if (!allow.test(path)) return Response.json({ error: 'not allowed' }, { status: 404 });
  const headers: Record<string, string> = { ...authHeaders() };
  const inm = req.headers.get('if-none-match');
  if (inm) headers['If-None-Match'] = inm;
  const res = await fetch(`${API_URL}/api/${path}${req.nextUrl.search}`, { ...init, headers: { ...headers, ...(init.headers as Record<string, string>) }, cache: 'no-store' });
  const out = new Headers();
  for (const h of ['content-type', 'etag', 'cache-control', 'x-cache']) {
    const v = res.headers.get(h);
    if (v) out.set(h, v);
  }
  return new Response(res.body, { status: res.status, headers: out });
}

export async function GET(req: NextRequest, ctx: Ctx) {
  return forward(req, ctx, READ);
}

export async function PUT(req: NextRequest, ctx: Ctx) {
  const body = await req.arrayBuffer();
  if (body.byteLength > 8 << 20) return Response.json({ error: 'screenshot larger than 8 MiB' }, { status: 413 });
  return forward(req, ctx, WRITE, { method: 'PUT', body, headers: { 'Content-Type': req.headers.get('content-type') || 'application/octet-stream' } });
}
