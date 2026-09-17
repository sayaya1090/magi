import { test, expect } from './fixtures.mjs';
import { markdownScenario } from './scenarios/markdown.mjs';

export { expect };

test(`[${markdownScenario.id}] ${markdownScenario.name}`, async ({ page }) => {
  await markdownScenario.run(page);
});
