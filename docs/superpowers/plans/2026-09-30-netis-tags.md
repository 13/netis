# Tags redesign (T-series) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make tags coloured, rounded, clickable pills from a contrast-checked palette, edit them as chips with suggestions, and manage them on a Settings page.

**Architecture:** Palette tokens in `app.css`; one Go resolver (`views.TagColor(name, stored) string`) and one templ component (`TagChip`) used by every page, fed by a `map[name]color` the handlers load once. Store gains tag management (counts, colour, rename with merge, delete). A small progressive-enhancement script turns the tags text input into a chip editor.

**Tech Stack:** Go, templ, htmx, plain JS, Playwright (e2e). Spec: `docs/superpowers/specs/2026-09-30-netis-tags-design.md`.

## Global Constraints

- Palette keys, in order: `slate`, `indigo`, `sky`, `teal`, `violet`, `pink`, `sand`. Stored `tag.color` is a key or `''` (auto). Auto = `palette[fnv32a(strings.ToLower(name)) % 7]`. Any other stored value resolves as auto.
- Tokens `--tag-<key>` and `--tag-<key>-soft`, `light-dark()` values from the spec table as starting points; `--tag-<key>` must reach 4.5:1 on its soft token and on `--surface` in both themes (adjust lightness, keep hue).
- No status hues (green, amber, red) and not the accent blue in the palette. The old `.chip` stays for non-tag labels.
- `.tag`: height 22 px, padding 0 10 px, `border-radius: var(--r-round)`, soft background, token text, 1 px border `color-mix(in srgb, var(--tag-<key>) 22%, transparent)`, `--text-xs`, `--w-medium`, max-width 16rem with ellipsis and `title`.
- Tag names: trimmed, non-empty, at most 64 characters.
- Copy: sentence case, plain words. Existing tokens and classes only, beyond the new tag tokens and `.tag*` classes.
- Every migration exists under the same name in both dialect directories; store tests use `storetest.EachDialect`.
- Commits: conventional prefix, ending with the session's attribution lines. `make generate` before `go test` when `.templ` changes.

## File map

- Create `internal/web/views/tags.go` (palette, `TagColor`, `TagColors` type), `internal/web/views/tags.templ` (`TagChip`, `TagSpan`, `tagList`), `internal/web/views/tags_test.go`.
- Modify `internal/web/static/app.css` (tokens, `.tag*`), `internal/web/contrast_test.go`, `internal/web/views/styleguide.templ`.
- Create `internal/store/migrations/{sqlite,postgres}/0017_tag_colors.sql`; modify `internal/store/meta.go` (`defaultTagColor = ""`, management methods) + tests.
- Modify `internal/web/devices.go`, `internal/web/views/device_list.templ`, `device_detail.templ`, `device_detected.go` (tag chips).
- Create `internal/web/tags.go` (settings handlers), `internal/web/views/settings_tags.templ`; modify `internal/web/server.go` (routes), `internal/web/settings.go` (admin tab), `internal/web/views/settings.templ` (nav), `internal/web/audit.go` (actions).
- Create `internal/web/static/tags.js`; modify `internal/web/views/device_form.templ`, `internal/web/views/layout.templ` (script), `internal/web/static/dialog.js` (A4 buttons use the editor).
- Create `e2e/tests/tags.spec.ts`; modify `e2e/tests/visual.spec.ts` (settings-tags page), baselines; `docs/usage.md`.

---

### Task 1: Palette, resolver, component, migration

**Files:** Create `internal/web/views/tags.go`, `internal/web/views/tags.templ`, `internal/web/views/tags_test.go`, both `0017_tag_colors.sql`. Modify `internal/web/static/app.css`, `internal/web/contrast_test.go`, `internal/web/views/styleguide.templ`, `internal/store/meta.go`, a store test.

**Interfaces (produces):**

```go
package views

// TagPalette is the tag colours, in the order the picker shows them.
var TagPalette = []string{"slate", "indigo", "sky", "teal", "violet", "pink", "sand"}

// TagColors maps a tag name to its stored colour ("" or a palette key).
type TagColors map[string]string

// TagColor resolves the palette key a tag is drawn in: its stored key, or,
// for "" and anything that is not a key, the hue its name hashes to.
func TagColor(name, stored string) string

// IsTagColor reports whether key is a palette key.
func IsTagColor(key string) bool
```

