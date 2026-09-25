// Loads the dev harness at fixed cameras and a frozen refresh time, checks that the renderer produced a
// non-trivial image without WebGL or console errors, and saves screenshots under test-results/.
import { existsSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { expect, test } from '@playwright/test';

const here = dirname(fileURLToPath(import.meta.url));
const pak = process.env['Q2_PAK'] ?? resolve(here, '../../../../assets/demo/baseq2/pak0.pak');

interface Status {
  ready: boolean;
  frames: number;
  errors: string[];
  glErrors: number[];
  drawCalls: number;
}

const views = [
  // outdoor courtyard: sky box, lightmapped walls, MD2 models, particles, beam, dlight
  { name: 'demo1-sky', query: 'map=demo1&time=5&pos=-200,1400,-80&ang=-15,-90,0' },
  // murky water: SURF_WARP surfaces with the turbsin warp
  { name: 'demo1-water', query: 'map=demo1&time=5&pos=160,1130,-220&ang=20,45,0' },
  // gl_lightmap 1 debug view
  { name: 'demo1-lightmap', query: 'map=demo1&time=5&pos=-200,1400,-80&ang=-15,-90,0&cvar_gl_lightmap=1' },
  { name: 'demo2-start', query: 'map=demo2&time=2' },
];

test.skip(!!process.env['Q2_NO_BROWSER'], `chromium cannot launch: ${process.env['Q2_NO_BROWSER'] ?? ''}`);

for (const v of views) {
  test(`renders ${v.name}`, async ({ page }, testInfo) => {
    test.skip(!existsSync(pak), 'demo pak0.pak missing');
    const consoleErrors: string[] = [];
    page.on('console', (m) => {
      if (m.type() === 'error') consoleErrors.push(m.text());
    });
    page.on('pageerror', (e) => consoleErrors.push(String(e)));

    await page.goto(`/?${v.query}&test=1`);
    await page.waitForFunction(
      () => {
        const s = (window as unknown as { __q2?: Status }).__q2;
        return s !== undefined && (s.ready || s.errors.length > 0);
      },
      null,
      { timeout: 90_000 },
    );
    const status = await page.evaluate(() => (window as unknown as { __q2: Status }).__q2);
    if (status.errors.some((e) => /WebGL2 is not available|could not create a WebGL2 context/.test(e))) {
      test.skip(true, 'no WebGL2 in this browser');
    }
    expect(status.errors, 'harness errors').toEqual([]);
    expect(status.glErrors, 'WebGL errors').toEqual([]);
    expect(consoleErrors, 'console errors').toEqual([]);
    expect(status.drawCalls).toBeGreaterThan(10);

    // pixel statistics of the WebGL canvas (preserveDrawingBuffer is on in test mode)
    const stats = await page.evaluate(() => {
      const c = document.getElementById('c') as HTMLCanvasElement;
      const t = document.createElement('canvas');
      t.width = c.width;
      t.height = c.height;
      const ctx = t.getContext('2d')!;
      ctx.drawImage(c, 0, 0);
      const d = ctx.getImageData(0, 0, t.width, t.height).data;
      let sum = 0;
      let sum2 = 0;
      const colors = new Set<number>();
      const n = d.length / 4;
      for (let i = 0; i < d.length; i += 4) {
        const l = 0.299 * d[i]! + 0.587 * d[i + 1]! + 0.114 * d[i + 2]!;
        sum += l;
        sum2 += l * l;
        if ((i & 63) === 0) colors.add((d[i]! << 16) | (d[i + 1]! << 8) | d[i + 2]!);
      }
      const mean = sum / n;
      return { mean, variance: sum2 / n - mean * mean, colors: colors.size };
    });
    testInfo.annotations.push({ type: 'pixels', description: JSON.stringify(stats) });
    expect(stats.mean).toBeGreaterThan(5);
    expect(stats.variance).toBeGreaterThan(50);
    expect(stats.colors).toBeGreaterThan(100);

    await page.screenshot({ path: testInfo.outputPath(`${v.name}.png`) });
  });
}
