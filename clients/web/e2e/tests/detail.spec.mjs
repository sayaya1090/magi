import { test, expect, open, openDetail } from './fixtures.mjs';

/**
 * 상세가 <b>데몬이 말한 것</b>을 그린다.
 *
 * 사용자가 본 것: 모델과 승인 모드 칸이 비어 있었다. 원인은 화면이 아니라 아래였다 — 가벼운
 * 명단(roster 문)이 그 값들을 나르지 않아서, 화면은 보여 줄 수 없는 값을 바꾸겠다고 내밀고
 * 있었다. 그래서 이 시험은 칸이 <b>차 있는지</b>를 본다: 비어 있으면 그 층이 다시 떨어진 것이다.
 */
test('명단 행이 승인 모드·백엔드·모델을 싣는다', async ({ page }) => {
  await open(page);
  const row = await page.evaluate(async () => {
    const r = await fetch('/fleet');
    const list = await r.json();
    return list[0] || null;
  });
  expect(row, 'a companion in the list').not.toBeNull();
  expect(row.permission, 'permission on the row').toBeTruthy();
  expect(row.model, 'model on the row').toBeTruthy();
  expect(row.backend, 'backend on the row').toBeTruthy();
});

test('상세가 승인 모드와 모델을 채워 그린다', async ({ page }) => {
  await openDetail(page);
  const approval = page.locator('#detail .f', { hasText: /Approval|승인/ }).first();
  await expect(approval).toBeVisible();
  await expect(approval).not.toHaveText(/^\s*(Approval|승인[^\s]*)\s*$/);
  // 이름으로 집는다 — `.first()`는 세션 고르개였고, 그것은 언제나 값이 있어 이 줄이 무엇을
  // 재든 초록이었다(맞는 답이 틀린 근거로 맞고 있던 자리).
  const model = page.locator('#detail md-outlined-select[data-aria-label="Model"]');
  await expect(model).toBeVisible();
  expect(await model.evaluate(el => el.value), 'the model select shows what it is on').not.toBe('');
});

test('상세의 사실들이 데몬의 것과 같다', async ({ page }) => {
  await openDetail(page);
  const text = await page.locator('#detail').innerText();
  const row = await page.evaluate(async () => (await (await fetch('/fleet')).json())[0]);
  expect(text, 'workspace').toContain(row.workdir);
  expect(text, 'pid').toContain(String(row.pid));
  // 세션은 글이 아니라 고르개다 — 이 대화 위에 서 있고, 같은 작업공간의 다른 대화로 건너뛴다.
  const sess = page.locator('#detail md-outlined-select[data-aria-label="Session"]');
  await expect(sess).toHaveJSProperty('value', row.session);
});
