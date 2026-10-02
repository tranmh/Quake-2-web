import { defineConfig } from 'vitest/config';

// Unit tests of the harness's pure modules (node environment); the WebGL rendering itself is covered by
// the Playwright render tests in e2e/ (pnpm --filter q2-dev e2e).
export default defineConfig({
  test: {
    include: ['test/**/*.test.ts'],
    testTimeout: 60_000,
  },
});
