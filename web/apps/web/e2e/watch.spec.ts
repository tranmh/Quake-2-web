// Full stack, bots (docs/plans/0002-ai-agent.md H.6): register → /bots → start a scripted bot on demo1 →
// /watch/[id] reaches ca_active on the relay's attractloop stream, the bot's server frames advance, the AI
// overlay shows a mode and probability bars → stop the bot → /bots/[id] replays the recorded demo with the
// overlay rebuilt from the decision trace. No console errors anywhere.
//
// The Go server must run bots for users (Q2_BOTS_ENABLED=1, Q2_BOTS_ALLOW_USERS=1); otherwise the test
// skips, unless Q2_E2E_REQUIRE=1 makes that a failure. Screenshots: test-results/watch-*.png.
import { resolve } from 'node:path';
import { expect, test, type Page } from '@playwright/test';

test.skip(!!process.env['Q2_E2E_SKIP'], process.env['Q2_E2E_SKIP'] ?? '');

interface Debug {
  mode: string;
  phase: string;
  connState: number;
  frames: number;
  errors: string[];
  serverFrame(): number;
  attractloop(): boolean;
  demoplaying(): boolean;
  replayLevel(): number;
}

const debug = (page: Page) =>
  page.evaluate(() => {
    const d = (window as unknown as { __q2web?: Debug }).__q2web;
    return d
      ? {
          mode: d.mode,
          phase: d.phase,
          connState: d.connState,
          frames: d.frames,
          errors: d.errors.slice(),
          serverFrame: d.serverFrame(),
          attractloop: d.attractloop(),
          demoplaying: d.demoplaying(),
          replayLevel: d.replayLevel(),
        }
      : null;
  });

/** The bots API refuses this server or this user: skip (or fail with Q2_E2E_REQUIRE=1). */
function unavailable(reason: string): void {
  if (process.env['Q2_E2E_REQUIRE'] === '1') throw new Error(`bots unavailable: ${reason}`);
  test.skip(true, `bots unavailable: ${reason}`);
}

async function overlayFilled(page: Page, timeout: number) {
  const overlay = page.getByTestId('ai-overlay');
  await expect(overlay).toBeVisible({ timeout });
  await expect(overlay.getByTestId('ai-mode')).toHaveAttribute('data-mode', /\w+/, { timeout });
  await expect
    .poll(() => overlay.getByTestId('prob-bar').count(), { timeout, message: 'probability bars' })
    .toBeGreaterThan(0);
}

