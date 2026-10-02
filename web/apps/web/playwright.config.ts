// End-to-end test of the Next.js shell against a real Go server (in-memory DB, demo pak).
// e2e/global-setup.ts builds and starts both; the tests skip (instead of failing) when Go, the demo pak or
// a launchable Chromium are missing, or the server runs no bots (watch.spec.ts). Chromium runs with
// SwiftShader WebGL (no GPU needed).
//   pnpm --filter web e2e        (make e2e)
// Env: Q2_E2E_REQUIRE=1 makes every self-skip a failure (CI),
//      Q2_E2E_SERVER_PORT (default 18080), Q2_E2E_WEB_PORT (default 13000), Q2_E2E_SERVER_BIN (a prebuilt
//      q2server), Q2_E2E_REUSE_BUILD=1 (keep a matching .next build), Q2_PAK (the demo pak),
//      Q2_E2E_WEB_URL to test an already running stack instead.
import { defineConfig } from '@playwright/test';

const webPort = Number(process.env['Q2_E2E_WEB_PORT'] ?? 13000);

export default defineConfig({
  testDir: 'e2e',
  globalSetup: './e2e/global-setup.ts',
  globalTeardown: './e2e/global-teardown.ts',
  outputDir: 'test-results',
  timeout: 180_000,
  retries: 0,
  workers: 1,
  // CI also writes playwright-report/ (uploaded when the e2e job fails)
  reporter: process.env['CI'] ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: process.env['Q2_E2E_WEB_URL'] ?? `http://127.0.0.1:${webPort}`,
    viewport: { width: 960, height: 600 },
    launchOptions: {
      args: [
        '--use-gl=angle',
        '--use-angle=swiftshader',
        '--enable-unsafe-swiftshader',
        '--ignore-gpu-blocklist',
        '--autoplay-policy=no-user-gesture-required',
      ],
    },
  },
});
