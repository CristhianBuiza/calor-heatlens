import Link from 'next/link';
import ConfirmSubmit from '@/components/ConfirmSubmit';
import { notFound } from 'next/navigation';
import ComponentEditor from '@/components/ComponentEditor';
import { ErrorNote, safe } from '@/components/ui';
import { apiGet, ApiError } from '@/lib/api';
import { componentsPath, editorPages } from '@/lib/components';
import { filterQuery, readFilters } from '@/lib/filters';
import { getSite } from '@/lib/site';
import type { Component } from '@/lib/types';
import { deleteComponent, saveComponent } from '../../actions';

export default async function EditComponent(props: PageProps<'/s/[site]/components/[id]/edit'>) {
  const { site: sid, id } = await props.params;
  const f = { ...(await readFilters(await props.searchParams)), component: '' };
  const site = await getSite(decodeURIComponent(sid));
  const [c, err] = await safe(apiGet<Component>(`${componentsPath(site.id)}/${encodeURIComponent(id)}`, { from: f.from, to: f.to, tz: f.tz }));
  if (err instanceof ApiError && err.status === 404) notFound();
  if (err || !c) return <ErrorNote error={err} />;
  const pages = await editorPages(site.id, f);
  return (
    <>
      <div className="page-head">
        <div>
          <div className="eyebrow">
            <Link href={`/s/${site.id}/components/${c.id}${filterQuery(f)}`}>← {c.name}</Link> · {site.name}
          </div>
          <h1>Edit component</h1>
          <p className="muted">Changing the rule re-tags recorded clicks.</p>
        </div>
        <form action={deleteComponent.bind(null, site.id, c.id)}>
          <ConfirmSubmit className="btn danger" message={`Delete ${c.name}? Its tags on recorded clicks are removed too.`}>
                        Delete component
                      </ConfirmSubmit>
        </form>
      </div>
      <ComponentEditor
        site={site.id}
        isNew={false}
        initial={{ name: c.name, selector: c.selector, attrName: c.attrName, attrValue: c.attrValue, pagePattern: c.pagePattern }}
        pages={pages}
        query={{ from: f.from, to: f.to, tz: String(f.tz), ...(f.device ? { device: f.device } : {}) }}
        action={saveComponent.bind(null, site.id, c.id)}
      />
    </>
  );
}
