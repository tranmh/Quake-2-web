import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vitest/config';

const here = path.dirname(fileURLToPath(import.meta.url));

// Unit tests of the shell's browser logic (node environment with small DOM stubs); the full stack is
// covered by the Playwright e2e tests (pnpm --filter web e2e).
export default defineConfig({
  resolve: { alias: { '@': path.join(here, 'src') } },
  test: {
    include: ['test/**/*.test.ts'],
    testTimeout: 60_000,
  },
});
