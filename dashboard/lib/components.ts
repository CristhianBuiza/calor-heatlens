import 'server-only';
import { cache } from 'react';
import { apiGet } from './api';
import { apiParams, type Filters } from './filters';
import type { ComponentList, PageList } from './types';

export const componentsPath = (site: string) => `/api/sites/${encodeURIComponent(site)}/components`;

/** Components of a site with click counts for the filter's range (empty on error). */
export const getComponents = cache(async (site: string, from = '', to = '', tz = 0, device = ''): Promise<ComponentList> => {
  try {
    return await apiGet<ComponentList>(componentsPath(site), { from, to, tz, device: device || undefined });
  } catch {
    return { components: [], limit: 0 };
  }
});

export const componentsFor = (site: string, f: Filters) => getComponents(site, f.from, f.to, f.tz, f.device);

/** Tracked pages offered in the component picker, busiest first. */
export async function editorPages(site: string, f: Filters, prefer = '') {
  try {
    const list = await apiGet<PageList>(`/api/sites/${encodeURIComponent(site)}/pages`, apiParams({ ...f, component: '' }, { sort: 'clicks', limit: 60 }));
    const rows = list.rows.map((r) => ({ url: r.url, path: r.path, clicks: r.clicks }));
    const i = prefer ? rows.findIndex((r) => r.url === prefer) : -1;
    if (i > 0) rows.unshift(...rows.splice(i, 1));
    return rows;
  } catch {
    return [];
  }
}
