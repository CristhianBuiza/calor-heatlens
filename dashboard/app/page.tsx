import { cookies } from 'next/headers';
import { redirect } from 'next/navigation';
import { apiGet } from '@/lib/api';
import type { Site } from '@/lib/types';

export default async function Home() {
  let sites: Site[] = [];
  try {
    sites = await apiGet<Site[]>('/api/sites');
  } catch {}
  const last = (await cookies()).get('calor_site')?.value;
  const site = sites.find((s) => s.id === last) || sites[0];
  redirect(site ? `/s/${encodeURIComponent(site.id)}` : '/sites');
}
