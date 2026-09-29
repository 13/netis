import { test } from '@playwright/test';
import * as path from 'path';
import { NOW } from '../tests/fixtures';

// Writes the images the README and docs/ show. The output directory is
// mounted at /images by `make screenshots`; outside Docker it falls back to
// docs/images in the repository.
const OUT = process.env.SHOTS_DIR || path.join(__dirname, '..', '..', 'docs', 'images');

const desktop = { width: 1440, height: 900 };
const phone = { width: 390, height: 844 };

const SHOTS = [
  { file: 'dashboard-light', path: '/', viewport: desktop, scheme: 'light' },
  { file: 'dashboard-dark', path: '/', viewport: desktop, scheme: 'dark' },
  { file: 'grid-light', path: '/subnets/1', viewport: desktop, scheme: 'light' },
  { file: 'grid-dark', path: '/subnets/1', viewport: desktop, scheme: 'dark' },
  { file: 'devices', path: '/devices', viewport: desktop, scheme: 'light' },
  { file: 'device', path: '/devices/5', viewport: desktop, scheme: 'light' },
  { file: 'phone', path: '/', viewport: phone, scheme: 'light', mobile: true },
] as const;

for (const s of SHOTS) {
  test(s.file, async ({ browser, baseURL }) => {
    const context = await browser.newContext({
      viewport: s.viewport,
      colorScheme: s.scheme,
      isMobile: 'mobile' in s,
      hasTouch: 'mobile' in s,
      // Phones get a 2x image so the narrow shot stays sharp next to the
      // desktop ones; desktop shots are already wide enough at 1x.
      deviceScaleFactor: 'mobile' in s ? 2 : 1,
      locale: 'en-US',
      timezoneId: 'UTC',
      reducedMotion: 'reduce',
    });
    await context.addCookies([{ name: 'netis_session', value: 'e2e-admin-session', url: baseURL! }]);
    const page = await context.newPage();
    await page.clock.setFixedTime(NOW);
    await page.goto(s.path);
    await page.evaluate(() => document.fonts.ready);
    await page.screenshot({ path: path.join(OUT, `${s.file}.png`), animations: 'disabled', caret: 'hide' });
    await context.close();
  });
}
