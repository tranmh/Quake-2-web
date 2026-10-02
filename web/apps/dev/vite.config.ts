// Dev harness for q2-render-gl. Serves the demo pak at /pak0.pak straight from assets/demo, and for the
// ?nav=1 overlay the agent's nav dumps at /nav/<map>.json and the route tables at /routes/<file>.json.
import { spawn } from 'node:child_process';
import { createReadStream, existsSync, mkdirSync, renameSync, statSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { defineConfig, type Plugin } from 'vite';

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, '../../..');
const pakPath = process.env['Q2_PAK'] ?? resolve(repo, 'assets/demo/baseq2/pak0.pak');
// the same directories the agent uses ($Q2_NAV_DIR, $Q2_ROUTES_DIR)
const navDir = process.env['Q2_NAV_DIR'] ?? resolve(repo, 'assets/nav');
const routesDir = process.env['Q2_ROUTES_DIR'] ?? resolve(repo, 'fixtures/agent/routes');

type Req = { url?: string };
type Res = import('node:http').ServerResponse;
type Handler = (req: Req, res: Res, next: () => void) => void;

function sendFile(res: Res, file: string, type: string): void {
  res.setHeader('Content-Type', type);
  res.setHeader('Content-Length', String(statSync(file).size));
  res.setHeader('Cache-Control', 'no-cache');
  createReadStream(file).pipe(res);
}

function sendError(res: Res, status: number, text: string): void {
  res.statusCode = status;
  res.setHeader('Content-Type', 'text/plain; charset=utf-8');
  res.end(text);
}

function plugin(name: string, handler: Handler): Plugin {
  return {
    name,
    configureServer(server) {
      server.middlewares.use(handler);
    },
    configurePreviewServer(server) {
      server.middlewares.use(handler);
    },
  };
}

function servePak(): Plugin {
  return plugin('q2-serve-pak', (req, res, next) => {
    if (!req.url || req.url.split('?')[0] !== '/pak0.pak') return next();
    if (!existsSync(pakPath)) return sendError(res, 404, 'pak0.pak not found (run tools/fetch-demo-pak.sh)');
    sendFile(res, pakPath, 'application/octet-stream');
  });
}

/**
 * Runs `q2nav dump` for a map into <navDir>/<map>.viz.json (what `make nav` writes), building the nav
 * graph first when the cache has none. One run per map at a time; resolves to an error text or null.
 */
function makeDumper(): (map: string, out: string) => Promise<string | null> {
  const running = new Map<string, Promise<string | null>>();
  return (map, out) => {
    let p = running.get(map);
    if (p) return p;
    p = new Promise<string | null>((done) => {
      mkdirSync(navDir, { recursive: true });
      const tmp = `${out}.${process.pid}.tmp`;
      const args = ['run', './cmd/q2nav', 'dump', '-pak', pakPath, '-map', map, '-nav', navDir, '-o', tmp];
      console.log(`[nav] go ${args.join(' ')}`);
      let log = '';
      const child = spawn('go', args, { cwd: resolve(repo, 'server'), stdio: ['ignore', 'pipe', 'pipe'] });
      child.stdout.on('data', (b: Buffer) => (log += b.toString()));
      child.stderr.on('data', (b: Buffer) => (log += b.toString()));
      child.on('error', (e) => done(`cannot run go: ${e.message} (run make nav)`));
      child.on('close', (code) => {
        if (code !== 0) return done(`q2nav dump exited ${code}: ${log.slice(-2000)}`);
        try {
          renameSync(tmp, out);
        } catch (e) {
          return done(`q2nav dump wrote no ${tmp}: ${String(e)}`);
        }
        console.log(`[nav] ${log.trim()}`);
        done(null);
      });
    }).finally(() => running.delete(map));
    running.set(map, p);
    return p;
  };
}

function serveNav(): Plugin {
  const dump = makeDumper();
  return plugin('q2-serve-nav', (req, res, next) => {
    const url = (req.url ?? '').split('?')[0]!;
    const nav = /^\/nav\/([A-Za-z0-9_]{1,32})\.json$/.exec(url);
    if (nav) {
      const file = resolve(navDir, `${nav[1]}.viz.json`);
      if (existsSync(file)) return sendFile(res, file, 'application/json');
      if (!existsSync(pakPath))
        return sendError(res, 404, 'pak0.pak not found (run tools/fetch-demo-pak.sh)');
      void dump(nav[1]!, file).then((err) =>
        err ? sendError(res, 500, `nav dump of ${nav[1]}: ${err}`) : sendFile(res, file, 'application/json'),
      );
      return;
    }
    const route = /^\/routes\/([A-Za-z0-9_-]{1,32}\.json)$/.exec(url);
    if (route) {
      const file = resolve(routesDir, route[1]!);
      if (!existsSync(file)) return sendError(res, 404, `no route table ${route[1]} in ${routesDir}`);
      return sendFile(res, file, 'application/json');
    }
    next();
  });
}

export default defineConfig({
  plugins: [servePak(), serveNav()],
  server: { port: 5199, strictPort: false },
  preview: { port: 5199 },
});