test('start a scripted bot, watch it live, stop it, replay it', async ({ page }) => {
  test.setTimeout(300_000);
  const consoleErrors: string[] = [];
  page.on('console', (m) => {
    if (m.type() === 'error') consoleErrors.push(m.text());
  });
  page.on('pageerror', (e) => consoleErrors.push(String(e)));

  // ---- register
  await page.goto('/register');
  const email = `e2e-bot-${Date.now()}-${Math.floor(Math.random() * 1e6)}@example.com`;
  await page.locator('input[name=email]').fill(email);
  await page.locator('input[name=displayName]').fill('e2e');
  await page.locator('input[name=password]').fill('password123');
  await page.getByRole('button', { name: 'Register' }).click();
  await page.waitForURL('**/servers');

  const probe = await page.request.get('/api/v1/bots');
  if (probe.status() === 503) return unavailable('Q2_BOTS_ENABLED is off');
  expect(probe.status()).toBe(200);

  // ---- start a scripted bot on demo1
  await page.goto('/bots');
  await expect(page.getByRole('link', { name: 'Bots', exact: true })).toBeVisible();
  const form = page.getByRole('form', { name: 'Start a bot' });
  await expect(form).toBeVisible();
  await form.locator('select[name=backend]').selectOption('scripted');
  await form.locator('select[name=maps]').selectOption('1'); // demo1 only
  await form.locator('input[name=name]').fill('e2e watch');
  await form.getByRole('button', { name: 'Start bot' }).click();
  const started = await Promise.race([
    page.waitForURL('**/watch/**', { timeout: 30_000 }).then(() => 'watch' as const),
    form
      .getByRole('alert')
      .waitFor({ timeout: 30_000 })
      .then(() => 'error' as const),
  ]);
  if (started === 'error') {
    const msg = (await form.getByRole('alert').textContent()) ?? '';
    if (/not allowed|unavailable/i.test(msg)) return unavailable(msg);
    throw new Error(`starting the bot failed: ${msg}`);
  }
  const botId = decodeURIComponent(new URL(page.url()).pathname.split('/').pop()!);

  // ---- watch: the relay stream is an attractloop, the bot's frames advance, the overlay fills
  await page.waitForFunction(
    () => {
      const d = (window as unknown as { __q2web?: Debug }).__q2web;
      return !!d && ((d.connState === 4 && d.attractloop()) || d.phase === 'error' || d.phase === 'ended');
    },
    null,
    { timeout: 120_000 },
  );
  let d = await debug(page);
  expect(d?.mode).toBe('watch');
  expect(d?.phase, `engine errors: ${d?.errors.join(' | ')}`).toBe('active');
  await expect(page.getByTestId('live-badge')).toContainText('LIVE');
  await expect(page.getByTestId('game-menu')).toHaveCount(0);
  const sf0 = d!.serverFrame;
  await page.waitForTimeout(3000);
  d = await debug(page);
  console.log(`watch: server frame ${sf0} -> ${d!.serverFrame} in 3 s, ${d!.frames} frames rendered`);
  expect(d!.serverFrame, 'the bot keeps playing (10 server frames/s)').toBeGreaterThan(sf0 + 10);
  expect(d!.connState).toBe(4);
  await overlayFilled(page, 30_000);
  // the viewer has no controls: no pointer lock prompt, no in-game menu even after a click
  await page.getByTestId('game-canvas').click();
  await expect(page.getByTestId('game-menu')).toHaveCount(0);
  expect(await page.evaluate(() => document.pointerLockElement)).toBeNull();
  await page.screenshot({ path: resolve(test.info().project.outputDir, 'watch-live.png') });

  // ---- stop it from its page
  await page.goto(`/bots/${encodeURIComponent(botId)}`);
  await page.getByRole('button', { name: 'Stop', exact: true }).click();
  await expect(page.getByTestId('bot-status')).toHaveText(/stopped|finished|failed/, { timeout: 60_000 });

  // ---- replay: the recorded demo plays, frames advance, the overlay fills from the trace
  const play = page.getByRole('button', { name: 'Play replay' });
  await expect(play).toBeVisible({ timeout: 60_000 });
  await play.click();
  await expect(page.getByTestId('replay-player')).toBeVisible();
  await page.waitForFunction(
    () => {
      const d = (window as unknown as { __q2web?: Debug }).__q2web;
      return !!d && d.mode === 'replay' && ((d.demoplaying() && d.connState === 4) || d.phase === 'error');
    },
    null,
    { timeout: 120_000 },
  );
  d = await debug(page);
  expect(d?.phase, `engine errors: ${d?.errors.join(' | ')}`).toBe('active');
  expect(d!.demoplaying).toBe(true);
  expect(d!.attractloop).toBe(true);
  expect(d!.replayLevel).toBe(0);
  const r0 = d!.serverFrame;
  await page.waitForTimeout(2000);
  d = await debug(page);
  console.log(`replay: server frame ${r0} -> ${d!.serverFrame} in 2 s`);
  expect(d!.serverFrame, 'demo frames advance').toBeGreaterThan(r0);
  await overlayFilled(page, 60_000);
  await page.screenshot({ path: resolve(test.info().project.outputDir, 'watch-replay.png') });

  d = await debug(page);
  expect(d!.errors, 'engine errors').toEqual([]);
  expect(consoleErrors, 'console errors').toEqual([]);
});
