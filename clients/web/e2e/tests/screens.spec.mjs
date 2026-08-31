import { test, expect, open, screen } from './fixtures.mjs';

/**
 * 문마다 화면이 실제로 선다 — <b>합쳐 놓은 한 벌</b>에서.
 *
 * 화면이 무엇을 그리는지는 모듈마다 제 코테스트가 잰다(GwtTestSpec, 목을 상대로, 훨씬 촘촘하게).
 * 그것들이 못 재는 자리가 여기다: 그 시험들은 저마다 제 모듈의 시험 페이지를 세우고, 사람이 여는
 * 것은 <b>조립된 콘솔 한 벌</b>이다. 한 모듈이 저 혼자서는 서면서 합친 번들에서는 안 서는 일이
 * 있고(중복 심볼, 늦게 오는 스크립트, 옆 모듈이 물고 온 스타일), 그때 사람이 보는 것은 빈 화면이다.
 *
 * 그래서 여기서 재는 것은 내용의 옳고 그름이 아니라 <b>문이 살아 있는가</b>뿐이다 — 내용을 여기서
 * 또 재면 모듈의 시험을 두 번 쓰는 것이고, 두 벌은 갈린다.
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
