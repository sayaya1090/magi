// 조립하는 자리. **여기만 무엇이 무엇인지 안다** — 안쪽 층은 서로를 인터페이스로만 안다.
import { Conversation } from './domain/Conversation.js';
import { QuoteSelection } from './usecase/QuoteSelection.js';
import { PointAtAdvice } from './usecase/PointAtAdvice.js';
import { SendTurn } from './usecase/SendTurn.js';
import { OfficeDeck } from './adapter/OfficeDeck.js';
import { FakeDeck } from './adapter/FakeDeck.js';
import { FakeChat } from './adapter/FakeChat.js';
import { View } from './ui/view.js';
import { mountFakeCanvas } from './ui/fakeCanvas.js';
import { fixture } from './ui/deckFixture.js';

// 시계가 이겼다는 표시. `null` 은 "호스트가 PowerPoint 가 아니다"라는 **다른 사실**이라 못 쓴다.
const TIMED_OUT = Symbol('timed-out');

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
 * 그래서 두 가지를 한다. 하나, 왜 가짜로 갔는지를 **갈라 돌려준다**(Office 가 없어서인지, 시계가
 * 이겨서인지). 둘, 진 뒤에도 `onReady` 를 **계속 듣는다** — 늦게 풀리면 그 사실을 화면에 남긴다.
 * 여기서 덱을 몰래 바꿔 끼우지는 않는다. 화면과 대화가 이미 그 덱을 들고 서 있고, 조용한 교체는
 * 조용한 오작동과 구분이 안 된다.
 */
async function pickDeck() {
  if (typeof Office === 'undefined') {
    return { deck: new FakeDeck(fixture), why: 'no-office', late: null };
  }
  let ready = null;
  try {
    ready = Office.onReady().then((info) => info?.host ?? null);
    const host = await Promise.race([
      ready,
      new Promise((r) => setTimeout(() => r(TIMED_OUT), 1500)),
    ]);
    if (host === Office.HostType.PowerPoint) return { deck: new OfficeDeck(), why: null, late: null };
    if (host !== TIMED_OUT) return { deck: new FakeDeck(fixture), why: 'not-powerpoint', late: null };
  } catch { /* Office 밖에서 office.js 가 던지는 경우 */ }
  return { deck: new FakeDeck(fixture), why: 'timeout', late: ready };
}

async function boot() {
  const { deck, why, late } = await pickDeck();
  const conversation = new Conversation();
  const chat = new FakeChat();

  const view = new View({
    conversation,
    quoteSelection: new QuoteSelection(deck, conversation),
    pointAt: new PointAtAdvice(deck),
    sendTurn: new SendTurn(chat, conversation),
    chat,
    deck,
  });
  view.mount();

  if (deck instanceof FakeDeck) {
    document.body.classList.add('standalone');
    mountFakeCanvas(deck, document.querySelector('#fake'));
  }

  if (why === 'timeout') {
    view.note('Office 응답이 1.5초 안에 안 와 가짜 덱으로 갔습니다. PowerPoint 안이라면 새로고침하세요.',
      { sticky: true });
  }
  // 늦게라도 풀리면 말해 준다. **바꿔 끼우지 않고 말만 한다** — 바꾸면 조용한 오작동과 같아진다.
  late?.then((host) => {
    if (host === Office.HostType.PowerPoint) {
      view.note('PowerPoint 를 늦게 잡았습니다. 새로고침하면 진짜 덱에 붙습니다.', { sticky: true });
    }
  }).catch(() => {});
}

boot();
