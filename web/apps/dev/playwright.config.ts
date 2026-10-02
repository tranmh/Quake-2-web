// Playwright render smoke test for the dev harness. Uses the bundled Chromium (headless shell) with
// SwiftShader WebGL so it runs without a GPU or system GL libraries.
import { defineConfig } from '@playwright/test';

const port = 5199;

export default defineConfig({
  testDir: 'e2e',
  globalSetup: './e2e/global-setup.ts',
  outputDir: 'test-results',
  timeout: 120_000,
  retries: 0,
  // CI also writes playwright-report/ (uploaded when the e2e job fails)
  reporter: process.env['CI'] ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    viewport: { width: 800, height: 600 },
    launchOptions: {
      args: [
        '--use-gl=angle',
        '--use-angle=swiftshader',
        '--enable-unsafe-swiftshader',
        '--ignore-gpu-blocklist',
      ],
    },
  },
  webServer: {
    command: `pnpm exec vite --port ${port} --strictPort --host 127.0.0.1`,
    url: `http://127.0.0.1:${port}/`,
    reuseExistingServer: true,
    timeout: 60_000,
  },
});
