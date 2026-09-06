/**
 * 이 창이 **손인가, 화면인가.**
 *
 * 창이 손이자 화면이다 — 헬퍼가 내려보낸 조작을 이 창이 Office.js 로 수행한다. 그런데 창은 바닥
 * (`WordApi 1.7`) 아래 호스트에서도 열린다(매니페스트로 막지 않는다). 거기서도 손으로 붙으면 **못 하는
 * 조작을 받아 Office.js 의 날 오류를 낸다.** 파워포인트 판이 실물 LTSC 2021 에서 그 화면을 봤다
 * (2026-09-05): 창이 도구를 다 광고했고, 첫 호출이 「'index' 속성을 사용할 수 없습니다」로 돌아왔고,
 * 모델은 「API 가 없다」고 결론짓고 셸로 돌아섰다.
 *
 * 그런 호스트에서 창은 **화면만** 맡는다 — 헬퍼의 `role=viewer` 로 붙어 전사만 받고 호출은 안 받는다.
 * (엑셀에는 파워포인트의 COM 손 같은 대체 손이 없다 — 2021 도 WordApi 1.14 라 창이 손이 되므로 아직
 * 필요하지 않았다.) 그 갈림을 여기서 정한다. 재는 것은 `isSetSupported` 가 답한 값이고
 * (`OfficeDocument.capabilities`), 여기는 그 답을 읽기만 한다 — Office.js 를 모른다.
 */

/** 손이 되려면 있어야 하는 집합. §3.3 이 고른 바닥과 같다. */
// 워드의 바닥은 1.3 — 표·목록·스타일·문서 속성이 거기서 온다. 2019·2021 이 1.3 이라 손이 된다; 2016(1.2 이하)은 화면만 맡는다.
// 메모·책갈피·변경 추적(1.4+)은 도구가 op 마다 이름을 대고 거절한다.
export const HAND_FLOOR = Object.freeze({ name: 'WordApi', version: '1.3' });

/**
 * @param {{isHost:boolean, caps:{measured:boolean, sets:Array<{name:string,version:string,ok:boolean|null}>}|null}} p
 * @returns {{role:'hand'|'viewer', why:string, top:string}}
 *   `top` 은 이 호스트가 지원한다고 말한 가장 높은 WordApi — 로그가 사유에 적는다.
 */
export function handRole({ isHost, caps }) {
  // 가짜 문서는 손을 밖에 안 내놓는다 — 그 규칙은 `main.js` 가 따로 지킨다. 여기서는 역할만 말한다.
  if (!isHost) return { role: 'hand', why: '', top: '' };
  // **못 쟀으면 손이다.** 앞 판본과 같은 동작이고, 모르는 것을 「없다」로 읽지 않는다.
  if (!caps || caps.measured !== true || !Array.isArray(caps.sets)) return { role: 'hand', why: '', top: '' };

  const floor = caps.sets.find((s) => s?.name === HAND_FLOOR.name && s?.version === HAND_FLOOR.version);
  const top = highest(caps.sets.filter((s) => s?.name === HAND_FLOOR.name && s?.ok === true).map((s) => s.version));
  if (floor?.ok === true) return { role: 'hand', why: '', top };

  return {
    role: 'viewer',
    top,
    // **화면에는 안 띄운다.** 사람은 손과 화면을 구분할 일이 없다(2026-09-06, 사용자). 이 문장은
    // 로그와 시험의 것이다 — 그래서 SKU 를 안 말한다: 1.3 아래는 2016·옛 Mac 이다.
    why: `이 호스트는 WordApi ${top || '어느 판도'} 까지라 이 창은 손이 아니다 — 편집을 못 한다`,
  };
}

/** '1.10' 이 '1.9' 보다 높다 — 문자열 비교로는 반대가 되므로 수로 잰다. */
function highest(versions) {
  let best = '';
  let bestN = -1;
  for (const v of versions) {
    const n = Number(String(v).split('.')[1] ?? -1);
    if (n > bestN) { bestN = n; best = String(v); }
  }
  return best;
}
