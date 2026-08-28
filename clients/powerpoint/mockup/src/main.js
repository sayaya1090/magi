// 조립하는 자리. **여기만 무엇이 무엇인지 안다** — 안쪽 층은 서로를 인터페이스로만 안다.
import { Composer } from './domain/Composer.js';
import { QuoteSelection } from './usecase/QuoteSelection.js';
import { PointAtAdvice } from './usecase/PointAtAdvice.js';
import { SendTurn } from './usecase/SendTurn.js';
import { WatchPrompt } from './usecase/WatchPrompt.js';
import { FakeDeck } from './adapter/FakeDeck.js';
import { pickDeck } from './adapter/pickDeck.js';
import { FakeChat } from './adapter/FakeChat.js';
import { FakeStatus } from './adapter/FakeStatus.js';
import { FakeTranscript } from './adapter/FakeTranscript.js';
import { ReadTranscript } from './usecase/ReadTranscript.js';
import { View } from './ui/view.js';
import { mountFakeCanvas } from './ui/fakeCanvas.js';
import { mountFakePrompts } from './ui/fakePrompts.js';
import { fixture } from './ui/deckFixture.js';

/**
 * 물음을 얼마나 자주 물어보는가.
 *
 * 스트림이 아니라 **폴이다.** 취향이 아니라 계약이라 그렇다 — 물음은 로그에 안 쌓이고 막힌
 * 데몬의 버스로만 나가므로 이벤트 스트림으로는 영영 안 온다(§5.7). 1초는 「사람이 물음을
 * 기다리는 시간」과 「빈 문을 두드리는 값」 사이에서 고른 값이고, 재 본 값이 아니다.
 */
const POLL_MS = 1000;

/** 목업이 붙은 척하는 대화. 진짜에서는 헬퍼가 데몬에게 물어 얻는다. */
const SESSION = 'sess-mock';

async function boot() {
  const { deck, why, host, late, error, office } = await pickDeck();
  const composer = new Composer();
  // 진짜 문이 아니라 흉내다. 여기서 바꿔 끼우는 것이 곧 「데몬에 붙인다」가 된다(§5.5).
  const status = new FakeStatus();
  const watchPrompt = new WatchPrompt(status);
  // **연결이 둘이다**(§5.7). 내는 것과 받는 것이 다른 연결이고, 서로의 생사 증거가 아니다.
  const stream = new FakeTranscript();
  const chat = new FakeChat(stream, { sessionId: SESSION });
  const readTranscript = new ReadTranscript(stream);

  const view = new View({
    composer,
    quoteSelection: new QuoteSelection(deck, composer),
    pointAt: new PointAtAdvice(deck),
    sendTurn: new SendTurn(chat, composer),
    deck,
    watchPrompt,
    readTranscript,
  });
  view.mount();
  readTranscript.attach(SESSION);

  // 첫 폴을 기다렸다가 돌린다 — 겹쳐 돌면 같은 물음에 답이 두 번 갈 자리가 생긴다.
  const tick = async () => {
    try { await watchPrompt.poll(); } finally { setTimeout(tick, POLL_MS); }
  };
  tick();

  if (deck instanceof FakeDeck) {
    document.body.classList.add('standalone');
    mountFakeCanvas(deck, document.querySelector('#fake'));
    mountFakePrompts(status, document.querySelector('#fake'), { stream, readTranscript,
      sessionId: SESSION, deck });
  }

  // 사유 넷 중 **화면이 말해야 하는 셋**. `no-office` 만 안 적는다 — 브라우저에서 그냥 연
  // 것이고, 옆에 뜬 가짜 캔버스가 그 자체로 설명이다. 나머지 셋은 옆에 아무 설명이 없다.
  if (why === 'timeout') {
    view.note('Office 응답이 1.5초 안에 안 와 가짜 덱으로 갔습니다. PowerPoint 안이라면 새로고침하세요.',
      { sticky: true });
  }
  // **던진 것은 늦은 것이 아니다.** 새로고침 권유가 여기서는 틀린 권유고, 늦은 답이 없어서
  // 뒤에 바로잡아 줄 것도 없다.
  if (why === 'threw') {
    view.note(`Office 를 부르다 던졌습니다(${error?.message ?? String(error)}). `
      + '가짜 덱으로 계속합니다 — 새로고침해도 같은 자리일 수 있습니다.', { sticky: true });
  }
  // **Office 안인데 PowerPoint 가 아니다.** 이건 갈라 놓고도 아무도 안 읽던 사유다. 가짜
  // 캔버스가 옆에 떠 있어도 여기서는 설명이 못 된다 — 사람은 자기 문서를 열어 놓고 있고,
  // 왜 그게 안 잡히는지를 알아야 한다.
  if (why === 'not-powerpoint') {
    view.note(`PowerPoint 가 아닌 Office 호스트입니다(${host ?? '호스트를 안 밝힘'}). `
      + '이 창은 PowerPoint 에서만 진짜 덱에 붙습니다.', { sticky: true });
  }
  // 늦게라도 풀리면 말해 준다. **바꿔 끼우지 않고 말만 한다** — 바꾸면 조용한 오작동과 같아진다.
  //
  // 늦은 답이 PowerPoint 가 **아닐** 때도 말한다. 위의 시한 쪽지는 「PowerPoint 안이라면
  // 새로고침하세요」라고 적혀 있는데, 늦게 온 답이 Word 면 그 권유는 **틀린 권유**다 —
  // 새로고침해도 같은 자리에 온다. 방금 안 것을 안 실어 보내면 화면은 우리보다 덜 아는 채로
  // 남고, 사람은 새로고침을 되풀이한다. 쪽지 자리는 하나라 늦은 말이 앞의 말을 덮는다.
  late?.then((lateHost) => {
    if (lateHost !== null && lateHost === office?.HostType?.PowerPoint) {
      view.note('PowerPoint 를 늦게 잡았습니다. 새로고침하면 진짜 덱에 붙습니다.', { sticky: true });
    } else {
      view.note('Office 가 늦게 답했는데 PowerPoint 가 아닙니다'
        + `(${lateHost ?? '호스트를 안 밝힘'}). 새로고침해도 같은 자리입니다.`, { sticky: true });
    }
  }).catch((e) => {
    // 던진 것도 답이다 — "끝내 못 잡았다". 앞의 쪽지는 「PowerPoint 안이라면」이라는 **조건부**라
    // 이 사실을 대신 말해 주지 못한다: 조건이 틀렸다는 것이 바로 지금 알게 된 것이다.
    view.note(`Office 를 끝내 못 잡았습니다(${e?.message ?? String(e)}). 가짜 덱으로 계속합니다.`,
      { sticky: true });
  });
}

boot();
