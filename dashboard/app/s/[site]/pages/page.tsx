import { PagesTableSection } from '@/components/sections';
import { ComponentScope, ScopeBanner } from '@/components/ComponentScope';
import { Card, Lazy } from '@/components/ui';
import { one, readFilters } from '@/lib/filters';
import { getSite } from '@/lib/site';

export default async function Pages(props: PageProps<'/s/[site]/pages'>) {
  const { site: id } = await props.params;
  const sp = await props.searchParams;
  const f = await readFilters(sp);
  const site = await getSite(decodeURIComponent(id));
  const q = one(sp.q);
  const path = `/s/${site.id}/pages`;
  return (
    <>
      <div className="page-head">
        <div>
          <div className="eyebrow">Pages · {site.domain}</div>
          <h1>Per-page breakdown</h1>
          <p className="muted">Clicks, unique visitors, scroll depth, entries and exits for every tracked page. Sorted and paginated by the API.</p>
        </div>
        <form className="search" action={path}>
          {['range', 'from', 'to', 'device', 'component'].map((k) => (one(sp[k]) ? <input key={k} type="hidden" name={k} value={one(sp[k])} /> : null))}
          <input name="q" defaultValue={q} placeholder="Filter by path…" aria-label="Filter pages" />
        </form>
        <ComponentScope site={site.id} f={f} />
      </div>
      <ScopeBanner site={site.id} f={f} path={path} />
      <Card>
        <Lazy height={600}>
          <PagesTableSection site={site.id} f={f} sort={one(sp.sort) || 'clicks'} order={one(sp.order) || 'desc'} offset={+one(sp.offset) || 0} limit={25} q={q} path={path} />
        </Lazy>
      </Card>
    </>
  );
}
