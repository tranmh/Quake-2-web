/* global console, process */
// Bundles q2-sound's AudioWorklet module (src/worklet.ts) into public/q2-sound-worklet.js. The worklet is
// loaded with audioWorklet.addModule(url), which bundlers do not follow, so it is prebuilt here and
// served as a static file (see src/game/sound.ts).
import { build } from 'esbuild';
import { existsSync } from 'node:fs';
import { createRequire } from 'node:module';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
let entry;
try {
  entry = path.join(path.dirname(require.resolve('q2-sound/package.json')), 'src/worklet.ts');
} catch {
  entry = path.join(here, '../../../packages/q2-sound/src/worklet.ts');
}
const out = path.join(here, '../public/q2-sound-worklet.js');
if (!existsSync(entry)) {
  console.warn(`[build-worklet] ${entry} not found: the game runs without sound`);
  process.exit(0);
}
await build({
  entryPoints: [entry],
  outfile: out,
  bundle: true,
  format: 'esm',
  target: 'es2022',
  minify: true,
  legalComments: 'none',
  logLevel: 'warning',
});
console.log(`[build-worklet] ${path.relative(process.cwd(), out)}`);
