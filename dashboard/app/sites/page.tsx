import Link from 'next/link';
import CopyButton from '@/components/CopyButton';
import { Card, ErrorNote, safe, Tag } from '@/components/ui';
import { apiGet, PUBLIC_API_URL } from '@/lib/api';
import { one } from '@/lib/filters';
import { num } from '@/lib/format';
import type { Site } from '@/lib/types';
import { createSite } from './actions';

function snippet(site: Site) {
  return `<script src="${PUBLIC_API_URL}/tracker/calor.min.js"
  data-site-id="${site.id}"
  data-endpoint="${PUBLIC_API_URL}/api/events"
  defer></script>`;
}

export default async function Sites(props: PageProps<'/sites'>) {
  const sp = await props.searchParams;
  const [sites, err] = await safe(apiGet<Site[]>('/api/sites'));
  const created = one(sp.created);
  const error = one(sp.error);
  return (
    <>
      <div className="page-head">
        <div>
          <div className="eyebrow">Settings</div>
          <h1>Sites &amp; tracking snippet</h1>
          <p className="muted">Each site has its own ID. Events are only accepted when the page and Origin match the registered domain (or a subdomain).</p>
        </div>
      </div>
      <div className="grid-2">
        <Card title="Register a site" subtitle="Leave the ID empty to generate one">
          <form action={createSite} className="form">
            <label>
              Name
              <input name="name" required placeholder="Acme Coffee Roasters" maxLength={100} />
            </label>
            <label>
              Domain
              <input name="domain" required placeholder="acme.com" />
              <small className="muted">Subdomains are included; www. is ignored.</small>
            </label>
            <label>
              Site ID (optional)
              <input name="id" placeholder="acme-coffee" pattern="[a-z0-9][a-z0-9_-]{2,63}" />
            </label>
            <button className="btn primary" type="submit">Register site</button>
            {error && <p className="error">{error}</p>}
          </form>
        </Card>
        <Card title="How it works">
          <ol className="steps">
            <li>Register the site and copy its snippet.</li>
            <li>Paste the snippet before <code>&lt;/head&gt;</code> on every page.</li>
            <li>Clicks, navigations, scroll depth and time on page arrive in batches (sendBeacon).</li>
            <li>Upload a full-page screenshot on a page&apos;s detail view to use as the heatmap background.</li>
          </ol>
        </Card>
      </div>
      {err || !sites ? (
        <ErrorNote error={err} />
      ) : (
        <div className="site-list" id="site-list">
          {sites.map((s) => (
            <Card
              key={s.id}
              id={`site-${s.id}`}
              className={s.id === created ? 'highlight' : ''}
              title={
                <>
                  {s.name} {s.id === created && <Tag tone="blue">new</Tag>}
                </>
              }
              subtitle={
                <>
                  <code>{s.domain}</code> · ID <code>{s.id}</code> · {num(s.pageviews || 0)} pageviews
                  {s.lastEventAt ? ` · last event ${new Date(s.lastEventAt).toISOString().slice(0, 16).replace('T', ' ')} UTC` : ' · no events yet'}
                </>
              }
              actions={
                <>
                  <Link className="btn small" href={`/s/${s.id}`}>Open dashboard</Link>
                  <CopyButton text={snippet(s)} label="Copy snippet" />
                </>
              }
            >
              <pre className="snippet">{snippet(s)}</pre>
            </Card>
          ))}
        </div>
      )}
    </>
  );
}
