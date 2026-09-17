import { test, expect, assertStepZeroErrors } from './fixtures.mjs';
import { createBundles } from './registry.mjs';
import { runAssetChecks } from './asset-checks.mjs';

export { expect };

const verifyAssetsOnly = process.env.MAGI_TRANSCRIPT_VERIFY_ASSETS_ONLY === '1';
const targetBundleName = process.env.MAGI_TRANSCRIPT_BUNDLE || null;
const reverseBundles = process.env.MAGI_TRANSCRIPT_REVERSE === '1';
const reverseThemes = process.env.MAGI_TRANSCRIPT_REVERSE_THEMES === '1';

// 1. Asset routing & preflight verification test (§5.8.6 Item 2)
// Registered first unless running a specific single functional bundle without asset verification.
// Uses plainPage fixture (uninstrumented) because it intentionally probes 404 routes.
if (verifyAssetsOnly || !targetBundleName) {
  test('[assets] Asset routing and preflight checks', async ({ plainPage }) => {
    await runAssetChecks(plainPage);
  });
}

// 2. Functional bundle tests (§5.8.6 Item 2)
// 1 bundle = 1 Playwright test, scenarios = test.steps sequentially sharing the same page context.
if (!verifyAssetsOnly) {
  let bundles = createBundles({ reverseThemes });

  if (targetBundleName) {
    bundles = bundles.filter((b) => b.name === targetBundleName);
  } else if (reverseBundles) {
    bundles = [...bundles].reverse();
  }

  for (const bundle of bundles) {
    test(`[bundle:${bundle.name}] ${bundle.description}`, async ({ page }) => {
      for (const scenario of bundle.scenarios) {
        await test.step(`[${scenario.id}] ${scenario.name}`, async () => {
          await scenario.run(page);
          assertStepZeroErrors(page, scenario.id);
        });
      }
    });
  }
}
