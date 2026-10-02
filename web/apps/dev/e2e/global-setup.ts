// Probes whether the bundled Chromium can start (it may lack system libraries); the render tests are
// skipped instead of failing when it cannot, unless Q2_E2E_REQUIRE=1 (CI) makes that a failure.
import { chromium } from '@playwright/test';

export default async function globalSetup(): Promise<void> {
  try {
    const b = await chromium.launch({
      args: ['--use-gl=angle', '--use-angle=swiftshader', '--enable-unsafe-swiftshader'],
    });
    await b.close();
  } catch (e) {
    if (process.env['Q2_E2E_REQUIRE'] === '1')
      throw new Error(`Q2_E2E_REQUIRE=1: chromium cannot launch: ${String(e)}`);
    process.env['Q2_NO_BROWSER'] = String(e).split('\n')[0];
    console.warn(`chromium cannot launch, skipping render tests: ${process.env['Q2_NO_BROWSER']}`);
  }
}
