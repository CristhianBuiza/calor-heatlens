import Link from 'next/link';
import { DeviceSplit, LineChart, ReachFunnel } from '@/components/charts';
import { Card, ErrorNote, safe } from '@/components/ui';
import { apiGet } from '@/lib/api';
import { apiParams, filterQuery, one, readFilters } from '@/lib/filters';
import { duration, hourLabel, num, pct, SITE_COLORS } from '@/lib/format';
import type { CompareSite, KPIs, Site } from '@/lib/types';

const METRICS: { key: keyof KPIs; label: string; fmt: (n: number) => string; lowerIsBetter?: boolean }[] = [
  { key: 'uniqueVisitors', label: 'Unique visitors', fmt: num },
  { key: 'pageviews', label: 'Pageviews', fmt: num },
  { key: 'clicks', label: 'Clicks', fmt: num },
  { key: 'clicksPerView', label: 'Clicks / pageview', fmt: (n) => n.toFixed(1) },
  { key: 'pagesPerSession', label: 'Pages / session', fmt: (n) => n.toFixed(1) },
  { key: 'avgScrollDepth', label: 'Avg scroll depth', fmt: (n) => pct(n) },
  { key: 'avgTimeOnPageMs', label: 'Avg time on page', fmt: duration },
  { key: 'bounceRate', label: 'Bounce rate', fmt: (n) => pct(n), lowerIsBetter: true },
  { key: 'rageClicks', label: 'Rage clicks', fmt: num, lowerIsBetter: true },
  { key: 'deadClicks', label: 'Dead clicks', fmt: num, lowerIsBetter: true },
];

export default async function Compare(props: PageProps<'/compare'>) {
  const sp = await props.searchParams;
  const f = await readFilters(sp);
  const [all] = await safe(apiGet<Site[]>('/api/sites'));
  const sites = all || [];
  // Accepts ?sites=a,b and the repeated ?sites=a&sites=b the picker form submits.
  let ids = [...new Set([sp.sites || ''].flat().flatMap((v) => v.split(',')).filter(Boolean))];
  if (ids.length < 2) ids = sites.slice(0, 3).map((s) => s.id);
  const [data, err] = ids.length >= 2 ? await safe(apiGet<CompareSite[]>('/api/compare', apiParams(f, { sites: ids.join(',') }))) : [null, new Error('Register at least two sites to compare.')];
  const keep = ['range', 'from', 'to', 'device'].filter((k) => one(sp[k]));

  return (
    <>
      <div className="page-head">
        <div>
          <div className="eyebrow">Compare</div>
          <h1>Site comparison</h1>
          <p className="muted">
            Side-by-side KPIs and charts for {ids.length} sites · {f.from} → {f.to}
          </p>
        </div>
        <form className="site-picker" action="/compare">
          {keep.map((k) => (
            <input key={k} type="hidden" name={k} value={one(sp[k])} />
          ))}
          {sites.map((s, i) => (
            <label key={s.id} className={ids.includes(s.id) ? 'on' : ''}>
              <input type="checkbox" name="sites" value={s.id} defaultChecked={ids.includes(s.id)} />
              <i style={{ background: SITE_COLORS[i % SITE_COLORS.length] }} />
              {s.name}
            </label>
          ))}
          <button className="btn small primary" type="submit">Compare</button>
        </form>
      </div>
      {err || !data ? (
        <ErrorNote error={err} />
      ) : (
        <CompareBody data={data} sites={sites} f={f} />
      )}
    </>
  );
}

