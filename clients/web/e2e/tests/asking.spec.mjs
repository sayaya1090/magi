import { test, expect, open, openDetail } from './fixtures.mjs';

/**
 * 열어 두기만 해도 나가던 물음들.
 *
 * 상세를 열면 /jobs와 /cron이 스물두 번 나갔다(사용자 실측). 옆 판이 세 카드를 명단 프레임마다
 * 다시 물었고, 명단은 초당 여러 번 흐른다. 카드마다 "답이 같으면 다시 그리지 않는다"는 검사가
 * 있어 화면은 조용했다 — 그래서 그리기만 보는 시험은 이것을 영원히 못 본다. 이 파일이 있는
 * 이유가 그것이다.
 */
test('열어 둔 상세는 잡·크론·인계를 제 시계로만 다시 묻는다', async ({ page, asked }) => {
  const socket = await openDetail(page);
  asked.forget();
  // 명단을 실제로 흔든다. 한가한 컴패니언 하나짜리 기계에서는 행이 거의 안 바뀌어서 「명단마다
  // 다시 묻기」와 「제 시계로 묻기」가 같은 수를 낸다 — 규칙을 걷어내도 초록이 나오는 자리다
  // (실측). 승인 모드는 명단 행이 나르는 값이라, 이것을 바꾸면 행이 바뀌고 화면이 다시 그려진다.
  const churn = (async () => {
    for (let i = 0; i < 10; i++) {
      await page.request.post('/permission?d=' + encodeURIComponent(socket), {
        form: { mode: i % 2 === 0 ? 'auto' : 'ask' },
      }).catch(() => {});
      await page.waitForTimeout(1_000);
    }
  })();
  await page.waitForTimeout(12_000);
  await churn;
  const n = asked.counts();
  // 옆 판의 제 시계는 5초다. 12초면 두세 번이고, 명단이 흔들려도 그 수는 안 는다.
  //
  // ⚠ 이 시험이 지금 무엇을 가르는지: 한가한 컴패니언 하나짜리 하네스에서는 「명단마다 다시
  // 묻기」와 「제 시계로 묻기」가 같은 수를 낸다 — 규칙을 걷어내고 재 봤고 셋씩으로 같았다.
  // 그러니 이 줄은 회귀의 상한을 지키는 것이지, 그 회귀를 재현해 잡는 것이 아니다. 재현하려면
  // 실제로 도는 컴패니언들이 필요하다(사용자가 본 스물두 번은 그런 기계에서 났다). 옆의
  // 「목록은 화면당 한 번」은 다르다 — 캐시를 끄고 재 보면 빨개진다(실측).
  for (const path of ['/jobs', '/cron', '/handoffs']) {
    expect(n[path] ?? 0, `${path} in 12s while the roster changed ten times`).toBeLessThanOrEqual(4);
  }
});

test('목록은 화면당 한 번 — 모델은 컴패니언마다, 프로바이더는 콘솔마다', async ({ page, asked }) => {
  await openDetail(page);
  await page.waitForTimeout(6_000);
  const n = asked.counts();
  // 모델 목록은 그 데몬의 사실이라 컴패니언 하나당 한 번.
  expect(n['/model'] ?? 0, 'model list').toBeLessThanOrEqual(1);
  // 프로바이더는 콘솔 하나의 사실이라 화면을 오가도 한 번.
  expect(n['/providers'] ?? 0, 'provider list').toBeLessThanOrEqual(1);
});

test('컴패니언을 오가도 모델 목록을 다시 묻지 않는다', async ({ page, asked }) => {
  await open(page);
  const cards = page.locator('#fleet a.card[data-socket]');
  await expect(cards.first()).toBeVisible();
  const socks = await cards.evaluateAll(els => els.map(e => e.dataset.socket));
  test.skip(socks.length < 2, '이 하네스에 컴패니언이 하나뿐이다');
  // 눌러서 간다 — 주소를 새로 여는 것은 콘솔을 새로 세우는 일이고, 그러면 재려는 기억이
  // 애초에 없다. 사용자가 겪은 것도 한 콘솔 안에서 오간 것이었다.
  const goto = async (sock) => {
    if (await page.locator('#detail').count()) {
      await page.locator('a[href="/"]').first().click();   // 상세에서 명단으로 돌아가는 문
      await expect(cards.first()).toBeVisible();
    }
    await page.locator(`#fleet a.card[data-socket="${sock}"]`).click();
    await expect(page.locator('#detail')).toBeVisible();
  };
  await goto(socks[0]);
  await goto(socks[1]);
  await page.waitForTimeout(1_000);
  asked.forget();
  await goto(socks[0]);                // 돌아온다 — 이 컴패니언의 목록은 이미 안다
  await page.waitForTimeout(2_000);
  const n = asked.counts();
  expect(n['/model'] ?? 0, 'model list on return').toBe(0);
});
