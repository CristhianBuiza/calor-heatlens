import Link from 'next/link';
import { redirect } from 'next/navigation';
import { DevicesSection, ElementsSection, FlowSection, HeatmapSection, KpiSection, ScrollSection, TimeOfDaySection, TrendSection } from '@/components/sections';
import { ComponentScope, ScopeBanner } from '@/components/ComponentScope';
import { Card, Lazy } from '@/components/ui';
import { filterQuery, one, readFilters } from '@/lib/filters';
import { pathOf } from '@/lib/format';
import { getSite } from '@/lib/site';

export default async function PageDetail(props: PageProps<'/s/[site]/detail'>) {
  const { site: id } = await props.params;
  const sp = await props.searchParams;
  const f = await readFilters(sp);
  const site = await getSite(decodeURIComponent(id));
  const url = one(sp.url);
  if (!url) redirect(`/s/${site.id}/pages${filterQuery(f)}`);
  const p = { site: site.id, f, url };
  return (
    <>
      <div className="page-head">
        <div>
          <div className="eyebrow">
            <Link href={`/s/${site.id}/pages${filterQuery(f)}`}>← All pages</Link> · {site.name}
          </div>
          <h1>
            <code>{pathOf(url)}</code>
          </h1>
          <p className="muted">
            <a href={url} target="_blank" rel="noreferrer">{url}</a> · {f.from} → {f.to}
            {f.device ? ` · ${f.device}` : ''}
          </p>
        </div>
        <ComponentScope site={site.id} f={f} />
      </div>
      <ScopeBanner site={site.id} f={f} path={`/s/${site.id}/detail`} keep={{ url }} />
      <Lazy height={190}>
        <KpiSection {...p} />
      </Lazy>
      <Card id="heatmap" title={f.component ? 'Component click heatmap' : 'Click heatmap'} subtitle={`Clicks${f.component ? ' inside the selected component' : ''} binned relative to document size, drawn over a full-page screenshot. Numbered pins mark the top elements.`}>
        <Lazy height={560}>
          <HeatmapSection {...p} />
        </Lazy>
      </Card>
      <div className="grid-2">
        <Lazy height={420}>
          <ElementsSection {...p} limit={12} />
        </Lazy>
        <Lazy height={360}>
          <ScrollSection {...p} />
        </Lazy>
        <Lazy height={200}>
          <DevicesSection {...p} />
        </Lazy>
        <Lazy height={360}>
          <TimeOfDaySection {...p} />
        </Lazy>
        <Lazy height={300}>
          <FlowSection {...p} />
        </Lazy>
        <Lazy height={300}>
          <TrendSection {...p} />
        </Lazy>
      </div>
    </>
  );
}
