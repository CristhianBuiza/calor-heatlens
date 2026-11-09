import { Suspense } from 'react';
import { ApiError } from '@/lib/api';

export function Card({ title, subtitle, children, actions, id, className = '' }: { title?: React.ReactNode; subtitle?: React.ReactNode; children: React.ReactNode; actions?: React.ReactNode; id?: string; className?: string }) {
  return (
    <section className={`card ${className}`} id={id}>
      {(title || actions) && (
        <header className="card-head">
          <div>
            {title && <h2>{title}</h2>}
            {subtitle && <p className="muted">{subtitle}</p>}
          </div>
          {actions && <div className="card-actions">{actions}</div>}
        </header>
      )}
      {children}
    </section>
  );
}

export function Skeleton({ height = 180 }: { height?: number }) {
  return <div className="skeleton" style={{ height }} aria-busy="true" />;
}

/** Streams a section independently so each view only waits for what it shows. */
export function Lazy({ children, height }: { children: React.ReactNode; height?: number }) {
  return <Suspense fallback={<Skeleton height={height} />}>{children}</Suspense>;
}

export function Kpi({ label, value, hint, tone }: { label: string; value: React.ReactNode; hint?: React.ReactNode; tone?: 'good' | 'warn' }) {
  return (
    <div className={`kpi ${tone || ''}`}>
      <div className="kpi-label">{label}</div>
      <div className="kpi-value">{value}</div>
      {hint && <div className="kpi-hint">{hint}</div>}
    </div>
  );
}

export function ErrorNote({ error }: { error: unknown }) {
  const msg = error instanceof ApiError ? `${error.status}: ${error.message}` : error instanceof Error ? error.message : String(error);
  return <p className="error">Could not load data ({msg}). Is the API running and CALOR_ADMIN_KEY set?</p>;
}

export async function safe<T>(p: Promise<T>): Promise<[T, null] | [null, unknown]> {
  try {
    return [await p, null];
  } catch (e) {
    return [null, e];
  }
}

export function Tag({ children, tone }: { children: React.ReactNode; tone?: 'red' | 'amber' | 'blue' | 'gray' }) {
  return <span className={`tag ${tone || 'gray'}`}>{children}</span>;
}
