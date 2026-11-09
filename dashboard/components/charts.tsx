// Server-rendered SVG/HTML charts: zero client JavaScript, no chart library.
import { compact, DAY_NAMES, hourLabel, num, pct, shortDate } from '@/lib/format';

type Series = { name: string; color: string; values: number[] };

function niceMax(v: number) {
  if (v <= 0) return 1;
  const p = Math.pow(10, Math.floor(Math.log10(v)));
  const m = v / p;
  return (m <= 1 ? 1 : m <= 2 ? 2 : m <= 2.5 ? 2.5 : m <= 5 ? 5 : 10) * p;
}

export function LineChart({ labels, series, height = 220, area = true, format = compact, partialLast = false }: { labels: string[]; series: Series[]; height?: number; area?: boolean; format?: (n: number) => string; partialLast?: boolean }) {
  const W = 860;
  const H = height;
  const pad = { l: 44, r: 12, t: 12, b: 28 };
  const iw = W - pad.l - pad.r;
  const ih = H - pad.t - pad.b;
  const max = niceMax(Math.max(1, ...series.flatMap((s) => s.values)));
  const n = Math.max(1, labels.length - 1);
  const x = (i: number) => pad.l + (labels.length === 1 ? iw / 2 : (i / n) * iw);
  const y = (v: number) => pad.t + ih - (v / max) * ih;
  const step = Math.max(1, Math.ceil(labels.length / 8));
  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="chart" role="img" aria-label={series.map((s) => s.name).join(', ')}>
      {[0, 0.25, 0.5, 0.75, 1].map((g) => (
        <g key={g}>
          <line x1={pad.l} x2={W - pad.r} y1={y(max * g)} y2={y(max * g)} className="grid" />
          <text x={pad.l - 8} y={y(max * g) + 4} textAnchor="end" className="axis">{format(max * g)}</text>
        </g>
      ))}
      {labels.map((l, i) =>
        i % step === 0 || i === labels.length - 1 ? (
          <text key={l + i} x={x(i)} y={H - 8} textAnchor="middle" className="axis">{l.length === 10 ? shortDate(l) : l}</text>
        ) : null,
      )}
      {series.map((s) => {
        const pts = s.values.map((v, i) => `${x(i).toFixed(1)},${y(v).toFixed(1)}`);
        return (
          <g key={s.name}>
            {area && series.length === 1 && pts.length > 1 && (
              <path d={`M${x(0)},${y(0)} L${pts.join(' L')} L${x(s.values.length - 1)},${y(0)} Z`} fill={s.color} opacity={0.12} />
            )}
            {partialLast && pts.length > 2 ? (
              <>
                <polyline points={pts.slice(0, -1).join(' ')} fill="none" stroke={s.color} strokeWidth={2.2} strokeLinejoin="round" />
                <polyline points={pts.slice(-2).join(' ')} fill="none" stroke={s.color} strokeWidth={2.2} strokeDasharray="4 4" />
              </>
            ) : (
              <polyline points={pts.join(' ')} fill="none" stroke={s.color} strokeWidth={2.2} strokeLinejoin="round" />
            )}
          </g>
        );
      })}
    </svg>
  );
}

export function BarChart({ values, labels, color = 'var(--accent)', height = 180, highlight, format = compact, labelEvery = 1 }: { values: number[]; labels: string[]; color?: string; height?: number; highlight?: number; format?: (n: number) => string; labelEvery?: number }) {
  const W = 860;
  const H = height;
  const pad = { l: 40, r: 8, t: 16, b: 24 };
  const iw = W - pad.l - pad.r;
  const ih = H - pad.t - pad.b;
  const max = niceMax(Math.max(1, ...values));
  const bw = iw / values.length;
  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="chart" role="img">
      {[0, 0.5, 1].map((g) => (
        <g key={g}>
          <line x1={pad.l} x2={W - pad.r} y1={pad.t + ih - g * ih} y2={pad.t + ih - g * ih} className="grid" />
          <text x={pad.l - 8} y={pad.t + ih - g * ih + 4} textAnchor="end" className="axis">{format(max * g)}</text>
        </g>
      ))}
      {values.map((v, i) => {
        const h = (v / max) * ih;
        return (
          <g key={i}>
            <rect x={pad.l + i * bw + bw * 0.14} y={pad.t + ih - h} width={bw * 0.72} height={Math.max(h, v > 0 ? 1 : 0)} rx={3} fill={color} opacity={highlight === undefined || highlight === i ? 1 : 0.45}>
              <title>{`${labels[i]}: ${num(v)}`}</title>
            </rect>
            {i % labelEvery === 0 && (
              <text x={pad.l + i * bw + bw / 2} y={H - 6} textAnchor="middle" className="axis">{labels[i]}</text>
            )}
          </g>
        );
      })}
    </svg>
  );
}

