import { defineConfig } from '@playwright/test';

// The fixture server is the Go test binary built by `make e2e`: a fixed data
// set at a frozen clock (internal/web/e2e_fixture_test.go). Screenshots are
// only comparable when taken in the pinned Playwright image, which CI and
// `make e2e` both use.
export const PORT = 18600;
// The interaction specs save data, so they get a fixture server of their
// own: the screenshots and axe checks never see what they change.
const INTERACTION_PORT = 18601;

// Specs that change the fixture's data; they run only in the interaction
// project.
const INTERACTION = /tags\.spec\.ts/;

const desktop = { width: 1440, height: 900 };
const phone = { width: 390, height: 844 };

export default defineConfig({
  testDir: './tests',
  snapshotPathTemplate: '{testDir}/../__screenshots__/{projectName}/{arg}{ext}',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: [['list'], ['html', { open: 'never' }]],
  expect: {
    // Full-page shots of the longer pages take a while with every worker busy.
    timeout: 20_000,
    toHaveScreenshot: {
      animations: 'disabled',
      caret: 'hide',
      scale: 'css',
      // A few anti-aliased pixels may differ; a moved or restyled element
      // changes far more than this.
      maxDiffPixelRatio: 0.002,
    },
  },
  use: {
    baseURL: `http://127.0.0.1:${PORT}`,
    locale: 'en-US',
    timezoneId: 'UTC',
    reducedMotion: 'reduce',
    trace: 'retain-on-failure',
  },
  projects: [
    { name: 'desktop-light', testIgnore: INTERACTION, use: { viewport: desktop, colorScheme: 'light' } },
    { name: 'desktop-dark', testIgnore: INTERACTION, use: { viewport: desktop, colorScheme: 'dark' } },
    { name: 'phone-light', testIgnore: INTERACTION, use: { viewport: phone, colorScheme: 'light', isMobile: true, hasTouch: true } },
    { name: 'phone-dark', testIgnore: INTERACTION, use: { viewport: phone, colorScheme: 'dark', isMobile: true, hasTouch: true } },
    {
      name: 'interaction',
      testMatch: INTERACTION,
      use: { viewport: desktop, colorScheme: 'light', baseURL: `http://127.0.0.1:${INTERACTION_PORT}` },
    },
  ],
  webServer: [fixtureServer(PORT), fixtureServer(INTERACTION_PORT)],
});

// fixtureServer runs the fixture on port, with the data as seeded.
export function fixtureServer(port: number) {
  return {
    command: `./.bin/netis-e2e -test.run '^TestE2EServe$' -test.timeout 0`,
    url: `http://127.0.0.1:${port}/healthz`,
    env: { TZ: 'UTC', NETIS_E2E_ADDR: `127.0.0.1:${port}` },
    reuseExistingServer: false,
    timeout: 30_000,
  };
}
