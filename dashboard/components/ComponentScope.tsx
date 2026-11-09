import Link from 'next/link';
import ComponentFilter from '@/components/ComponentFilter';
import { componentsFor } from '@/lib/components';
import { filterQuery, type Filters } from '@/lib/filters';

/** Component filter control plus, when a component is selected, what it scopes. */
export async function ComponentScope({ site, f }: { site: string; f: Filters }) {
  const { components } = await componentsFor(site, f);
  return <ComponentFilter components={components.map((c) => ({ id: c.id, name: c.name }))} />;
}

export async function ScopeBanner({ site, f, path, keep = {} }: { site: string; f: Filters; path: string; keep?: Record<string, string> }) {
  if (!f.component) return null;
  const { components } = await componentsFor(site, f);
  const c = components.find((x) => x.id === f.component);
  return (
    <div className="scope-banner" id="scope-banner">
      <span className="scope-dot" />
      <div>
        Showing clicks inside <b>{c ? c.name : f.component}</b>
        {c && <code>{c.browserSelector}</code>}. Clicks, heatmaps, elements and time of day are scoped to the component; pageviews, scroll depth and sessions stay page-wide.
      </div>
      {c && (
        <Link className="btn small" href={`/s/${encodeURIComponent(site)}/components/${c.id}${filterQuery(f, { component: '' })}`}>
          Component report
        </Link>
      )}
      <Link className="btn small" href={`${path}${filterQuery(f, { ...keep, component: '' })}`}>
        Clear
      </Link>
    </div>
  );
}
