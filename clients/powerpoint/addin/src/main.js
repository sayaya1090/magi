// 조립하는 자리. **여기만 무엇이 무엇인지 안다** — 안쪽 층은 서로를 인터페이스로만 안다.
import { Composer } from './domain/Composer.js';
import { QuoteSelection } from './usecase/QuoteSelection.js';
import { PointAtAdvice } from './usecase/PointAtAdvice.js';
import { SendTurn } from './usecase/SendTurn.js';
import { WatchPrompt } from './usecase/WatchPrompt.js';
import { FakeDeck } from './adapter/FakeDeck.js';
import { pickDeck, pickNote, lateNote, lateFailNote } from './adapter/pickDeck.js';
import { stableDeckId } from './adapter/OfficeDeck.js';
import { FakeChat } from './adapter/FakeChat.js';
import { FakeStatus } from './adapter/FakeStatus.js';
import { FakeTranscript } from './adapter/FakeTranscript.js';
import { HelperApi } from './adapter/helperApi.js';
import { HelperStream } from './adapter/HelperStream.js';
import { HelperChat, HelperStatus, HelperTranscript } from './adapter/HelperPorts.js';
import { FakeHand } from './adapter/FakeHand.js';
import { OfficeHand } from './adapter/OfficeHand.js';
import { ServeHand } from './usecase/ServeHand.js';
import { handRole } from './usecase/HandRole.js';
import { mountPick } from './ui/pick.js';
import { ReadTranscript } from './usecase/ReadTranscript.js';
import { View } from './ui/view.js';
import { guideBoard } from './ui/screen.js';

/**
 * 스프라이트에서 아이콘 하나를 꺼낸다(`taskpane.html` 의 `<defs>`).
 *
 * **부르는 자리는 반드시 `title` 과 `aria-label` 을 같이 단다** — 아이콘만으로는 무슨 단추인지
 * 모르고, 그 글은 **동작**을 적어야 한다(아이콘 이름이 아니라, M3 icon-buttons).
 */