```templ
// TagChip is a tag as a link to the devices that carry it.
templ TagChip(name, stored string)   // <a class="tag tag-<key>" href="/devices?tag=<url-escaped name>" title={ name }>{ name }</a>
// TagSpan is a tag that links nowhere (editor, pickers, previews).
templ TagSpan(name, stored string)   // <span class="tag tag-<key>" title={ name }>{ name }</span>
```

Plus `func (c TagColors) Of(name string) string` returning the stored value (or "").

- [ ] **Step 1: Failing tests** (`tags_test.go`): `TagColor("nas","teal")=="teal"`; `TagColor("nas","")==TagColor("NAS","")` and is in `TagPalette`; `TagColor("nas","#888888")==TagColor("nas","")`; over names `tag0..tag99` every palette key is hit at least once; render `TagChip("media & more","")` and assert the href is `/devices?tag=media+%26+more` (or `%20`, whichever `url.QueryEscape` gives, consistently) and the text is escaped. Store test (`EachDialect`): `SetDeviceTags` on a new tag stores color `''`. Migration test: a tag inserted with `#888888` before migration reads back `''`; a tag with `teal` keeps it (follow the pattern of `internal/store/migration0015_test.go`). Contrast: in `TestTokenContrast` add, for every key, `tag-<key>` on `tag-<key>-soft` and on `surface`, 4.5, both themes.
- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement.**
  - `tags.go`: `fnv.New32a` over `strings.ToLower(name)`.
  - Migration `0017_tag_colors.sql`: `UPDATE tag SET color='' WHERE color NOT IN ('slate','indigo','sky','teal','violet','pink','sand');`
  - `meta.go`: `defaultTagColor = ""` and update its comment.
  - `app.css`: in `:root` after the status tokens, 14 tokens from the spec table; the `.tag` rules from the Global Constraints; `.tag-<key> { --tag: var(--tag-<key>); --tag-soft: var(--tag-<key>-soft); }` so `.tag` uses `var(--tag)`/`var(--tag-soft)`; `a.tag:hover, a.tag:focus-visible { border-color: color-mix(in srgb, var(--tag) 60%, transparent); text-decoration:none; }`; `.tag-list { display:flex; flex-wrap:wrap; gap:var(--sp-1); }`. Make sure `colorTokens()` (the CSS token parser used by contrast and styleguide) picks the new tokens up; adjust any value failing contrast.
  - `styleguide.templ`: a "Tags" section rendering `TagSpan` for every key (label = key) and one auto example.
- [ ] **Step 4: Run** `make generate && go test ./internal/web/... ./internal/store/...` — PASS.
- [ ] **Step 5: Commit** `feat(web): tag palette and tag chip component`.

---

### Task 2: Tags as chips everywhere

**Files:** Modify `internal/web/devices.go` (list, device page), `internal/web/views/device_list.templ`, `internal/web/views/device_detail.templ`, `internal/web/views/device_detected.go` / the detected panel markup, and wherever else a tag name is rendered as text (grep `TagNames`, `.Tags`, `chip">{ t` in `internal/web/views`). Tests in `internal/web/devices_list_test.go`, `internal/web/device_detail_test.go`.

**Interfaces:** Consumes `TagChip`, `TagColors`, `TagColor` (Task 1). The device list page data (`p`) gains `TagColors views.TagColors` (filled from the `ListTags` call already at `devices.go:~107`); `views.DeviceDetail` gains nothing new (its `Tags []store.Tag` carry colours).

Behaviour:
- Device list tags column: `<span class="tag-list">` of `TagChip(name, p.TagColors.Of(name))`.
- When `p.Filter.Tag != ""`, next to the tag select render the active tag as a `TagSpan` followed by a small clear link (`aria-label="Clear tag filter"`, href = current URL without `tag`, built server-side). Keep the select.
- Device page About panel: `tag-list` of `TagChip(t.Name, t.Color)`.
- Detected panel: for a hint with `Field == "tag"`, render the value as `TagSpan(value, colors.Of(value))` (load `ListTags` in the handler for the map).
- Phone widths: the tags column already hides with `.opt`; chips wrap in the device page.

- [ ] **Step 1: Failing tests:** list page with a device tagged `nas` (stored `teal`) contains `class="tag tag-teal"` and `href="/devices?tag=nas"`; `/devices?tag=nas` contains the clear link; device page contains the chip; detected panel with a tag hint contains `class="tag tag-`.
- [ ] **Step 2: Run** — FAIL. **Step 3: Implement.** **Step 4:** `make generate && go test ./internal/web/` — PASS.
- [ ] **Step 5: Commit** `feat(web): show tags as coloured chips that filter the device list`.

---

### Task 3: Tag management — store and Settings, Tags

