// Full stack: register → create a single player game on demo1 → /play/[id] → the client reaches
// ca_active over the WebSocket → hold +forward (UPARROW) for 2 s → the player moved, the canvas shows a
// non-trivial image and nothing was logged as an error. Screenshot: test-results/play-demo1.png.
import { resolve } from 'node:path';
import { expect, test, type Page } from '@playwright/test';

test.skip(!!process.env['Q2_E2E_SKIP'], process.env['Q2_E2E_SKIP'] ?? '');

interface Debug {
  phase: string;
  connState: number;
  frames: number;
  errors: string[];
  prints: string[];
  origin(): number[];
}

const debug = (page: Page) =>
  page.evaluate(() => {
    const d = (window as unknown as { __q2web?: Debug }).__q2web;
    return d
      ? { phase: d.phase, connState: d.connState, frames: d.frames, errors: d.errors.slice(), origin: d.origin(), prints: d.prints.join('') }
      : null;
  });

async function shotStats(page: Page, name: string) {
  const shotPath = resolve(test.info().project.outputDir, name);
  const png = await page.screenshot({ path: shotPath });
  const stats = await page.evaluate(async (b64) => {
    const img = new Image();
    img.src = `data:image/png;base64,${b64}`;
    await img.decode();
    const c = document.createElement('canvas');
    c.width = img.width;
    c.height = img.height;
    const g = c.getContext('2d')!;
    g.drawImage(img, 0, 0);
    const px = g.getImageData(0, 0, c.width, c.height).data;
    let sum = 0;
    let sum2 = 0;
    const colors = new Set<number>();
    const n = px.length / 4;
    for (let i = 0; i < px.length; i += 4) {
      const l = 0.299 * px[i]! + 0.587 * px[i + 1]! + 0.114 * px[i + 2]!;
      sum += l;
      sum2 += l * l;
      if (colors.size < 5000) colors.add((px[i]! << 16) | (px[i + 1]! << 8) | px[i + 2]!);
    }
    const mean = sum / n;
    return { mean, variance: sum2 / n - mean * mean, colors: colors.size };
  }, png.toString('base64'));
  console.log(`screenshot ${shotPath}: mean ${stats.mean.toFixed(1)} variance ${stats.variance.toFixed(1)} colors ${stats.colors}`);
  return stats;
}

test('register, start demo1, play', async ({ page }) => {
  const consoleErrors: string[] = [];
  page.on('console', (m) => {
    if (m.type() === 'error') consoleErrors.push(m.text());
  });
  page.on('pageerror', (e) => consoleErrors.push(String(e)));

  // ---- landing + register
  await page.goto('/');
  await expect(page.getByRole('link', { name: 'Play the demo' })).toBeVisible();
  await page.goto('/register');
  const email = `e2e-${Date.now()}-${Math.floor(Math.random() * 1e6)}@example.com`;
  await page.locator('input[name=email]').fill(email);
  await page.locator('input[name=displayName]').fill('e2e');
  await page.locator('input[name=password]').fill('password123');
  await page.getByRole('button', { name: 'Register' }).click();
  await page.waitForURL('**/servers');

  // ---- create game
  await page.getByRole('button', { name: 'New game' }).click();
  const dialog = page.getByRole('dialog', { name: 'Start a game' });
  await expect(dialog).toBeVisible();
  await dialog.locator('select[name=mode]').selectOption('sp');
  await expect(dialog.locator('select[name=map] option[value=demo1]')).toHaveCount(1);
  await dialog.locator('select[name=map]').selectOption('demo1');
  await dialog.getByRole('button', { name: 'Begin' }).click();
  await page.waitForURL('**/play/**');

  // ---- connect
  await page.waitForFunction(
    () => {
      const d = (window as unknown as { __q2web?: Debug }).__q2web;
      return !!d && (d.connState === 4 || d.phase === 'error');
    },
    null,
    { timeout: 120_000 },
  );
  let d = await debug(page);
  expect(d?.phase, `engine errors: ${d?.errors.join(' | ')}`).toBe('active');
  await expect(page.getByTestId('loading-overlay')).toHaveCount(0);

  // let the world settle for a few frames, then walk forward for 2 s
  await page.waitForTimeout(1500);
  const spawn = await shotStats(page, 'play-demo1-spawn.png');
  expect(spawn.variance, 'spawn view has structure').toBeGreaterThan(100);
  expect(spawn.colors).toBeGreaterThan(200);
  const before = (await debug(page))!.origin;
  await page.getByTestId('game-canvas').click(); // focus + pointer lock (may be refused headless)
  // if the lock was refused/lost the in-game menu may be open: resume it
  if (await page.getByTestId('game-menu').count()) await page.keyboard.press('Escape');
  await page.keyboard.down('ArrowUp');
  await page.waitForTimeout(2000);
  await page.keyboard.up('ArrowUp');
  await page.waitForTimeout(500);
  d = await debug(page);
  const after = d!.origin;
  const moved = Math.hypot(after[0]! - before[0]!, after[1]! - before[1]!);
  console.log(`origin ${before.map((v) => v.toFixed(1)).join(',')} -> ${after.map((v) => v.toFixed(1)).join(',')} (moved ${moved.toFixed(1)})`);
  expect(d!.connState).toBe(4);
  expect(d!.frames).toBeGreaterThan(60);
  expect(moved, 'player moved while +forward was held').toBeGreaterThan(64);

  // ---- screenshot + pixel statistics (decoded in the page: no PNG library needed). After walking the
  // view may face a wall up close, so the strict variance check uses the spawn view taken above.
  const walked = await shotStats(page, 'play-demo1.png');
  expect(walked.mean, 'not a black frame').toBeGreaterThan(3);
  expect(walked.colors, 'world + HUD colours').toBeGreaterThan(200);

  // ---- in-game menu: losing the pointer lock opens it; Options; Save; Resume
  await page.evaluate(() => document.exitPointerLock());
  const menu = page.getByTestId('game-menu');
  await expect(menu).toBeVisible();
  await page.waitForTimeout(300); // menu pictures
  await page.screenshot({ path: resolve(test.info().project.outputDir, 'play-menu.png') });
  await menu.getByRole('button', { name: 'Options', exact: true }).click();
  await menu.getByRole('tab', { name: 'Key bindings' }).click();
  await expect(menu.getByRole('button', { name: 'Bind walk forward' })).toContainText('UPARROW');
  await menu.getByRole('button', { name: 'Back', exact: true }).click();
  await menu.getByRole('button', { name: 'Save / Load' }).click();
  await menu.getByRole('radio', { name: 'save1', exact: true }).check();
  await menu.getByRole('button', { name: 'Save', exact: true }).click();
  await expect
    .poll(
      async () => {
        const r = await page.request.get('/api/v1/saves');
        const j = (await r.json()) as { saves: { slot: string }[] | null };
        return (j.saves ?? []).map((x) => x.slot);
      },
      { timeout: 15_000, message: 'save1 stored on the server' },
    )
    .toContain('save1');
  await menu.getByRole('button', { name: 'Back', exact: true }).click();
  await menu.getByRole('button', { name: 'Resume', exact: true }).click();
  await expect(menu).toHaveCount(0);
  d = await debug(page);
  expect(d!.connState).toBe(4);

  expect(d!.errors, 'engine errors').toEqual([]);
  expect(consoleErrors, 'console errors').toEqual([]);
});
