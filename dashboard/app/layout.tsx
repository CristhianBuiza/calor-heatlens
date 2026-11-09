import type { Metadata } from 'next';
import { cookies } from 'next/headers';
import { Suspense } from 'react';
import FilterBar from '@/components/FilterBar';
import SideNav from '@/components/SideNav';
import SiteSwitcher from '@/components/SiteSwitcher';
import { apiGet } from '@/lib/api';
import type { Site } from '@/lib/types';
import './globals.css';

export const metadata: Metadata = { title: 'Calor · Heatmap analytics', description: 'Self-hosted click heatmaps and page analytics' };

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  let sites: Site[] = [];
  let apiDown = false;
  try {
    sites = await apiGet<Site[]>('/api/sites');
  } catch {
    apiDown = true;
  }
  const last = (await cookies()).get('calor_site')?.value;
  const fallback = sites.find((s) => s.id === last)?.id || sites[0]?.id || '';
  return (
    <html lang="en">
      <body>
        <div className="app">
          <aside className="side">
            <a href="/" className="brand">
              <span className="brand-mark" />
              Calor
            </a>
            <Suspense>
              <SideNav fallbackSite={fallback} siteIds={sites.map((s) => s.id)} />
            </Suspense>
            <div className="side-foot">
              {apiDown ? <span className="tag red">API unreachable</span> : <span className="muted">{sites.length} sites tracked</span>}
            </div>
          </aside>
          <main className="main">
            <div className="topbar">
              <Suspense>
                <SiteSwitcher sites={sites} />
                <FilterBar />
              </Suspense>
            </div>
            <div className="content">{children}</div>
          </main>
        </div>
      </body>
    </html>
  );
}
