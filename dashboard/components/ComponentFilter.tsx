'use client';

import { usePathname, useRouter, useSearchParams } from 'next/navigation';
import { useTransition } from 'react';

export default function ComponentFilter({ components }: { components: { id: string; name: string }[] }) {
  const pathname = usePathname();
  const sp = useSearchParams();
  const router = useRouter();
  const [pending, start] = useTransition();
  if (!components.length) return null;
  const current = sp.get('component') || '';
  return (
    <label className={`component-filter ${current ? 'active' : ''} ${pending ? 'pending' : ''}`}>
      <span>Component</span>
      <select
        value={current}
        aria-label="Component filter"
        onChange={(e) => {
          const q = new URLSearchParams(sp.toString());
          if (e.target.value) q.set('component', e.target.value);
          else q.delete('component');
          q.delete('offset');
          start(() => router.push(`${pathname}${q.size ? `?${q}` : ''}`));
        }}
      >
        <option value="">All clicks</option>
        {components.map((c) => (
          <option key={c.id} value={c.id}>
            {c.name}
          </option>
        ))}
      </select>
    </label>
  );
}