export function HourChart({ hours, peak }: { hours: number[]; peak?: number }) {
  return <BarChart values={hours} labels={hours.map((_, h) => hourLabel(h))} highlight={peak} labelEvery={2} />;
}

export function Punchcard({ matrix }: { matrix: number[][] }) {
  const max = Math.max(1, ...matrix.flat());
  const order = [1, 2, 3, 4, 5, 6, 0];
  return (
    <div className="punch">
      <div className="punch-row punch-head">
        <span />
        {Array.from({ length: 24 }, (_, h) => (
          <span key={h} className="axis">{h % 3 === 0 ? hourLabel(h) : ''}</span>
        ))}
      </div>
      {order.map((d) => (
        <div className="punch-row" key={d}>
          <span className="axis">{DAY_NAMES[d]}</span>
          {matrix[d].map((v, h) => {
            const a = v / max;
            return <span key={h} className="punch-cell" title={`${DAY_NAMES[d]} ${hourLabel(h)}: ${num(v)} clicks`} style={{ background: `rgba(99,102,241,${a === 0 ? 0.04 : 0.12 + a * 0.88})` }} />;
          })}
        </div>
      ))}
    </div>
  );
}

export function HBarList({ rows, empty = 'No data yet', valueLabel }: { rows: { label: React.ReactNode; value: number; share?: number; sub?: React.ReactNode; href?: string; key?: string }[]; empty?: string; valueLabel?: string }) {
  if (!rows.length) return <p className="empty">{empty}</p>;
  const max = Math.max(...rows.map((r) => r.value), 1);
  return (
    <div className="hbars">
      {valueLabel && (
        <div className="hbar-head">
          <span />
          <span>{valueLabel}</span>
        </div>
      )}
      {rows.map((r, i) => {
        const body = (
          <>
            <div className="hbar-fill" style={{ width: `${(r.value / max) * 100}%` }} />
            <div className="hbar-label">
              {r.label}
              {r.sub && <div className="hbar-sub">{r.sub}</div>}
            </div>
            <div className="hbar-value">
              {num(r.value)}
              {r.share !== undefined && <span className="muted"> · {pct(r.share)}</span>}
            </div>
          </>
        );
        return r.href ? (
          <a className="hbar" href={r.href} key={r.key || i}>{body}</a>
        ) : (
          <div className="hbar" key={r.key || i}>{body}</div>
        );
      })}
    </div>
  );
}

export function ReachFunnel({ reach }: { reach: { depth: number; share: number; pageviews: number }[] }) {
  return (
    <div className="reach">
      {reach.map((r) => (
        <div className="reach-row" key={r.depth}>
          <span className="reach-depth">{r.depth}%</span>
          <div className="reach-track">
            <div className="reach-fill" style={{ width: `${r.share}%` }} />
          </div>
          <span className="reach-val">
            <b>{pct(r.share)}</b> <span className="muted">({num(r.pageviews)})</span>
          </span>
        </div>
      ))}
    </div>
  );
}

const DEVICE_COLORS: Record<string, string> = { desktop: '#6366f1', tablet: '#0ea5e9', mobile: '#f97316', unknown: '#94a3b8' };

export function DeviceSplit({ devices }: { devices: { device: string; share: number; pageviews: number; visitors: number }[] }) {
  if (!devices.length) return <p className="empty">No data yet</p>;
  return (
    <div>
      <div className="stack">
        {devices.map((d) => (
          <div key={d.device} style={{ width: `${d.share}%`, background: DEVICE_COLORS[d.device] }} title={`${d.device} ${pct(d.share)}`} />
        ))}
      </div>
      <div className="legend">
        {devices.map((d) => (
          <div key={d.device}>
            <i style={{ background: DEVICE_COLORS[d.device] }} />
            <b>{d.device}</b> {pct(d.share)} <span className="muted">· {num(d.visitors)} visitors</span>
          </div>
        ))}
      </div>
    </div>
  );
}
