import { defineConfig } from '@playwright/test';
import base from './playwright.config';

// The README and docs/ screenshots (`make screenshots`), taken from the same
// fixture server and pinned image as the visual regression suite, so the
// data is fictional and the images come out the same on every run. Each
// shot picks its own viewport and theme, so there is a single project.
export default defineConfig({
  testDir: './screenshots',
  fullyParallel: true,
  retries: 0,
  reporter: [['list']],
  use: { ...base.use, trace: 'off' },
  webServer: base.webServer,
});
