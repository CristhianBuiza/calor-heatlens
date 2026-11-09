'use client';

import { usePathname, useRouter, useSearchParams } from 'next/navigation';
import { useEffect, useTransition } from 'react';

const RANGES = [
  ['7d', '7 days'],
  ['14d', '14 days'],
  ['30d', '30 days'],
  ['90d', '90 days'],
];

export default function FilterBar() {
  const pathname = usePathname();
  const sp = useSearchParams();
  const router = useRouter();
  const [pending, start] = useTransition();

  // Hour-of-day charts use the viewer's timezone (read server-side from a cookie).
  useEffect(() => {
    const tz = String(-new Date().getTimezoneOffset());
    const m = document.cookie.match(/(?:^|; )calor_tz=([^;]*)/);
    if (!m || m[1] !== tz) {
      document.cookie = `calor_tz=${tz}; path=/; max-age=31536000; samesite=lax`;
      router.refresh();
    }
  }, [router]);

  if (pathname.startsWith('/sites')) return null;

  const custom = !!(sp.get('from') && sp.get('to'));
  const range = custom ? 'custom' : sp.get('range') || '30d';
  const ymd = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
  const shownTo = custom ? sp.get('to')! : ymd(new Date());
  const shownFrom = custom ? sp.get('from')! : ymd(new Date(Date.now() - (parseInt(range, 10) - 1) * 86_400_000));

  function update(patch: Record<string, string | null>) {
    const q = new URLSearchParams(sp.toString());
    for (const [k, v] of Object.entries(patch)) {
      if (v) q.set(k, v);
      else q.delete(k);
    }
    q.delete('offset');
    start(() => router.push(`${pathname}${q.size ? `?${q}` : ''}`));
  }

  return (
    <div className={`filters ${pending ? 'pending' : ''}`}>
      <div className="seg" role="group" aria-label="Date range">
        {RANGES.map(([v, label]) => (
          <button key={v} className={range === v ? 'on' : ''} onClick={() => update({ range: v === '30d' ? null : v, from: null, to: null })}>
            {label}
          </button>
        ))}
      </div>
      <label className="date">
        <input type="date" value={shownFrom} max={shownTo} onChange={(e) => e.target.value && update({ from: e.target.value, to: shownTo, range: null })} aria-label="From" />
        <span>→</span>
        <input type="date" value={shownTo} min={shownFrom} onChange={(e) => e.target.value && update({ to: e.target.value, from: shownFrom, range: null })} aria-label="To" />
      </label>
      <select value={sp.get('device') || ''} onChange={(e) => update({ device: e.target.value || null })} aria-label="Device">
        <option value="">All devices</option>
        <option value="desktop">Desktop</option>
        <option value="tablet">Tablet</option>
        <option value="mobile">Mobile</option>
      </select>
      {pending && <span className="spinner" aria-label="Loading" />}
    </div>
  );
}
