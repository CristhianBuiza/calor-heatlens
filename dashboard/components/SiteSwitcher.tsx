'use client';

import { usePathname, useRouter, useSearchParams } from 'next/navigation';
import { useEffect, useRef, useState } from 'react';
import { SITE_COLORS } from '@/lib/format';
import type { Site } from '@/lib/types';

export function currentSiteId(pathname: string) {
  const m = pathname.match(/^\/s\/([^/]+)/);
  return m ? decodeURIComponent(m[1]) : '';
}

export default function SiteSwitcher({ sites }: { sites: Site[] }) {
  const pathname = usePathname();
  const sp = useSearchParams();
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const id = currentSiteId(pathname);
  const current = sites.find((s) => s.id === id);
  const color = (sid: string) => SITE_COLORS[Math.max(0, sites.findIndex((s) => s.id === sid)) % SITE_COLORS.length];
  const comparing = pathname.startsWith('/compare');

  useEffect(() => {
    const close = (e: MouseEvent) => ref.current && !ref.current.contains(e.target as Node) && setOpen(false);
    document.addEventListener('mousedown', close);
    return () => document.removeEventListener('mousedown', close);
  }, []);

  function go(site: Site) {
    setOpen(false);
    document.cookie = `calor_site=${encodeURIComponent(site.id)}; path=/; max-age=31536000; samesite=lax`;
    const q = new URLSearchParams(sp.toString());
    q.delete('url');
    const sub = id && pathname.includes('/pages') ? '/pages' : '';
    router.push(`/s/${encodeURIComponent(site.id)}${sub}${q.size ? `?${q}` : ''}`);
  }

  return (
    <div className="switcher" ref={ref}>
      <button className="switcher-btn" onClick={() => setOpen((o) => !o)} aria-haspopup="listbox" aria-expanded={open}>
        <span className="site-dot" style={{ background: current ? color(current.id) : '#94a3b8' }} />
        <span className="switcher-name">
          <b>{current ? current.name : comparing ? 'Comparing sites' : 'Select a site'}</b>
          <small>{current ? current.domain : `${sites.length} sites registered`}</small>
        </span>
        <span className="chev">▾</span>
      </button>
      {open && (
        <div className="switcher-menu" role="listbox">
          <div className="switcher-title">Switch site</div>
          {sites.map((s) => (
            <button key={s.id} role="option" aria-selected={s.id === id} className={`switcher-item ${s.id === id ? 'on' : ''}`} onClick={() => go(s)}>
              <span className="site-dot" style={{ background: color(s.id) }} />
              <span className="switcher-name">
                <b>{s.name}</b>
                <small>
                  {s.domain} · {new Intl.NumberFormat('en-US').format(s.pageviews || 0)} pageviews
                </small>
              </span>
              {s.id === id && <span className="check">✓</span>}
            </button>
          ))}
          <div className="switcher-foot">
            <a href={`/compare?sites=${sites.map((s) => s.id).join(',')}`}>Compare sites</a>
            <a href="/sites">Manage sites</a>
          </div>
        </div>
      )}
    </div>
  );
}
