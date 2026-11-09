import Link from 'next/link';
import { BarChart, DeviceSplit, HBarList, HourChart, LineChart, Punchcard, ReachFunnel } from '@/components/charts';
import HeatmapLoader from '@/components/heatmap/HeatmapLoader';
import { Card, ErrorNote, Kpi, safe, Tag } from '@/components/ui';
import { apiGet } from '@/lib/api';
import { apiParams, filterQuery, type Filters } from '@/lib/filters';
import { DAY_NAMES, duration, hostOf, hourLabel, num, pathOf, pct } from '@/lib/format';
import type { Day, DeviceStat, ElementList, EntryExit, Flow, KPIs, PageList, Referrer, ScrollStats, TimeOfDay } from '@/lib/types';

type P = { site: string; f: Filters; url?: string };
const base = (site: string) => `/api/sites/${encodeURIComponent(site)}`;
const detailHref = (site: string, f: Filters, url: string) => `/s/${encodeURIComponent(site)}/detail${filterQuery(f, { url })}`;

export async function KpiSection({ site, f, url }: P) {
  const [k, err] = await safe(apiGet<KPIs>(`${base(site)}/kpis`, apiParams(f, { url })));
  if (err || !k) return <ErrorNote error={err} />;
  return (
    <div className="kpis">
      <Kpi label="Unique visitors" value={num(k.uniqueVisitors)} hint="distinct sessions" />
      <Kpi label="Pageviews" value={num(k.pageviews)} hint={url ? `${num(k.entries)} entries` : `${k.pagesPerSession} pages / session`} />
      <Kpi label="Clicks" value={num(k.clicks)} hint={`${k.clicksPerView} per pageview`} />
      <Kpi label="Avg scroll depth" value={pct(k.avgScrollDepth)} hint="max depth per pageview" />
      <Kpi label="Avg time on page" value={duration(k.avgTimeOnPageMs)} hint="visible time" />
      <Kpi label="Bounce rate" value={pct(k.bounceRate)} hint={`${num(k.bounces)} single-page sessions`} tone={k.bounceRate > 60 ? 'warn' : undefined} />
      <Kpi label={url ? 'Exit rate' : 'Navigations'} value={url ? pct(k.exitRate) : num(k.navigations)} hint={url ? `${num(k.exits)} exits` : 'link + SPA transitions'} />
      <Kpi label="Rage clicks" value={num(k.rageClicks)} hint={`${num(k.deadClicks)} dead clicks`} tone={k.rageClicks > 0 ? 'warn' : undefined} />
    </div>
  );
}

export async function TrendSection({ site, f, url }: P) {
  const [days, err] = await safe(apiGet<Day[]>(`${base(site)}/daily`, apiParams(f, { url })));
  return (
    <Card title="Traffic trend" subtitle={`Daily unique visitors, pageviews and clicks${f.partial ? ' · dashed = today so far' : ''}`} className="span-2">
      {err || !days ? (
        <ErrorNote error={err} />
      ) : (
        <>
          <div className="legend inline">
            <div><i style={{ background: '#6366f1' }} />Visitors</div>
            <div><i style={{ background: '#0ea5e9' }} />Pageviews</div>
            <div><i style={{ background: '#f97316' }} />Clicks</div>
          </div>
          <LineChart
            partialLast={f.partial}
            labels={days.map((d) => d.date)}
            series={[
              { name: 'Visitors', color: '#6366f1', values: days.map((d) => d.visitors) },
              { name: 'Pageviews', color: '#0ea5e9', values: days.map((d) => d.pageviews) },
              { name: 'Clicks', color: '#f97316', values: days.map((d) => d.clicks) },
            ]}
          />
        </>
      )}
    </Card>
  );
}

