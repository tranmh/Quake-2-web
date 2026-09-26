// Starts the Go server (in-memory repository, demo pak ingested at startup) and `next start`, both
// detached; PIDs go to .e2e/e2e-pids.json for global-teardown. When something cannot be started
// the reason is exported as Q2_E2E_SKIP and the tests skip.
import { spawn, spawnSync, type ChildProcess } from 'node:child_process';
import { existsSync, mkdirSync, openSync, readFileSync, renameSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from '@playwright/test';

const here = dirname(fileURLToPath(import.meta.url));
const appDir = resolve(here, '..');
const repo = resolve(appDir, '../../..');
const serverDir = resolve(repo, 'server');
// not test-results/: Playwright empties its outputDir after global setup
const outDir = resolve(appDir, '.e2e');
const pak = process.env['Q2_PAK'] ?? resolve(repo, 'assets/demo/baseq2/pak0.pak');

const serverPort = Number(process.env['Q2_E2E_SERVER_PORT'] ?? 18080);
const webPort = Number(process.env['Q2_E2E_WEB_PORT'] ?? 13000);

async function waitHttp(url: string, timeoutMs: number, proc?: ChildProcess): Promise<boolean> {
  const end = Date.now() + timeoutMs;
  while (Date.now() < end) {
    if (proc && proc.exitCode !== null) return false;
    try {
      const r = await fetch(url);
      if (r.ok) return true;
    } catch {
      // not up yet
    }
    await new Promise((r) => setTimeout(r, 500));
  }
  return false;
}

function skip(reason: string): void {
  process.env['Q2_E2E_SKIP'] = reason;
  console.warn(`[e2e] skipping: ${reason}`);
}

function start(cmd: string, args: string[], cwd: string, env: Record<string, string>, log: string): ChildProcess {
  const fd = openSync(resolve(outDir, log), 'w');
  const p = spawn(cmd, args, { cwd, env: { ...process.env, ...env }, stdio: ['ignore', fd, fd], detached: true });
  p.unref();
  return p;
}

export default async function globalSetup(): Promise<void> {
  mkdirSync(outDir, { recursive: true });
  const pids: number[] = [];
  const savePids = () => writeFileSync(resolve(outDir, 'e2e-pids.json'), JSON.stringify(pids));

  try {
    const b = await chromium.launch({ args: ['--use-gl=angle', '--use-angle=swiftshader', '--enable-unsafe-swiftshader'] });
    await b.close();
  } catch (e) {
    return skip(`chromium cannot launch: ${String(e).split('\n')[0]}`);
  }

  const webUrl = process.env['Q2_E2E_WEB_URL'];
  if (webUrl) {
    if (!(await waitHttp(webUrl, 10_000))) skip(`${webUrl} is not reachable`);
    return; // externally managed stack
  }
  if (!existsSync(pak)) return skip(`demo pak missing (${pak}); run tools/fetch-demo-pak.sh`);

  // ---- Go server
  const serverUrl = `http://127.0.0.1:${serverPort}`;
  if (!(await waitHttp(`${serverUrl}/readyz`, 500))) {
    // Q2_E2E_SERVER_BIN: a prebuilt q2server binary; otherwise `go build ./cmd/q2server` (equivalent to
    // `go run`, but a binary can be killed cleanly). A failing build falls back to the previous binary.
    let bin = process.env['Q2_E2E_SERVER_BIN'] ?? '';
    if (!bin) {
      bin = resolve(outDir, 'q2server');
      if (spawnSync('go', ['version']).status !== 0) {
        if (!existsSync(bin)) return skip('go toolchain not found');
      } else {
        console.log('[e2e] building the Go server…');
        const build = spawnSync('go', ['build', '-o', bin + '.new', './cmd/q2server'], { cwd: serverDir, stdio: 'inherit' });
        if (build.status === 0) renameSync(bin + '.new', bin);
        else if (existsSync(bin)) console.warn('[e2e] go build failed: using the previously built server binary');
        else return skip('go build ./cmd/q2server failed');
      }
    }
    const blobs = resolve(outDir, 'blobs');
    mkdirSync(blobs, { recursive: true });
    const srv = start(
      bin,
      [],
      serverDir,
      {
        Q2_HTTP_ADDR: `127.0.0.1:${serverPort}`,
        Q2_BLOB_DIR: blobs,
        Q2_UPLOAD_DIR: resolve(outDir, 'uploads'),
        Q2_DEMO_PAK: pak,
        Q2_COOKIE_SECURE: 'false',
        Q2_CORS_ORIGINS: `http://127.0.0.1:${webPort},http://localhost:${webPort}`,
        Q2_LOG_FORMAT: 'text',
        DATABASE_URL: '',
      },
      'server.log',
    );
    pids.push(srv.pid!);
    savePids();
    if (!(await waitHttp(`${serverUrl}/readyz`, 120_000, srv))) {
      return skip(`Go server did not become ready (see .e2e/server.log)`);
    }
  }

  // ---- Next.js production build (the rewrites to the Go server are fixed at build time). Always rebuilt
  // unless Q2_E2E_REUSE_BUILD=1 and the existing build targets the same upstream.
  const marker = resolve(appDir, '.next/e2e-upstream');
  const reuse =
    process.env['Q2_E2E_REUSE_BUILD'] === '1' &&
    existsSync(resolve(appDir, '.next/BUILD_ID')) &&
    existsSync(marker) &&
    readFileSync(marker, 'utf8') === serverUrl;
  if (!reuse) {
    console.log('[e2e] next build…');
    const b = spawnSync('pnpm', ['run', 'build'], {
      cwd: appDir,
      env: { ...process.env, Q2_SERVER_URL: serverUrl, NEXT_TELEMETRY_DISABLED: '1' },
      stdio: 'inherit',
    });
    if (b.status !== 0) return skip('next build failed');
    writeFileSync(marker, serverUrl);
  }
  const web = start(
    'pnpm',
    ['exec', 'next', 'start', '--port', String(webPort), '--hostname', '127.0.0.1'],
    appDir,
    { NEXT_TELEMETRY_DISABLED: '1' },
    'next.log',
  );
  pids.push(web.pid!);
  savePids();
  if (!(await waitHttp(`http://127.0.0.1:${webPort}/`, 60_000, web))) skip('next start did not come up (see .e2e/next.log)');
}
