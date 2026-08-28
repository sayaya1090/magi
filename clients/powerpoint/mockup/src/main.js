// 조립하는 자리. **여기만 무엇이 무엇인지 안다** — 안쪽 층은 서로를 인터페이스로만 안다.
import { Composer } from './domain/Composer.js';
import { QuoteSelection } from './usecase/QuoteSelection.js';
import { PointAtAdvice } from './usecase/PointAtAdvice.js';
import { SendTurn } from './usecase/SendTurn.js';
import { WatchPrompt } from './usecase/WatchPrompt.js';
import { FakeDeck } from './adapter/FakeDeck.js';
import { pickDeck, pickNote, lateNote, lateFailNote } from './adapter/pickDeck.js';
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
  //
  // **작업창이 접혀 있는 동안 이 시계가 느려진다.** 브라우저는 안 보이는 판의 `setTimeout` 을
  // 1분까지 늘린다. 접힌 동안 낡는 것은 아무도 안 보므로 거짓말이 아닌데, **펴는 순간**이
  // 다르다 — 다음 틱까지 최대 1분 전 사실이 「지금」으로 서 있고, 그중 하나가 **단추 달린
  // 물음**이다(§5.7). 이미 끝난 물음에 답을 보내면 코어가 *"이미 결정됐거나 만료됐다"* 는
  // **틀린 사유**로 떨어뜨린다. 좁아진 구간에서 봐야 하는 곳은 구간이 아니라 **구간의 끝**이다.
  //
  // 그래서 다시 보이는 순간에 한 번 더 묻는다. 겹치지 않게 `busy` 로 막는데, 도는 폴이 있으면
  // 그 답이 곧 새 답이라 안 물어도 맞다. 최악이 폴 한 번 더인 값이다.
  let timer = null;
  let busy = false;
  const tick = async () => {
    if (busy) return;
    busy = true;
    try {
      await watchPrompt.poll();
    } finally {
      busy = false;
      clearTimeout(timer);
      timer = setTimeout(tick, POLL_MS);
    }
  };
  tick();
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') tick();
  });

  if (deck instanceof FakeDeck) {
    document.body.classList.add('standalone');
    mountFakeCanvas(deck, document.querySelector('#fake'));
    mountFakePrompts(status, document.querySelector('#fake'), { stream, readTranscript,
      sessionId: SESSION, deck });
  }

  // 사유 넷 중 **화면이 말해야 하는 셋**. 어느 문장이 나갈지는 `pickNote` 가 정한다 — 여기
  // 두면 「호스트를 안 밝힌 답」과 「Word 라고 밝힌 답」이 같은 문장으로 나가고, 그 차이를
  // 시험이 못 잰다(이 파일은 DOM 이 있어야 돈다).
  //
  // **누름 쪽지(`note`)가 아니라 판 자리(`where`)로 간다.** 이건 이번 한 번의 일이 아니라 창이
  // 사는 동안 계속 참인 말이라 수명이 다르다. 한 칸을 같이 쓰던 동안엔 첫 누름이 이 문장을
  // 지웠고, 그 뒤로 사람은 자기가 PowerPoint 안이 아니라는 걸 다시 알 길이 없었다.
  const note = pickNote({ why, host, error });
  if (note) view.where(note);

  // 늦게라도 풀리면 말해 준다. **바꿔 끼우지 않고 말만 한다** — 바꾸면 조용한 오작동과 같아진다.
  // 판 자리는 하나라 늦은 말이 앞의 말을 덮는다. 여기선 그게 맞다 — 같은 것에 대한 더 새 사실이다.
  late
    ?.then((lateHost) => view.where(lateNote(lateHost, office?.HostType?.PowerPoint ?? null)))
    .catch((e) => view.where(lateFailNote(e)));
}

boot();
