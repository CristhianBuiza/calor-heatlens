import Link from 'next/link';

export default function NotFound() {
  return (
    <div className="card">
      <h2>Not found</h2>
      <p className="muted">That site or page is not registered.</p>
      <Link className="btn small" href="/sites">Manage sites</Link>
    </div>
  );
}
