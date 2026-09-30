import { test as base, expect, Page } from '@playwright/test';

// The instant the fixture server's clock is frozen at
// (internal/web/e2e_fixture_test.go); the browser clock is frozen there too,
// so time.js renders the same relative times as the server.
export const NOW = new Date('2026-09-15T12:00:00Z');

// The admin session the fixture creates.
const SESSION = 'e2e-admin-session';

// The pages under test. A page opens signed in unless anon is set.
export const PAGES = [
  { name: 'dashboard', path: '/' },
  { name: 'devices', path: '/devices' },
  { name: 'device', path: '/devices/5' },
  { name: 'device-edit', path: '/devices/5/edit' },
  { name: 'grid', path: '/subnets/1' },
  { name: 'events', path: '/events' },
  { name: 'settings-network', path: '/settings/network' },
  { name: 'settings-tags', path: '/settings/tags' },
  { name: 'login', path: '/login', anon: true },
];

export const test = base.extend<{ open: (path: string, anon?: boolean) => Promise<Page> }>({
  open: async ({ page, context, baseURL }, use) => {
    await use(async (path, anon = false) => {
      await page.clock.setFixedTime(NOW);
      if (!anon) {
        await context.addCookies([{ name: 'netis_session', value: SESSION, url: baseURL! }]);
      }
      await page.goto(path);
      await page.evaluate(() => document.fonts.ready);
      return page;
    });
  },
});

export { expect };