function CompareBody({ data, sites, f }: { data: CompareSite[]; sites: Site[]; f: Awaited<ReturnType<typeof readFilters>> }) {
  const color = (id: string) => SITE_COLORS[Math.max(0, sites.findIndex((s) => s.id === id)) % SITE_COLORS.length];
  const labels = data[0].daily.map((d) => d.date);
  const hourShare = (c: CompareSite) => {
    const t = c.hours.reduce((a, b) => a + b, 0) || 1;
    return c.hours.map((h) => (h * 100) / t);
  };
  return (
    <div id="compare">
      <Card title="KPI comparison" subtitle="Best value per metric highlighted">
        <table className="table compare-table">
          <thead>
            <tr>
              <th>Metric</th>
              {data.map((c) => (
                <th key={c.site.id} className="num">
                  <span className="site-dot" style={{ background: color(c.site.id) }} /> {c.site.name}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {METRICS.map((m) => {
              const vals = data.map((c) => c.kpis[m.key] as number);
              const best = m.lowerIsBetter ? Math.min(...vals) : Math.max(...vals);
              return (
                <tr key={m.key}>
                  <td>{m.label}</td>
                  {data.map((c, i) => (
                    <td key={c.site.id} className={`num ${vals[i] === best && vals.some((v) => v !== best) ? 'best' : ''}`}>{m.fmt(vals[i])}</td>
                  ))}
                </tr>
              );
            })}
          </tbody>
        </table>
      </Card>
      <div className="grid-2">
        <Card title="Daily unique visitors" subtitle={`All selected sites on one axis${f.partial ? ' · dashed = today so far' : ''}`}>
          <div className="legend inline">
            {data.map((c) => (
              <div key={c.site.id}>
                <i style={{ background: color(c.site.id) }} />
                {c.site.name}
              </div>
            ))}
          </div>
          <LineChart labels={labels} area={false} partialLast={f.partial} series={data.map((c) => ({ name: c.site.name, color: color(c.site.id), values: c.daily.map((d) => d.visitors) }))} />
        </Card>
        <Card title="Clicks by hour of day" subtitle="Share of each site's clicks per hour">
          <div className="legend inline">
            {data.map((c) => (
              <div key={c.site.id}>
                <i style={{ background: color(c.site.id) }} />
                {c.site.name}
              </div>
            ))}
          </div>
          <LineChart labels={Array.from({ length: 24 }, (_, h) => hourLabel(h))} area={false} format={(n) => pct(n, 0)} series={data.map((c) => ({ name: c.site.name, color: color(c.site.id), values: hourShare(c) }))} />
        </Card>
      </div>
      <div className="compare-cols" style={{ gridTemplateColumns: `repeat(${data.length}, minmax(0, 1fr))` }}>
        {data.map((c) => (
          <section className="card compare-col" key={c.site.id} style={{ borderTopColor: color(c.site.id) }}>
            <header className="card-head">
              <div>
                <h2>
                  <Link href={`/s/${c.site.id}${filterQuery(f)}`}>{c.site.name}</Link>
                </h2>
                <p className="muted">{c.site.domain}</p>
              </div>
            </header>
            <div className="mini-kpis">
              <div><b>{num(c.kpis.uniqueVisitors)}</b><span>visitors</span></div>
              <div><b>{num(c.kpis.clicks)}</b><span>clicks</span></div>
              <div><b>{pct(c.kpis.avgScrollDepth, 0)}</b><span>avg scroll</span></div>
              <div><b>{pct(c.kpis.bounceRate, 0)}</b><span>bounce</span></div>
            </div>
            <h3 className="mini">Pageviews per day</h3>
            <LineChart labels={labels} height={160} partialLast={f.partial} series={[{ name: 'Pageviews', color: color(c.site.id), values: c.daily.map((d) => d.pageviews) }]} />
            <h3 className="mini">Scroll reach</h3>
            <ReachFunnel reach={c.reach} />
            <h3 className="mini">Devices</h3>
            <DeviceSplit devices={c.devices} />
            <h3 className="mini">Top pages</h3>
            <ol className="top-pages">
              {c.topPages.map((p) => (
                <li key={p.url}>
                  <Link href={`/s/${c.site.id}/detail${filterQuery(f, { url: p.url })}`}>
                    <code>{p.path}</code>
                  </Link>
                  <span className="muted">{num(p.pageviews)} views</span>
                </li>
              ))}
            </ol>
          </section>
        ))}
      </div>
    </div>
  );
}
