import Link from 'next/link';
import { notFound } from 'next/navigation';
import { HBarList } from '@/components/charts';
import { DevicesSection, ElementsSection, HeatmapSection, TimeOfDaySection, TrendSection } from '@/components/sections';
import { Card, ErrorNote, Kpi, Lazy, safe, Tag } from '@/components/ui';
import { apiGet, ApiError } from '@/lib/api';
import { componentsPath } from '@/lib/components';
import { apiParams, filterQuery, one, readFilters } from '@/lib/filters';
import { num, pathOf, pct } from '@/lib/format';
import { getSite } from '@/lib/site';
import type { Component, ComponentStats } from '@/lib/types';

export default async function ComponentReport(props: PageProps<'/s/[site]/components/[id]'>) {
  const { site: sid, id } = await props.params;
  const sp = await props.searchParams;
  const f = { ...(await readFilters(sp)), component: '' };
  const site = await getSite(decodeURIComponent(sid));
  const [res, err] = await safe(apiGet<{ component: Component; stats: ComponentStats }>(`${componentsPath(site.id)}/${encodeURIComponent(id)}/stats`, apiParams(f)));
  if (err instanceof ApiError && err.status === 404) notFound();
  if (err || !res) return <ErrorNote error={err} />;
  const { component: c, stats: s } = res;
  const scoped = { ...f, component: c.id };
  const page = one(sp.page) || s.pages[0]?.url || '';
  const base = `/s/${site.id}/components`;
  return (
    <>
      <div className="page-head">
        <div>
          <div className="eyebrow">
            <Link href={`${base}${filterQuery(f)}`}>← Components</Link> · {site.name}
          </div>
          <h1>{c.name}</h1>
          <p className="muted rule-line">
            <Tag tone={c.selector ? 'blue' : 'amber'}>{c.selector ? 'CSS selector' : 'data attribute'}</Tag> <code>{c.selector || c.browserSelector}</code>
            {c.selector && c.attrName ? (
              <>
                {' '}
                or <code>{c.browserSelector.split(', ').pop()}</code>
              </>
            ) : null}{' '}
            · {c.pagePattern ? <>pages <code>{c.pagePattern}</code></> : 'all pages'} · {f.from} → {f.to}
            {f.device ? ` · ${f.device}` : ''}
          </p>
        </div>
        <div className="card-actions">
          <Link className="btn" href={`/s/${site.id}${filterQuery(scoped)}`}>
            Filter overview
          </Link>
          <Link className="btn primary" href={`${base}/${c.id}/edit${filterQuery(f)}`}>
            Edit
          </Link>
        </div>
      </div>
      {!c.evaluable && <p className="note">This rule uses an attribute the tracker does not record, so only clicks matched in visitors&apos; browsers (after the component was created) are counted.</p>}
      <div className="kpis" id="component-kpis">
        <Kpi label="Clicks" value={num(s.clicks)} hint={`${pct(s.clickShare)} of all ${num(s.siteClicks)} clicks`} />
        <Kpi label="Visitors" value={num(s.visitors)} hint="sessions with a click inside" />
        <Kpi label="Click rate" value={pct(s.clickRate)} hint={`${num(s.clickedPageviews)} of ${num(s.exposedPageviews)} pageviews${s.exposureSource === 'pattern' ? ' on matching pages' : ' on pages where it was clicked'}`} />
        <Kpi label="Rage clicks" value={num(s.rageClicks)} hint={`${num(s.deadClicks)} dead clicks`} tone={s.rageClicks > 0 ? 'warn' : undefined} />
      </div>
      {s.clicks === 0 ? (
        <Card>
          <p className="empty">No clicks inside this component in the selected range.</p>
        </Card>
      ) : (
        <>
          <div className="grid-2">
            <Card title="Pages" subtitle="Where the component was clicked">
              <HBarList valueLabel="clicks" rows={s.pages.slice(0, 10).map((p) => ({ key: p.url, label: <code>{pathOf(p.url)}</code>, value: p.count, share: p.share, href: `${base}/${c.id}${filterQuery(f, { page: p.url })}#heatmap` }))} />
            </Card>
            <Lazy height={200}>
              <DevicesSection site={site.id} f={scoped} />
            </Lazy>
          </div>
          {page && (
            <Card
              id="heatmap"
              title="Heatmap of clicks inside the component"
              subtitle={
                <>
                  <code>{pathOf(page)}</code> ·{' '}
                  <Link href={`/s/${site.id}/detail${filterQuery(scoped, { url: page })}`}>open page detail with this filter</Link>
                </>
              }
              actions={
                <div className="chips-row">
                  {s.pages.slice(0, 5).map((p) => (
                    <Link key={p.url} className={`chip ${p.url === page ? 'on' : ''}`} href={`${base}/${c.id}${filterQuery(f, { page: p.url })}#heatmap`} scroll={false}>
                      {pathOf(p.url)}
                    </Link>
                  ))}
                </div>
              }
            >
              <Lazy height={560}>
                <HeatmapSection site={site.id} f={scoped} url={page} />
              </Lazy>
            </Card>
          )}
          <div className="grid-2">
            <Lazy height={420}>
              <ElementsSection site={site.id} f={scoped} limit={10} />
            </Lazy>
            <Lazy height={300}>
              <TrendSection site={site.id} f={scoped} />
            </Lazy>
            <Lazy height={360}>
              <TimeOfDaySection site={site.id} f={scoped} />
            </Lazy>
          </div>
        </>
      )}
    </>
  );
}
