import 'server-only';
import { notFound } from 'next/navigation';
import { cache } from 'react';
import { apiGet, ApiError } from './api';
import type { Site } from './types';

export const getSite = cache(async (id: string): Promise<Site> => {
  try {
    return await apiGet<Site>(`/api/sites/${encodeURIComponent(id)}`);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) notFound();
    throw e;
  }
});
