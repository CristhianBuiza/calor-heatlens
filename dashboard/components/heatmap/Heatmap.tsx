'use client';

import { useRouter } from 'next/navigation';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { Heatmap as HeatmapData } from '@/lib/types';

export type HeatmapProps = {
  site: string;
  url: string;
  query: Record<string, string>;
  screenshot: { width: number; height: number; updatedAt: number } | null;
  pins: { label: string; relX: number; relY: number; clicks: number }[];
};

type Mode = 'screenshot' | 'live' | 'grid';
const COLS = 100;

let palette: Uint8ClampedArray | null = null;
function getPalette() {
  if (palette) return palette;
  const c = document.createElement('canvas');
  c.width = 256;
  c.height = 1;
  const g = c.getContext('2d')!;
  const grad = g.createLinearGradient(0, 0, 256, 0);
  grad.addColorStop(0, 'rgba(37,99,235,1)');
  grad.addColorStop(0.3, 'rgba(6,182,212,1)');
  grad.addColorStop(0.55, 'rgba(132,204,22,1)');
  grad.addColorStop(0.75, 'rgba(250,204,21,1)');
  grad.addColorStop(1, 'rgba(220,38,38,1)');
  g.fillStyle = grad;
  g.fillRect(0, 0, 256, 1);
  palette = g.getImageData(0, 0, 256, 1).data;
  return palette;
}

function draw(canvas: HTMLCanvasElement, data: HeatmapData, W: number, H: number, radius: number, opacity: number) {
  canvas.width = W;
  canvas.height = H;
  const ctx = canvas.getContext('2d', { willReadFrequently: true });
  if (!ctx || !data.cells.length) return;
  const r = Math.max(4, radius);
  const sprite = document.createElement('canvas');
  sprite.width = sprite.height = r * 2;
  const sg = sprite.getContext('2d')!;
  const grad = sg.createRadialGradient(r, r, 0, r, r, r);
  grad.addColorStop(0, 'rgba(0,0,0,1)');
  grad.addColorStop(1, 'rgba(0,0,0,0)');
  sg.fillStyle = grad;
  sg.fillRect(0, 0, r * 2, r * 2);

  const cw = W / data.cols;
  const ch = H / data.rows;
  for (const c of data.cells) {
    ctx.globalAlpha = Math.min(1, Math.pow(c.n / data.max, 0.6));
    ctx.drawImage(sprite, (c.x + 0.5) * cw - r, (c.y + 0.5) * ch - r);
  }
  ctx.globalAlpha = 1;
  const img = ctx.getImageData(0, 0, W, H);
  const px = img.data;
  const pal = getPalette();
  for (let i = 3; i < px.length; i += 4) {
    const a = px[i];
    if (!a) continue;
    const o = a * 4;
    px[i - 3] = pal[o];
    px[i - 2] = pal[o + 1];
    px[i - 1] = pal[o + 2];
    px[i] = Math.min(255, (40 + a) * opacity);
  }
  ctx.putImageData(img, 0, 0);
}

