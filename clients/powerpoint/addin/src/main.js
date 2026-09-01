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
import { HelperApi } from './adapter/helperApi.js';
import { HelperStream } from './adapter/HelperStream.js';
import { HelperChat, HelperStatus, HelperTranscript } from './adapter/HelperPorts.js';
import { FakeHand } from './adapter/FakeHand.js';
import { OfficeHand } from './adapter/OfficeHand.js';
import { ServeHand } from './usecase/ServeHand.js';
import { mountPick } from './ui/pick.js';
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

  // **헬퍼가 페이지를 내줬으면 진짜로 돈다.** 토큰이 페이지에 박혀 오는 것이 그 표시이고
  // (§5.5·§12 #7), 없으면 가짜다 — **조용히 진짜인 척하지 않는 것**이 이 갈래의 요점이다.
  const boot = (typeof window !== 'undefined' && window.MAGI) ? window.MAGI : null;
  const real = Boolean(boot?.token);

  const api = real ? new HelperApi({ token: boot.token }) : null;
  // 진짜 문이 아니라 흉내다. 여기서 바꿔 끼우는 것이 곧 「데몬에 붙인다」가 된다(§5.5).
  const helperStream = real
    ? new HelperStream({ token: boot.token, presentation: boot.presentation ?? '', label: boot.label ?? '' }).open()
    : null;
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
  view.mount();
  // **대화 이름을 우리가 짓지 않는다.** 이름을 가진 쪽은 컴패니언이고(`.sock.session`),
  // `ReadTranscript` 는 남의 대화 이벤트를 신원으로 걸러 낸다 — 여기서 지어낸 이름에 붙이면
  // **진짜 이벤트가 전부 그 그물에 걸린다.** 실물에서 그 화면을 봤다(2026-09-01): 모델은 덱의
  // 제목을 실제로 고쳤는데 창은 「보냈습니다」에 멈춘 채였고, 메아리가 안 오니 사람이 적은
  // 글도 안 지워졌다. 진짜 갈래는 아래에서 컴패니언이 든 이름에 붙는다.
  if (!real) readTranscript.attach(SESSION);
  // 대화가 끊기거나 다시 붙으면 브랜드 줄도 같이 움직인다. **한 사건에 한 자리**라
  // 여기서 걸어 두고, 뷰는 자기가 받은 값만 그린다.
  const drawn = readTranscript.onChange;
  readTranscript.onChange = () => { drawn?.(); void refreshBrand(); };

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
    view.brand({ companion: bound, streamLive: readTranscript?.view?.live !== false, hands });
  };
  // 부팅 직후 한 번. **비워 두면 「아직 안 골랐다」와 「골랐는데 화면이 안 그렸다」가 같은
  // 빈칸이 된다** — 가짜 갈래에서는 이 한 줄이 「가짜 덱」이라고 적는 자리다.
  void refreshBrand();

  if (real) {
    // 손이 붙는다. **조작을 수행하는 것은 애드인이고**, 헬퍼는 그 손을 부린다(§5.1).
    // PowerPoint 안이 아니면 가짜 손을 붙인다 — 그 화면에서 도구를 눌러 볼 수 있어야
    // 「붙었는데 아무 일도 안 일어난다」를 사람이 가려낼 수 있다.
    const hand = deck instanceof FakeDeck
      ? new FakeHand(structuredClone(fixture))
      : new OfficeHand({});
    new ServeHand({ stream: helperStream, api, hand, onNote: (s) => view.where(s) }).start();

    /**
     * 이 대화에 창을 붙인다. **고르기 전에 부른다** — `choose` 가 문을 여는 순간 헬퍼가 로그를
     * 처음부터 흘려보내는데, 그때 창이 다른 이름에 붙어 있으면 그 replay 를 통째로 버린다.
     *
     * 「붙었다」고 적는 것은 여기가 아니다(그건 `choose` 가 성공한 뒤다). 여기서 하는 것은
     * **어느 이름의 이벤트를 우리 것으로 셀지**를 정하는 것뿐이라, 실패해도 거짓말이 안 된다.
     */
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
          // **붙었다는 증거는 ack 가 아니라 도구 이름이다**(§5.0.1).
          const name = nameOf(companion);
          view.where(`${name} 에 붙었습니다 — 도구 ${out?.tools?.length ?? 0} 개.` +
            (out?.chat ? ` 다만 채팅은 아직입니다: ${out.chat}` : ''));
          bound = name;
          saidStale = false;   // 다시 골랐으니 그 말은 끝났다
          // 이제부터는 스트림·데몬에 대한 말이 뜻을 갖는다(§5.7). 그 전에는 안 띄운다.
          view.setBound(true);
          await refreshBrand();
        } catch (e) {
          // **끝내 못 붙으면 말한다**(§5.3). 조용하면 화면이 「할 일 없음」처럼 보인다.
          pick.note(`못 붙였습니다: ${e?.message ?? e}`);
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
    const companionRestarted = () => {
      if (saidStale) return;
      saidStale = true;
      bound = null;
      view.setBound(false);
      view.where('붙어 있던 컴패니언이 다시 떴습니다 — 덱 도구가 떨어졌으니 다시 골라 주세요.');
      void showCompanions();
    };

    const showCompanions = async () => {
      try {
        const list = await api.companions();
        pick.render(list);
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
          view.setBound(true);
          await refreshBrand();
        }
      } catch (e) {
        pick.render({ companions: [] });
        pick.note(`컴패니언을 못 훑었습니다: ${e?.message ?? e}`);
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
    const attachOwn = async () => {
      const began = Date.now();
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
        pick.note(`자동으로 준비하지 못했습니다: ${e?.message ?? e}. 아래에서 골라 주세요.`);
        return false;
      }
      // 냉시동을 실측했다(2026-09-02). 5분을 천장으로 두되 **끝나면 바로 나간다.**
      const until = began + 5 * 60 * 1000;
      while (r?.phase === 'working' && Date.now() < until) {
        said();
        await new Promise((go) => setTimeout(go, 1500));
        try { r = await api.own(); } catch { /* 잠깐 못 물어본 것은 실패가 아니다 */ }
      }
      if (r?.phase !== 'ready') {
        // **왜 못 했는지와 어디를 볼지**를 같이 준다. 「안 됩니다」만으로는 할 일이 없다.
        const why = r?.why || (r?.phase === 'working' ? '아직 안 떴습니다(5분 넘음)' : '알 수 없음');
        pick.note(`자동으로 준비하지 못했습니다: ${why}`);
        if (r?.log) pick.note(`자세한 사유는 여기 있습니다: ${r.log}`);
        view.where('자동으로 준비하지 못했습니다 — 아래에서 컴패니언을 골라 주세요.');
        return false;
      }
      listenTo(r.session);
      pick.hide();
      // **붙었다는 증거는 ack 가 아니라 도구 이름이다**(§5.0.1).
      bound = baseNameOf(r.workdir) || 'PowerPoint';
      saidStale = false;
      view.where(`준비됐습니다 — 도구 ${r.tools?.length ?? 0} 개.`
        + (r.started ? ' (magi 를 방금 띄웠습니다)' : '')
        + (r.chat ? ` 다만 채팅은 아직입니다: ${r.chat}` : ''));
      view.setBound(true);
      await refreshBrand();
      return true;
    };

    await showCompanions();
    // 이미 붙어 있으면(작업창을 껐다 켠 경우) 그것을 흔들지 않는다 — 다시 붙이는 것은 첫 등록을
    // 떨어뜨리는 일이다(§5.0.1).
    if (!bound) await attachOwn();

    // **컴패니언이 다시 뜬 것을 폴이 알려 준다.** 화면이 이미 그리는 것 뒤에 한 줄 더 건다 —
    // `readTranscript.onChange` 를 감싼 것과 같은 자리, 같은 이유다(한 사건에 한 자리).
    const drewAsk = watchPrompt.onChange;
    watchPrompt.onChange = () => {
      drewAsk?.();
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
