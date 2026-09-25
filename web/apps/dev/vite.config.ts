// Dev harness for q2-render-gl. Serves the demo pak at /pak0.pak straight from assets/demo.
import { createReadStream, existsSync, statSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { defineConfig, type Plugin } from 'vite';

const here = dirname(fileURLToPath(import.meta.url));
const pakPath = process.env['Q2_PAK'] ?? resolve(here, '../../../assets/demo/baseq2/pak0.pak');

function servePak(): Plugin {
  const handler = (
    req: { url?: string },
    res: import('node:http').ServerResponse,
    next: () => void,
  ): void => {
    if (!req.url || req.url.split('?')[0] !== '/pak0.pak') return next();
    if (!existsSync(pakPath)) {
      res.statusCode = 404;
      res.end('pak0.pak not found (run tools/fetch-demo-pak.sh)');
      return;
    }
    res.setHeader('Content-Type', 'application/octet-stream');
    res.setHeader('Content-Length', String(statSync(pakPath).size));
    createReadStream(pakPath).pipe(res);
  };
  return {
    name: 'q2-serve-pak',
    configureServer(server) {
      server.middlewares.use(handler);
    },
    configurePreviewServer(server) {
      server.middlewares.use(handler);
    },
  };
}

export default defineConfig({
  plugins: [servePak()],
  server: { port: 5199, strictPort: false },
  preview: { port: 5199 },
});
