import puppeteer from 'puppeteer-core';

export const API = process.env.CALOR_API || 'http://127.0.0.1:8080';
export const DASHBOARD = process.env.DASHBOARD || 'http://localhost:3000';
export const DEMO_PORT = +(process.env.DEMO_PORT || 5500);
export const KEY = process.env.CALOR_ADMIN_KEY || 'dev-admin-key';
export const CHROME = process.env.CHROME_PATH || '/usr/bin/google-chrome';

export const auth = { Authorization: `Bearer ${KEY}` };

export async function api(method, path, body, headers = {}) {
  const res = await fetch(API + path, {
    method,
    headers: { ...auth, ...(body && !(body instanceof Uint8Array) ? { 'Content-Type': 'application/json' } : {}), ...headers },
    body: body instanceof Uint8Array ? body : body ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  let json;
  try {
    json = JSON.parse(text);
  } catch {}
  return { status: res.status, json, text };
}

export function launch(opts = {}) {
  return puppeteer.launch({
    executablePath: CHROME,
    headless: true,
    args: ['--no-sandbox', '--disable-dev-shm-usage', '--hide-scrollbars', '--font-render-hinting=none'],
    defaultViewport: { width: 1440, height: 900, deviceScaleFactor: 1 },
    ...opts,
  });
}

export async function waitFor(url, tries = 60) {
  for (let i = 0; i < tries; i++) {
    try {
      const r = await fetch(url);
      if (r.status < 500) return;
    } catch {}
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error(`timeout waiting for ${url}`);
}

// Deterministic PRNG so seeded data is reproducible.
export function rng(seed) {
  let a = seed >>> 0;
  const next = () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
  const api = {
    next,
    int: (n) => Math.floor(next() * n),
    range: (a, b) => a + next() * (b - a),
    chance: (p) => next() < p,
    normal: (mu = 0, sd = 1) => mu + sd * Math.sqrt(-2 * Math.log(1 - next())) * Math.cos(2 * Math.PI * next()),
    poisson: (l) => {
      const L = Math.exp(-l);
      let k = 0;
      let p = 1;
      do {
        k++;
        p *= next();
      } while (p > L);
      return k - 1;
    },
    pick: (weights) => {
      const entries = Array.isArray(weights) ? weights : Object.entries(weights);
      const total = entries.reduce((s, [, w]) => s + w, 0);
      let r = next() * total;
      for (const [k, w] of entries) if ((r -= w) <= 0) return k;
      return entries[entries.length - 1][0];
    },
  };
  return api;
}
