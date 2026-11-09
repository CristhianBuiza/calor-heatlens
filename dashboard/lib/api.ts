import 'server-only';

// Server-side client for the Go API. The admin key never reaches the browser:
// server components call the API directly and client components go through
// the allow-listed proxy in app/api/calor/[...path]/route.ts.

export const API_URL = (process.env.CALOR_API_URL || 'http://127.0.0.1:8080').replace(/\/$/, '');
export const PUBLIC_API_URL = (process.env.CALOR_PUBLIC_API_URL || 'http://localhost:8080').replace(/\/$/, '');
const ADMIN_KEY = process.env.CALOR_ADMIN_KEY || '';

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
  }
}

export function authHeaders(): Record<string, string> {
  return ADMIN_KEY ? { Authorization: `Bearer ${ADMIN_KEY}` } : {};
}

export async function apiGet<T>(path: string, params?: Record<string, string | number | undefined>): Promise<T> {
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(params || {})) if (v !== undefined && v !== '') qs.set(k, String(v));
  const url = `${API_URL}${path}${qs.size ? `?${qs}` : ''}`;
  const res = await fetch(url, { headers: authHeaders(), cache: 'no-store' });
  if (!res.ok) {
    let msg = res.statusText;
    try {
      msg = ((await res.json()) as { error?: string }).error || msg;
    } catch {}
    throw new ApiError(res.status, msg);
  }
  return (await res.json()) as T;
}

export async function apiSend<T>(method: string, path: string, body: unknown): Promise<T> {
  const res = await fetch(`${API_URL}${path}`, {
    method,
    headers: { ...authHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    cache: 'no-store',
  });
  const data = (await res.json().catch(() => ({}))) as T & { error?: string };
  if (!res.ok) throw new ApiError(res.status, data.error || res.statusText);
  return data;
}
