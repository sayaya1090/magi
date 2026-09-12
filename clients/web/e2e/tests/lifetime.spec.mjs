import { test, expect, open, harness } from './fixtures.mjs';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

/**
 * <b>탭은 컴패니언의 수명이 아니다</b> — 설계 §3·L12.
 *
 * 「브라우저 새로고침, SSE 재연결, 목록 폴링은 데몬 생성의 근거가 될 수 없다」이고, 「탭 종료로
 * 데몬을 끄지 않는다」이다. 화면이 무엇을 그리는지로는 그것을 못 잰다 — 명단이 여전히 두 줄이어도
 * 그 줄 뒤의 프로세스가 바뀌었을 수 있고, 사람이 잃는 것은 줄이 아니라 <b>돌던 일</b>이다.
 *
 * 그래서 <b>공개 기록을 읽는다</b>. 데몬이 소켓 옆에 적어 둔 pid·instance 가 탭을 열고 닫고
 * 새로고침하는 동안 그대로여야 한다. 하네스가 제 설정 디렉토리를 적어 두므로 그 파일들을 직접
 * 볼 수 있다(fixtures 의 `harness()`).
 *
 * ⚠ <b>파일 비교만으로는 프로세스가 살아 있음을 증명하지 못한다.</b> 기록은 데몬이 죽어도 그
 * 자리에 남는다 — 같은 pid 가 같은 파일에 적혀 있는 것과 그 pid 가 도는 것은 다른 사실이다.
 * 그래서 `kill(pid, 0)` 로 실제 생존까지 묻는다: 신호를 안 보내고 커널에 그 프로세스가 있는지만
 * 묻는, 이 저장소의 Go 쪽 `procalive` 와 같은 방법이다.
 */
function daemons() {
  const { config } = harness();
  const out = new Map();
  for (const name of readdirSync(config)) {
    if (!name.endsWith('.sock.session')) continue;
    try {
      const r = JSON.parse(readFileSync(join(config, name), 'utf8'));
      out.set(name, { pid: r.pid, instance: r.instance ?? null, alive: alive(r.pid) });
    } catch { /* 아직 쓰는 중인 기록은 다음 읽기에 보인다 */ }
  }
  return out;
}

/** 그 pid 가 실제로 도는가. 신호는 안 보내고 존재만 묻는다. */
function alive(pid) {
  if (!pid) return false;
  try { process.kill(pid, 0); return true; } catch { return false; }
}

/** 두 기록이 같은 세대를 가리키나 — 수가 아니라 프로세스를 견준다. */
function same(before, after) {
  if (before.size !== after.size) return `기록 수가 ${before.size} → ${after.size}`;
  for (const [name, was] of before) {
    const now = after.get(name);
    if (!now) return `${name} 이 사라졌다`;
    if (now.pid !== was.pid) return `${name} 의 pid 가 ${was.pid} → ${now.pid}`;
    if (was.instance && now.instance && now.instance !== was.instance) {
      return `${name} 의 instance 가 바뀌었다 — 프로세스가 갈렸다`;
    }
    // 기록은 죽은 데몬 자리에도 남는다. 같은 글자가 적혀 있는 것과 그것이 도는 것은 다른 사실이다.
    if (!now.alive) return `${name} 의 pid ${now.pid} 가 더 이상 돌지 않는다 — 기록만 남았다`;
  }
  return null;
}

test('새로고침은 컴패니언을 만들지도 끝내지도 않는다', async ({ page }) => {
  await open(page);
  const before = daemons();
  expect(before.size, '하네스가 세운 컴패니언이 안 보인다 — 이 시험이 아무것도 안 재고 있다')
    .toBeGreaterThan(1);
  // 바닥: 시작부터 죽어 있었으면 아래 비교는 「그대로다」로 통과한다.
  expect([...before.values()].every(d => d.alive), '시작부터 도는 컴패니언이 없다').toBe(true);

  await page.reload();
  await expect(page.locator('#fleet a.card[data-socket]').first()).toBeVisible({ timeout: 20_000 });
  await page.reload();
  await expect(page.locator('#fleet a.card[data-socket]').first()).toBeVisible({ timeout: 20_000 });

  expect(same(before, daemons()), '새로고침이 컴패니언을 바꿨다').toBeNull();
});

test('탭을 하나 닫아도 남은 탭과 컴패니언은 그대로다', async ({ page, context }) => {
  await open(page);
  const before = daemons();
  expect(before.size).toBeGreaterThan(1);

  // 둘째 탭. 같은 콘솔을 보는 두 사람이고, 한쪽이 떠나는 것이 다른 쪽의 일을 끝내면 안 된다.
  const second = await context.newPage();
  await second.goto('/');
  await expect(second.locator('#fleet a.card[data-socket]').first()).toBeVisible({ timeout: 20_000 });
  await second.close();

  // 닫힘이 서버에 닿을 시간을 준다 — 끊긴 스트림을 서버가 치우는 데 한 박자 걸린다.
  await page.waitForTimeout(1_000);

  expect(same(before, daemons()), '탭 하나가 닫히면서 컴패니언이 같이 갔다').toBeNull();
  // 그리고 남은 탭은 여전히 도는 화면이다 — 명단이 살아 있다.
  await page.reload();
  await expect(page.locator('#fleet a.card[data-socket]').first()).toBeVisible({ timeout: 20_000 });
  expect(same(before, daemons()), '남은 탭의 새로고침이 컴패니언을 바꿨다').toBeNull();
});
