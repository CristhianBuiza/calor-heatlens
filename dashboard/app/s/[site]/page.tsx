import Link from 'next/link';
import { DevicesSection, ElementsSection, EntryExitSection, KpiSection, PagesTableSection, ReferrersSection, ScrollSection, TimeOfDaySection, TrendSection } from '@/components/sections';
import { Card, Lazy } from '@/components/ui';
import { filterQuery, readFilters } from '@/lib/filters';
import { getSite } from '@/lib/site';

export default async function Overview(props: PageProps<'/s/[site]'>) {
  const { site: id } = await props.params;
  const f = await readFilters(await props.searchParams);
  const site = await getSite(decodeURIComponent(id));
  const p = { site: site.id, f };
  return (
    <>
      <div className="page-head">
        <div>
          <div className="eyebrow">Overview · {site.domain}</div>
          <h1>{site.name}</h1>
          <p className="muted">
            {f.from} → {f.to}
            {f.device ? ` · ${f.device} only` : ''}
          </p>
        </div>
      </div>
      <div id="overview-kpis">
        <Lazy height={190}>
          <KpiSection {...p} />
        </Lazy>
      </div>
      <div className="grid-2">
        <Lazy height={300}>
          <TrendSection {...p} />
        </Lazy>
        <Card title="Pages" subtitle="Top pages by clicks · click a column to sort" className="span-2" actions={<Link className="btn small" href={`/s/${site.id}/pages${filterQuery(f)}`}>Full table →</Link>}>
          <Lazy height={320}>
            <PagesTableSection {...p} limit={8} path={`/s/${site.id}`} paginate={false} />
          </Lazy>
        </Card>
        <Lazy height={360}>
          <TimeOfDaySection {...p} />
        </Lazy>
        <Lazy height={360}>
          <ScrollSection {...p} />
        </Lazy>
        <Lazy height={200}>
          <DevicesSection {...p} />
        </Lazy>
        <Lazy height={300}>
          <EntryExitSection {...p} />
        </Lazy>
        <Lazy height={260}>
          <ReferrersSection {...p} />
        </Lazy>
        <Lazy height={420}>
          <ElementsSection {...p} limit={10} />
        </Lazy>
      </div>
    </>
  );
}