function icon(name) {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('class', 'i');
  svg.setAttribute('viewBox', '0 0 16 16');
  const use = document.createElementNS('http://www.w3.org/2000/svg', 'use');
  use.setAttribute('href', '#' + name);
  svg.append(use);
  return svg;
}
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

  // **헬퍼가 페이지를 내줬으면 진짜로 돈다.** 토큰이 페이지에 박혀 오는 것이 그 표시이고
  // (§5.5·§12 #7), 없으면 가짜다 — **조용히 진짜인 척하지 않는 것**이 이 갈래의 요점이다.
  const boot = (typeof window !== 'undefined' && window.MAGI) ? window.MAGI : null;
  const real = Boolean(boot?.token);

  // **헬퍼는 하나고 앱은 셋이다.** 한 프로세스가 `/ppt`·`/xl`·`/word` 아래에 세 판을 내주므로
  // (clients/office/helper), 이 창의 문은 오리진에 그 접두를 붙인 자리다. 접두는 부팅 값이 준다.
  const origin = real ? location.origin + (boot.base ?? '') : undefined;
  const api = real ? new HelperApi({ token: boot.token, origin }) : null;
  // 진짜 문이 아니라 흉내다. 여기서 바꿔 끼우는 것이 곧 「데몬에 붙인다」가 된다(§5.5).
  // **덱이 자기 이름을 들게 하고, 그 이름으로 붙는다.** 없으면 허브가 붙을 때마다 새 번호를
  // 발급하고, 그때마다 이 창의 대화가 끊긴다(`stableDeckId` 의 주석).
  const deckId = real ? await stableDeckId() : '';
  // **이 창이 손인가, 화면인가.** 바닥(PowerPointApi 1.8) 아래 호스트에서는 화면만 맡는다 —
  // 편집은 COM 손이 하고, 이 창이 손으로 붙으면 못 하는 호출을 받아 날 오류를 낸다. 실물
  // LTSC 2021 에서 그 화면을 봤다(2026-09-05, HandRole.js). 재는 것은 isSetSupported 의 답이다.
  const role = (real && typeof deck.capabilities === 'function')
    ? handRole({ isHost: deck.isHost, caps: deck.capabilities() })
    : { role: 'hand', why: '' };
  const helperStream = real
    ? new HelperStream({
      token: boot.token,
      origin,
      presentation: deckId || (boot.presentation ?? ''),
      label: boot.label ?? '',
      role: role.role,
    }).open()
    : null;
  // **헬퍼가 준 문서 키로 말한다.** `hello` 는 스트림이 서고 나서 오므로, 오는 즉시 넘긴다 —
  // 그때부터 이 창의 API 호출은 자기 덱의 대화로 간다. 그 전의 호출은 열쇠 없는 대화로 가고,
  // 그것도 답이다(창이 아직 어느 덱인지 모르는 때).
  const status = real ? new HelperStatus(api) : new FakeStatus();
  const watchPrompt = new WatchPrompt(status);
  // **연결이 둘이다**(§5.7). 내는 것과 받는 것이 다른 연결이고, 서로의 생사 증거가 아니다.
  const stream = real ? new HelperTranscript(helperStream) : new FakeTranscript();
  const chat = real ? new HelperChat(api) : new FakeChat(stream, { sessionId: SESSION });
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
  // 창이 잰 요구 집합을 헬퍼로 넘기는 길. **가짜 갈래엔 없다** — 헬퍼가 없으면 보낼 곳도 없다.
  if (api) view.tellCaps = (caps) => { void api.caps(caps).catch(() => {}); };
  // 도구의 그림을 여는 길. 헬퍼가 데몬의 images 디렉토리에서 내준다 — 가짜 갈래엔 없다.
  if (api) view.loadImage = (path) => api.image(path);
  // 하던 일을 세우는 길. **문이 있을 때만 손잡이를 보인다**(`View.renderBusy`).
  if (api) {
    view.canStop = true;
    document.querySelector('#stop')?.addEventListener('click', () => {
      void (async () => {
        try {
          await api.interrupt();
          // 세운 것은 **실패가 아니다.** 여기까지 한 것은 덱에 그대로 남아 있고, 그 사실을
          // 말해 주지 않으면 사람은 되돌려진 줄 안다.
          view.note('멈췄습니다 — 여기까지 한 것은 그대로 남아 있습니다.');
        } catch (e) {
          view.note(`멈추지 못했습니다: ${e?.message ?? e}`, { sticky: true });
        }
      })();
    });
  }
  view.mount();
  // **손과 화면의 구분은 사람에게 안 말한다**(2026-09-06, 사용자: 「둘을 구분할 필요가 없어」).
  // 말하는 것은 사람이 할 일이 있을 때 하나 — 편집하는 손(magi-ppt-hand)이 아직 안 떠 있을 때.
  // 스트림이 `nohand` 로 알려 주면 켜고, 손이 붙어 `hello` 가 오면 끈다. 붙는 과정이 덮는
  // 판 사실 칸(`where`)이 아니라 자기 칸(`role`)이다 — `where` 에 적었더니 「준비됐습니다 —
  // 도구 48 개」가 덮었다(실물 LTSC 2021, 2026-09-06).
  if (helperStream && role.role === 'viewer') {
    helperStream.on('stream', (d) => { if (d?.reason === 'nohand') view.role(d.why); });
    helperStream.on('hello', () => view.role(''));
  }
  // 손이 붙는다. **조작을 수행하는 것은 애드인이고**, 헬퍼는 그 손을 부린다(§5.1).
  // PowerPoint 안이 아니면 가짜 손을 붙인다 — 그 화면에서 도구를 눌러 볼 수 있어야
  // 「붙었는데 아무 일도 안 일어난다」를 사람이 가려낼 수 있다.
  //
  // **진짜 갈래 밖에 세운다.** 앞 판본은 헬퍼에 붙는 자리에서 만들었는데, 그러면 **목업
  // 화면에는 손이 없다** — 제안 카드(§6.20)가 브라우저에서 영영 안 뜨고, 이 저장소가 화면을
  // 눈으로 재는 유일한 자리가 그 기능만 못 재게 된다.
  const hand = deck instanceof FakeDeck
    ? new FakeHand(structuredClone(fixture))
    : new OfficeHand({});
  // **화면도 손을 쓴다.** 덱에 저장된 제안을 읽고, 「적용」이 그 손을 부른다 — 헬퍼를 거치지
  // 않는다. 사람이 누른 것은 모델의 턴이 아니고, 모델의 로그에 남을 일도 아니다.
  // **손이 아닌 창은 제안도 안 읽는다.** 제안은 덱의 태그(PowerPointApi 1.3)에 사는데, 바닥 아래
  // 호스트에서 그 읽기는 Office.js 의 날 오류(「'index' 속성을 사용할 수 없습니다」)로 화면에 떴다
  // (실물 LTSC 2021, 2026-09-06). 사람이 할 일이 없는 문장은 안 띄운다.
  if (role.role === 'hand') view.useHand(hand);
  // **대화 이름을 우리가 짓지 않는다.** 이름을 가진 쪽은 컴패니언이고(`.sock.session`),
  // `ReadTranscript` 는 남의 대화 이벤트를 신원으로 걸러 낸다 — 여기서 지어낸 이름에 붙이면
  // **진짜 이벤트가 전부 그 그물에 걸린다.** 실물에서 그 화면을 봤다(2026-09-01): 모델은 덱의
  // 제목을 실제로 고쳤는데 창은 「보냈습니다」에 멈춘 채였고, 메아리가 안 오니 사람이 적은
  // 글도 안 지워졌다. 진짜 갈래는 아래에서 컴패니언이 든 이름에 붙는다.
  if (!real) readTranscript.attach(SESSION);
  // 대화가 끊기거나 다시 붙으면 브랜드 줄도 같이 움직인다. **한 사건에 한 자리**라
  // 여기서 걸어 두고, 뷰는 자기가 받은 값만 그린다.
  const drawn = readTranscript.onChange;
  // **매 프레임마다 덱을 읽지 않는다.** `onChange` 는 글자 한 조각마다 뛴다(한 턴에 수천 번).
  // 거기서 제안을 다시 읽으면 작업창이 PowerPoint 를 쉬지 않고 두드리고, 그 사이로 모델의
  // 조작이 끼어들 자리가 없어진다. **조용해진 뒤 한 번**만 읽는다.
  let fixTimer = null;
  const laterFixes = () => {
    clearTimeout(fixTimer);
    fixTimer = setTimeout(() => { void view.loadFixes(); }, 2000);
  };
  readTranscript.onChange = () => { drawn?.(); void refreshBrand(); laterFixes(); };

  // 브랜드 줄이 늘 말하는 것 셋: 어디에 붙었나 · 대화가 살아 있나 · 손이 몇인가.
  // **가짜 갈래에서도 사실을 적는다** — 「가짜 덱」이라고 적히지 않으면 그 화면은 진짜인 척한다.
  let bound = real ? null : '가짜 덱';
  if (!real) view.setBound(true);
  const baseNameOf = (p) => String(p ?? '').split(/[\\/]/).filter(Boolean).pop() ?? '';
  const refreshBrand = async () => {
    let hands;
    if (real) {
      try { hands = (await api.documents())?.documents?.length; } catch { hands = undefined; }
    }
    view.brand({
      companion: bound,
      streamLive: readTranscript?.view?.live !== false,
      hands,
      session: watchPrompt?.view?.session ?? '',
    });
  };
  // 부팅 직후 한 번. **비워 두면 「아직 안 골랐다」와 「골랐는데 화면이 안 그렸다」가 같은
  // 빈칸이 된다** — 가짜 갈래에서는 이 한 줄이 「가짜 덱」이라고 적는 자리다.
  void refreshBrand();

  /**
   * 컨텍스트 띠 — 붙어 있는 동안 10초마다, 그리고 대화가 바뀔 때. 전사의 `context.usage` 는 퍼센트만 실어 오고
   * **무엇으로 찼는지**는 데몬의 `context` 문만 답하므로 따로 묻는다. 못 물으면 띠를 숨긴다 — 낡은 띠를 세워 두면
   * 그것이 사실인 줄 안다.
   */
  const refreshContext = async () => {
    // ⋯ 로 편 줄 안에 산다 — 접혀 있으면 안 묻는다(사용자: 「상시 나올 필요는 없는데」). 펼 때 한 번 묻고 그 뒤 10초마다.
    if (document.querySelector('#advanced')?.hidden !== false) return;
    if (!real || !(watchPrompt?.view?.session)) { view.contextMeter(null); return; }
    try { view.contextMeter(await api.context()); } catch { view.contextMeter(null); }
  };
  setInterval(() => { void refreshContext(); }, 10000);
  /** 프로바이더·모델 목록 — 펼 때 묻는다. 매 폴에 물으면 심마다 카탈로그를 두드린다. */
  const loadModels = async () => {
    if (!real) { view.modelPicker({ providers: [], models: [], warning: '브라우저 목업에서는 고를 것이 없습니다' }); return; }
    try { view.modelPicker(await api.models()); } catch (e) { view.modelPicker({ providers: [], models: [], error: e?.message ?? String(e) }); }
  };

  if (real) {
    // 손이 붙는다. **조작을 수행하는 것은 애드인이고**, 헬퍼는 그 손을 부린다(§5.1).
    // PowerPoint 안이 아니면 가짜 손을 붙인다 — 그 화면에서 도구를 눌러 볼 수 있어야
    // 「붙었는데 아무 일도 안 일어난다」를 사람이 가려낼 수 있다.
    // ⚠ **목업은 손을 헬퍼에 내놓지 않는다.**
    //
    // 이 페이지를 브라우저에서 열면 덱은 `FakeDeck` 인데, 앞 판본은 그래도 손을 등록했다.
    // 그러면 모델에게 **열린 덱이 둘**로 보이고 둘 중 하나가 가짜인 것을 알 길이 없다 —
    // 실물에서 그 값을 치렀다(2026-09-04, 웨이브 5): 화면을 확인하느라 브라우저 탭을 여덟 번
    // 새로고침했더니 등록이 매번 새 번호를 받아, 도는 모델이 방금 받은 문서 번호로 부를 때마다
    // 「그런 덱은 없다」를 받았다. 한 판에 여섯 번. 노트를 가짜 덱에 쓰려 한 호출도 있었다.
    //
    // 화면에서 눌러 보는 손은 그대로 둔다(위 `view.useHand`) — 제안 카드는 브라우저에서
    // 확인해야 하고, 그건 **이 창 안의 일**이라 모델과 무관하다. 멈추는 것은 **밖에 내놓는
    // 것**뿐이다.
    // **화면은 손을 내놓지 않는다.** 스트림이 viewer 로 붙어 호출이 오지 않지만, 여기서도 막는다 —
    // 두 자리 중 하나가 어긋나도 이 창이 1.2 호스트에서 조작을 받는 일은 없어야 한다.
    if (deck.isHost && role.role === 'hand') {
      new ServeHand({ stream: helperStream, api, hand, onNote: (s) => view.where(s) }).start();
    }

    /**
     * 이 대화에 창을 붙인다. **고르기 전에 부른다** — `choose` 가 문을 여는 순간 헬퍼가 로그를
     * 처음부터 흘려보내는데, 그때 창이 다른 이름에 붙어 있으면 그 replay 를 통째로 버린다.
     *
     * 「붙었다」고 적는 것은 여기가 아니다(그건 `choose` 가 성공한 뒤다). 여기서 하는 것은
     * **어느 이름의 이벤트를 우리 것으로 셀지**를 정하는 것뿐이라, 실패해도 거짓말이 안 된다.
     */
    // **붙고 나서 한 번 적는다.**
    //
    // 창은 자기 덱 이름을 `hello` 가 와야 안다. 그 전에 상태를 물으면 열쇠 없는 대화(아무 덱에도
    // 안 매인 자리)의 이름이 오고, 창이 둘이면 **둘 다 같은 것**을 그린다 — 이름을 보여 주는
    // 목적이 「어느 창이 어느 대화인가」인데 정확히 그 반대를 한다. 실물에서 그 화면을 봤다
    // (2026-09-05: "둘 다 똑같은게 떠 있는데").
    //
    // 그래서 **덱을 알게 된 뒤 자기 것으로 한 번** 묻고, 그 이름만 적는다.
    if (real) {
      helperStream.on('hello', (d) => {
        api.useDeck(d?.document ?? '');
        void (async () => {
          try {
            const mine = await api.companions();
            const sid = mine?.bound?.session ?? '';
            if (sid) {
              listenTo(sid);
              view.ready(bound, sid);
            }
          } catch { /* 못 물었으면 그 줄은 안 뜬다 — 틀린 이름보다 없는 게 낫다 */ }
        })();
      });
    }

    const listenTo = (session) => {
      if (session && readTranscript.sessionId !== session) readTranscript.attach(session);
    };
    const nameOf = (c) => c?.name || baseNameOf(c?.workdir) || c?.socket || '';

    const pick = mountPick(document.querySelector('#pick'), {
      onChoose: async (companion) => {
        try {
          listenTo(companion.session);
          const out = await api.choose(companion.socket, companion.session);
          pick.hide();
          offerRepick(true);
          // **붙었다는 증거는 ack 가 아니라 도구 이름이다**(§5.0.1).
          const name = nameOf(companion);
          view.where(`${name} 에 연결했습니다 — 도구 ${out?.tools?.length ?? 0} 개.` +
            (out?.chat ? ` 다만 채팅은 아직입니다: ${out.chat}` : ''));
          bound = name;
          saidStale = false;   // 다시 골랐으니 그 말은 끝났다
          // 이제부터는 스트림·데몬에 대한 말이 뜻을 갖는다(§5.7). 그 전에는 안 띄운다.
          view.setBound(true);
          await refreshBrand();
        } catch (e) {
          // **끝내 못 붙으면 말한다**(§5.3). 조용하면 화면이 「할 일 없음」처럼 보인다.
          pick.note(`연결하지 못했습니다: ${e?.message ?? e}`);
        }
      },
      onRefresh: () => { void showCompanions(); },
    });
    /**
     * 붙어 있던 컴패니언이 **다시 떴다.** 소켓 경로는 워크스페이스에서 유도되므로 그대로고
     * dial 도 성공하지만, 우리 MCP 등록은 죽은 프로세스와 같이 사라졌고 이 창이 든 대화
     * 이름도 남의 생애의 것이다. 실물에서 그 화면을 봤다(2026-09-01): 창은 「대화 연결됨」
     * 이라고 적었고, 모델에게는 덱 도구가 하나도 없어서 셸로 우회하려 들었다.
     *
     * **고르는 판을 도로 세운다.** 몰래 다시 붙이지 않는다 — 다시 붙이는 것은 「이 컴패니언에
     * 맡긴다」를 다시 말하는 일이고, 그 말은 사람이 한다(§5.0).
     */
    let saidStale = false;
    /**
     * 붙어 있던 컴패니언이 다시 떴다. **사람에게 묻지 않는다** — 이 창의 컴패니언은 헬퍼가
     * 마련하는 것이라, 다시 뜬 것에도 헬퍼가 다시 붙이면 된다(`attachOwn`: 헬퍼는 생애가
     * 갈린 것을 보고 새 대화를 열어 덱 도구를 다시 싣는다). 앞 판본은 「다시 골라 주세요」와
     * 명단을 띄웠고, 사용자 교정(2026-09-05): 「데몬 재기동시 바인딩은 피피티 컴패니언에게
     * 자동으로 붙으면 돼, 사람에게 물을 일이 없어」. 명단은 자동으로도 못 붙었을 때만 편다.
     */
    const companionRestarted = async () => {
      if (saidStale) return;
      saidStale = true;
      bound = null;
      view.setBound(false);
      view.where('연결돼 있던 컴패니언이 다시 시작됐습니다 — 다시 연결하는 중입니다.');
      if (await attachOwn()) return;
      void showCompanions();
    };

    /**
     * 명단을 읽는다. **`show` 일 때만 그린다.**
     *
     * 앞 판본은 늘 그렸고, 그래서 컴패니언을 마련하는 동안 **「켜져 있는 컴패니언이 하나도
     * 없습니다 — 덱이 있는 폴더에서 `magi --daemon` 을 띄운 뒤 새로고침하세요」**가 화면에
     * 떠 있었다. 그건 이 판이 없애려던 바로 그 화면이고, PC 를 잘 다루지 못하는 사람에게
     * 터미널 명령을 시키는 문장이다. 게다가 그 사이 명단의 단추가 눌리면 사람이 고른 컴패니언이
     * 잠시 뒤 끝난 `makeOwn` 에게 **말없이 덮인다.**
     *
     * 그래서 그리는 것은 **자동으로 못 마련했을 때뿐**이다.
     */
    const showCompanions = async (show = true) => {
      try {
        const list = await api.companions();
        // **명단도 데려온다.** 이 판은 스크롤 영역 맨 위에 서는데 그것을 여는 손은 화면
        // 아래(브랜드 줄 → `⋯`)에 있다. 대화가 길면 명단이 저 위에 열리고, 사람이 보기에는
        // **단추를 눌렀는데 아무 일도 안 일어난 것**이다 — 실물에서 그 화면을 봤다(2026-09-04).
        // 「늘 지킬 것」·「가이드」는 같은 날 고쳤는데 이 자리를 빠뜨렸다.
        if (show) { pick.render(list); offerRepick(false); view.reveal(document.querySelector('#pick')); }
        // **헬퍼가 이미 붙어 있을 수 있다.** PowerPoint 는 작업창을 껐다 켤 때마다 이 창을
        // 새로 만드는데 헬퍼는 그대로 살아 있으므로, 「아무 데도 안 붙었다」로 시작하면 붙어
        // 있는 것을 안 붙었다고 적는다. 그때 물려받을 것은 **이름 둘**이다 — 대화 이름(그래야
        // 이벤트를 우리 것으로 센다)과 컴패니언 이름(그래야 브랜드 줄이 사실을 적는다).
        // **다시 뜬 뒤에는 물려받지 않는다.** 헬퍼의 Bridge 는 여전히 그 소켓에 묶여 있어서
        // 여기서 물려받으면 방금 적은 「다시 골라 주세요」를 스스로 덮는다.
        const sock = saidStale ? '' : list?.bound?.socket;
        if (sock) {
          listenTo(list?.bound?.session);
          bound = nameOf((list.companions ?? [])
            .map((e) => e.companion).find((c) => c?.socket === sock)) || baseNameOf(sock);
          // **붙어 있으면 고르는 화면을 접는다.**
          //
          // 헬퍼가 미리 붙여 뒀거나(§4.2) 작업창을 껐다 켰으면 여기로 물려받는데, 앞 판본은
          // 상태만 물려받고 명단을 그대로 뒀다. 실물에서 그 화면을 봤다(2026-09-02): 아래쪽
          // 브랜드 줄은 「대화 연결됨」인데 위쪽에는 「어느 컴패니언에 붙일까요」가 떠 있었고,
          // 그건 다 된 화면이 **아직 뭔가 해야 한다**고 말하는 것이다. 고르고 붙은 길은
          // (`onChoose`) 접는데 물려받은 길만 안 접고 있었다.
          pick.hide();
          offerRepick(true);
          view.setBound(true);
          // **물려받았을 때도 한 줄 적는다.** 앞 판본은 여기서 아무 말도 안 했고, 헬퍼가 미리
          // 붙여 둔 첫 화면은 **빈 판**이었다 — 아래 브랜드 줄에 증거가 있긴 하지만, 처음 여는
          // 사람이 거기부터 보지는 않는다.
          // **이 말은 첫 줄이 서는 순간 증명된다.** 그래서 `where`(창이 사는 동안 참인 칸)가
          // 아니라 `ready` 로 간다 — 조건이 사라지면 문장도 같이 사라져야 한다.
          // **여기서는 대화 이름을 안 적는다.** 이 길은 창이 자기 덱을 알기 전에도 지나가고,
          // 그때 오는 것은 열쇠 없는 대화의 이름이다 — 창이 둘이면 둘 다 같은 것을 그린다.
          // 이름은 붙은 뒤에 한 번 적는다(아래 `hello`).
          view.ready(bound);
          await refreshBrand();
        }
      } catch (e) {
        // 못 훑은 것도 **그릴 때만** 적는다 — 준비 중에는 이 화면 자체가 안 나와야 한다.
        if (show) {
          pick.render({ companions: [] });
          pick.note(`컴패니언을 못 훑었습니다: ${e?.message ?? e}`);
        }
      }
    };
    /**
     * **자기 컴패니언에 알아서 붙는다** — 고르는 화면을 안 거치는 길.
     *
     * 명단 화면은 이미 데몬이 떠 있는 사람에게만 뜻이 있다. 메일에서 받은 `.pptx` 를 더블클릭한
     * 사람에게는 **늘 비어 있고**, 「컴패니언을 고르세요」는 그 사람 머릿속에 대응하는 개념이
     * 없는 말이다. 이 도구의 목표가 PC 를 잘 다루지 못하는 사람이면 첫 화면이 막다른 길이었다.
     *
     * 그래서 열면 헬퍼가 파워포인트 몫의 컴패니언을 마련한다(`/api/own`). **명단은 안 없앤다** —
     * 저장소에서 일하다 코드를 보는 에이전트에게 덱을 맡기고 싶은 경우가 실제로 있고, 자동으로
     * 못 마련했을 때 사람이 갈 곳도 거기다.
     *
     * **기다리는 동안 말을 한다.** 데몬 냉시동은 오래 걸릴 수 있는데, 아무 말 없는 화면은
     * 고장으로 읽힌다 — 무엇을 기다리는지도, 다시 눌러야 하는지도 모른다.
     */
    // 못 마련한 사유는 **명단을 세운 뒤에** 적는다 — `pick.render` 가 자식을 갈아 끼우므로,
    // 먼저 적으면 지워진다.
    let failedWhy = '';
    let failedLog = '';
    const attachOwn = async () => {
      const began = Date.now();
      let askFailed = '';
      const said = () => {
        const secs = Math.round((Date.now() - began) / 1000);
        // **몇 초째인지 말한다.** 실측(2026-09-02) 대개 6초면 끝나지만, 느린 머신에서는 더
        // 걸릴 수 있다 — 「준비 중」만 적어 두면 사람은 그것을 고장과 구별하지 못한다.
        view.where(`magi 를 준비하는 중입니다 — ${secs}초째.`
          + (secs > 20 ? ' 처음 한 번은 좀 더 걸릴 수 있습니다.' : ''));
      };
      let r;
      try {
        r = await api.own();
      } catch (e) {
        // 이 헬퍼가 그 자리를 안 가진 판본일 수 있다. **없는 길을 고장으로 적지 않는다** —
        // 명단이 그대로 있으므로 사람이 할 수 있는 일이 남아 있다.
        failedWhy = `${e?.message ?? e}`;
        return false;
      }
      // 냉시동을 실측했다(2026-09-02). 5분을 천장으로 두되 **끝나면 바로 나간다.**
      const until = began + 5 * 60 * 1000;
      while (r?.phase === 'working' && Date.now() < until) {
        said();
        await new Promise((go) => setTimeout(go, 1500));
        // 잠깐 못 물어본 것은 실패가 아니다 — 다만 **무엇이 실패했는지는 들고 있는다.**
        try { r = await api.own(); askFailed = ''; }
        catch (e) { askFailed = String(e?.message ?? e); }
      }
      if (r?.phase !== 'ready') {
        // **왜 못 했는지와 어디를 볼지**를 같이 준다. 「안 됩니다」만으로는 할 일이 없다.
        // **못 물어본 것과 데몬이 안 뜬 것을 가른다.** 앞 판본은 물어보다 실패해도 마지막에 받은
        // `working` 을 그대로 들고 있어서, 헬퍼가 5분 내내 안 답해도 「데몬이 아직 안 떴습니다」로
        // 적었다 — 데몬 이야기를 하는데 사실은 헬퍼 이야기였다.
        const why = r?.why
          || (askFailed ? `magi 헬퍼가 답하지 않습니다(${askFailed})` : null)
          || (r?.phase === 'working' ? '아직 시작되지 않았습니다(5분 넘음)' : '알 수 없음');
        failedWhy = why;
        failedLog = r?.log ?? '';
        view.where('자동으로 준비하지 못했습니다 — 아래에서 컴패니언을 골라 주세요.');
        return false;
      }
      listenTo(r.session);
      pick.hide();
      offerRepick(true);
      // **붙었다는 증거는 ack 가 아니라 도구 이름이다**(§5.0.1).
      bound = baseNameOf(r.workdir) || 'PowerPoint';
      saidStale = false;
      view.where(`준비됐습니다 — 도구 ${r.tools?.length ?? 0} 개.`
        + (r.started ? ' (magi 를 방금 시작했습니다)' : '')
        + (r.chat ? ` 다만 채팅은 아직입니다: ${r.chat}` : ''));
      view.setBound(true);
      await refreshBrand();
      return true;
    };

    /**
     * 붙은 뒤 **명단으로 돌아가는 길.**
     *
     * 명단을 남겨 둔 이유는 「저장소에서 일하다 코드를 보는 에이전트에게 덱을 맡기고 싶다」가
     * 실제로 있어서인데, 붙고 나면 `pick.hide()` 뒤로 그것을 다시 세울 방법이 화면 어디에도
     * 없었다 — **길이 코드에만 있고 화면에는 없었다.** 리뷰가 짚었다(2026-09-02).
     *
     * 붙기 전에는 안 보인다. 그때는 명단 자체가 떠 있고, 같은 것을 두 자리에서 권하면 화면이
     * 무엇을 하라는 것인지 흐려진다.
     */
    const advanced = document.querySelector('#advanced');
    /**
     * **가끔 쓰는 넷을 여는 문.**
     *
     * 넷을 브랜드 줄에 그대로 놓으면 176px 라(32×4 + 16×3) 붙은 컴패니언 이름이 밀린다.
     * 그 이름은 늘 보여야 하는 값이라, 대신 **문 하나**만 두고 넷은 컴포저 위에서 편다.
     * 접혀 있으면 그 줄은 0px 다.
     *
     * `aria-expanded` 를 같이 돌린다 — 화면을 안 보는 손에게는 이 값이 「펴졌다」의 전부다.
     */
    const more = document.querySelector('#more');
    more?.addEventListener('click', () => {
      if (!advanced) return;
      const open = advanced.hidden;
      advanced.hidden = !open;
      more.setAttribute('aria-expanded', String(open));
      more.classList.toggle('icon-on', open);
      if (open) { void loadModels(); void refreshContext(); }
    });
    /** 압축 — 데몬이 접는다. 눌렀다는 것과 끝났다는 것을 가른다: 끝은 띠가 바뀌는 것으로 본다. */
    document.querySelector('#compact')?.addEventListener('click', () => {
      void (async () => {
        // 데몬의 compact 문은 접기가 끝나야 답한다 — 그동안 위 진행 막대가 돈다(사용자 요청 2026-09-06).
        view.folding(true);
        view.where('컨텍스트를 접는 중입니다…');
        try {
          const out = await api.compact();
          view.where(out?.note || '접었습니다.');
          void refreshContext();
        } catch (e) { view.where(`접지 못했습니다: ${e?.message ?? e}`); }
        finally { view.folding(false); }
      })();
    });
    /** 프로바이더·모델 — 고르면 바로 보낸다. 백엔드를 바꾸면 모델 어휘가 바뀌므로 목록을 다시 묻는다. */
    const sendModel = async (body) => {
      try {
        const out = await api.setModel(body);
        view.where(out?.note || '바꿨습니다 — 다음 턴부터입니다.');
      } catch (e) { view.where(`못 바꿨습니다: ${e?.message ?? e}`); }
      await loadModels();
    };
    // M3 메뉴: 단추가 펴고, 항목이 고르고, 바깥 클릭·Esc 가 접는다.
    for (const [btnId, menuId, key] of [['#provider', '#provider-menu', 'base'], ['#model', '#model-menu', 'model']]) {
      const btn = document.querySelector(btnId); const menu = document.querySelector(menuId);
      btn?.addEventListener('click', () => view.menu(btnId, menu?.hidden !== false));
      menu?.addEventListener('click', (ev) => {
        const item = ev.target.closest('[role="option"]'); if (!item) return;
        view.menu(btnId, false);
        if (item.getAttribute('aria-selected') === 'true') return;
        void sendModel({ [key]: item.dataset.value });
      });
    }
    document.addEventListener('click', (ev) => { if (!ev.target.closest('#pick-model')) view.menu(null, false); });
    document.addEventListener('keydown', (ev) => { if (ev.key === 'Escape') view.menu(null, false); });
    document.querySelector('#repick')?.addEventListener('click', () => {
      void showCompanions(true);
    });

    /**
     * **카운슬 스위치.** 켜면 턴 끝을 위원 셋이 재고(느리고 꼼꼼), 끄면 모델이 끝냈다면 끝이다(빠르다).
     * 기동 때 정해지는 값이라 헬퍼가 설정을 고치고 컴패니언을 다시 띄운다 — **대화가 새로 시작된다.**
     * 그 값은 단추의 title 이 미리 적고(`councilButton`), 누른 뒤에는 헬퍼의 note 가 적는다. 다시 붙는 것은
     * 재기동 사건 처리(`companionRestarted`)가 한다 — 여기서 따로 하지 않는다.
     */
    document.querySelector('#council')?.addEventListener('click', () => {
      void (async () => {
        const want = watchPrompt.view.council !== true;
        // 데몬을 다시 띄우는 일이라 **먼저 묻는다** — 같은 데몬의 다른 창·플러그인까지 끊긴다.
        if (!await view.ask('council', want ? 'on' : 'off')) return;
        view.where(`카운슬을 ${want ? '켜는' : '끄는'} 중입니다 — 컴패니언을 다시 띄웁니다.`);
        try {
          const out = await api.setCouncil(want);
          view.where(out?.note || `카운슬을 ${want ? '켰습니다' : '껐습니다'} — 컴패니언을 다시 띄우는 중입니다.`);
        } catch (e) {
          view.where(`카운슬을 못 바꿨습니다: ${e?.message ?? e}. 지금 상태 그대로입니다.`);
        }
      })();
    });

    /**
     * **늘 지킬 것** — 한 번 적어 두면 매번 지켜지는 말.
     *
     * 「불릿은 한 줄로」, 「강조는 우리 회사 파랑으로」. 이런 것은 부탁이 아니라 **취향이고**
     * **규칙**이라, 대화마다 다시 말하게 하면 사람이 지친다. 그리고 지치면 안 말하게 되고,
     * 안 말하면 결과가 매번 조금씩 다르다 — 발표 자료에서 그건 눈에 띈다.
     *
     * 적어 둔 글은 컴패니언 워크스페이스의 `AGENTS.md` 로 가고, magi 가 그것을 **매 턴**
     * 시스템 프롬프트에 넣는다. 우리가 매번 실어 보내는 것이 아니다.
     */
    const rulesPanel = document.querySelector('#rules-panel');
    const rulesText = document.querySelector('#rules-text');
    const rulesNote = document.querySelector('#rules-note');
    const sayRules = (msg) => {
      if (!rulesNote) return;
      rulesNote.textContent = msg ?? '';
      rulesNote.hidden = !msg;
    };
    document.querySelector('#rules')?.addEventListener('click', () => {
      void (async () => {
        if (!rulesPanel) return;
        if (!rulesPanel.hidden) { rulesPanel.hidden = true; return; }
        rulesPanel.hidden = false;
        // 여는 단추가 화면 아래라, 안 데려오면 이 판은 대화 저 위에서 열린다.
        view.reveal(rulesPanel);
        sayRules('');
        try {
          const got = await api.rules();
          // **적혀 있던 것을 그대로 보여 준다.** 빈 칸으로 열면 사람은 자기가 적어 둔 것이
          // 사라진 줄 알고, 저장을 누르면 진짜로 사라진다.
          if (rulesText) rulesText.value = got?.text ?? '';
          if (got?.path) sayRules(`이 파일에 있습니다: ${got.path}`);
        } catch (e) {
          // 못 읽었으면 **빈 칸을 보여 주지 않는다** — 빈 칸은 「아무것도 안 적혀 있다」는
          // 거짓말이고, 그 위에 저장을 누르면 적어 둔 것이 날아간다.
          if (rulesText) rulesText.disabled = true;
          sayRules(`지금 읽지 못했습니다: ${e?.message ?? e}. 저장하면 덮어쓰게 되므로 막아 뒀습니다.`);
        }
      })();
    });
    document.querySelector('#rules-close')?.addEventListener('click', () => {
      if (rulesPanel) rulesPanel.hidden = true;
    });
    document.querySelector('#rules-save')?.addEventListener('click', () => {
      void (async () => {
        try {
          const out = await api.setRules(rulesText?.value ?? '');
          if (rulesText) rulesText.value = out?.text ?? '';
          // **언제부터 듣는지 적는다.** 「저장했습니다」만 적으면 사람은 지금 도는 턴에도
          // 걸리는 줄 안다.
          sayRules(out?.note ?? '적어 뒀습니다.');
        } catch (e) {
          sayRules(`저장하지 못했습니다: ${e?.message ?? e}`);
        }
      })();
    });

    /**
     * **가이드 판.** 여러 벌의, 이름 붙은, 껐다 켤 수 있는 규칙(§가이드).
     *
     * 위 「늘 지킬 것」과 문이 갈린 이유는 **실리는 방식**이다: 저건 매 턴 통째로 실려 반드시
     * 지켜지고, 이건 모델이 필요할 때 불러 읽는다 — 안 부르면 안 지켜진다. 화면이 그 말을
     * 적어 두는 이유가 이것이고, 여기서는 **못 한 것을 못 했다고 적는 것**이 그 짝이다.
     */
    const gPanel = document.querySelector('#guides-panel');
    const gList = document.querySelector('#guides-list');
    const gEdit = document.querySelector('#guides-edit');
    const gName = document.querySelector('#guide-name');
    const gBody = document.querySelector('#guide-body');
    const gNote = document.querySelector('#guides-note');
    let editing = null; // 고치는 중인 이름. null 이면 새로 만드는 중이다.
    const sayGuides = (msg) => {
      if (!gNote) return;
      gNote.textContent = msg ?? '';
      gNote.hidden = !msg;
    };
    const drawGuides = async () => {
      if (!gList) return;
      let board;
      try {
        const got = await api.guides();
        board = guideBoard({ guides: got?.guides ?? [] });
      } catch (e) {
        // **못 읽은 것과 아직 없는 것을 가른다.** 빈 목록을 그리면 사람은 자기가 적어 둔 것이
        // 사라진 줄 안다.
        board = guideBoard({ error: e?.message ?? String(e) });
      }
      gList.replaceChildren();
      const head = document.createElement('p');
      head.className = 'rules-why';
      head.textContent = board.headText;
      gList.append(head);
      if (board.note) sayGuides(board.note);
      for (const row of board.rows) {
        const el = document.createElement('div');
        el.className = 'guide-row' + (row.enabled ? '' : ' guide-off');
        const nm = document.createElement('strong');
        nm.textContent = row.name;
        const desc = document.createElement('div');
        desc.className = 'guide-desc' + (row.descMissing ? ' guide-desc-missing' : '');
        desc.textContent = row.descText;
        const size = document.createElement('span');
        size.className = 'guide-size';
        size.textContent = row.sizeText;
        // **아이콘 단추.** 글자 셋이 한 줄에 서면 348px 에서 이름이 잘린다. 다만 아이콘만
        // 두는 대가가 있어서 셋을 같이 단다: 툴팁(동작을 적는다) · `aria-label`(낭독기) ·
        // 그리고 켜짐은 글리프와 굵기 **두 속성**으로 말한다(M3).
        /**
         * **스위치다 — 아이콘 단추가 아니다.**
         *
         * M3 가 셋을 갈라 두는 기준이 명시적이다: 체크박스는 목록에서 여럿, 라디오는 하나,
         * **스위치는 독립적인 설정**. 가이드는 서로 무관하고 하나씩 켜고 꺼지며 저장 없이
         * 즉시 먹으므로 스위치가 그 자리다. 앞 판본의 `◉`/`○` 는 읽는 사람에게 라디오의
         * 모양이라 「이 중 하나만」으로 읽혔다.
         *
         * `<button role="switch">` 인 것은 **키보드를 공짜로 얻으려는 것**이다 — M3 가 요구하는
         * Space·Enter 가 네이티브 단추의 기본 동작이다. 상태는 `aria-checked` 로 말한다
         * (`aria-pressed` 가 아니다: 그건 눌린 단추이고, 이건 켜진 설정이다).
         */
        const toggle = document.createElement('button');
        toggle.type = 'button';
        toggle.className = 'switch' + (row.enabled ? ' on' : '');
        toggle.setAttribute('role', 'switch');
        const handle = document.createElement('span');
        handle.className = 'switch-handle';
        toggle.append(handle);
        toggle.title = row.toggleTip;
        toggle.setAttribute('aria-label', row.toggleTip);
        toggle.setAttribute('aria-checked', row.enabled ? 'true' : 'false');
        toggle.addEventListener('click', () => void (async () => {
          try {
            await api.guide(row.enabled ? 'disable' : 'enable', row.name);
            // **끄는 것은 지우는 것이 아니다** — 글은 그대로 두고 자리만 옮긴다.
            sayGuides(row.enabled ? `${row.name} 를 껐습니다 — 글은 그대로입니다.` : `${row.name} 를 켰습니다.`);
          } catch (e) { sayGuides(`바꾸지 못했습니다: ${e?.message ?? e}`); }
          await drawGuides();
        })());
        const edit = document.createElement('button');
        edit.type = 'button';
        edit.className = 'icon-btn';
        edit.append(icon('i-edit'));
        edit.title = row.editTip;
        edit.setAttribute('aria-label', row.editTip);
        edit.addEventListener('click', () => void (async () => {
          try {
            const got = await api.guide('read', row.name);
            editing = row.name;
            if (gName) { gName.value = row.name; gName.disabled = true; }
            if (gBody) gBody.value = got?.body ?? '';
            if (gEdit) { gEdit.hidden = false; view.reveal(gEdit); }
            sayGuides('');
          } catch (e) { sayGuides(`읽지 못했습니다: ${e?.message ?? e}`); }
        })());
        const del = document.createElement('button');
        del.type = 'button';
        del.className = 'icon-btn icon-danger';
        del.append(icon('i-trash'));
        del.title = row.deleteTip;
        del.setAttribute('aria-label', row.deleteTip);
        del.addEventListener('click', () => void (async () => {
          // **한 번 더 묻는다.** 되돌릴 곳이 없는 일이고, 끄는 것이 바로 옆에 있다.
          // 판 안의 다이얼로그다 — `window.confirm` 은 이 창에서 안 뜰 수 있고, 안 뜨면
          // 지우기가 거절도 실패도 아닌 채로 조용히 아무 일도 안 한다.
          if (!await view.ask('delete-guide', row.name)) return;
          try {
            await api.guide('delete', row.name);
            sayGuides(`${row.name} 를 지웠습니다.`);
          } catch (e) { sayGuides(`지우지 못했습니다: ${e?.message ?? e}`); }
          await drawGuides();
        })());
        // **스위치가 맨 오른쪽이다.** 켜고 끄는 것은 이 목록에서 가장 자주 하는 일이고,
        // 지우기가 그 자리에 있으면 자주 하는 손이 되돌릴 수 없는 손 옆에 선다.
        el.append(nm, size, edit, del, toggle, desc);
        gList.append(el);
      }
    };
    document.querySelector('#guides')?.addEventListener('click', () => {
      if (!gPanel) return;
      if (!gPanel.hidden) { gPanel.hidden = true; return; }
      gPanel.hidden = false;
      view.reveal(gPanel);
      if (gEdit) gEdit.hidden = true;
      sayGuides('');
      void drawGuides();
    });
    document.querySelector('#guides-close')?.addEventListener('click', () => {
      if (gPanel) gPanel.hidden = true;
    });
    document.querySelector('#guides-new')?.addEventListener('click', () => {
      editing = null;
      if (gName) { gName.value = ''; gName.disabled = false; }
      if (gBody) gBody.value = '---\ndescription: \n---\n\n';
      // 편집 칸은 목록 **아래**에 선다 — 목록이 길면 화면 밖이라, 열어 놓고 아무 일도 안
      // 일어난 것으로 보인다. 실물에서 그 화면을 봤다(2026-09-04: `top` 이 803, 창은 673).
      if (gEdit) { gEdit.hidden = false; view.reveal(gEdit); }
      sayGuides('');
    });
    document.querySelector('#guide-cancel')?.addEventListener('click', () => {
      if (gEdit) gEdit.hidden = true;
    });
    document.querySelector('#guide-save')?.addEventListener('click', () => {
      void (async () => {
        try {
          const out = await api.guide('save', editing ?? (gName?.value ?? ''), gBody?.value ?? '');
          if (gEdit) gEdit.hidden = true;
          // **켜져 있는지를 같이 적는다** — 꺼진 것을 고쳐도 켜지지 않으므로, 안 적으면
          // 사람은 저장했으니 도는 줄 안다.
          sayGuides(out?.enabled ? `${out.name} 를 저장했습니다 — 켜져 있습니다.`
            : `${out?.name} 를 저장했습니다 — 꺼져 있어 지금은 안 읽힙니다.`);
          await drawGuides();
        } catch (e) { sayGuides(`저장하지 못했습니다: ${e?.message ?? e}`); }
      })();
    });

    /**
     * **새 대화.** 채팅을 쓰는 사람은 누구나 아는 그 단추다.
     *
     * 파워포인트 컴패니언은 워크스페이스가 하나라 대화도 하나이고, 그 하나가 영원히 쌓인다.
     * 실물에서 봤다(2026-09-02): 한 번 헤맨 대화가 다음 부탁까지 끌고 가서, 사람이 19번 장을
     * 보고 있는데 모델이 8번 장에 정렬을 걸고 6~17번을 헤맸다. PC 를 잘 다루지 못하는 사람에게
     * 「새 대화」는 **유일하게 아는 복구 수단**이고, 그게 없으면 이상해진 판 앞에서 할 수 있는
     * 일이 없다.
     *
     * **덱은 안 건드린다는 것을 먼저 말한다.** 이 단추가 슬라이드를 지우는 것으로 읽히면
     * 아무도 못 누르고, 그러면 있으나 마나다.
     */
    document.querySelector('#fresh')?.addEventListener('click', () => {
      void (async () => {
        view.where('새 대화를 여는 중입니다 — 슬라이드는 그대로입니다.');
        try {
          const out = await api.fresh();
          // **창을 새 이름으로 옮겨 앉힌다.** 안 그러면 새 대화의 이벤트가 전부 남의 것으로
          // 걸러져서, 눌렀는데 아무 말도 안 보이는 화면이 된다(§5.7).
          listenTo(out?.session);
          view.where(out?.note || '새 대화를 열었습니다 — 슬라이드는 그대로입니다.');
          // **새 이름을 화면에 적는다.** 이 단추의 결과는 「대화가 바뀌었다」인데, 바뀐 이름을
          // 안 보여 주면 사람은 무엇이 달라졌는지 못 본다 — 창을 둘 띄웠을 때 특히 그렇다.
          view.ready(bound, out?.session ?? '');
          await refreshBrand();
        } catch (e) {
          // 못 열었으면 **쓰던 대화는 그대로다.** 그 사실까지 적는다 — 안 적으면 사람은
          // 자기 대화가 어떻게 됐는지 모른다.
          view.where(`새 대화를 못 열었습니다: ${e?.message ?? e}. 쓰던 대화는 그대로입니다.`);
        }
      })();
    });
    // 붙기 전에는 문도 넷도 안 보인다 — 그때는 명단 자체가 떠 있고, 같은 것을 두 자리에서
    // 권하면 화면이 무엇을 하라는 것인지 흐려진다. **닫는 쪽은 판까지 같이 닫는다**: 문만
    // 감추고 판을 펴 둔 채로 두면 닫을 손이 없는 줄이 남는다.
    const offerRepick = (on) => {
      if (more) more.hidden = !on;
      if (!on && advanced) advanced.hidden = true;
      if (!on && more) {
        more.setAttribute('aria-expanded', 'false');
        more.classList.remove('icon-on');
      }
    };

    // **자기 덱 이름을 먼저 안다.**
    //
    // 붙이는 호출(`/api/own`)은 덱을 실어 보내야 그 덱의 대화에 묶인다. 그런데 창은 덱 이름을
    // `hello` 가 와야 알고, 앞 판본은 그것을 안 기다리고 쐈다 — 덱 없이 간 호출은 열쇠 없는
    // 자리만 묶었고, 창은 자기 자리가 빈 것을 보고 「데몬에 안 닿습니다」를 그렸다. 사람이 그
    // 화면을 네 번 말했다(2026-09-05: "플러그인에서 뭔가 쏴야한다고").
    //
    // 오래는 안 기다린다 — 안 오면 여태처럼 덱 없이 간다. 그 길도 답이 있고(열쇠 없는 자리),
    // 여기서 붙잡으면 스트림이 늦은 판에서 창이 아무것도 못 한다.
    if (real && !api.deck) {
      await Promise.race([
        new Promise((go) => { helperStream.on('hello', () => go()); }),
        new Promise((go) => { setTimeout(go, 3000); }),
      ]);
    }
    // **명단은 안 그리고** 읽기만 한다 — 이미 붙어 있는지만 보면 된다.
    await showCompanions(false);
    // 이미 붙어 있으면(헬퍼가 미리 붙였거나 작업창을 껐다 켠 경우) 그것을 흔들지 않는다 —
    // 다시 붙이는 것은 첫 등록을 떨어뜨리는 일이다(§5.0.1).
    if (!bound) {
      // **못 마련했을 때에만 명단을 세운다.** 그 화면은 터미널 명령을 시키는 문장을 품고 있어서,
      // 자동으로 될 수 있는 사람에게는 보여 주면 안 된다.
      if (!await attachOwn()) {
        await showCompanions(true);
        if (failedWhy) pick.note(`자동으로 준비하지 못했습니다: ${failedWhy}`);
        if (failedLog) pick.note(`자세한 사유는 여기 있습니다: ${failedLog}`);
      }
    }

    // **컴패니언이 다시 뜬 것을 폴이 알려 준다.** 화면이 이미 그리는 것 뒤에 한 줄 더 건다 —
    // `readTranscript.onChange` 를 감싼 것과 같은 자리, 같은 이유다(한 사건에 한 자리).
    const drewAsk = watchPrompt.onChange;
    // **붙는 순간에 이름을 적는다.**
    //
    // 앞 판본은 `hello` 때 한 번만 적었다. 그 순간에는 아직 어느 대화에도 안 붙어 있어서 이름이
    // 없고, 그 뒤로 다시 그리는 자리가 없었다 — 자동으로 다 붙은 뒤에도 그 줄은 영영 비었다.
    // 사람이 물었다(2026-09-05): "세션아이디는 언제 나옴?"
    //
    // 폴이 붙음을 보는 그 자리에서 적는다. **바뀔 때만** — 매 폴마다 그리면 첫 줄이 선 뒤에도
    // 다시 세워질 수 있고, 그 문장은 대화가 서면 사라져야 한다.
    let saidSession = '';
    watchPrompt.onChange = () => {
      drewAsk?.();
      const sid = watchPrompt.view.session ?? '';
      if (sid && sid !== saidSession) {
        saidSession = sid;
        listenTo(sid);
        // 브랜드 줄이 그 이름을 든다 — 창이 그 대화에 붙어 있는 동안 계속.
        void refreshBrand();
        void refreshContext();
      }
      view.councilButton(watchPrompt.view.council);
      if (watchPrompt.view.stale) companionRestarted();
    };
  }

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

  // 사유 넷 중 **화면이 말해야 하는 셋**(성공은 사유가 `null` 이라 이 넷 밖이고, 잠잠하다 —
  // 한동안 그 `null` 이 「모르는 사유」 그물에 걸려 진짜 PowerPoint 안에서 판 자리에
  // 「가짜 덱에 붙었습니다」가 떴다). 어느 문장이 나갈지는 `pickNote` 가 정한다 — 여기
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
