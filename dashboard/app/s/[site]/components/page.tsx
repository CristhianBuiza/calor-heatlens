import Link from 'next/link';
import ConfirmSubmit from '@/components/ConfirmSubmit';
import { Card, ErrorNote, safe, Tag } from '@/components/ui';
import { apiGet } from '@/lib/api';
import { componentsPath } from '@/lib/components';
import { apiParams, filterQuery, one, readFilters } from '@/lib/filters';
import { num } from '@/lib/format';
import { getSite } from '@/lib/site';
import type { ComponentList, DiscoveredAttrs } from '@/lib/types';
import { deleteComponent } from './actions';

export default async function Components(props: PageProps<'/s/[site]/components'>) {
  const { site: id } = await props.params;
  const sp = await props.searchParams;
  const f = { ...(await readFilters(sp)), component: '' };
  const site = await getSite(decodeURIComponent(id));
  const base = `/s/${site.id}/components`;
  const [list, err] = await safe(apiGet<ComponentList>(componentsPath(site.id), apiParams(f)));
  const [found] = await safe(apiGet<DiscoveredAttrs>(`${componentsPath(site.id)}/discovered`, apiParams(f, { limit: 20 })));
  const max = Math.max(1, ...(list?.components || []).map((c) => c.clicks));
  return (
    <>
      <div className="page-head">
        <div>
          <div className="eyebrow">Components · {site.domain}</div>
          <h1>Tagged components</h1>
          <p className="muted">
            Name parts of your pages with a CSS selector or a <code>data-calor-component</code> attribute, then scope heatmaps and click stats to them.
          </p>
        </div>
        <Link className="btn primary" href={`${base}/new${filterQuery(f)}`} id="new-component">
          + New component
        </Link>
      </div>
      {one(sp.deleted) && <p className="note">Component deleted.</p>}
      <Card title="Components" subtitle={list ? `${list.components.length} of ${list.limit} · clicks and visitors for ${f.from} → ${f.to}` : undefined}>
        {err || !list ? (
          <ErrorNote error={err} />
        ) : list.components.length === 0 ? (
          <p className="empty">
            No components yet. <Link href={`${base}/new${filterQuery(f)}`}>Create one</Link> by picking an element on a page, typing a selector, or registering a <code>data-calor-component</code> value found below.
          </p>
        ) : (
          <table className="table" id="components-table">
            <thead>
              <tr>
                <th>Component</th>
                <th>Rule</th>
                <th>Pages</th>
                <th className="num">Clicks</th>
                <th className="num">Visitors</th>
                <th>Last click</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {list.components.map((c) => (
                <tr key={c.id}>
                  <td>
                    <Link href={`${base}/${c.id}${filterQuery(f)}`}>
                      <b>{c.name}</b>
                    </Link>
                  </td>
                  <td>
                    <Tag tone={c.selector ? 'blue' : 'amber'}>{c.selector ? 'selector' : 'attribute'}</Tag> <code className="selector inline">{c.selector || c.browserSelector}</code>
                    {!c.evaluable && <Tag>browser-only</Tag>}
                  </td>
                  <td>{c.pagePattern ? <code>{c.pagePattern}</code> : <span className="muted">all pages</span>}</td>
                  <td className="num">
                    <b>{num(c.clicks)}</b>
                    <div className="bar-cell">
                      <i style={{ width: `${(c.clicks / max) * 100}%` }} />
                    </div>
                  </td>
                  <td className="num">{num(c.visitors)}</td>
                  <td className="muted">{c.lastSeen ? new Date(c.lastSeen).toISOString().slice(0, 16).replace('T', ' ') : '—'}</td>
                  <td className="row-actions">
                    <Link className="btn small" href={`/s/${site.id}${filterQuery(f, { component: c.id })}`} title="Overview scoped to this component">
                      Filter
                    </Link>
                    <Link className="btn small" href={`${base}/${c.id}/edit${filterQuery(f)}`}>
                      Edit
                    </Link>
                    <form action={deleteComponent.bind(null, site.id, c.id)}>
                      <ConfirmSubmit className="btn small danger" message={`Delete ${c.name}? Its tags on recorded clicks are removed too.`}>
                        Delete
                      </ConfirmSubmit>
                    </form>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Card>
      <Card
        id="discovered"
        title={
          <>
            Discovered in markup <code>data-calor-component</code>
          </>
        }
        subtitle="Values the tracker found on clicked elements (closest ancestor). Register one to name it and use it as a filter."
      >
        {!found || found.values.length === 0 ? (
          <p className="empty">
            No tagged clicks yet. Add <code>data-calor-component=&quot;name&quot;</code> to an element in your markup; the tracker records it on every click inside.
          </p>
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>Value</th>
                <th className="num">Clicks</th>
                <th className="num">Pages</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {found.values.map((v) => {
                const reg = list?.components.find((c) => c.id === v.componentId);
                return (
                  <tr key={v.value}>
                    <td>
                      <code>{v.value}</code>
                    </td>
                    <td className="num">{num(v.clicks)}</td>
                    <td className="num">{num(v.pages)}</td>
                    <td className="row-actions">
                      {reg ? (
                        <Link href={`${base}/${reg.id}${filterQuery(f)}`}>
                          <Tag tone="blue">{`registered · ${reg.name}`}</Tag>
                        </Link>
                      ) : (
                        <Link className="btn small" href={`${base}/new${filterQuery(f, { attr: v.value })}`}>
                          Register
                        </Link>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </Card>
    </>
  );
}
