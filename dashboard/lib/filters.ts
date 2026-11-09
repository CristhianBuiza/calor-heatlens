import { cookies } from 'next/headers';

export type SearchParams = Record<string, string | string[] | undefined>;

export type Filters = {
  range: string; // 7d | 14d | 30d | 90d | custom
  from: string; // YYYY-MM-DD (inclusive)
  to: string; // YYYY-MM-DD (inclusive)
  device: string;
  tz: number; // minutes east of UTC
  partial: boolean; // range ends today, so the last day is incomplete
  component: string; // component id scoping click metrics, or ""
};

export const RANGES = ['7d', '14d', '30d', '90d'] as const;

export function one(v: string | string[] | undefined): string {
  return Array.isArray(v) ? v[0] || '' : v || '';
}

function ymd(ms: number, tz: number) {
  return new Date(ms + tz * 60_000).toISOString().slice(0, 10);
}

const dateRe = /^\d{4}-\d{2}-\d{2}$/;

export async function readFilters(sp: SearchParams): Promise<Filters> {
  const tzCookie = Number((await cookies()).get('calor_tz')?.value);
  const tz = Number.isFinite(tzCookie) && Math.abs(tzCookie) <= 14 * 60 ? Math.round(tzCookie) : 0;
  const device = ['mobile', 'tablet', 'desktop'].includes(one(sp.device)) ? one(sp.device) : '';
  let from = one(sp.from);
  let to = one(sp.to);
  let range = one(sp.range);
  if (dateRe.test(from) && dateRe.test(to) && from <= to) {
    range = 'custom';
  } else {
    if (!(RANGES as readonly string[]).includes(range)) range = '30d';
    const days = parseInt(range, 10);
    const now = Date.now();
    to = ymd(now, tz);
    from = ymd(now - (days - 1) * 86_400_000, tz);
  }
  const component = /^cmp_[a-z0-9]{1,40}$/.test(one(sp.component)) ? one(sp.component) : '';
  return { range, from, to, device, tz, partial: to === ymd(Date.now(), tz), component };
}

/** Query parameters understood by the Go API. Dates align to local midnight,
 * so the API can answer heatmaps from its hourly rollup table. */
export function apiParams(f: Filters, extra: Record<string, string | number | undefined> = {}) {
  return { from: f.from, to: f.to, tz: f.tz, device: f.device || undefined, component: f.component || undefined, ...extra };
}

/** Preserve the current filter in links; an extra value of '' drops that key. */
export function filterQuery(f: Filters, extra: Record<string, string | undefined> = {}) {
  const q = new URLSearchParams();
  if (f.range === 'custom') {
    q.set('from', f.from);
    q.set('to', f.to);
  } else if (f.range !== '30d') q.set('range', f.range);
  if (f.device) q.set('device', f.device);
  if (f.component) q.set('component', f.component);
  for (const [k, v] of Object.entries(extra)) {
    if (v) q.set(k, v);
    else if (v === '') q.delete(k);
  }
  const s = q.toString();
  return s ? `?${s}` : '';
}
