import { test as base, expect } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

/**
 * 하네스가 어디에 세웠는지. serve.mjs가 적어 둔다 — 시험은 다른 프로세스라 그 경로를 물려받지
 * 못하고, 모르면 화면이 그린 경로가 진짜인지 대조할 수가 없다.
 */
export function harness() {
  const here = dirname(fileURLToPath(import.meta.url));
  return JSON.parse(readFileSync(join(here, '..', '.harness.json'), 'utf8'));
}

/**
 * 이 시험들이 재는 것: 화면이 도는 동안 <b>회선에 무엇이 몇 번 나가는가</b>, 그리고 받은 것을
 * 어떻게 그리는가.
 *
 * 앞의 것이 이 자리가 생긴 이유다. 상세를 열면 /jobs와 /cron이 스물두 번 나갔는데, 카드마다
 * "답이 같으면 다시 그리지 않는다"는 검사가 있어 <b>화면은 조용했다</b> — 그리기만 보는 시험은
 * 그것을 영원히 못 본다.
 *
 * 상대는 진짜 magi-web과 진짜 데몬이다(serve.mjs). 정적 데모로도 화면은 뜨지만 거기서는 명단이
 * 흐르지 않아, 규칙을 걷어내도 초록이 나온다(실측).
 */
export const test = base.extend({
  asked: async ({ page }, use) => {
    const seen = [];
    page.on('request', r => {
      try {
        const u = new URL(r.url());
        // 자산은 화면의 물음이 아니다 — 한 번 받아 캐시에 눕는다.
        if (/^\/(ui|font|vendor|i18n)\//.test(u.pathname)) return;
        if (u.pathname === '/' || u.pathname.endsWith('.js') || u.pathname.endsWith('.css')) return;
        seen.push(u.pathname);
      } catch { /* 셈이 화면을 깨우지 않는다 */ }
    });
    await use({
      /** 길마다 몇 번인지. */
      counts() {
        const n = {};
        for (const p of seen) n[p] = (n[p] || 0) + 1;
        return n;
      },
      /** 지금까지의 셈을 잊는다 — "이 동작 이후"만 재고 싶을 때. */
      forget() { seen.length = 0; },
    });
  },
});

export { expect };

/** 콘솔이 서고 명단에 컴패니언이 그려질 때까지. */
export async function open(page, query = '') {
  await page.goto('/' + query);
  await expect(page.locator('#fleet a.card[data-socket]').first()).toBeVisible({ timeout: 20_000 });
}

/** 첫 컴패니언의 상세로 들어간다. */
export async function openDetail(page) {
  await open(page);
  const card = page.locator('#fleet a.card[data-socket]').first();
  const socket = await card.getAttribute('data-socket');
  await card.click();
  // 상세가 실제로 선 자리 — 화면이 그 판을 어디에 두는지는 배치의 일이라, 이 시험은 "속이
  // 있는가"만 본다(사실판 자신이 그 규칙을 적어 두고 있다).
  await expect(page.locator('#detail')).toBeVisible();
  await expect(page.locator('#detail .f').first()).toBeVisible({ timeout: 15_000 });
  return socket;
}

/** 화면 하나로 간다(?v=). */
export async function screen(page, id) {
  await page.goto('/?v=' + id);
}
