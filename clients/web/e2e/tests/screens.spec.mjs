import { test, expect, open, screen } from './fixtures.mjs';

/**
 * 문마다 화면이 실제로 선다.
 *
 * 카탈로그의 규칙이 "눌러서 빈 화면에 닿는 문은 없는 문보다 나쁘다"이므로, 이 시험이 재는 것은
 * 각 문이 <b>무엇인가를</b> 그리는가이다 — 내용의 옳고 그름이 아니라, 문이 살아 있는가.
 */
const screens = ['fleet', 'skills', 'meet', 'settings', 'board', 'map', 'access'];

for (const id of screens) {
  test(`${id} 화면이 선다`, async ({ page }) => {
    const errors = [];
    page.on('pageerror', e => errors.push(String(e).slice(0, 200)));
    await screen(page, id);
    // 셸은 언제나 서고, 그 안에 그 화면의 판이 든다.
    await expect(page.locator('#frame')).toBeVisible();
    await expect(page.locator('#frame')).not.toBeEmpty({ timeout: 20_000 });
    expect(errors, `${id} threw while drawing`).toEqual([]);
  });
}

test('레일이 모든 문을 내놓고, 누르면 주소가 바뀐다', async ({ page }) => {
  await open(page);
  const links = page.locator('#railNav a[href*="v="], #railFoot a[href*="v="]');
  expect(await links.count(), 'doors in the rail').toBeGreaterThanOrEqual(3);
  const href = await links.first().getAttribute('href');
  await links.first().click();
  await expect(page).toHaveURL(new RegExp(href.replace(/[.*+?^${}()|[\]\\]/g, '\\$&').replace(/^\//, '')));
});
