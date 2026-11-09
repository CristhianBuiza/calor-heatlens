import type { NextConfig } from 'next';
import path from 'node:path';

const nextConfig: NextConfig = {
  // Self-contained server bundle (.next/standalone) with only the traced
  // node_modules it needs: small to ship and to keep resident.
  output: 'standalone',
  outputFileTracingRoot: path.join(__dirname),
  poweredByHeader: false,
  reactStrictMode: true,
  productionBrowserSourceMaps: false,
  images: { unoptimized: true },
};

export default nextConfig;
