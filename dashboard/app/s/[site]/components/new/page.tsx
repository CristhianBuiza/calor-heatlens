import Link from 'next/link';
import ComponentEditor from '@/components/ComponentEditor';
import { editorPages } from '@/lib/components';
import { filterQuery, one, readFilters } from '@/lib/filters';
import { getSite } from '@/lib/site';
import { saveComponent } from '../actions';

export default async function NewComponent(props: PageProps<'/s/[site]/components/new'>) {
  const { site: id } = await props.params;
  const sp = await props.searchParams;
  const f = { ...(await readFilters(sp)), component: '' };
  const site = await getSite(decodeURIComponent(id));
  const attr = one(sp.attr);
  const pages = await editorPages(site.id, f);
  return (
    <>
      <div className="page-head">
        <div>
          <div className="eyebrow">
            <Link href={`/s/${site.id}/components${filterQuery(f)}`}>← Components</Link> · {site.name}
          </div>
          <h1>New component</h1>
        </div>
      </div>
      <ComponentEditor
        site={site.id}
        isNew
        initial={{ name: attr ? attr.replace(/[-_]+/g, ' ').replace(/^./, (c) => c.toUpperCase()) : '', selector: '', attrName: attr ? 'data-calor-component' : '', attrValue: attr, pagePattern: '' }}
        pages={pages}
        query={{ from: f.from, to: f.to, tz: String(f.tz), ...(f.device ? { device: f.device } : {}) }}
        action={saveComponent.bind(null, site.id, null)}
      />
    </>
  );
}