export async function TimeOfDaySection({ site, f, url }: P) {
  const [t, err] = await safe(apiGet<TimeOfDay>(`${base(site)}/time-of-day`, apiParams(f, { url })));
  const tzLabel = `UTC${f.tz >= 0 ? '+' : '−'}${Math.floor(Math.abs(f.tz) / 60)}${f.tz % 60 ? `:${String(Math.abs(f.tz) % 60).padStart(2, '0')}` : ''}`;
  return (
    <Card id="time-of-day" title="Clicks by time of day" subtitle={t ? `Peak at ${hourLabel(t.peak.hour)} · busiest day ${DAY_NAMES[t.peak.day]} · ${tzLabel}` : tzLabel} className="span-2">
      {err || !t ? (
        <ErrorNote error={err} />
      ) : (
        <div className="split">
          <div>
            <h3 className="mini">Hour of day</h3>
            <HourChart hours={t.hours} peak={t.peak.hour} />
            <h3 className="mini">Day of week</h3>
            <BarChart values={[1, 2, 3, 4, 5, 6, 0].map((d) => t.days[d])} labels={[1, 2, 3, 4, 5, 6, 0].map((d) => DAY_NAMES[d])} height={140} color="#0ea5e9" highlight={[1, 2, 3, 4, 5, 6, 0].indexOf(t.peak.day)} />
          </div>
          <div>
            <h3 className="mini">Weekday × hour</h3>
            <Punchcard matrix={t.matrix} />
          </div>
        </div>
      )}
    </Card>
  );
}

export async function ScrollSection({ site, f, url }: P) {
  const [s, err] = await safe(apiGet<ScrollStats>(`${base(site)}/scroll`, apiParams(f, { url })));
  return (
    <Card id="scroll-depth" title="Scroll depth" subtitle={s ? `${num(s.pageviews)} pageviews · average max depth ${pct(s.avgDepth)} · avg visible time ${duration(s.avgTimeMs)}` : undefined}>
      {err || !s ? (
        <ErrorNote error={err} />
      ) : (
        <>
          <h3 className="mini">Share of pageviews reaching</h3>
          <ReachFunnel reach={s.reach} />
          <h3 className="mini">Where visitors stopped scrolling</h3>
          <BarChart values={s.buckets} labels={s.buckets.map((_, i) => `${i * 10}–${i * 10 + 10}%`)} format={(n) => pct(n, 0)} height={150} color="#10b981" />
        </>
      )}
    </Card>
  );
}

export async function DevicesSection({ site, f, url }: P) {
  const [d, err] = await safe(apiGet<DeviceStat[]>(`${base(site)}/devices`, apiParams(f, { url })));
  return (
    <Card title="Devices" subtitle="Pageviews by viewport width (mobile < 768px ≤ tablet < 1024px ≤ desktop)">
      {err || !d ? <ErrorNote error={err} /> : <DeviceSplit devices={d} />}
    </Card>
  );
}

export async function EntryExitSection({ site, f }: P) {
  const [e, err] = await safe(apiGet<EntryExit>(`${base(site)}/entry-exit`, apiParams(f, { limit: 8 })));
  if (err || !e) return <ErrorNote error={err} />;
  const rows = (list: EntryExit['entries']) => list.map((p) => ({ key: p.url, label: <code>{pathOf(p.url)}</code>, value: p.count, share: p.share, href: detailHref(site, f, p.url) }));
  return (
    <>
      <Card title="Top entry pages" subtitle="First page of each session">
        <HBarList rows={rows(e.entries)} valueLabel="sessions" />
      </Card>
      <Card title="Top exit pages" subtitle="Last page of each session">
        <HBarList rows={rows(e.exits)} valueLabel="sessions" />
      </Card>
      <Card title="Top exit points" subtitle="Last element clicked before leaving the site">
        <HBarList
          valueLabel="sessions"
          rows={e.exitPoints.map((p, i) => ({
            key: `${p.pageUrl}${p.selector}${i}`,
            label: p.selector ? <b>{p.label}</b> : <span className="muted">{p.label}</span>,
            sub: (
              <>
                <code>{pathOf(p.pageUrl)}</code> {p.tag && <Tag>{`<${p.tag}>`}</Tag>}
              </>
            ),
            value: p.count,
            share: p.share,
          }))}
        />
      </Card>
    </>
  );
}

export async function ReferrersSection({ site, f }: P) {
  const [r, err] = await safe(apiGet<Referrer[]>(`${base(site)}/referrers`, apiParams(f, { limit: 8 })));
  return (
    <Card title="Top referrers" subtitle="Where landing visits came from">
      {err || !r ? <ErrorNote error={err} /> : <HBarList rows={r.map((x) => ({ key: x.source, label: x.source, value: x.count, share: x.share }))} valueLabel="visits" />}
    </Card>
  );
}

