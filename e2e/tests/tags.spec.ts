import { test, expect } from './fixtures';

// "garage" hashes to the "indigo" palette key under both static/tags.js's
// autoColor and Go's views.TagColor (FNV-1a 32-bit over the lower-cased
// name, mod 7) — pinned as a literal here and in
// internal/web/views/tags_test.go's TestTagColorPinned, so the two staying
// in step is actually checked rather than compared against a JS copy of the
// Go function.
const GARAGE_COLOR = 'tag-indigo';

// The chip editor on the device form (static/tags.js). This spec saves the
// device, so it runs in the "interaction" project against its own fixture
// server (playwright.config.ts) and never changes what the screenshots see.
test('tags are edited as chips with suggestions', async ({ open }) => {
  const page = await open('/devices/5/edit');
  const box = page.getByRole('combobox', { name: 'Add a tag' });
  // Direct children only: the suggestion listbox lives inside the editor too
  // and its options are also .tag chips.
  const chips = page.locator('.tag-editor > .tag');
  const hidden = page.locator('#df-tags');

  await expect(hidden).toBeHidden();
  await expect(page.getByText('Separate tags with commas.')).toBeHidden();
  await expect(chips).toHaveText(['infra', 'media']);

  await box.fill('garage');
  await box.press('Enter');
  await expect(chips).toHaveText(['infra', 'media', 'garage']);
  await expect(page.locator('.tag-editor > .tag', { hasText: 'garage' })).toHaveClass(new RegExp(`\\b${GARAGE_COLOR}\\b`));

  await box.pressSequentially('cam,');
  await expect(chips).toHaveText(['infra', 'media', 'garage', 'cam']);
  await expect(box).toHaveValue('');

  await box.press('Backspace');
  await expect(chips).toHaveText(['infra', 'media', 'garage']);

  await page.getByRole('button', { name: 'Remove tag infra' }).click();
  await expect(chips).toHaveText(['media', 'garage']);
  await expect(box).toBeFocused();

  // "media" is on the device already, so typing "me" suggests nothing.
  const list = page.getByRole('listbox', { name: 'Existing tags' });
  await box.pressSequentially('me');
  await expect(list.getByRole('option')).toHaveCount(0);
  await box.fill('');

  await box.pressSequentially('in');
  await expect(list).toBeVisible();
  await expect(box).toHaveAttribute('aria-expanded', 'true');
  await expect(list.getByRole('option')).toHaveText(['infra']);
  await box.press('ArrowDown');
  const active = await box.getAttribute('aria-activedescendant');
  expect(active).toBeTruthy();
  await expect(page.locator(`#${active}`)).toHaveText('infra');
  await box.press('Enter');
  await expect(chips).toHaveText(['media', 'garage', 'infra']);
  await expect(list).toBeHidden();
  await expect(box).toHaveAttribute('aria-expanded', 'false');

  await expect(hidden).toHaveValue('media, garage, infra');

  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page).toHaveURL(/\/devices\/5$/);
  const saved = page.locator('dd .tag-list .tag');
  await expect(saved).toHaveText(['garage', 'infra', 'media']);
  // Go-rendered chip, same literal colour as the JS editor chip above: the
  // two sides of the hash agree without comparing against a JS copy of the
  // Go function.
  await expect(saved.filter({ hasText: 'garage' })).toHaveClass(new RegExp(`\\b${GARAGE_COLOR}\\b`));
});

// The form in a dialog gets the editor too, and Escape with suggestions open
// closes them, not the dialog.
test('the edit dialog edits tags as chips', async ({ open }) => {
  const page = await open('/devices/1');
  await page.getByRole('link', { name: 'Edit' }).first().click();
  const dialog = page.locator('#modal dialog');
  await expect(dialog).toBeVisible();
  await dialog.locator('summary').filter({ hasText: 'Vendor, model' }).evaluate((s) => {
    (s.parentElement as HTMLDetailsElement).open = true;
  });
  const box = dialog.getByRole('combobox', { name: 'Add a tag' });
  await expect(dialog.locator('.tag-editor > .tag')).toHaveText(['infra']);
  await box.pressSequentially('me');
  const list = dialog.getByRole('listbox', { name: 'Existing tags' });
  await expect(list.getByRole('option')).toHaveText(['media']);
  await box.press('Escape');
  await expect(list).toBeHidden();
  await expect(dialog).toBeVisible();
  await box.press('Escape');
  await expect(dialog).toBeHidden();
});
