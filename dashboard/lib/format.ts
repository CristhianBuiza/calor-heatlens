const nf = new Intl.NumberFormat('en-US');

export function num(n: number) {
  return nf.format(Math.round(n));
}

export function compact(n: number) {
  if (n >= 1e6) return `${(n / 1e6).toFixed(1)}M`;
  if (n >= 1e4) return `${Math.round(n / 1e3)}k`;
  if (n >= 1e3) return `${(n / 1e3).toFixed(1)}k`;
  return String(Math.round(n));
}

export function pct(n: number, digits = 1) {
  return `${n.toFixed(digits).replace(/\.0+$/, '')}%`;
}

export function duration(ms: number) {
  if (!ms) return '–';
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  return `${m}m ${String(s % 60).padStart(2, '0')}s`;
}

export function pathOf(url: string) {
  try {
    const u = new URL(url);
    return u.pathname || '/';
  } catch {
    return url;
  }
}

export function hostOf(url: string) {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}

export const DAY_NAMES = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

export function hourLabel(h: number) {
  if (h === 0) return '12a';
  if (h === 12) return '12p';
  return h < 12 ? `${h}a` : `${h - 12}p`;
}

export function shortDate(d: string) {
  const [, m, day] = d.split('-');
  return `${['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'][+m - 1]} ${+day}`;
}

export const SITE_COLORS = ['#6366f1', '#f97316', '#10b981', '#ec4899', '#0ea5e9', '#eab308'];