export async function ElementsSection({ site, f, url, limit = 10 }: P & { limit?: number }) {
  const [el, err] = await safe(apiGet<ElementList>(`${base(site)}/elements`, apiParams(f, { url, limit })));
  return (
    <Card id="top-elements" title="Most-clicked elements" subtitle={el ? `${num(el.totalClicks)} clicks${url ? ' on this page' : ' across the site'} · labels from aria-label, text or id` : undefined} className="span-2">
      {err || !el ? (
        <ErrorNote error={err} />
      ) : el.rows.length === 0 ? (
        <p className="empty">No clicks in this range.</p>
      ) : (
        <table className="table">
          <thead>
            <tr>
              <th>#</th>
              <th>Element</th>
              {!url && <th>Page</th>}
              <th className="num">Clicks</th>
              <th className="num">Visitors</th>
              <th className="num">Share</th>
              <th>Signals</th>
            </tr>
          </thead>
          <tbody>
            {el.rows.map((r, i) => (
              <tr key={`${r.pageUrl}${r.selector}${r.text}${i}`}>
                <td className="muted">{i + 1}</td>
                <td>
                  <div className="el-label">
                    <Tag tone="blue">{r.tag}</Tag> <b>{r.label}</b>
                  </div>
                  <code className="selector">{r.selector}</code>
                </td>
                {!url && (
                  <td>
                    <Link href={detailHref(site, f, r.pageUrl)} prefetch={false}>
                      <code>{pathOf(r.pageUrl)}</code>
                    </Link>
                  </td>
                )}
                <td className="num">
                  <b>{num(r.clicks)}</b>
                  <div className="bar-cell">
                    <i style={{ width: `${(r.clicks / el.rows[0].clicks) * 100}%` }} />
                  </div>
                </td>
                <td className="num">{num(r.visitors)}</td>
                <td className="num">{pct(r.share)}</td>
                <td>
                  {r.rageClicks > 0 && <Tag tone="red">{`${r.rageClicks} rage`}</Tag>} {r.deadClicks > 0 && <Tag tone="amber">{`${r.deadClicks} dead`}</Tag>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Card>
  );
}

const COLUMNS: [string, string][] = [
  ['url', 'Page'],
  ['clicks', 'Clicks'],
  ['uniqueVisitors', 'Unique visitors'],
  ['pageviews', 'Pageviews'],
  ['avgScrollDepth', 'Avg scroll'],
  ['avgTimeMs', 'Avg time'],
  ['entries', 'Entries'],
  ['exits', 'Exits'],
  ['exitRate', 'Exit rate'],
  ['bounceRate', 'Bounce'],
  ['rageClicks', 'Rage'],
];

export async function PagesTableSection({ site, f, sort = 'clicks', order = 'desc', offset = 0, limit = 25, q = '', path, paginate = true }: P & { sort?: string; order?: string; offset?: number; limit?: number; q?: string; path: string; paginate?: boolean }) {
  const [list, err] = await safe(apiGet<PageList>(`${base(site)}/pages`, apiParams(f, { sort, order, offset, limit, q: q || undefined })));
  if (err || !list) return <ErrorNote error={err} />;
  const href = (extra: Record<string, string | undefined>) => `${path}${filterQuery(f, { sort: list.sort, order: list.order, q: q || undefined, ...extra })}`;
  const cell = (r: PageList['rows'][number], k: string) => {
    switch (k) {
      case 'avgScrollDepth':
        return (
          <span className="scroll-cell">
            <i style={{ width: `${r.avgScrollDepth}%` }} />
            {pct(r.avgScrollDepth, 0)}
          </span>
        );
      case 'avgTimeMs':
        return duration(r.avgTimeMs);
      case 'exitRate':
        return pct(r[k], 0);
      case 'bounceRate':
        return r.entries ? pct(r[k], 0) : <span className="muted" title="No sessions started on this page">—</span>;
      default:
        return num(r[k as keyof typeof r] as number);
    }
  };
  return (
    <div className="table-wrap" id="page-table">
      <table className="table sortable">
        <thead>
          <tr>
            {COLUMNS.map(([k, label]) => {
              const active = list.sort === k;
              const nextOrder = active ? (list.order === 'desc' ? 'asc' : 'desc') : k === 'url' ? 'asc' : 'desc';
              return (
                <th key={k} className={k === 'url' ? '' : 'num'}>
                  <Link href={href({ sort: k, order: nextOrder, offset: undefined })} className={active ? 'sorted' : ''} prefetch={false} scroll={false}>
                    {label}
                    {active ? (list.order === 'desc' ? ' ↓' : ' ↑') : ''}
                  </Link>
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody>
          {list.rows.map((r) => (
            <tr key={r.url}>
              <td>
                <Link href={detailHref(site, f, r.url)} prefetch={false} className="page-link">
                  <code>{r.path}</code>
                </Link>
                {r.hasScreenshot && <Tag>screenshot</Tag>}
              </td>
              {COLUMNS.slice(1).map(([k]) => (
                <td key={k} className="num">{cell(r, k)}</td>
              ))}
            </tr>
          ))}
          {!list.rows.length && (
            <tr>
              <td colSpan={COLUMNS.length} className="empty">No pages match.</td>
            </tr>
          )}
        </tbody>
      </table>
      {paginate && list.total > list.limit && (
        <div className="pager">
          <span className="muted">
            {list.offset + 1}–{Math.min(list.total, list.offset + list.limit)} of {list.total} pages
          </span>
          {list.offset > 0 && <Link href={href({ offset: String(Math.max(0, list.offset - list.limit)) })} className="btn small">← Prev</Link>}
          {list.offset + list.limit < list.total && <Link href={href({ offset: String(list.offset + list.limit) })} className="btn small">Next →</Link>}
        </div>
      )}
      {!paginate && list.total > list.rows.length && (
        <div className="pager">
          <Link href={`/s/${encodeURIComponent(site)}/pages${filterQuery(f)}`} className="btn small">All {list.total} pages →</Link>
        </div>
      )}
    </div>
  );
}

export async function FlowSection({ site, f, url }: P) {
  const [fl, err] = await safe(apiGet<Flow>(`${base(site)}/flow`, apiParams(f, { url, limit: 8 })));
  if (err || !fl) return <ErrorNote error={err} />;
  const kind = (k: string, ext: boolean) => (
    <Tag tone={ext ? 'amber' : k === 'link' ? 'blue' : 'gray'}>{ext ? 'external' : k === 'load' ? 'page load' : k}</Tag>
  );
  const label = (u: string) => (u.startsWith('http') && hostOf(u) === hostOf(url || '') ? <code>{pathOf(u)}</code> : <span>{u.replace(/^https?:\/\//, '')}</span>);
  return (
    <>
      <Card title="Arrived from" subtitle={`${num(fl.pageviews)} pageviews by referrer / previous route`}>
        <HBarList rows={fl.inbound.map((e, i) => ({ key: e.url + e.kind + i, label: label(e.url), sub: kind(e.kind, e.external), value: e.count, share: e.share }))} />
      </Card>
      <Card title="Went to" subtitle={`Link clicks and SPA route changes · ${num(fl.unloads)} left by closing, back or typed URL`}>
        <HBarList rows={fl.outbound.map((e, i) => ({ key: e.url + e.kind + i, label: label(e.url), sub: kind(e.kind, e.external), value: e.count, share: e.share, href: e.external ? undefined : detailHref(site, f, e.url) }))} />
      </Card>
      <Card title="Exit points" subtitle="Last click on this page when it ended the session">
        <HBarList rows={fl.exitPoints.map((p, i) => ({ key: p.selector + i, label: p.selector ? <b>{p.label}</b> : <span className="muted">{p.label}</span>, sub: p.selector ? <code className="selector">{p.selector}</code> : undefined, value: p.count, share: p.share }))} />
      </Card>
    </>
  );
}

export async function HeatmapSection({ site, f, url }: P & { url: string }) {
  const [meta, err] = await safe(apiGet<{ screenshot: { width: number; height: number; updatedAt: number } | null }>(`${base(site)}/screenshot/meta`, { url }));
  const [el] = await safe(apiGet<ElementList>(`${base(site)}/elements`, apiParams(f, { url, limit: 10 })));
  if (err) return <ErrorNote error={err} />;
  const query: Record<string, string> = { from: f.from, to: f.to, tz: String(f.tz) };
  if (f.device) query.device = f.device;
  return (
    <HeatmapLoader
      site={site}
      url={url}
      query={query}
      screenshot={meta?.screenshot || null}
      pins={(el?.rows || []).map((r) => ({ label: r.label, relX: r.relX, relY: r.relY, clicks: r.clicks }))}
    />
  );
}
