// Probes whether the bundled Chromium can start (it may lack system libraries); the render tests are
// skipped instead of failing when it cannot.
import { chromium } from '@playwright/test';

export default async function globalSetup(): Promise<void> {
  try {
    const b = await chromium.launch({
      args: ['--use-gl=angle', '--use-angle=swiftshader', '--enable-unsafe-swiftshader'],
    });
    await b.close();
  } catch (e) {
    process.env['Q2_NO_BROWSER'] = String(e).split('\n')[0];
    console.warn(`chromium cannot launch, skipping render tests: ${process.env['Q2_NO_BROWSER']}`);
  }
}
