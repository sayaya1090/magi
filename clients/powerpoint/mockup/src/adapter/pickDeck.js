import { OfficeDeck } from './OfficeDeck.js';
import { FakeDeck } from './FakeDeck.js';
import { fixture } from '../ui/deckFixture.js';

/**
 * 시계가 이겼다는 표시. `null` 은 "호스트가 PowerPoint 가 아니다"라는 **다른 사실**이라 못 쓴다.
 */
export const TIMED_OUT = Symbol('timed-out');

/**
 * 어느 덱에 붙을지 고른다.
 *
 * Office 밖에서는 `Office.onReady()` 가 **영영 안 풀린다.** 그래서 기다리기만 하면 빈 화면으로
 * 멈춘다 — 경주를 붙여 1.5초 뒤에는 가짜로 간다. 넘겨짚는 게 아니라 **못 정했다는 것을 정하는**
 * 것이고, 화면이 어느 쪽인지 그대로 띄운다.
 *
 * ⚠ **그런데 그 시계가 진짜 PowerPoint 안에서도 돈다.** 호스트가 느리게 뜨는 날(찬 시작, 큰 덱)
 * 1.5초를 넘기면 사용자는 **PowerPoint 안에서 가짜 덱을 보게 된다** — 가짜 캔버스에 가짜 도형이
 * 뜨고, 그걸 인용하고, 자기 슬라이드가 왜 안 잡히는지 모른다. 라벨은 "가짜 덱"이라 적히지만
 * 목업에서 그 라벨은 늘 그럴 법한 말이라 **경고로 안 읽힌다.**
 *
 * 그래서 두 가지를 한다. 하나, 왜 가짜로 갔는지를 **갈라 돌려준다.** 둘, 진 뒤에도 `onReady` 를
 * **계속 듣는다** — 늦게 풀리면 그 사실을 화면에 남긴다. 여기서 덱을 몰래 바꿔 끼우지는 않는다.
 * 화면과 대화가 이미 그 덱을 들고 서 있고, 조용한 교체는 조용한 오작동과 구분이 안 된다.
 *
 * # 사유는 넷이고, **넷 다 부르는 쪽까지 가야 한다**
 *
 * `no-office` / `not-powerpoint` / `timeout` / `threw`. 앞 판본은 이걸 갈라 놓고도 **던진 것을
 * `timeout` 이라 적었다.** 「시계가 이겼다」와 「Office 가 던졌다」는 다른 사실이고, 사람에게
 * 주는 지시가 갈린다 — 앞은 새로고침하면 되고 뒤는 안 된다. 게다가 `Office.onReady()` 자체가
 * 그 자리에서 던지면 늦은 답(`late`)이 아예 없어서 **뒤늦게 바로잡아 줄 것도 없었다.** 화면에
 * 남는 것은 끝까지 틀린 말 하나다.
 *
 * `host` 도 같이 돌려준다. `not-powerpoint` 는 **Office 안**이라는 뜻이라 가짜 캔버스가 그
 * 설명이 못 된다 — 무엇에 붙었는지를 사람이 알아야 왜 자기 문서가 안 잡히는지 안다.
 *
 * @param {{office?:object|null, waitMs?:number, deck?:function}} deps 시험이 손으로 넣는다.
 *   `office` 는 전역을 그대로 받고(없으면 `null`), `deck` 은 가짜 덱을 만드는 함수다.
 * @returns {Promise<{deck:object, why:(null|'no-office'|'not-powerpoint'|'timeout'|'threw'),
 *                    host:*, late:(Promise|null), error:(Error|null), office:(object|null)}>}
 *   `office` 는 **늦은 답을 읽는 쪽이 쓴다** — `late` 가 풀렸을 때 그것이 PowerPoint 인지
 *   보려면 같은 `HostType` 이 필요하고, 부르는 쪽이 전역을 다시 짚으면 이 함수에 넣어 준
 *   것과 다른 물건을 볼 수 있다(시험이 정확히 그 경우다).
 */
export async function pickDeck({
  office = typeof Office === 'undefined' ? null : Office,
  waitMs = 1500,
  deck = () => new FakeDeck(fixture),
} = {}) {
  if (!office) {
    return { deck: deck(), why: 'no-office', host: null, late: null, error: null, office: null };
  }

  let ready = null;
  try {
    ready = office.onReady().then((info) => info?.host ?? null);
    const host = await Promise.race([
      ready,
      new Promise((r) => setTimeout(() => r(TIMED_OUT), waitMs)),
    ]);
    // `HostType` 이 없는 판이면 `want` 가 `null` 이다. 그걸 그대로 비교하면 호스트를 안 밝힌
    // 답(`host === null`)이 **PowerPoint 로 통과한다** — 모르는 둘을 같다고 세는 자리다.
    const want = office.HostType?.PowerPoint ?? null;
    if (want !== null && host === want) {
      return { deck: new OfficeDeck(), why: null, host, late: null, error: null, office };
    }
    if (host !== TIMED_OUT) {
      return { deck: deck(), why: 'not-powerpoint', host, late: null, error: null, office };
    }
  } catch (e) {
    // **던진 것은 늦은 것이 아니다.** 여기서 `timeout` 을 돌려주면 화면이 「1.5초 안에 안 와」
    // 라고 적는데, 그건 안 일어난 일이다. `late` 도 안 돌려준다 — 늦게 올 답이 없다(이미
    // 던졌다). 늦은 답이 없다는 것은 **뒤늦게 바로잡아 줄 것도 없다**는 뜻이므로, 사유는
    // 여기서 제대로 실려 나가는 수밖에 없다.
    return { deck: deck(), why: 'threw', host: null, late: null, error: e, office };
  }
  return { deck: deck(), why: 'timeout', host: null, late: ready, error: null, office };
}
