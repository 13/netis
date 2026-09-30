# netis tags redesign (T-series): design

Date: 2026-09-30. Status: planned in
`docs/superpowers/plans/2026-09-30-netis-tags.md`.

## Problem

Tags look like every other label: a grey `.chip` (4 px radius, muted text),
the same component as kind, lease and role labels. A tag's stored `color` is
never shown; every tag created by the UI gets `#888888`. Editing tags means
typing a comma list into a text field, and there is nowhere to rename,
recolour or delete a tag. Tags are the one thing people use to group their
own devices, and they read as the least important text on the page.

## Goals

- Tags look like tags: modern, rounded, coloured, instantly scannable, and
  clearly not a status.
- Colour means something the person chose, or at least stays stable for a
  name, and every colour is readable in both themes.
- Adding and removing tags is direct: chips with a remove button and
  suggestions, not a comma list.
- One place to rename, recolour, merge and delete tags.
- Clicking a tag shows the devices that carry it.

Non-goals: arbitrary hex colours, tag icons or emoji, nested tags, tag
descriptions, per-user tags, API changes (the API keeps `tags: [names]`).

## Approaches considered

1. **Tinted pill from a fixed palette (chosen).** Soft tinted background,
   darker text of the same hue, full radius. Each palette entry is a pair of
   tokens with light and dark values, checked for contrast in
   `contrast_test.go`. Colour "auto" picks a hue from a hash of the name, so
   an unconfigured tag still gets a stable colour.
2. Neutral pill with a coloured dot (Linear style). Rejected: a leading dot
   is exactly what a status LED looks like here (`.status::before`), and the
   design brief keeps status colour for status only.
3. Free colour picker (GitHub labels). Rejected: contrast cannot be
   guaranteed in both themes, and the palette keeps the UI calm.

## Palette

Seven hues, none of them the status hues (green online, amber activity, red
fault) or the accent blue used for links and focus:

| Key | Light fg / soft | Dark fg / soft |
|---|---|---|
| slate | #4A5563 / #EDF0F3 | #B8C1CC / #2A3139 |
| indigo | #4338CA / #EDEEFC | #A9B4FB / #272B4C |
| sky | #0B6A9E / #E2F1FA | #7CCDF5 / #16303F |
| teal | #0F7066 / #E1F4F1 | #62E0CC / #15332F |
| violet | #6B2FD0 / #F0EAFD | #C3B3FB / #2E2647 |
| pink | #B3175A / #FCE7F0 | #F5A6CD / #3C2231 |
| sand | #85562A / #F5EDE3 | #DDB78A / #352B20 |

Tokens: `--tag-<key>` (text) and `--tag-<key>-soft` (background) in
`app.css` `:root`, `light-dark()` like every other colour token. Values are
starting points: each `--tag-<key>` must reach 4.5:1 on its own soft
background and on `--surface`, in both themes; the implementer adjusts a
value that fails and keeps the hue.

Stored value: `tag.color` holds a palette key or `''` (auto). Auto resolves
to `palette[fnv32a(lower(name)) % 7]`. Migration `0017_tag_colors.sql` (both
dialects) sets every value that is not a palette key to `''`; the store's
default for new tags becomes `''`.

## Component

`TagChip(name, color string)` in `internal/web/views/tags.templ`:

```html
<a class="tag tag-teal" href="/devices?tag=nas">nas</a>
```

- `.tag`: inline-flex, height 22 px, padding 0 10 px, `border-radius:
  var(--r-round)`, background `--tag-<key>-soft`, text `--tag-<key>`, 1 px
  border of the text colour at 22 % (`color-mix(in srgb, var(--tag-x) 22%,
  transparent)`), `--text-xs`, `--w-medium`, `max-width: 16rem` with
  ellipsis and the full name in `title`.
- Link hover and focus: border at 60 %, no underline; focus ring as every
  other control.
- A non-link form (`<span class="tag …">`) for places where a link makes no
  sense (the tag editor).
