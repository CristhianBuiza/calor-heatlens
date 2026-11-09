'use server';

import { revalidatePath } from 'next/cache';
import { redirect } from 'next/navigation';
import { apiSend, ApiError } from '@/lib/api';
import type { Site } from '@/lib/types';

export async function createSite(formData: FormData) {
  const name = String(formData.get('name') || '').trim();
  const domain = String(formData.get('domain') || '').trim();
  const id = String(formData.get('id') || '').trim();
  let site: Site;
  try {
    site = await apiSend<Site>('POST', '/api/sites', { name, domain, ...(id ? { id } : {}) });
  } catch (e) {
    const msg = e instanceof ApiError ? e.message : 'failed to register site';
    redirect(`/sites?error=${encodeURIComponent(msg)}`);
  }
  revalidatePath('/', 'layout');
  redirect(`/sites?created=${encodeURIComponent(site.id)}#site-${site.id}`);
}
