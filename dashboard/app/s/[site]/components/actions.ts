'use server';

import { revalidatePath } from 'next/cache';
import { redirect } from 'next/navigation';
import { apiSend, ApiError } from '@/lib/api';
import type { Component } from '@/lib/types';

export type SaveState = { error?: string };

const text = (fd: FormData, k: string) => String(fd.get(k) || '').trim();

export async function saveComponent(siteId: string, id: string | null, _prev: SaveState, fd: FormData): Promise<SaveState> {
  const attribute = text(fd, 'mode') === 'attribute';
  const body = {
    name: text(fd, 'name'),
    selector: attribute ? '' : text(fd, 'selector'),
    attrName: attribute ? text(fd, 'attrName') || 'data-calor-component' : '',
    attrValue: attribute ? text(fd, 'attrValue') : '',
    pagePattern: text(fd, 'pagePattern'),
  };
  const base = `/api/sites/${encodeURIComponent(siteId)}/components`;
  let saved: Component;
  try {
    const res = await apiSend<{ component: Component }>(id ? 'PATCH' : 'POST', id ? `${base}/${encodeURIComponent(id)}` : base, body);
    saved = res.component;
  } catch (e) {
    return { error: e instanceof ApiError ? e.message : 'Could not save the component' };
  }
  revalidatePath(`/s/${siteId}`, 'layout');
  redirect(`/s/${encodeURIComponent(siteId)}/components/${saved.id}`);
}

export async function deleteComponent(siteId: string, id: string) {
  try {
    await apiSend('DELETE', `/api/sites/${encodeURIComponent(siteId)}/components/${encodeURIComponent(id)}`, {});
  } catch (e) {
    if (!(e instanceof ApiError && e.status === 404)) throw e;
  }
  revalidatePath(`/s/${siteId}`, 'layout');
  redirect(`/s/${encodeURIComponent(siteId)}/components?deleted=1`);
}
