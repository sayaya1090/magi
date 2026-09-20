import assert from 'node:assert/strict';
import { layoutBundle } from './scenarios/layout.mjs';
import { asksBundle } from './scenarios/asks.mjs';
import { autocompleteBundle } from './scenarios/autocomplete.mjs';
import { diffBundle } from './scenarios/diff.mjs';
import { createA11yBundle } from './scenarios/a11y.mjs';
import { markdownBundle } from './scenarios/markdown.mjs';

export const EXPECTED_BUNDLE_COUNTS = {
  layout: 7,
  asks: 14,
  autocomplete: 2,
  diff: 13,
  a11y: 8,
  markdown: 1,
};

export const TOTAL_EXPECTED_SCENARIOS = 45;

/**
 * Validates bundle registry invariants (§5.8.6):
 * 1. Exactly 6 bundles in expected order: layout, asks, autocomplete, diff, a11y, markdown
 * 2. Exact scenario count per bundle (7, 14, 2, 13, 8, 1 = 45)
 * 3. Zero duplicate scenario IDs
 * 4. Zero missing scenario IDs
 *
 * @param {Array<object>} bundles
 * @returns {Array<object>} the validated bundles
 */
export function validateBundleRegistry(bundles) {
  assert.ok(Array.isArray(bundles), 'bundles must be an array');
  assert.equal(bundles.length, 6, 'Must contain exactly 6 bundles');

  const bundleNames = bundles.map((b) => b.name);
  assert.deepEqual(
    bundleNames,
    ['layout', 'asks', 'autocomplete', 'diff', 'a11y', 'markdown'],
    'Bundles must be defined in exact expected order'
  );

  const seenIds = new Set();
  let totalCount = 0;

  for (const bundle of bundles) {
    const expected = EXPECTED_BUNDLE_COUNTS[bundle.name];
    assert.ok(expected !== undefined, `Unknown bundle name: ${bundle.name}`);
    assert.equal(
      bundle.scenarios.length,
      expected,
      `Bundle ${bundle.name} must have exactly ${expected} scenarios (got ${bundle.scenarios.length})`
    );

    for (const sc of bundle.scenarios) {
      assert.ok(sc.id, `Scenario in bundle ${bundle.name} missing id`);
      assert.ok(sc.name, `Scenario ${sc.id} missing name`);
      assert.equal(typeof sc.run, 'function', `Scenario ${sc.id} missing run function`);
      assert.equal(
        seenIds.has(sc.id),
        false,
        `Duplicate scenario ID detected: ${sc.id}`
      );
      seenIds.add(sc.id);
      totalCount++;
    }
  }

  assert.equal(
    totalCount,
    TOTAL_EXPECTED_SCENARIOS,
    `Total scenario count must be exactly ${TOTAL_EXPECTED_SCENARIOS} (got ${totalCount})`
  );

  return bundles;
}

/**
 * Creates a fresh, isolated registry of bundles and scenarios (§5.8.6).
 * Does not read or mutate any CLI global state.
 *
 * @param {object} [options]
 * @param {boolean} [options.reverseThemes] Whether accessibility themes should be audited in reverse order
 * @returns {Array<object>} 6 bundles with 40 scenarios
 */
export function createBundles(options = {}) {
  const { reverseThemes = false } = options;

  const bundles = [
    {
      name: layoutBundle.name,
      description: layoutBundle.description,
      scenarios: [...layoutBundle.scenarios],
    },
    {
      name: asksBundle.name,
      description: asksBundle.description,
      scenarios: [...asksBundle.scenarios],
    },
    {
      name: autocompleteBundle.name,
      description: autocompleteBundle.description,
      scenarios: [...autocompleteBundle.scenarios],
    },
    {
      name: diffBundle.name,
      description: diffBundle.description,
      scenarios: [...diffBundle.scenarios],
    },
    createA11yBundle({ reverseThemes }),
    {
      name: markdownBundle.name,
      description: markdownBundle.description,
      scenarios: [...markdownBundle.scenarios],
    },
  ];

  return validateBundleRegistry(bundles);
}
