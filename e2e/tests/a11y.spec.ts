import AxeBuilder from '@axe-core/playwright';
import { test, expect, PAGES } from './fixtures';

// An accessibility smoke test: axe finds no serious or critical violation on
// any page, in either theme or viewport.
for (const p of PAGES) {
  test(`${p.name} has no serious accessibility violations`, async ({ open }) => {
    const page = await open(p.path, p.anon);
    const { violations } = await new AxeBuilder({ page }).analyze();
    const bad = violations
      .filter((v) => v.impact === 'serious' || v.impact === 'critical')
      .map((v) => `${v.impact}: ${v.id} (${v.help})\n  ${v.nodes.map((n) => n.target.join(' ')).join('\n  ')}`);
    expect(bad, bad.join('\n')).toEqual([]);
  });
}