export default function Heatmap({ site, url, query, screenshot, pins }: HeatmapProps) {
  const router = useRouter();
  const wrap = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const [width, setWidth] = useState(0);
  const [type, setType] = useState<'click' | 'navigation' | 'all'>('click');
  const [mode, setMode] = useState<Mode>(screenshot ? 'screenshot' : 'live');
  const [radius, setRadius] = useState(22);
  const [opacity, setOpacity] = useState(0.85);
  const [showPins, setShowPins] = useState(true);
  const [data, setData] = useState<HeatmapData | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [shot, setShot] = useState(screenshot);
  const [uploading, setUploading] = useState(false);

  useEffect(() => {
    const el = wrap.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setWidth(Math.floor(el.clientWidth)));
    ro.observe(el);
    setWidth(Math.floor(el.clientWidth));
    return () => ro.disconnect();
  }, []);

  // Aspect ratio: screenshot if shown, otherwise the average tracked document size.
  const aspect = useMemo(() => {
    if (mode === 'screenshot' && shot && shot.width) return shot.height / shot.width;
    if (data && data.docW && data.docH) return data.docH / data.docW;
    return 1.6;
  }, [mode, shot, data]);
  const rows = Math.max(4, Math.min(800, Math.round(COLS * aspect)));

  const qs = useMemo(() => new URLSearchParams({ ...query, url, type, cols: String(COLS), rows: String(rows) }).toString(), [query, url, type, rows]);

  useEffect(() => {
    const ctl = new AbortController();
    setLoading(true);
    fetch(`/api/calor/sites/${encodeURIComponent(site)}/heatmap?${qs}`, { signal: ctl.signal })
      .then(async (r) => {
        if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error || r.statusText);
        return r.json();
      })
      .then((d: HeatmapData) => {
        setData(d);
        setError('');
      })
      .catch((e) => e.name !== 'AbortError' && setError(String(e.message || e)))
      .finally(() => setLoading(false));
    return () => ctl.abort();
  }, [site, qs]);

  const W = width;
  const H = Math.round(W * aspect);
  useEffect(() => {
    if (canvas.current && data && W > 0 && data.rows === rows) draw(canvas.current, data, W, H, Math.round((radius * W) / 1000), opacity);
  }, [data, W, H, radius, opacity, rows]);

  const upload = useCallback(
    async (file: File) => {
      setUploading(true);
      try {
        const r = await fetch(`/api/calor/sites/${encodeURIComponent(site)}/screenshot?url=${encodeURIComponent(url)}`, { method: 'PUT', body: file });
        const j = await r.json();
        if (!r.ok) throw new Error(j.error || r.statusText);
        setShot({ width: j.width, height: j.height, updatedAt: Date.now() });
        setMode('screenshot');
        router.refresh();
      } catch (e) {
        setError(`Upload failed: ${(e as Error).message}`);
      } finally {
        setUploading(false);
      }
    },
    [site, url, router],
  );

  const liveW = data?.viewportW && data.viewportW > 300 ? data.docW || data.viewportW : 1280;
  const scale = W / liveW;

  return (
    <div className="heatmap">
      <div className="hm-toolbar">
        <div className="seg" role="group" aria-label="Event type">
          {(['click', 'navigation', 'all'] as const).map((t) => (
            <button key={t} className={type === t ? 'on' : ''} onClick={() => setType(t)}>
              {t === 'click' ? 'Clicks' : t === 'navigation' ? 'Link exits' : 'All'}
            </button>
          ))}
        </div>
        <div className="seg" role="group" aria-label="Background">
          <button className={mode === 'screenshot' ? 'on' : ''} disabled={!shot} onClick={() => setMode('screenshot')} title={shot ? '' : 'Upload a screenshot first'}>
            Screenshot
          </button>
          <button className={mode === 'live' ? 'on' : ''} onClick={() => setMode('live')}>
            Live page
          </button>
          <button className={mode === 'grid' ? 'on' : ''} onClick={() => setMode('grid')}>
            Grid
          </button>
        </div>
        <label className="slider">
          Radius
          <input type="range" min={8} max={60} value={radius} onChange={(e) => setRadius(+e.target.value)} />
        </label>
        <label className="slider">
          Opacity
          <input type="range" min={0.3} max={1} step={0.05} value={opacity} onChange={(e) => setOpacity(+e.target.value)} />
        </label>
        <label className="check">
          <input type="checkbox" checked={showPins} onChange={(e) => setShowPins(e.target.checked)} /> Element pins
        </label>
        <label className="btn small upload">
          {uploading ? 'Uploading…' : shot ? 'Replace screenshot' : 'Upload screenshot'}
          <input type="file" accept="image/png,image/jpeg,image/webp" hidden onChange={(e) => e.target.files?.[0] && upload(e.target.files[0])} />
        </label>
      </div>
      <div className="hm-meta">
        <span>
          {data ? (
            <>
              <b>{data.total.toLocaleString()}</b> {type === 'navigation' ? 'link exits' : 'clicks'} from <b>{data.visitors.toLocaleString()}</b> visitors · grid {data.cols}×{data.rows} · served from {data.source}
              {data.docW ? ` · avg document ${data.docW}×${data.docH}px` : ''}
            </>
          ) : (
            'Loading heatmap…'
          )}
        </span>
        <span className="hm-scale">
          low <i /> high
        </span>
      </div>
      {error && <p className="error">{error}</p>}
      <div className={`hm-stage ${mode}`} ref={wrap} style={{ height: H || 400 }}>
        {mode === 'screenshot' && shot && (
          // eslint-disable-next-line @next/next/no-img-element
          <img alt="Page screenshot" src={`/api/calor/sites/${encodeURIComponent(site)}/screenshot?url=${encodeURIComponent(url)}&v=${shot.updatedAt}`} width={W} height={H} />
        )}
        {mode === 'live' && W > 0 && (
          <iframe title="Live page" src={url} sandbox="" loading="lazy" style={{ width: liveW, height: H / scale, transform: `scale(${scale})` }} />
        )}
        {mode === 'grid' && <div className="hm-grid" />}
        <canvas ref={canvas} className="hm-canvas" style={{ width: W, height: H }} />
        {showPins &&
          pins.slice(0, 10).map((p, i) => (
            <span key={i} className="pin" style={{ left: p.relX * W, top: p.relY * H }} title={`${p.label}: ${p.clicks} clicks`}>
              {i + 1}
            </span>
          ))}
        {loading && <div className="hm-loading">Loading…</div>}
        {data && !data.total && !loading && <div className="hm-loading">No {type === 'click' ? 'clicks' : 'events'} in this range</div>}
      </div>
      {mode === 'live' && <p className="muted small">Live page is sandboxed (no scripts, so it is not tracked); sites that send X-Frame-Options will not render here, so upload a full-page screenshot for best results.</p>}
    </div>
  );
}
