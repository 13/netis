import { test, expect, PAGES } from './fixtures';

// One full-page screenshot per page, viewport and theme (the projects in
// playwright.config.ts). A style change that moves, hides or recolours
// anything on these pages fails here; if the change is intended, regenerate
// the baselines with `make e2e-update` and review the new images.
for (const p of PAGES) {
  test(`${p.name} looks as before`, async ({ open }) => {
    const page = await open(p.path, p.anon);
    await expect(page).toHaveScreenshot(`${p.name}.png`, { fullPage: true });
  });
}
