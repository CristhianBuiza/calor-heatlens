import { build } from 'esbuild';
import { copyFileSync, mkdirSync, readFileSync } from 'node:fs';
import { gzipSync, brotliCompressSync } from 'node:zlib';

mkdirSync('dist', { recursive: true });
copyFileSync('src/calor.js', 'dist/calor.js');
await build({
  entryPoints: ['src/calor.js'],
  outfile: 'dist/calor.min.js',
  minify: true,
  target: ['es2017'],
  legalComments: 'inline',
});

for (const f of ['dist/calor.js', 'dist/calor.min.js']) {
  const buf = readFileSync(f);
  console.log(`${f.padEnd(20)} ${String(buf.length).padStart(6)} B  gzip ${String(gzipSync(buf, { level: 9 }).length).padStart(5)} B  brotli ${brotliCompressSync(buf).length} B`);
}