**Files:** Modify `internal/store/meta.go` (+ `meta_test.go` or new `tags_test.go`), create `internal/web/tags.go`, `internal/web/views/settings_tags.templ`, modify `internal/web/server.go`, `internal/web/settings.go`, `internal/web/views/settings.templ`, `internal/web/audit.go`; tests `internal/web/tags_test.go`.

**Interfaces (produces):**

```go
// store
type TagCount struct {
	Tag
	Devices int
}
func (s *Store) ListTagsWithCounts(ctx context.Context) ([]TagCount, error) // ordered by name
func (s *Store) SetTagColor(ctx context.Context, id int64, color string) error
// RenameTag renames tag id. If another tag already has name, id's devices
// join that tag and id is deleted; mergedInto is that tag's id (0 for a
// plain rename). Renaming to its own name is a no-op.
func (s *Store) RenameTag(ctx context.Context, id int64, name string) (mergedInto int64, err error)
func (s *Store) DeleteTag(ctx context.Context, id int64) error
```

Routes (admin): `GET /settings/tags` (section "tags" in the Admin group, after Notifications), `POST /settings/tags/{id}/color`, `POST /settings/tags/{id}/rename`, `POST /settings/tags/{id}/delete`. Audit actions: `tag.color`, `tag.rename`, `tag.delete` in `auditActions`.

Behaviour:
- Store: all writes in one transaction each. Merge: `INSERT INTO device_tag (device_id, tag_id) SELECT device_id, ? FROM device_tag WHERE tag_id=? ON CONFLICT DO NOTHING`, then delete the old tag (its device_tag rows first if no cascade — check `0001_init.sql`/baseline). Name match for merge is exact (tags are unique by exact name today).
- Page: panel "Tags" with a table (`table-stack` pattern for phones): chip (`TagSpan`), devices (link `/devices?tag=`), colour (radio swatches: "Auto" showing a dot/sample in the resolved hue, then the seven keys; each swatch is a `<label>` with a visually hidden radio and an `aria-label` of the colour name; htmx `hx-post` on change to the color route, `hx-swap="none"` + toast "Colour saved"; a Save button inside `<noscript>`), rename (text input + Save; when the new name exists, the server merges and the toast says "Merged into nas"), delete (button with `data-confirm` using the dialog pattern the device list uses; message "Remove <name> from N devices?").
- Validation: colour must be `""` or a palette key (400 "choose a colour from the list"); name trimmed, 1-64 chars (400 "a tag name is 1 to 64 characters").
- Empty state as in the spec.
- Redirect back to `/settings/tags` after non-htmx posts, with the flash toast pattern used elsewhere.

- [ ] **Step 1: Failing tests:** store (`EachDialect`): counts; set colour; plain rename; merge (device on both tags ends with one row; survivor keeps colour; old tag gone; returns survivor id); delete detaches. Web: page lists tags with counts for admin, 403/redirect for viewer (follow existing admin-route tests); colour post updates; bad colour 400; rename; rename-merge toast; long name 400; delete.
- [ ] **Step 2: Run** — FAIL. **Step 3: Implement.** **Step 4:** `make generate && go vet ./... && go test ./...`; Postgres via `make pg` + `NETIS_TEST_PG_DSN=postgres://netis:netis@127.0.0.1:55432/netis?sslmode=disable go test ./internal/store/...` + `make pg-stop` — PASS.
- [ ] **Step 5: Commit** `feat(web): manage tags in settings: colour, rename, merge, delete`.

---

### Task 4: Chip editor in the device form

**Files:** Create `internal/web/static/tags.js`, `e2e/tests/tags.spec.ts`. Modify `internal/web/views/device_form.templ`, `internal/web/views/layout.templ` (load `tags.js` with the other global scripts), `internal/web/static/dialog.js` (A4 `data-append-tag` goes through the editor when present), `internal/web/static/app.css` or the form's page CSS (`.tag-editor` styles), `internal/web/devices.go` (form gets all tags with colours).

**Interfaces:** `views.DeviceForm` gains `AllTags []store.Tag` (from `ListTags` in `deviceFormLists`). The tags field becomes:

```templ
<div class="field">
	<label for="df-tags">Tags</label>
	<input type="text" id="df-tags" name="tags" value={ tagNamesJoin(f.Tags) } placeholder="media, upstairs" autocomplete="off" data-tag-editor data-tags={ tagsJSON(f.AllTags) }/>
	<span class="hint">Separate tags with commas.</span>
</div>
```

`tagsJSON` renders `[{"name":"nas","color":"teal"}]` with colours already resolved through `TagColor`.

