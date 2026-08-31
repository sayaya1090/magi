import { test, expect, openDetail, harness } from './fixtures.mjs';

/**
 * 컨트롤이 <b>데몬에 닿는다</b>.
 *
 * 화면이 값을 바꿔 그리는 것과 돌아가는 프로세스가 그 값으로 도는 것은 다른 일이고, 이 콘솔은
 * 한동안 앞의 것만 했다(고르개는 움직였고, 명단은 옛 값을 계속 답했다). 그래서 이 시험은 누른
 * 뒤에 <b>문에 다시 물어</b> 확인한다 — 화면을 다시 읽으면 제 낙관을 제가 채점하게 된다.
 */
const perm = (page) => page.locator('#detail md-outlined-select[data-aria-label="Approvals"]');

/** 상세를 새로 열고 고르개에서 하나를 고른다. 새로 여는 이유는 아래 시험이 적어 둔다. */
async function choose(page, socket, want) {
  await page.goto(`/?d=${encodeURIComponent(socket)}`);
  await expect(perm(page)).toBeVisible({ timeout: 20_000 });
  await perm(page).click();
  await perm(page).locator(`md-select-option[value="${want}"]`).click();
}

async function daemonSays(page, socket, field) {
  return await page.evaluate(async ([sock, f]) => {
    const rows = await (await fetch('/fleet')).json();
    return (rows.find(r => r.socket === sock) || {})[f];
  }, [socket, field]);
}

test('승인 모드를 바꾸면 데몬이 그 모드로 답한다', async ({ page }) => {
  const socket = await openDetail(page);
  const before = await daemonSays(page, socket, 'permission');
  expect(before, 'the daemon has an approval mode to begin with').toBeTruthy();

  const options = await perm(page).locator('md-select-option').evaluateAll(o => o.map(x => x.getAttribute('value')));
  const next = options.find(v => v && v !== before);
  expect(next, `another mode to switch to among ${options.join(',')}`).toBeTruthy();

  // 되돌리기는 finally에 둔다. 이 시험은 <b>살아 있는 데몬</b>을 실제로 바꾸고, 그 데몬은 런
  // 하나를 통째로 산다 — 가운데서 넘어져 되돌리지 못하면 뒤따르는 모든 시험이(그리고 CI의 재시도
  // 한 번이) 바뀐 세계를 물려받는다. 재시도는 더 나쁘다: 그때의 `before`는 망가진 상태를 읽는다.
  try {
    await choose(page, socket, next);
    await expect.poll(() => daemonSays(page, socket, 'permission'), {
      message: 'the running daemon must be on the mode that was chosen',
      timeout: 10_000,
    }).toBe(next);
  } finally {
    // 되돌릴 때 판을 새로 여는 이유: 고르고 나면 이 판은 데몬의 새 답으로 다시 그려지고, 그 사이
    // 열어 둔 메뉴는 화면에 없는 옛 판의 것이 된다(눌러도 아무 일이 없다 — 실측). 한 번 고르고
    // 새로 여는 것이 사람이 하는 일과도 같다.
    await choose(page, socket, before);
  }
  await expect.poll(() => daemonSays(page, socket, 'permission'), { timeout: 10_000 }).toBe(before);
});

test('모델 고르개가 데몬이 답한 이름들로 선다', async ({ page }) => {
  await openDetail(page);
  const model = page.locator('#detail md-outlined-select[data-aria-label="Model"]');
  const names = await model.locator('md-select-option').evaluateAll(o => o.map(x => x.getAttribute('value')));
  expect(names.filter(Boolean).length, 'models offered').toBeGreaterThan(0);
  // 지금 켜져 있는 것도 그 목록 안에 있다 — 목록과 현재값이 다른 데서 오면 고를 수 없는 값이 켜진다.
  expect(names, 'the model it is on must be among the ones offered').toContain(await model.evaluate(el => el.value));
});

test('환경설정 화면이 진짜 쓰이는 파일을 가리킨다', async ({ page }) => {
  await page.goto('/?v=settings');
  await expect(page.locator('#frame')).not.toBeEmpty({ timeout: 20_000 });
  const text = await page.locator('#frame').innerText();
  expect(text, 'the config file this magi actually reads').toContain(harness().config);
});
