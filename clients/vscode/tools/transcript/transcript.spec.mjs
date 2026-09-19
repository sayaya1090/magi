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
      // ⚠ **한 묶음이 한 test 라는 뜻은, 30초 기본 제한이 묶음 하나가 하는 일 전부의 예산이라는 것이다.**
      // a11y 묶음은 axe-core 감사를 42번 돈다(7 화면 × 3 테마 × 2 뷰포트). 실측 2026-09-19, 이 기계:
      // 34번째까지 36초가 걸려 제한에서 죽었다 — **위반은 0인데** 레인은 빨강이었다. 그런 빨강은
      // 무엇도 가르치지 않고, 빨강을 정상으로 만든다.
      //
      // 그래서 예산이 묶음의 크기를 따라간다. 숫자를 기계에 맞춰 올리는 것이 아니라, 하는 일에 맞춘다.
      test.setTimeout(30_000 + bundle.scenarios.length * 15_000);
      for (const scenario of bundle.scenarios) {
        await test.step(`[${scenario.id}] ${scenario.name}`, async () => {
          await scenario.run(page);
          assertStepZeroErrors(page, scenario.id);
        });
      }
    });
  }
}