JS behaviour (`tags.js`, plain ES, no dependencies; initialise on `DOMContentLoaded` and on `htmx:afterSwap` for elements with `[data-tag-editor]` not yet initialised):
- Build, after the input: `<div class="tag-editor">` containing chips (`<span class="tag tag-<key>">name<button type="button" class="tag-remove" aria-label="Remove tag name">×</button></span>`) and an `<input type="text" class="tag-editor-input" role="combobox" aria-expanded="false" aria-controls="<id>-list" aria-autocomplete="list" aria-label="Add a tag">`, plus `<ul role="listbox" id="<id>-list" hidden>`. Hide the original input (`hidden`, keep it in the form) and hide the "Separate tags with commas" hint; point the `<label for>` at the new text box.
- Colour of a new chip: the known tag's colour, else the auto hue computed in JS with the same FNV-1a 32-bit over the lower-cased name mod 7 over the same key order (keep the two implementations in sync; the Playwright test checks a known pair).
- Add on Enter, comma, Tab (Tab only when text is non-empty), and on blur with text; split pasted text on commas; trim; skip empty and case-insensitive duplicates; cap 64 chars.
- Backspace on an empty box removes the last chip; the remove button removes its chip and returns focus to the box.
- Suggestions: existing tags whose name starts with (then contains) the typed text, case-insensitive, excluding present ones, max 8; ArrowUp/Down move `aria-activedescendant`, Enter picks the active option, Escape closes, click picks.
- After every change write `chips.join(", ")` into the hidden input.
- Expose `window.netisTags.add(inputEl, name)` for dialog.js; the A4 "Add detected tag" handler calls it when the target input has an editor, else appends text as today.
- CSS: `.tag-editor` looks like a text input (border, radius, padding, min-height `var(--h-md)`, focus-within ring), chips wrap inside; `.tag-remove` is a 16 px round button inheriting the chip colour, visible focus; the listbox is a popover panel (surface, border, `--overlay-shadow`), options show a `TagSpan`-styled chip, active option has `--surface-2` background.

- [ ] **Step 1: Failing tests:** web — the edit form's tags input has `data-tag-editor` and a `data-tags` JSON containing an existing tag with its resolved colour. Playwright `e2e/tests/tags.spec.ts` (runs in the existing e2e setup against the fixture; fixture device 5 is "nas" with tags infra, media): open `/devices/5/edit`; expect chips "infra" and "media"; type "garage" + Enter → chip; type "cam," → chip; Backspace on empty box removes "cam"; click the remove button of "infra" → gone; type "me" → listbox shows "media"? (media already present, so it is excluded — instead type "in" → option "infra" appears, ArrowDown + Enter re-adds it); the hidden input's value is "media, garage, infra"; submit and the device page shows those three chips. Also assert `.tag` of "garage" has the class the auto hash gives (compute expected class in the test with the same FNV function).
- [ ] **Step 2: Run** `go test ./internal/web/` and `make e2e` (the new spec) — FAIL.
- [ ] **Step 3: Implement.** **Step 4:** `make generate && go test ./... && make e2e` — PASS (visual baselines may change for device-edit; that's re-baselined in Task 5 — if `make e2e` fails only on those screenshots, note it and continue).
- [ ] **Step 5: Commit** `feat(web): edit tags as chips with suggestions`.

---

### Task 5: Docs and visual baselines

**Files:** `docs/usage.md` (tags: chips, clicking filters, the editor keys, Settings, Tags: colour, rename to merge, delete), `e2e/tests/visual.spec.ts` (add the `settings-tags` page), `e2e/__screenshots__/**`, `internal/web/testdata/e2e_seed.sql` (give the fixture 4-6 tags across several colours incl. one auto, so screenshots show the palette).

- [ ] **Step 1:** Update the seed and docs; add the page to the visual list.
- [ ] **Step 2:** `make e2e-update`; inspect every changed image (device list, device page, device edit, settings tags, styleguide; nothing else should change); view at least the device list and settings tags in light and dark with the Read tool; `make e2e` — PASS (visual, a11y, tags).
- [ ] **Step 3:** `go test ./...` (docs link checker) — PASS.
- [ ] **Step 4: Commit** `docs: tags; test: re-baseline screenshots for coloured tags`.

## Self-review notes

- Spec coverage: palette + contrast (1), component + everywhere + filter chip (2), settings page with rename/merge/delete/colour + audit (3), chip editor + suggestions + A4 integration + interaction test (4), docs + baselines (5). Autofill untouched by design.
