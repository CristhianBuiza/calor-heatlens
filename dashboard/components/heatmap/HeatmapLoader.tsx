'use client';

import dynamic from 'next/dynamic';
import type { HeatmapProps } from './Heatmap';

// Canvas code ships in its own chunk, loaded only on page-detail views.
const Heatmap = dynamic(() => import('./Heatmap'), {
  ssr: false,
  loading: () => <div className="skeleton" style={{ height: 520 }} />,
});

export default function HeatmapLoader(props: HeatmapProps) {
  return <Heatmap {...props} />;
}