- A `.tag-list` wrapper: flex, wrap, gap `--sp-1`.
- The old `.chip` stays for kind, lease and role labels.

## Where tags appear

- Device list, tags column: chips, links to the filtered list. When the
  list is filtered by a tag, the filter bar shows that tag as a chip with a
  clear button next to the select.
- Device page, About panel: chips.
- "What netis detected" panel: a tag hint's value is shown as a chip.
- Styleguide: a Tags section with every palette colour and "auto".
- Everywhere colours come from one lookup: the handler loads `ListTags`
  once and passes a `map[name]color` (`views.TagColors`) to the view.

## Tag editor in the device form

Progressive enhancement of the existing `tags` text input (still the
fallback without JavaScript, and still what the form posts):

- JS (`static/tags.js`) hides the text input and renders a field of chips,
  each with a remove button (`aria-label="Remove tag nas"`), followed by a
  text box.
- Enter, comma or Tab adds the typed text (trimmed, de-duplicated
  case-insensitively against present chips). Backspace in an empty box
  removes the last chip. Pasting "a, b" adds both.
- Suggestions: a listbox under the box with existing tags (coloured chips)
  that match the typed prefix, arrow keys to move, Enter to pick, Escape to
  close; `role="combobox"`, `aria-expanded`, `aria-activedescendant`.
  Existing tags and their colours come from a `data-tags` JSON attribute on
  the field.
- Every change writes the chips back into the hidden text input as a comma
  list.
- The A4 "Add detected tag" buttons add a chip through the same code.
- The bulk "Add tag" box in the device list keeps its datalist.

## Tags settings page

Settings, Admin, **Tags** (`/settings/tags`, admin only):

- A row per tag: the chip, the number of devices (a link to the filtered
  list), a colour picker, rename, delete.
- Colour picker: a radio group of swatches, "Auto" first (showing the hue
  it resolves to), then the seven hues; saves on change (htmx) and falls
  back to a Save button without JavaScript.
- Rename: an inline form. Renaming to a name another tag already has merges
  them: every device of the renamed tag is attached to the existing tag,
  and the renamed tag is deleted; the existing tag keeps its colour. The form says so before
  saving ("nas already exists; its 3 devices and these 2 will share it").
- Delete: confirm ("Remove media from 4 devices?"), then detach and delete.
- Empty state: "No tags yet. Add tags to devices from their edit form or
  the device list."
- Audit actions: `tag.color`, `tag.rename`, `tag.delete`.

Autofill needs no change: a deleted or renamed tag leaves its autofill
records without a matching device tag, so the next pass marks them owned
and never re-adds the old name.

## Store

- `ListTagsWithCounts(ctx) ([]TagCount, error)` (`Tag` + `Devices int`).
- `SetTagColor(ctx, id, color string) error` (web validates the key).
- `RenameTag(ctx, id, name string) (mergedInto int64, err error)`: one
  transaction; a plain rename when the name is free, otherwise the merge
  above; renaming to the same name is a no-op.
- `DeleteTag(ctx, id) error` (device_tag rows cascade or are deleted first).
- Tag name rules as today: trimmed, non-empty; max 64 characters (new,
  enforced in the web layer and the tag editor).

## Testing

- Contrast: every `--tag-<key>` on `--tag-<key>-soft` and on `--surface`,
  4.5:1, both themes.
- Colour resolution: auto is stable for a name, case-insensitive, and
  covers the palette; unknown stored values resolve as auto.
- Store (both dialects): counts, set colour, rename, rename-merge (devices
  de-duplicated, colour of the survivor kept), delete, migration.
- Web: chips render with the right class and link in list, device page and
  detected panel; filter chip; settings page renders, recolours, renames,
  merges, deletes, rejects bad keys and long names, admin only.
- JS: the editor's contract is covered by a Playwright interaction test
  (add by Enter and comma, remove, backspace, suggestion pick, the posted
  value) in `e2e/tests/tags.spec.ts`, the first interaction test in the
  suite.
- Visual: screenshots re-baselined (device list, device page, device edit,
  settings tags, styleguide), axe clean.
