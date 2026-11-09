'use client';

import Link from 'next/link';
import { usePathname, useSearchParams } from 'next/navigation';
import { currentSiteId } from './SiteSwitcher';

export default function SideNav({ fallbackSite, siteIds }: { fallbackSite: string; siteIds: string[] }) {
  const pathname = usePathname();
  const sp = useSearchParams();
  const site = currentSiteId(pathname) || fallbackSite;
  const keep = new URLSearchParams();
  for (const k of ['range', 'from', 'to', 'device']) {
    const v = sp.get(k);
    if (v) keep.set(k, v);
  }
  const q = keep.size ? `?${keep}` : '';
  const base = `/s/${encodeURIComponent(site)}`;
  const cmpQ = new URLSearchParams(keep);
  cmpQ.set('sites', siteIds.slice(0, 3).join(','));
  const items = [
    { href: `${base}${q}`, label: 'Overview', icon: '◎', active: pathname === base },
    { href: `${base}/pages${q}`, label: 'Pages', icon: '☰', active: pathname.startsWith(`${base}/pages`) || pathname.startsWith(`${base}/detail`) },
    { href: `/compare?${cmpQ}`, label: 'Compare sites', icon: '⇄', active: pathname.startsWith('/compare') },
    { href: '/sites', label: 'Sites & snippet', icon: '⚙', active: pathname.startsWith('/sites') },
  ];
  return (
    <nav className="side-nav">
      {items.map((it) =>
        site || it.href.startsWith('/sites') || it.href.startsWith('/compare') ? (
          <Link key={it.label} href={it.href} className={it.active ? 'on' : ''} prefetch={false}>
            <span className="ico">{it.icon}</span>
            {it.label}
          </Link>
        ) : null,
      )}
    </nav>
  );
}
