// PowerPoint 없이 도는 확인. `node tools/smoke.mjs`
//
// 이게 이 목업에서 **오늘 실제로 검증되는 전부**다. 유스케이스가 Office.js 를 모르기 때문에
// FakeDeck 하나만 갈아 끼우면 흐름이 끝까지 돈다.
//
// `OfficeDeck` 에서 **호스트 없이 도는 것은 둘**이다. `capabilities()` 는 Office.js 를
// 호출하지 않고 `isSetSupported` 가 답한 것을 나르기만 하는 함수라, 그 자리에 stub 을 세우면
// 나르는 계약(여섯을 요약 안 한다 / 던진 것을 「아니오」로 안 접는다)을 진짜로 잰다.
// `selection()` 은 `PowerPoint.run` 을 흉내 낸 stub 위에서 도는데, **그건 호스트가 아니다** —
// 거기서 무는 것은 우리가 고른 가지(1.8 이 없으면 index 를 안 묻는다 / 빈 선택은 왕복 한 번 /
// 글을 잃어도 신원은 산다)뿐이고, 호스트가 실제로 어떻게 답하는지는 **여전히 안 재 봤다.**
// `point()` 는 아직 한 번도 안 돌았다. S13·S14 는 둘 다 열려 있다 — 안 돌려 본 것을 "된다"고
// 세지 않는다.
import { Composer, promptOf } from '../src/domain/Composer.js';
import { Quote } from '../src/domain/Quote.js';
import { Advice, targetLabel, SlideNumbers } from '../src/domain/Advice.js';
import { foldAdvice, adviceNote } from '../src/domain/AdviceBoard.js';
import { DeckPort } from '../src/port/DeckPort.js';
import { FakeDeck } from '../src/adapter/FakeDeck.js';
import { OfficeDeck } from '../src/adapter/OfficeDeck.js';
import { pickDeck, pickNote, lateNote, lateFailNote } from '../src/adapter/pickDeck.js';
import { QuoteSelection, quoteNote } from '../src/usecase/QuoteSelection.js';
import { SendTurn, logShapeOf, sendNote } from '../src/usecase/SendTurn.js';
import { FakeChat } from '../src/adapter/FakeChat.js';
import { PointAtAdvice } from '../src/usecase/PointAtAdvice.js';
import { readFileSync, readdirSync } from 'node:fs';
import { fixture } from '../src/ui/deckFixture.js';
import {
  headOf, rowHead, rowShape, rowClass, argsCell, endText, bodyText,
  isSendKey, askAction, askReveal, askKind, askHead, whatText, argsText, placeLine, doingLine,
  lastAskShape, decisionClass, failNote, noteLife, capsOf, capsText, streamLine,
  unknownLine, quoteBody, quoteMeta, adviceBoard, adviceTargetText, pretty, clip,
  capsSummary, brandState, resultCell, permissionText, councilBody, skippedLine,
  adapterText, readyText, guideBoard, planBoard, changedLines, toolLabel, labelledTools,
  planAnchor, reviewAsk, appendAsk, confirmAsk, thinkHead, oneLine, turnRunning,
} from '../src/ui/screen.js';
import { Transcript } from '../src/domain/Transcript.js';
import { FakeTranscript } from '../src/adapter/FakeTranscript.js';
import { ReadTranscript } from '../src/usecase/ReadTranscript.js';
import { FakeStatus } from '../src/adapter/FakeStatus.js';
import { WatchPrompt, askSig } from '../src/usecase/WatchPrompt.js';
import { Pending, DECISIONS, CLEARED, clearedNote, askArgs } from '../src/domain/Pending.js';
import { Cursor } from '../src/domain/Cursor.js';

let failed = 0;
const ok = (name, cond, detail = '') => {
  console.log(`${cond ? '  ok  ' : '  FAIL'} ${name}${detail ? ' — ' + detail : ''}`);
  if (!cond) failed++;
};

/**
 * 훑어서 「전부 그렇다」를 묻는다. **빈 것에는 참을 안 준다.**
 *
 * `[].every(f)` 는 늘 참이라, 훑을 것이 없는 단언은 술어가 무엇이든 초록이다 — 「하나도 안
 * 틀렸다」와 「볼 것이 없었다」가 같은 글자로 찍힌다. 이 파일에 그런 줄이 실제로 하나 서
 * 있었다(번호 붙은 슬라이드가 없는 판에서 번호표를 훑었다). 술어를 상수 거짓으로 바꿔도
 * 스위트가 통과하는 것으로 쟀다.
 *
 * 「없다」를 물을 때 이 파일이 쓰는 말은 `.length === 0` 이지 `every` 가 아니다. 그래서
 * 여기서 `every` 는 언제나 「이만큼 있고 전부 그렇다」는 뜻이고, 빈 것은 답이 아니라 결함이다.
 *
 * 안에서 `.every(` 를 안 쓰는 것은 일부러다 — 쓰면 아래 스캔이 자기 정의를 예외로 빼야 하고,
 * 늘리는 값이 0 인 예외 목록은 곧 전부가 된다.
 */
const everyOf = (arr, pred) => arr.length > 0 && !arr.some((x, i) => !pred(x, i));

const deck = new FakeDeck(structuredClone(fixture));
const conv = new Composer();
const quote = new QuoteSelection(deck, conv);
const point = new PointAtAdvice(deck);

// 아무것도 안 골랐을 때. "빈 선택"과 "포커스가 선택을 가져감"을 뭉뚱그리면 S14 를 못 잰다.
ok('빈 선택은 empty', (await quote.run()).empty === true);

// S14 의 계측기. 누를 때 한 번 읽어서는 못 가른다 — 누르기 **전** 읽기가 있어야
// "안 골랐다"·"포커스가 가져갔다"·"모른다"가 갈린다. 답을 정해 주는 덱으로 셋을 다 만든다.
{
  const shape = { id: 'shX', name: '제목', type: 'TextBox', text: '가', width: 1, height: 1 };
  const one = { slideId: 's1', slideNo: 1, shapes: [shape] };
  const none = { slideId: 's1', slideNo: 1, shapes: [] };
  // 답을 순서대로 내고 다 떨어지면 빈 선택. 마지막을 되풀이하면 "낡은 읽기" 검사가 헛돈다.
  const scripted = (...answers) => ({
    label: 'scripted', async selection() { return answers.shift() ?? none; },
  });

  const lost = new QuoteSelection(scripted(one, none), new Composer());
  await lost.sampleBeforeFocus();
  const rl = await lost.run();
  ok('포커스가 가져간 선택은 lostFocus',
     rl.reason === 'lostFocus' && rl.beforeCount === 1, rl.reason);

  const empty2 = new QuoteSelection(scripted(none, none), new Composer());
  await empty2.sampleBeforeFocus();
  ok('원래 빈 선택은 none', (await empty2.run()).reason === 'none');

  // 단축키·키보드로 누르면 포인터가 단추에 들어온 적이 없다 → 앞 읽기가 아예 없다.
  const blind = new QuoteSelection(scripted(none), new Composer());
  ok('앞 읽기가 없으면 unknown', (await blind.run()).reason === 'unknown');

  // 앞 읽기는 한 번 쓰고 버린다. 안 버리면 두 번째 누름이 낡은 값으로 lostFocus 를 지어낸다.
  const stale = new QuoteSelection(scripted(one, none, none), new Composer());
  await stale.sampleBeforeFocus();
  await stale.run();
  ok('낡은 앞 읽기는 다음 누름에 안 샌다', (await stale.run()).reason === 'unknown');

  // 인용에 성공한 길에도 사유 칸이 있고, 거기엔 사유가 없다.
  const okrun = new QuoteSelection(scripted(one), new Composer());
  ok('인용되면 사유는 null', (await okrun.run()).reason === null);

  // 앞 읽기는 **왕복**이라 누름보다 늦게 올 수 있다. 빨리 누르면 실제로 그렇다.
  // 늦게 온 값을 안 버리면 그게 다음 누름의 앞 읽기 자리에 앉아 lostFocus 를 지어낸다.
  {
    let release;
    const slow = new Promise((r) => { release = r; });
    let firstRead = true;
    const laggy = { async selection() {
      if (firstRead) { firstRead = false; await slow; return one; }
      return none;
    } };
    const q = new QuoteSelection(laggy, new Composer());
    const inflight = q.sampleBeforeFocus();      // 호버 — 안 기다린다. 화면도 안 기다린다.
    const fast = await q.run();                  // 읽기가 오기 전에 눌렸다
    release();
    await inflight;                              // 늦은 읽기가 이제 도착한다
    ok('늦게 온 앞 읽기는 그 누름에 못 쓴다', fast.reason === 'unknown', fast.reason);
    ok('늦게 온 앞 읽기는 다음 누름에도 안 앉는다',
       (await q.run()).reason === 'unknown');
  }
}

// 하나 고르고 인용.
deck.click('sh8c30', false);
const r1 = await quote.run();
ok('하나 인용', r1.added.length === 1 && conv.pending.length === 1);
ok('인용문에 식별자가 남는다', conv.pending[0].toPrompt().includes('shape=sh8c30'),
   conv.pending[0].headline);

// 사람은 번호로, 도구는 id 로. 번호는 슬라이드를 끌어 옮기면 낡으므로 모델에게 안 간다.
ok('카드는 번호로 적는다', conv.pending[0].where === '슬라이드 4', conv.pending[0].where);
ok('인용문에는 번호가 없다', !conv.pending[0].toPrompt().includes('슬라이드'));
ok('번호를 모르면 id 로 적는다',
   new Quote({ slideId: 's9', shapeId: 'x' }).where === '슬라이드 s9');
ok('가짜 덱은 번호표를 준다', (await deck.slideNumbers()).get('s7') === 7);

// 긴 글은 자르되 **자른 티가 나야** 한다 — 모델이 그걸 전문으로 읽으면 뒤쪽을 없는 셈 친다.
{
  const long = new Quote({
    slideId: 's1', shapeId: 'sh1', type: 'TextBox', text: '가'.repeat(900),
  });
  const p = long.toPrompt();
  ok('긴 인용은 잘리고 잘렸다고 적힌다', p.includes('textTruncated=900') && p.length < 900);
  const short = new Quote({ slideId: 's1', shapeId: 'sh1', type: 'TextBox', text: '짧다' });
  ok('짧은 인용에는 그 표시가 없다', !short.toPrompt().includes('textTruncated'));
}

// 같은 도형 또 인용 — 중복은 안 쌓인다.
const r2 = await quote.run();
ok('같은 도형은 skipped', r2.skipped === 1 && conv.pending.length === 1);

// 여러 개.
deck.click('sh8c31', true);
await quote.run();
ok('추가 인용은 쌓인다', conv.pending.length === 2);

// 그런데 **위 두 줄은 shift 를 안 눌러도 초록이다.** 인용은 누를 때마다 쌓이니까 「둘이 됐다」가
// 「둘을 한꺼번에 골랐다」를 안 말한다 — `click` 의 `additive` 를 통째로 떨어뜨려도 스위트가
// 안 죽었다(인자 드롭 계측). shift-클릭은 사람이 「이것과 이것」을 한 번에 말하는 유일한 손짓이라,
// 조용히 없어지면 두 번째 클릭이 첫 번째를 지우고 사람은 왜 하나만 인용됐는지 모른다.
{
  const d = new FakeDeck(structuredClone(fixture));
  const c = new Composer();
  const q = new QuoteSelection(d, c);
  const [a, b] = d.slide(d.currentSlide).shapes;
  d.click(a.id, false);
  d.click(b.id, true);
  ok('shift 를 누르면 고른 것이 쌓인다', d.selected.size === 2, d.selected.size);
  const r = await q.run();
  ok('한 번 눌러 둘이 인용된다', r.added.length === 2 && c.pending.length === 2,
    `${r.added.length} / ${c.pending.length}`);
  // 그리고 shift 없이 다시 고르면 **앞엣것이 지워진다.** 쌓이기만 하면 사람이 고름을 못 되돌린다.
  d.click(a.id, false);
  ok('shift 없이 고르면 앞엣것이 풀린다', d.selected.size === 1 && d.selected.has(a.id),
    [...d.selected].join(','));
}

// 내면 인용이 **글로 접혀** 나간다 — 문의 submit 은 글 하나만 받는다.
{
  const held = conv.hold('이 두 개를 한 줄로 붙여줘', 0);
  ok('인용이 프롬프트 글에 실린다',
    held.prompt.includes('shape=sh8c30') && held.prompt.includes('shape=sh8c31'),
    held.prompt.slice(0, 40));
  ok('사람이 적은 말이 뒤에 붙는다', held.prompt.endsWith('이 두 개를 한 줄로 붙여줘'));
  // **안 비운다.** 지우는 것은 로그의 메아리다(§5.7).
  ok('내도 인용은 그대로 있다', conv.pending.length === 2);
  ok('기다리는 동안은 못 낸다', conv.canSend('또') === false);
  conv.clear();
  ok('메아리가 오면 비운다', conv.pending.length === 0 && conv.waiting === false);
}

// 안내가 가리키는 것 — 있는 도형은 되고, 없는 도형은 **다른 걸 대신 가리키지 않고 실패한다.**
const good = await point.run(
  new Advice({ message: '여기가 넘칩니다', slideId: 's4f2a1', shapeIds: ['sh8c30'] }));
ok('가리키기 성공', good.ok === true);
const bad = await point.run(
  new Advice({ message: '사라진 도형', slideId: 's4f2a1', shapeIds: ['sh-없음'] }));
ok('없는 도형은 대체 없이 실패', bad.ok === false, bad.reason);
// **사유를 `ok` 의 곁다리로만 넘기고 있었다.** 위 줄은 `bad.reason` 을 detail 로만 찍지
// 단언하지 않아서, 던진 것의 말을 통째로 버려도(`reason: undefined`) 초록이었다. 그러면
// 화면은 「가리키지 못했습니다」라고만 적고 **왜인지는 어디에도 안 남는다** — 이 창에서만
// 세 번째 같은 모양이다(`msgOf` 의 `?? String(e)`, `Transcript.restart(why)`).
ok('실패에 던진 쪽의 말이 실려 온다',
   /찾을 수 없는 도형/.test(bad.reason ?? '') && /sh-없음/.test(bad.reason ?? ''), bad.reason);

// 다른 슬라이드의 도형은 슬라이드까지 옮겨야 가리켜진다.
const cross = await point.run(new Advice({ message: '뒷장', slideId: 's7', shapeIds: ['sh7b'] }));
ok('슬라이드 이동 후 가리킨다', cross.ok === true && deck.currentSlide === 's7');

// 도형을 안 실은 안내. **슬라이드는 가되 앞의 것을 놓는다** — 안 놓으면 캔버스에 서 있는
// 것이 앞 안내의 도형이라, 새 안내가 저 도형에 대한 것이라고 말하게 된다(§12 #9).
{
  const bare = await point.run(
    new Advice({ message: '이 장 전체', slideId: 's4f2a1', shapeIds: [] }));
  ok('도형 없는 안내도 가리켜진다',
     bare.ok === true && deck.currentSlide === 's4f2a1', bare.reason);
  ok('도형이 없으면 앞의 선택을 놓는다', deck.selected.size === 0,
     [...deck.selected].join(','));
}

// 계측이 스스로에 대해 거짓말하지 않는가. 가짜 덱이 `measured:true` 를 내면 화면이 실측처럼
// 보이고, 그러면 §12 #4 를 답해 줄 유일한 줄이 처음부터 못 쓰게 된다.
const caps = deck.capabilities();
ok('가짜 덱은 잰 게 없다고 말한다', caps.measured === false && caps.sets.length === 0);
ok('안 쟀으면 사유가 있다', typeof caps.note === 'string' && caps.note.length > 0);

// 안 덮은 어댑터도 사유를 낸다 — 오늘 프로덕션 어댑터 둘은 `capabilities()` 를 다 덮으므로
// 이 기본값을 보는 사람은 **다음 어댑터를 쓰는 사람**뿐이고, 그래서 아무도 안 밟는다. 여기서
// 한 번 밟아 둔다: 사유가 비면 화면은 「요구 집합: 」만 적고 그건 계측을 안 한 것과 못 한 것을
// 같은 침묵으로 만든다. (사유가 `measured:false` 를 되풀이하는 것만 적는 결함은 기계가 못
// 가른다 — 그건 사람이 읽어야 한다.)
{
  class BarePort extends DeckPort {}
  const base = new BarePort().capabilities();
  ok('안 덮은 어댑터의 기본값도 사유를 낸다',
     base.measured === false && typeof base.note === 'string' && base.note.length > 0, base.note);
}


// ── 대화 스트림(§5.7). 문의 계약을 그대로 시험한다 — 여기는 PowerPoint 가 없어도 다 잰다.
{
  const ev = (seq, type, text) => ({ seq, sessionId: 'A', type, data: { text } });
  const port = new FakeTranscript({
    A: [ev(1, 'prompt.submitted', '제목 키워'), ev(2, 'part.appended', '키웠습니다')],
    B: [{ seq: 1, sessionId: 'B', type: 'prompt.submitted', data: { text: '다른 대화' } }],
  });
  const read = new ReadTranscript(port);

  ok('첫 접속은 전부를 청한다', read.attach('A') === -1);
  ok('받은 만큼 커서가 선다', read.cursor.seq === 2 && read.cursor.sessionId === 'A');
  ok('다시 붙으면 그 자리부터', read.attach('A') === 2);

  // **대화가 옮겨 가면 따라간다.**
  //
  // 「새 대화 시작」은 세션을 새로 만들고 `session.moved` 하나를 남긴다. 앞 판본은 그것을
  // 모르는 이벤트로 흘려보냈고, 그 뒤로 오는 것은 전부 다른 sessionId 라 걸름망에 걸려
  // 사라졌다 — 창은 「대화 스트림이 끊겼습니다」를 띄운 채 영영 아무것도 안 그렸다.
  // 실물에서 그 화면을 봤다(2026-09-03): 모델은 그동안 슬라이드 일곱 장을 만들고 있었는데
  // 사람은 빈 창을 보고 있었다.
  {
    const moved = { seq: 3, sessionId: 'A', type: 'session.moved', data: { to: 'B' } };
    const port2 = new FakeTranscript({
      A: [ev(1, 'prompt.submitted', '앞 대화'), moved],
      B: [{ seq: 1, sessionId: 'B', type: 'part.appended', data: { text: '옮겨 온 뒤의 말' } }],
    });
    const follow = new ReadTranscript(port2);
    follow.attach('A');
    ok('옮겨 간 대화를 따라간다', follow.sessionId === 'B', String(follow.sessionId));
    ok('옮겨 온 뒤의 말이 화면에 선다',
      follow.view.rows.some((r) => r.text === '옮겨 온 뒤의 말'),
      JSON.stringify(follow.view.rows.map((r) => r.text)));
    ok('앞 대화는 안 남는다', !follow.view.rows.some((r) => r.text === '앞 대화'));
  }

  // 대화가 바뀌면 커서를 버린다. **서버가 못 잡아 주는 자리**라 우리가 메운다.
  ok('대화가 바뀌면 커서를 버린다', read.attach('B') === -1);
  // **`every` 는 빈 것에 참이다.** 지운 쪽만 물면 「앞엣것을 지웠다」와 「아무것도 안 그렸다」가
  // 같은 초록이 되고, 통째로 비우는 구현이 만점을 받는다. 이 대화의 줄이 실제로 섰는지까지
  // 같이 문다 — 부재를 재는 단언은 무엇이 남아 있어야 하는지를 같이 적어야 잰다.
  ok('앞 대화가 화면에 안 남고 이 대화가 선다',
    everyOf(read.view.rows, (r) => r.text !== '키웠습니다')
      && read.view.rows.some((r) => r.text === '다른 대화'),
    read.view.rows.map((r) => r.text).join('|'));

  // **말한 것과 보낸 것.** 위 세 줄이 무는 것은 `attach` 의 **반환값**이고, 문이 실제로 받은
  // 값은 가짜의 `calls` 에 있다. 가짜는 그걸 보라고(「시험이 보는 것: 실제로 보낸 since」)
  // 들고 있는데 **여태 아무도 안 봤다** — `since` 를 통째로 안 실어도 스위트가 초록이었다
  // (필드 드롭 계측). 정직하게 적자면 셈을 상수로 바꾸는 뮤턴트는 옆의 거절·배우 시험들이
  // 이미 잡는다. 이 줄이 혼자 잡는 것은 **계측기 쪽이 조용히 죽는 경우**고, 그게 제일 나쁜
  // 종류다 — 계측기가 안 보면 나머지 단언들이 무엇을 봤는지도 못 믿게 된다.
  ok('문에 실제로 간 것이 말한 것과 같다',
    port.calls.map((c) => `${c.sessionId}:${c.since}`).join(' → ') === 'A:-1 → A:2 → B:-1',
    port.calls.map((c) => `${c.sessionId}:${c.since}`).join(' → '));

  // 거절 프레임. 안 읽으면 보던 대화 뒤에 같은 대화의 처음이 이어 붙는다.
  const port2 = new FakeTranscript({ A: [ev(1, 'prompt.submitted', '첫 줄')] });
  const read2 = new ReadTranscript(port2);
  read2.cursor = read2.cursor.advanced('A', 40);   // 어제 커서를 들고 왔다
  read2.sessionId = 'A';
  read2.attach('A');
  // **`!== null` 은 화면이 쓰는 물음이 아니다.** view 는 `if (v.refusal)` 로 읽으므로 빈
  // 문자열이면 아무 줄도 안 그린다 — 그런데 `!== null` 은 초록이다. 그 틈으로 「거절당했다」가
  // 시험에서만 참이고 사람에게는 아무 말도 안 하는 상태가 지나간다. 서버가 준 문장을 그대로
  // 실어 오는지까지 문다(인자 드롭 계측: `Transcript.restart` 의 `why` 를 비워도 초록이었다).
  ok('로그 끝을 넘은 커서는 거절당한다', Boolean(read2.view.refusal), read2.view.refusal);
  ok('거절에 서버가 댄 사유가 실려 온다',
    /40/.test(read2.view.refusal ?? '') && /past the end/.test(read2.view.refusal ?? ''),
    read2.view.refusal);
  ok('거절 뒤 화면은 한 벌뿐이다', read2.view.rows.length === 1, `${read2.view.rows.length}줄`);
  ok('거절당한 커서는 버려진다', read2.cursor.seq === 1);

  // 모르는 종류를 안 버린다. 버리면 화면이 "아무 일도 없었다"처럼 보인다.
  //
  // **예로 든 종류가 또 바뀌었다 — 두 번째다.** 처음엔 `council.verdict` 였고 그것을 그리게
  // 되면서 `todos.changed` 로 갈았는데, 이제 그것도 판으로 그린다. 예를 그리게 되면 그 시험은
  // **규칙이 아니라 예를 지키는 것**이 되므로 아직 진짜로 안 그리는 것으로 다시 갈았다.
  //
  // 두 번 겪었으니 적어 둔다: 이 시험이 무는 것은 「이 종류를 못 그린다」가 아니라 **「못 그린
  // 것을 세어서 말한다」**이고, 그래서 예는 **언젠가 반드시 낡는다.** 낡을 때 이 자리가 빨개지는
  // 것이 정상이고, 고치는 법은 예를 갈아 끼우는 것이지 시험을 지우는 것이 아니다.
  const port3 = new FakeTranscript({ A: [] });
  const read3 = new ReadTranscript(port3);
  read3.attach('A');
  port3.push({ seq: 1, sessionId: 'A', type: 'workflow.phase', data: {} });
  port3.push({ seq: 2, sessionId: 'A', type: 'labels.changed', data: {} });
  // `!== null` 은 화면이 쓰는 물음이 아니다 — `renderUnknown` 은 `el.hidden = !note` 로
  // 읽으므로 `undefined` 면 조용히 감춘다. 뷰 모델이 이 칸을 통째로 안 실어도 이 줄이
  // `!== null` 이던 동안은 초록이었다(필드 드롭 계측). **거절 사유와 같은 어긋남이다.**
  ok('모르는 종류는 안 그려도 안 사라진다', read3.view.rows.length === 0
    && Boolean(read3.view.unknownNote), read3.view.unknownNote ?? '(말이 없다)');
  ok('안 그린 것이 몇 건인지 그 말에 든다',
    /workflow\.phase/.test(read3.view.unknownNote ?? '')
    && /labels\.changed/.test(read3.view.unknownNote ?? ''), read3.view.unknownNote);
  ok('모르는 것도 커서는 민다', read3.cursor.seq === 2);

  // 위 두 사건은 `data` 가 비어 있어서 **못 그린 것과 안 실은 것이 같게 생긴다.** 알맹이를
  // 실은 모르는 사건을 하나 넣어 본다: 줄은 서되 **글은 안 옮겨 실려야** 한다. 못 알아본 모양의
  // 페이로드를 화면 글로 옮기는 것은 우리가 무슨 말을 그리는지 모르는 채로 그리는 일이다
  // (`textOf` 의 `kind === 'unknown'` 줄). 그 인자를 떨어뜨려도 스위트가 안 죽었다.
  {
    const t = new Transcript();
    t.append({ seq: 1, type: 'workflow.phase', data: { text: '워크플로가 뭐라고 했다' } });
    ok('모르는 종류도 줄은 선다', t.rows.length === 1 && t.rows[0].kind === 'unknown',
      `${t.rows.length} / ${t.rows[0]?.kind}`);
    ok('모르는 종류의 알맹이는 글로 안 옮겨진다', t.rows[0].text === '', t.rows[0].text);
    ok('모르는 줄은 안 그려진다', t.drawnRows.length === 0, t.drawnRows.length);
  }

  // 배우를 안 보면 정책이 한 일이 사용자가 한 말로 붙는다. §5.7 이 이름까지 대 놓은 결함이라
  // 여기서 못 박는다 — 버리지도 않고(그건 TUI 가 겪은 반대쪽 결함), 말풍선으로도 안 그린다.
  const port4 = new FakeTranscript({ A: [] });
  const read4 = new ReadTranscript(port4);
  read4.attach('A');
  port4.push({ seq: 1, sessionId: 'A', type: 'prompt.submitted',
    actor: { kind: 'user', id: 'u' }, data: { text: '제목 키워' } });
  port4.push({ seq: 2, sessionId: 'A', type: 'prompt.submitted',
    actor: { kind: 'system', id: 'policy' }, data: { text: 'allow-once (기본값)' } });
  const kinds4 = read4.view.rows.map((r) => r.kind);
  ok('정책이 낸 줄은 사용자 말풍선이 아니다',
    kinds4.length === 2 && kinds4[0] === 'user' && kinds4[1] === 'note', kinds4.join('/'));

  // 버스 전용 이벤트는 자리를 안 가진다(seq 0). 그대로 커서에 넣으면 자리가 **뒤로 가고**,
  // 0 은 계약상 "전부"라 다음 접속이 대화를 통째로 다시 받는다 — 거절 프레임도 없이 조용히.
  port4.push({ seq: 0, sessionId: 'A', type: 'part.delta',
    data: { messageId: 'm1', text: '키' } });
  ok('자리 없는 이벤트는 자리를 안 만든다',
    read4.transcript.rows.at(-1).positioned === false);
  ok('그래서 다시 붙어도 처음부터가 아니다', read4.attach('A') === 2);

  // 배우를 **안 밝힌** 줄. 코어의 `Actor` 는 구조체라 빈 `kind` 로도 오고, 그건 「사용자가
  // 넣었다」가 아니다. 「user 가 아니면」으로 물으면 이게 말풍선이 되고, 그 다음이 더 나쁘다 —
  // 낸 글을 지우는 신호가 사용자 줄의 **수**라 남의 줄 하나가 사람이 쓰던 글을 지운다.
  port4.push({ seq: 3, sessionId: 'A', type: 'prompt.submitted',
    actor: { kind: '', id: '' }, data: { text: '누가 넣었는지 안 실린 줄' } });
  port4.push({ seq: 4, sessionId: 'A', type: 'prompt.submitted',
    data: { text: '배우 자체가 없는 줄' } });
  const kindsAnon = read4.view.rows.slice(3).map((r) => r.kind);
  ok('안 밝힌 배우를 사용자로 세지 않는다',
    kindsAnon.length === 2 && everyOf(kindsAnon, (k) => k === 'note'), kindsAnon.join('/'));
  ok('안 밝힌 것과 밝힌 것을 줄이 구분해 든다',
    read4.view.rows[1].attributed === true && read4.view.rows[3].attributed === false
      && read4.view.rows[4].attributed === false);

  // 화면이 그 차이를 실제로 말하는가. 「사람이 아닌 배우가 넣었다」는 밝혔을 때만 할 수 있는
  // 말이고, 안 밝힌 줄에 그걸 적으면 모르는 것을 아는 것처럼 적는 것이다.
  ok('머리도 둘을 다르게 적는다',
    headOf(read4.view.rows[1]) !== headOf(read4.view.rows[3])
      && headOf(read4.view.rows[3]) === '⟳ 누가 넣었는지 안 밝힌 줄',
    String(headOf(read4.view.rows[3])));

  // 그리고 이게 왜 화면 모양만의 문제가 아닌지 — 컴포저까지 내려가서 잰다.
  const anonComp = new Composer();
  anonComp.hold('사람이 쓰던 글', read4.view.rows.filter((r) => r.kind === 'user').length);
  port4.push({ seq: 5, sessionId: 'A', type: 'prompt.submitted',
    actor: { kind: '', id: '' }, data: { text: '또 안 밝힌 줄' } });
  ok('안 밝힌 줄은 사람이 쓰던 글을 안 지운다',
    anonComp.echoed(read4.view.rows.filter((r) => r.kind === 'user').length) === false);

  // 델타와 완성본은 같은 말 두 번이다(같은 messageId). 둘 다 쌓으면 모델의 답이 두 번 뜨고,
  // 다시 붙은 창은 `appended` 만 받으므로 **붙어 있던 창과 화면이 갈린다.**
  const port5 = new FakeTranscript({ A: [] });
  const read5 = new ReadTranscript(port5);
  read5.attach('A');
  port5.push({ seq: 0, sessionId: 'A', type: 'part.delta', data: { messageId: 'm1', text: '키' } });
  port5.push({ seq: 0, sessionId: 'A', type: 'part.delta',
    data: { messageId: 'm1', text: '웠습니다' } });
  const live5 = read5.view.rows.map((r) => r.text);
  ok('조각은 한 줄로 이어진다', live5.length === 1 && live5[0] === '키웠습니다', live5.join('|'));
  port5.push({ seq: 1, sessionId: 'A', type: 'part.appended',
    data: { messageId: 'm1', part: { text: '키웠습니다' } } });
  const after5 = read5.view.rows;
  ok('완성본이 와도 줄이 늘지 않는다',
    after5.length === 1 && after5[0].text === '키웠습니다', `${after5.length}줄`);
  ok('완성본이 오면 자리가 생긴다', after5[0].positioned && read5.cursor.seq === 1);

  // 그리고 나중에 붙은 창(= replay 로 `appended` 만 받는 쪽)이 같은 화면을 봐야 한다.
  const read5b = new ReadTranscript(port5);
  read5b.attach('A');
  ok('나중에 붙은 창도 같은 화면이다',
    read5b.view.rows.length === 1 && read5b.view.rows[0].text === '키웠습니다',
    read5b.view.rows.map((r) => r.text).join('|'));

  // 빈 대화에 붙으면 이벤트가 한 장도 안 온다. 그때 알려 주지 않으면 화면에는 **붙기 전에
  // 그린 그림**이 그대로 서 있다 — 브라우저에서 「스트림이 끊겼습니다」가 그렇게 떠 있었다.
  {
    const empty = new FakeTranscript({ Z: [] });
    let drew = 0;
    const r = new ReadTranscript(empty);
    r.onChange = () => { drew += 1; };
    r.attach('Z');
    ok('빈 대화에 붙어도 한 번은 알린다', drew === 1, String(drew));
    ok('붙자마자 살아 있다고 말한다', r.view.live === true);
  }

  // 끊김. 문은 깨끗한 끝을 에러로 안 준다 — 그래서 조용한 대화와 죽은 스트림이 똑같이 생겼다.
  ok('붙어 있는 동안은 살아 있다', read3.view.live === true);
  port3.drop();
  ok('끊기면 화면이 그걸 안다', read3.view.live === false);

  // 연결이 둘이라는 사실(§5.7 — `transcript` 는 연결을 통째로 가져가므로 헬퍼는 두 번 붙는다).
  // 요청 쪽이 멀쩡히 도는 것이 스트림이 살아 있다는 증거가 아니다. 그래서 제출이 성공해도
  // `live` 가 되살아나면 안 된다 — 되살아나면 화면은 죽은 스트림을 살아 있다고 그린다.
  const chat = new FakeChat(new FakeTranscript(), { sessionId: 'sess-other', delay: -1 });
  const send = new SendTurn(chat, new Composer());
  const sent = await send.run('제목 줄여줘', { userRows: 0, live: read3.view.live });
  ok('스트림이 죽어도 제출은 간다', sent.sent === true && chat.sent[0] === '제목 줄여줘');
  ok('제출 성공이 스트림을 되살리지 않는다', read3.view.live === false);
  // 메아리가 올 곳이 없는데 잠그면 **영영 안 풀린다.** 그 대신 갔는지 모른다고 말한다.
  ok('메아리를 못 받을 땐 안 잠근다',
    sent.blind === true && send.composer.waiting === false);
}

// ── 조각의 종류(§5.7). 코어는 `messageId` 하나에 조각 **하나**를 싣는다(`PartAppendedData`).
// 그래서 「모델이 말하고 도구를 부른 턴」은 같은 messageId 로 이벤트가 둘 온다.
{
  const app = (mid, part) => ({ seq: 0, type: 'part.appended', data: { messageId: mid, part } });
  const dlt = (mid, kind, text) =>
    ({ seq: 0, type: 'part.delta', data: { messageId: mid, kind, text } });

  // 도구 호출이 답을 지우던 자리. 완성본은 통째라 덮어쓰는데, 조각 종류를 안 보면
  // **글 없는 도구 조각이 모델의 답을 덮는다.**
  const t1 = new Transcript();
  t1.append(app('m1', { kind: 'text', text: '키웠습니다' }));
  const call = { callId: 'c1', name: 'mcp__ppt__set_text' };
  t1.append(app('m1', { kind: 'tool-call', toolCall: call }));
  const said = t1.rows.find((r) => r.kind === 'model');
  ok('도구 호출이 모델의 답을 안 지운다', said?.text === '키웠습니다', said?.text ?? '(없음)');
  ok('도구 호출은 제 줄로 선다', t1.rows.length === 2 && t1.rows[1].kind === 'tool');
  ok('도구 줄은 이름을 들고 있다',
    t1.rows[1].tool === 'mcp__ppt__set_text', t1.rows[1].tool ?? '(없음)');

  // 추론은 모델의 혼잣말이지 사용자에게 한 말이 아니다. 델타도 종류를 싣는다(`PartDeltaData`).
  const t2 = new Transcript();
  t2.append(dlt('m2', 'reasoning', '음… 상자 폭 문제군'));
  t2.append(dlt('m2', 'text', '키웠습니다'));
  const answer = t2.rows.find((r) => r.kind === 'model');
  ok('추론이 답풍선에 안 섞인다', answer?.text === '키웠습니다', answer?.text ?? '(없음)');
  ok('그렇다고 추론을 버리지도 않는다', t2.rows.some((r) => r.kind === 'think'));

  // 델타로 쌓다 완성본이 오면 같은 말이라 덮어쓴다. 여기까지는 예전 그대로.
  const t3 = new Transcript();
  t3.append(dlt('m3', 'text', '키'));
  t3.append(dlt('m3', 'text', '웠습니다'));
  t3.append(app('m3', { kind: 'text', text: '키웠습니다' }));
  ok('델타 뒤 완성본은 되풀이가 아니다',
    t3.rows.length === 1 && t3.rows[0].text === '키웠습니다', t3.rows.map((r) => r.text).join('|'));

  // 그런데 **완성본 둘**은 되풀이가 아니라 다음 조각이다(로그를 처음부터 읽으면 델타가 없다).
  const t4 = new Transcript();
  t4.append(app('m4', { kind: 'text', text: '먼저.' }));
  t4.append(app('m4', { kind: 'text', text: ' 그리고.' }));
  ok('완성본 둘은 이어 붙는다',
    t4.rows.length === 1 && t4.rows[0].text === '먼저. 그리고.', t4.rows[0].text);

  // 못 그리는 조각. 「part.appended 3건」은 무엇을 못 그렸는지 안 알려 준다.
  const t5 = new Transcript();
  t5.append(app('m5', { kind: 'image', image: { path: 'a.png' } }));
  ok('못 그린 조각은 조각 이름까지 적는다',
    /part\.appended \(image\)/.test(t5.unknownNote ?? ''), t5.unknownNote ?? '(없음)');
}

// ── 권한 물음(§5.7). 스트림에 안 오는 것이라 따로 돈다.
{
  const st = new FakeStatus();
  let drew = 0;
  const w = new WatchPrompt(st, { onChange: () => { drew++; } });

  st.ask({ id: 'call_7', kind: 'permission', what: 'mcp__ppt__set_text',
    reason: '쓰기 도구는 허용 규칙에 없습니다' });
  await w.poll();
  ok('묻는 것이 서면 화면에 선다', w.view.pending?.id === 'call_7');
  // 이 물음에는 인자가 없다 — 소켓의 `Args` 는 `omitempty` 라 **진짜로 이렇게 온다.** 화면이
  // 이때 인자 칸을 통째로 안 만들면 사람은 무엇을 허가하는지 모른 채 누른다(`askArgs`).
  ok('인자 없이 온 권한 물음은 그 사실이 칸의 내용이다',
    askArgs(w.view.pending)?.note != null);

  // 폴링이 같은 것을 계속 실어 온다. 매번 새로 그리면 고르던 것이 지워지고, 스크린 리더는
  // 대기가 이어지는 내내 같은 말을 되풀이한다.
  const before = drew;
  await w.poll(); await w.poll();
  ok('같은 물음을 다시 그리지 않는다', drew === before, `${drew - before}회 더 그림`);

  // 답을 보내는 것과 물음이 내려가는 것은 다른 일이다. 직접 내리면 답이 실패했는데도 사라진다.
  await w.answer('always');
  ok('답은 call id 로 간다',
    st.answers.length === 1 && st.answers[0].callId === 'call_7'
    && st.answers[0].decision === 'always');
  ok('보냈다고 화면에서 안 내린다', w.view.pending?.id === 'call_7');
  st.clear();
  await w.poll();
  ok('내려가는 것은 다음 status 가 말한다', w.view.pending === null);
  ok('우리가 답한 것으로 적힌다', w.view.clearedBy === CLEARED.answered);

  // 남이 답한 경우. 무엇으로 답했는지는 안 찍는다 — 찍으면 남의 입에 결정을 넣는 것이 된다.
  st.ask({ id: 'call_8', kind: 'permission', what: 'mcp__ppt__delete_shape' });
  await w.poll();
  st.clear();
  await w.poll();
  ok('남이 답하면 사유가 다르다', w.view.clearedBy === CLEARED.elsewhere);
  ok('무엇으로 답했는지는 안 찍는다',
    !['allow', 'deny', 'always', 'persist'].includes(w.view.clearedBy));

  // 못 닿음. 「묻는 게 없다」와 값이 같으면 안 된다 — 앞은 아는 것이고 뒤는 모르는 것이다.
  st.ask({ id: 'call_9', kind: 'permission', what: 'mcp__ppt__set_text' });
  await w.poll();
  st.reachable = false;
  await w.poll();
  ok('못 닿으면 세운 것을 내리되 사유가 다르다',
    w.view.pending === null && w.view.clearedBy === CLEARED.unreachable);
  // 여기도 `!== null` 이었다. 화면은 `lostEl(v.lostNote)` 의 `textContent` 에 그대로 꽂으므로
  // 이 칸이 비면 **「undefined」라는 글자가 사람에게 뜬다** — 안 뜨는 것보다 나쁘다.
  ok('못 닿으면 소리 내어 말한다', Boolean(w.view.lostNote), String(w.view.lostNote));
  ok('그 말이 마지막으로 읽은 것임을 밝힌다',
    /마지막으로 읽은/.test(w.view.lostNote ?? ''), w.view.lostNote);
  const said = drew;
  await w.poll(); await w.poll();
  ok('못 닿는다는 말은 한 번뿐이다', drew === said, `${drew - said}회 더 말함`);

  // 다시 닿음. **물음이 하나도 안 실려 오는 조용한 데몬**이라야 이 줄이 뭘 잡는지 보인다 —
  // 물음이 같이 오면 그 분기가 대신 그려 줘서, 고장 나 있어도 시험은 통과한다.
  st.clear();
  st.reachable = true;
  const lost = drew;
  await w.poll();
  ok('다시 닿으면 바꿔 그린다', drew > lost, '「안 닿습니다」가 그대로 서 있음');
  ok('다시 닿아도 왜 내려갔는지는 남는다', w.view.clearedBy === CLEARED.unreachable);
  const back = drew;
  await w.poll(); await w.poll();
  ok('다시 닿았다는 말도 한 번뿐이다', drew === back, `${drew - back}회 더 말함`);

  // 문이 아예 안 열리는 것도 못 닿은 것이다. 예외를 삼키되 사실은 남긴다.
  const st2 = new FakeStatus();
  st2.throwOnStatus = true;
  const w2 = new WatchPrompt(st2);
  await w2.poll();
  ok('dial 실패도 못 닿음이다',
    w2.view.reachable === false && Boolean(w2.view.lostNote), String(w2.view.lostNote));

  // 「…하는 중」은 **지금**에 대한 말이라 못 닿는 순간 근거가 없어진다. 로그 줄은 지나간
  // 일이라 못 닿아도 참인데 이건 아니다 — 그대로 두면 죽은 데몬이 영영 일하는 중으로 선다.
  // 지우지도 않는다(뭘 하다 놓쳤는지는 알아야 한다). 값과 **아직 유효한지**를 같이 싣는다.
  const stD = new FakeStatus();
  const wD = new WatchPrompt(stD);
  stD.doing = '도구를 실행하는 중';
  await wD.poll();
  ok('하는 일은 status 가 말한 그대로 온다',
    wD.view.doing === '도구를 실행하는 중' && wD.view.doingFresh === true);
  stD.reachable = false;
  await wD.poll();
  ok('못 닿으면 하는 일을 안 지우고', wD.view.doing === '도구를 실행하는 중', wD.view.doing);
  ok('지금 읽은 것이 아니라고 값에 싣는다', wD.view.doingFresh === false);
  // 다시 닿았는데 이제 아무것도 안 하면, 마지막 읽기가 그 자리를 계속 지키면 안 된다.
  stD.reachable = true;
  stD.doing = '';
  await wD.poll();
  ok('다시 닿아 조용해지면 그 말은 내려간다',
    wD.view.doing === '' && wD.view.doingFresh === true, wD.view.doing);

  // 단추 문구가 여는 폭을 말해야 한다.
  // 「허용」/「항상 허용」이면 세션 전체를 여는 줄 모르고 누른다.
  const widths = new Set(DECISIONS.map((d) => d.width));
  ok('넷이 다 있고 폭이 셋으로 갈린다',
    DECISIONS.length === 4 && widths.size === 3, [...widths].join('/'));
  ok('문구가 폭을 말한다',
    everyOf(DECISIONS, (d) => d.width === 'call' || /세션|계속|설정/.test(d.label)),
    DECISIONS.map((d) => d.label).join(' · '));

  // 모르는 종류. 코어의 `Waiting.Event` 는 `default:` 로 질문 아닌 것을 전부 권한 물음으로
  // 되살린다 — 새 종류가 생기면 옛 창이 「허용/거절」 단추를 달고 그리고, 사람이 누른 결정은
  // 그 종류가 기다리는 답이 아니다. 이 창은 넘겨짚지 않는다.
  const st3 = new FakeStatus();
  const w3 = new WatchPrompt(st3);
  st3.ask({ id: 'call_10', kind: 'confirm', what: '무언가' });
  await w3.poll();
  ok('모르는 종류도 대기 중이라는 사실은 보여 준다', w3.view.pending?.id === 'call_10');
  ok('모르는 종류를 권한으로 넘겨짚지 않는다', w3.view.pending?.known === false);
  ok('모르는 종류는 사실만 적는다', /kind=confirm/.test(w3.view.unknownKindNote ?? ''));
  let refused = false;
  try { await w3.answer('allow'); } catch { refused = true; }
  ok('모르는 종류에 allow 를 안 보낸다', refused && st3.answers.length === 0);

  // 종류가 없는 것도 권한이 아니다 — 없는 것을 기본값으로 메우면 위와 같은 사고가 된다.
  st3.ask({ id: 'call_11', what: '종류 없음' });
  await w3.poll();
  ok('종류가 없으면 없는 대로 든다', w3.view.pending?.kind === '' && !w3.view.pending.known);

  // 질문은 손이 다르다. 권한은 정해진 낱말 넷이고 질문은 사람이 고른 글이다.
  st3.ask({ id: 'call_12', kind: 'question', what: '어느 장에 넣을까요?',
    options: ['3장', '새 장'] });
  await w3.poll();
  await w3.choose('새 장');
  ok('질문의 답은 글로 간다',
    st3.answers.length === 1 && st3.answers[0].callId === 'call_12'
    && st3.answers[0].text === '새 장');
  // 사유까지 본다. 이 물음은 **이미 답한** 것이기도 해서 거절 이유가 둘 겹치는데, 종류
  // 어긋남은 이 코드의 결함이고 「이미 보냄」은 사람이 두 번 누른 흔한 일이다. 결함이 흔한
  // 일에 가리면 안 되므로 나와야 하는 말은 종류 쪽이다.
  let wrongHand = '';
  try { await w3.answer('allow'); } catch (e) { wrongHand = e.message; }
  ok('질문에 권한의 낱말을 안 보낸다', wrongHand !== '' && st3.answers.length === 1);
  ok('겹치면 종류 어긋남을 먼저 말한다', /kind=question/.test(wrongHand), wrongHand);

  // 두 번 누르기. 답을 보내도 물음은 다음 `status` 까지 화면에 서 있으므로 단추도 서 있다.
  // 둘째 답은 코어까지 가면 어차피 떨어지지만, 돌아오는 말이 "이미 결정됐거나 만료됐다"라
  // 아무 잘못 없는 사람에게 오류로 뜬다. 여기서 막는다.
  const st4 = new FakeStatus();
  const w4 = new WatchPrompt(st4);
  st4.ask({ id: 'call_20', kind: 'permission', what: 'bash' });
  await w4.poll();
  ok('보내기 전에는 안 잠겨 있다', w4.view.answered === false);
  await w4.answer('allow');
  ok('보낸 뒤에는 잠긴다', w4.view.answered === true);
  let twice = false;
  try { await w4.answer('deny'); } catch { twice = true; }
  ok('같은 물음에 답이 두 번 안 간다',
    twice && st4.answers.length === 1 && st4.answers[0].decision === 'allow');
  // 폴이 계속 같은 것을 실어 와도 잠김이 안 풀린다 — 풀리면 두 번 누르기가 되살아난다.
  await w4.poll();
  ok('같은 물음이 계속 와도 잠김이 안 풀린다', w4.view.answered === true);
  // 다음 물음은 새 물음이다. 앞의 잠김을 물려받으면 답할 수 있는 것을 못 답한다.
  st4.ask({ id: 'call_21', kind: 'permission', what: 'write_file' });
  await w4.poll();
  ok('새 물음은 안 잠겨 있다', w4.view.answered === false);
  await w4.answer('deny');
  ok('새 물음에는 답이 간다', st4.answers.length === 2 && st4.answers[1].callId === 'call_21');

  // 두 폴 사이에 물음이 갈렸는데 id 와 종류가 같은 경우. call id 는 모델이 붙이는 것이라
  // 세션이 새로 세면 되풀이된다. 물은 시각을 안 보면 이게 「안 바뀜」으로 보이고, 그러면 앞
  // 물음의 잠김이 새 물음에 그대로 걸려 **답할 수 있는 것을 못 답한다.**
  const st6 = new FakeStatus();
  const w6 = new WatchPrompt(st6);
  st6.ask({ id: 'call_1', kind: 'permission', what: 'bash', since: '2026-08-29T01:00:00Z' });
  await w6.poll();
  await w6.answer('allow');
  ok('보낸 뒤 잠긴다 (id 되풀이 대비)', w6.view.answered === true);
  st6.ask({ id: 'call_1', kind: 'permission', what: 'bash', since: '2026-08-29T01:07:00Z' });
  await w6.poll();
  ok('id 가 같아도 물은 시각이 다르면 새 물음이다', w6.view.answered === false);
  await w6.answer('deny');
  ok('되풀이된 id 의 새 물음에도 답이 간다',
    st6.answers.length === 2 && st6.answers[1].decision === 'deny');
  // 같은 것이 계속 오는 것은 여전히 같은 것이다 — 이걸 새 것으로 보면 매 폴마다 다시 그린다.
  await w6.poll();
  ok('시각까지 같으면 같은 물음이다', w6.view.answered === true);

  // 같은 물음을 보는 **동안** 뒤에 물음이 더 쌓이는 경우. 신원(id·종류·시각)은 안 바뀌므로
  // 「같은 물음」인 것이 맞고, 판을 다시 세우면 사람이 적던 답이 지워지니 안 세우는 것도 맞다.
  // 틀린 것은 **값을 옛것으로 계속 쥐는 것**이다 — 「모두 2개」가 3개가 돼도 영영 2개로 선다.
  // 신원이 같다는 말과 보여 줄 것이 같다는 말은 다른 말이다.
  const st7 = new FakeStatus();
  let drew7 = 0;
  const w7 = new WatchPrompt(st7, { onChange: () => { drew7++; } });
  const q7 = { id: 'call_30', kind: 'question', what: '어느 쪽으로?',
               since: '2026-08-29T02:00:00Z', index: 1, total: 2 };
  st7.ask(q7);
  await w7.poll();
  ok('처음엔 실린 대로 선다', w7.view.pending?.placement === '1번째 · 모두 2개',
     String(w7.view.pending?.placement));
  const rang = drew7;
  st7.ask({ ...q7, total: 3 });
  await w7.poll();
  ok('같은 물음을 보는 동안 뒤가 늘면 그 수가 따라 온다',
     w7.view.pending?.placement === '1번째 · 모두 3개', String(w7.view.pending?.placement));
  ok('보여 줄 것이 달라졌을 때만 종이 울린다', drew7 === rang + 1, `${drew7 - rang}회`);
  // 값만 바뀌고 보일 것이 그대로면 종은 안 울린다 — 울리면 매 폴마다 판이 다시 선다.
  const rang2 = drew7;
  st7.ask({ ...q7, total: 3 });
  await w7.poll();
  ok('같은 것이 또 와도 종은 안 울린다', drew7 === rang2, `${drew7 - rang2}회`);

  // 물음이 **무엇을 근거로** 왔는지. 코어가 소켓으로 실어 보내는데 이 창이 버리면 화면에
  // 남는 것은 예/아니오뿐이고, 그건 판단이 아니라 클릭이다.
  const st5 = new FakeStatus();
  const w5 = new WatchPrompt(st5);
  st5.ask({ id: 'call_30#1', kind: 'question', what: '어느 쪽으로 맞출까요?',
    options: ['왼쪽', '가운데'],
    report: [{ key: 'tried', text: '2·5·9쪽은 왼쪽입니다' },
      { key: 'leaning', text: '왼쪽으로 기웁니다' }],
    index: 1, total: 2 });
  await w5.poll();
  ok('근거를 버리지 않는다', w5.view.pending?.report.length === 2);
  ok('근거의 차례를 안 바꾼다',
    w5.view.pending.report.map((r) => r.key).join(',') === 'tried,leaning');
  ok('몇 번째 물음인지 말한다', w5.view.pending.placement === '1번째 · 모두 2개');
  // 안 실린 것을 1/1 로 지어내지 않는다.
  st5.ask({ id: 'call_31', kind: 'permission', what: 'bash' });
  await w5.poll();
  ok('안 실린 자리는 비워 둔다', w5.view.pending.placement === null
    && w5.view.pending.report.length === 0);
}

// ── 낸 것을 언제 화면에서 지우는가(§5.7). 문의 `submit` 은 식별자를 안 돌려주고 밖에서
// 붙은 창은 전부 `attach` 로 찍히므로, 메아리를 **신원으로는 못 맞춘다.**
{
  const port = new FakeTranscript({ live: [] });
  const read = new ReadTranscript(port);
  read.attach('live');
  const chat = new FakeChat(port, { sessionId: 'live', delay: -1 });
  const comp = new Composer();
  const send = new SendTurn(chat, comp);
  const shape = { id: 'sh1', name: '제목', type: 'TextBox', text: '3분기 매출 전망과 지역별 분해',
    width: 100, height: 20 };
  comp.attach(new Quote({ slideId: 's4', slideNo: 4, ...shape, shapeId: 'sh1' }));

  // **셈을 여기서 다시 짓지 않는다.** 앞 판본은 이 자리에 `filter(kind === 'user')` 를 손으로
  // 적어 뒀는데, 그러면 아래 블록 전체가 프로덕션의 셈이 아니라 **시험이 베낀 규칙**을 재게
  // 된다 — 화면 쪽 셈이 바뀌어도 여기는 초록이다. 지금은 화면이 부르는 그 함수를 그대로 쓴다.
  const rows = () => logShapeOf(read.view).userRows;
  const r1 = await send.run('제목 줄여줘', { userRows: rows(), live: true });
  ok('보내면 간다', r1.sent === true && chat.sent.length === 1);
  // 여기서 화면에 미리 붙이면 로그가 같은 말을 실어 올 때 두 벌이 된다.
  ok('낸 것을 화면이 미리 안 붙인다',
    read.view.rows.filter((r) => r.kind === 'user').length === 1
    && read.view.rows[0].text.includes('제목 줄여줘'));
  ok('메아리가 오면 컴포저가 빈다',
    send.settle(rows()) === true && comp.pending.length === 0 && comp.waiting === false);

  // **사람 줄만 센다.** 이 셈이 모든 줄을 세면 모델이 한 마디 하는 순간 수가 늘어 `settle` 이
  // 그걸 메아리로 읽고, **아직 안 돌아온 사람 글을 지운다** — 사람은 자기가 적은 것이 갔는지
  // 모른 채 빈 칸을 본다. 위 블록은 이 갈래를 못 가른다(사람 줄만 밀어 넣으므로 어느 셈이든
  // 같은 수가 나온다). 여기서 모델 줄을 하나 밀어 가른다.
  // 표시는 `run` 을 안 거치고 직접 찍는다. 가짜 문의 `submit` 은 사람 줄을 **그 자리에서**
  // 로그에 앉히므로 `run` 으로는 이 틈이 안 생기는데, 진짜 데몬에서는 submit 이 왕복이라
  // 사람 줄이 늦게 오고 그 사이에 **앞 턴의 모델 델타**가 먼저 도착한다. 재려는 것이 그 틈이다.
  const comp7 = new Composer();
  const send7 = new SendTurn(chat, comp7);
  const mark7 = rows();
  comp7.hold('세어 보자', mark7);
  port.push({ type: 'part.appended', data: { messageId: 'm7',
    part: { kind: 'text', text: '모델이 한 마디' } } });
  ok('모델 줄은 사람 줄 수를 안 올린다', rows() === mark7, `${mark7} → ${rows()}`);
  ok('모델이 말했다고 사람 글이 지워지지 않는다',
    send7.settle(rows()) === false && comp7.waiting === true);
  port.push({ type: 'prompt.submitted', actor: { kind: 'user', id: 'attach' },
    data: { messageId: 'u7', parts: [{ kind: 'text', text: '세어 보자' }] } });
  ok('사람 줄이 오면 그때 비운다', send7.settle(rows()) === true && comp7.waiting === false);

  // 읽는 유스케이스가 없으면 **읽는 중이 아니다.** `live` 를 여기서 참으로 지어내면 위 셋째
  // 갈래(눈감고 보냄)가 안 돌고, 사람은 안 올 메아리를 기다리며 잠긴다.
  ok('읽는 데가 없으면 눈감은 것이다',
    logShapeOf(null).live === false && logShapeOf(undefined).userRows === 0);
  ok('살아 있음은 지어내지 않고 그대로 나른다',
    logShapeOf({ rows: [], live: false }).live === false
    && logShapeOf({ rows: [], live: true }).live === true);

  // 낸 뒤 메아리 전에는 잠긴다 — 두 벌로 나가는 것을 막는 자리.
  const comp2 = new Composer();
  comp2.attach(new Quote({ slideId: 's4', slideNo: 4, shapeId: 'sh2', name: '부제',
    type: 'TextBox', text: '지역별 분해' }));
  const send2 = new SendTurn(chat, comp2);
  const before = rows();
  await send2.run('한 번 더', { userRows: 999, live: true });   // 로그가 아직 안 따라왔다
  ok('메아리 전에는 잠긴다', comp2.waiting === true);
  const again = await send2.run('또', { userRows: 999, live: true });
  ok('잠긴 동안은 두 번 안 나간다', again.sent === false && again.why === 'waiting');
  ok('안 나간 것은 로그에도 없다', rows() === before + 1, String(rows()));
  // 로그가 움직였다고 다 메아리는 아니다. 도구 줄 하나에 컴포저를 비우면 사람이 낸 글이
  // **가지도 않은 채** 화면에서 사라진다.
  ok('메아리가 아니면 안 비운다',
    send2.settle(rows()) === false && comp2.waiting === true && comp2.pending.length === 1);
  // 데몬이 물음에 막혀 있으면 메아리가 한참 뒤에 오거나 안 온다. 나가는 문이 있어야 한다.
  comp2.release();
  ok('그만 기다리면 잠금이 풀린다', comp2.waiting === false);
  // 갔는지 모르는 채로 사람 글을 지우면 화면이 「갔다」를 말한 셈이 된다.
  ok('그만 기다려도 인용은 그대로다',
    comp2.pending.length === 1 && comp2.pending[0].shapeId === 'sh2');

  // 셋째 갈래 — **갔는데 로그를 못 읽는다.** 잠그느냐 마느냐는 이미 위에서 진짜 끊긴
  // 스트림으로 잰다(「메아리를 못 받을 땐 안 잠근다」). 여기서 더 재는 것은 **쥐고 있던
  // 인용**이다: 갔는지 모르는 채로 그걸 버리면 화면이 「갔다」를 말한 셈이 된다.
  const comp4 = new Composer();
  comp4.attach(new Quote({ slideId: 's4', slideNo: 4, shapeId: 'sh3', name: '표',
    type: 'Table', text: '지역별' }));
  const r4 = await new SendTurn(chat, comp4).run('안 보이는 채로', { userRows: 0, live: false });
  ok('끊겨도 인용은 그대로다', r4.blind === true && comp4.pending.length === 1);

  // `live` 를 **안 넘기면** 「살아 있다」가 아니라 「모른다」다. 모르는 채 잠그면 갇힌다.
  const comp5 = new Composer();
  const r5 = await new SendTurn(chat, comp5).run('안 알려주고 낸다', { userRows: 0 });
  ok('live 를 안 넘기면 살아 있다고 치지 않는다', r5.sent === true && r5.blind === true);
  ok('안 넘겼으면 안 잠근다', comp5.waiting === false);
  const comp6 = new Composer();
  const r6 = await new SendTurn(chat, comp6).run('두 번째 인자 자체가 없다');
  ok('둘째 인자가 통째로 없어도 마찬가지다',
    r6.blind === true && comp6.waiting === false);

  // 문이 던지면 잠금을 푼다. 삼키면 사람은 간 줄 안다.
  const boom3 = new Error('문이 닫혔습니다');
  const bad = { async submit() { throw boom3; } };
  const comp3 = new Composer();
  const r3 = await new SendTurn(bad, comp3).run('안 갈 말', { userRows: 0, live: true });
  ok('못 가면 사유가 온다', r3.sent === false && r3.why === 'failed');
  ok('못 갔으면 안 잠긴다', comp3.waiting === false);
  // **`why` 만 재고 던진 물건을 안 쟀다.** 화면은 `r.error.message` 를 감싸는 것 없이 읽으므로
  // (`view.js` 의 `onSend`), 이 칸이 비면 「못 보냈습니다」를 적으려다 그 자리에서 또 던진다 —
  // 실패를 알리는 길이 실패한다. 값이 온다가 아니라 **던진 그 물건이** 와야 한다.
  ok('못 간 사유에 던진 물건이 실린다', r3.error === boom3, String(r3.error));
  // ⚠ 짝인 `blind: false` 는 **안 문다.** 소비자가 `if (r.blind)` 라 `undefined` 와 구별을
  // 못 하고, 못 하는 것을 못박으면 아무도 안 쓰는 값을 시험이 지키는 꼴이 된다.
}

// ── 낸 결과가 **무슨 말로 나가는가**. 앞 판본은 화면 안의 `if` 둘(`failed`·`waiting`)이라
// 거기 안 걸린 결과는 **아무 말 없이** 나갔다 — 그런데 이 자리는 못 보냈을 때 사람 글을 **그대로
// 남긴다.** 조용하면 사람은 남은 글을 보고 「아직 안 눌렀나」로 읽고 다시 누른다. `SendTurn` 의
// `run` 주석이 이 침묵을 이름 대어 걱정해 두고도 아무 데서도 소리가 안 나던 자리다.
//
// **손으로 지은 답으로 안 잰다.** 다섯 중 넷은 진짜 `run` 이 낸 것을 그대로 먹인다 — 손으로
// 적으면 생산자의 철자가 바뀌어도 여기는 초록이고, 그건 두 벌이 맞는 게 아니라 갈라진 것이다.
{
  const port = new FakeTranscript({ live: [] });
  const read = new ReadTranscript(port);
  read.attach('live');
  const chat = new FakeChat(port, { sessionId: 'live', delay: -1 });
  const rows = () => logShapeOf(read.view).userRows;

  const comp = new Composer();
  const send = new SendTurn(chat, comp);
  const rLive = await send.run('갔다', { userRows: rows(), live: true });
  ok('가고 로그도 읽는 중이면 할 말이 없다', sendNote(rLive) === null, JSON.stringify(rLive));

  // 위에서 안 비웠으니 아직 잠겨 있다 — 그 잠금이 그대로 둘째 갈래를 만든다.
  const rWait = await send.run('또', { userRows: rows(), live: true });
  const nWait = sendNote(rWait);
  ok('기다리는 중이면 왜 안 갔는지 말해 준다',
    rWait.why === 'waiting' && nWait !== null && nWait.text.includes('아직'));
  // 곧 메아리가 와서 스스로 풀리는 사정이라, 이 줄은 붙어 있을 필요가 없다.
  ok('기다리라는 말은 붙어 있지 않는다', nWait.sticky === false);

  // 눈감고 보낸 것. **글이 남는다는 사실까지 적어야** 사람이 다시 안 누른다.
  const rBlind = await new SendTurn(chat, new Composer()).run('안 보이는 채로',
    { userRows: 0, live: false });
  const nBlind = sendNote(rBlind);
  ok('눈감고 보낸 것은 확인 못 한다고 말한다',
    rBlind.blind === true && nBlind !== null && nBlind.text.includes('확인'));
  ok('남은 글이 왜 남았는지까지 적는다', nBlind.text.includes('그대로 뒀습니다'));
  // 스스로 사라지면 남은 글만 남고, 그 글은 「안 눌렀다」로 읽힌다.
  ok('눈감고 보낸 말은 붙어 있는다', nBlind.sticky === true);

  const boom = new Error('문이 닫혔습니다');
  const bad = { async submit() { throw boom; } };
  const rFail = await new SendTurn(bad, new Composer()).run('안 갈 말',
    { userRows: 0, live: true });
  const nFail = sendNote(rFail);
  // 던진 쪽의 말을 안 실으면 사람은 무엇을 고쳐야 할지 모른 채 같은 단추를 다시 누른다.
  ok('못 간 것은 던진 말을 그대로 싣는다',
    nFail !== null && nFail.text.includes('문이 닫혔습니다'), JSON.stringify(nFail));
  ok('못 갔다는 말은 붙어 있는다', nFail.sticky === true);

  // 빈 상자. **여기만 조용해도 된다** — 사람이 방금 빈 칸에서 누른 것을 안다.
  const rEmpty = await new SendTurn(chat, new Composer()).run('   ',
    { userRows: 0, live: true });
  ok('빈 상자에는 할 말이 없다', rEmpty.why === 'empty' && sendNote(rEmpty) === null);

  // 여섯째 결말. **이것만 손으로 짓는다** — 오늘 `run` 이 못 내는 값이고, 못 내는 값을 위해
  // 생산자에 갈래를 하나 심으면 시험이 프로덕션을 늘리는 꼴이 된다. 재려는 것도 생산자가
  // 아니라 **화면이 모르는 것을 만났을 때**다.
  const nUnknown = sendNote({ sent: false, why: 'quota' });
  ok('모르는 사유는 조용히 안 나간다',
    nUnknown !== null && nUnknown.text.includes('quota') && nUnknown.sticky === true,
    JSON.stringify(nUnknown));

  // 갈라 놓고 같은 말로 내보내면 갈라 놓은 값이 없는 것과 같다.
  const said = [nWait, nBlind, nFail, nUnknown].map((n) => n.text);
  ok('결말마다 다른 말이 나간다', new Set(said).size === 4, said.join(' | '));
}

// ── 내려간 물음의 **사유가 무슨 말로 나가는가**. 「없다」만 남기면 셋이 화면에서 똑같이
// 생긴다는 것이 `CLEARED` 를 둔 이유인데, 그 셋을 문장으로 바꾸는 자리가 화면 안이라 안 재고
// 있었다. 게다가 거기서는 셋에 안 맞는 사유가 **`null` 로 떨어져 줄이 통째로 사라졌다** —
// 「내려간 물음이 없다」와 같은 모양으로. 없애려던 뭉갬이 한 겹 위에서 되살아난 자리다.
{
  // 사유는 손으로 안 적고 `CLEARED` 를 그대로 쓴다. 값이 한쪽에서만 바뀌면 드리프트다.
  const said = Object.values(CLEARED).map((c) => clearedNote(c));
  ok('세 사유가 다 제 말을 갖는다', everyOf(said, (t) => typeof t === 'string' && t.length > 0));
  ok('셋이 서로 다른 말이다', new Set(said).size === 3);
  // 「모르게 된 것」을 「답했다」로 읽으면 사람이 그 물음을 잊는다.
  ok('못 닿아 내려간 것은 끝난 것이 아니라고 적는다',
    clearedNote(CLEARED.unreachable).includes('끝난 것이 아닙니다'));
  // 무엇으로 답했는지는 이 창이 모른다. 찍으면 남의 입에 결정을 넣는 것이 된다.
  ok('남이 답한 것은 무엇으로 답했는지 안 적는다',
    !DECISIONS.some((d) => clearedNote(CLEARED.elsewhere).includes(d.value)),
    clearedNote(CLEARED.elsewhere));
  // **여기만 조용해도 된다.** 내려간 물음이 없다는 뜻이라 적을 말이 없다.
  ok('내려간 것이 없으면 할 말이 없다',
    clearedNote(null) === null && clearedNote(undefined) === null);
  // **넷째 사유는 조용히 숨지 않는다.** 숨으면 물음이 사라진 자리에서 화면이 아무 말도 안 하고,
  // 답을 기다리던 사람은 자기가 뭘 놓쳤는지도 모른다.
  const fourth = clearedNote('expired');
  ok('모르는 사유는 줄을 지우는 대신 제 말을 갖고 온다',
    typeof fourth === 'string' && fourth.includes('expired'), String(fourth));
  // 객체 조회는 프로토타입까지 뒤진다 — 사유가 그런 이름이면 함수가 문장 자리에 앉았다.
  ok('프로토타입의 이름도 사유로 안 샌다', typeof clearedNote('constructor') === 'string');
}

// ── 판을 **다시 세울지** 재는 서명. 이 한 줄에 사람이 적던 답과 포커스가 달려 있는데,
// 화면 안에 있는 동안은 DOM 이 있어야 돌아서 한 번도 안 재 봤다. 재는 쪽이 없으면 이 목록은
// 나중에 고치는 사람에게 그냥 다섯 칸짜리 배열로 보이고, 한 칸 더 넣는 것이 사람이 적던 답을
// 지우는 일이라는 것을 아무도 안 말해 준다.
{
  const st = new FakeStatus();
  const w = new WatchPrompt(st, {});
  const ask = { id: 'call_9', kind: 'permission', what: 'mcp__ppt__set_text',
    reason: '쓰기 도구는 허용 규칙에 없습니다', index: 1, total: 2 };
  st.ask({ ...ask });
  await w.poll();
  const base = askSig(w.view);

  // **뒤에 쌓인 수는 서명에 안 든다.** 들면 뒤가 늘 때마다 판이 다시 서고 적던 답이 지워진다.
  st.ask({ ...ask, total: 3 });
  await w.poll();
  ok('뒤가 늘어도 판을 다시 안 세운다',
    askSig(w.view) === base && w.view.pending.placement.includes('3개'),
    `${base} / ${w.view.pending.placement}`);

  // 답을 보내면 단추가 잠긴다 — 그건 다시 그려야 보인다.
  await w.answer('always');
  ok('답을 보낸 것은 판을 다시 세운다', askSig(w.view) !== base && w.view.answered === true);

  // 다른 물음이면 다른 판이다. 신원이 안 들면 새 물음이 옛 판 위에 그려진다.
  const w2 = new WatchPrompt(new FakeStatus(), {});
  const sigOf = async (p, f = () => {}) => {
    const s2 = new FakeStatus(); const ww = new WatchPrompt(s2, {});
    s2.ask(p); await ww.poll(); await f(ww, s2); return askSig(ww.view);
  };
  const a = await sigOf({ ...ask });
  ok('물음이 바뀌면 판이 바뀐다', await sigOf({ ...ask, id: 'call_10' }) !== a);
  ok('종류가 바뀌면 판이 바뀐다', await sigOf({ ...ask, kind: 'question' }) !== a);
  // 못 닿는 동안 세워 둔 판은 답할 수 있는 판이면 안 된다.
  ok('닿지 않게 되면 판이 바뀐다',
    await sigOf({ ...ask }, async (ww, s2) => { s2.reachable = false; await ww.poll(); }) !== a);
  // 내려간 사유가 화면에 뜬다(남이 답했다 / 정책이 답했다 / 못 닿는다). 사유가 안 들면 그 셋이
  // 화면에서 똑같이 생기던 자리로 돌아간다.
  ok('내려간 사유가 바뀌면 판이 바뀐다',
    await sigOf({ ...ask }, async (ww, s2) => { s2.clear(); await ww.poll(); }) !== a);
  ok('선 물음이 하나도 없던 처음과는 다르다', askSig(w2.view) !== a);

  // **조용한 데몬이 죽는 길.** 물음이 하나도 없는 채로 못 닿게 되면 위 갈래 어느 것도 값이
  // 안 바뀐다 — 내릴 물음이 없으니 사유도 안 적힌다(`poll` 의 `if (this.pending)` 밖이다).
  // `reachable` 이 서명에 없으면 판이 그대로 서고, 화면은 「안 닿습니다」를 **영영 안 그린다.**
  // 사람은 데몬이 죽은 줄 모르고 조용한 창을 본다. 위 다섯 칸 중 이 칸만 이 길을 잡는다.
  {
    const s3 = new FakeStatus(); const q = new WatchPrompt(s3, {});
    await q.poll();
    const quiet = askSig(q.view);
    s3.reachable = false;
    await q.poll();
    ok('물음 없이 죽어도 판이 바뀐다', askSig(q.view) !== quiet,
      `${quiet} / ${askSig(q.view)}`);
  }

  // **내려간 사유는 물음이 없는 자리에서 갈린다.** 둘 다 선 물음이 없고 닿는 중인데, 화면이
  // 적을 말이 다르다(「남이 답했습니다」 / 「답을 보냈습니다」). 사유가 서명에 없으면 한쪽에서
  // 다른 쪽으로 갈 때 판이 안 서고, 앞의 말이 그대로 남는다.
  {
    const down = async (answerFirst) => {
      const s4 = new FakeStatus(); const q = new WatchPrompt(s4, {});
      s4.ask({ ...ask }); await q.poll();
      if (answerFirst) await q.answer('always');
      s4.clear(); await q.poll();
      return { sig: askSig(q.view), why: q.view.clearedBy };
    };
    const bySelf = await down(true); const byOther = await down(false);
    ok('내려간 사유가 다르면 판도 다르다',
      bySelf.sig !== byOther.sig && bySelf.why !== byOther.why,
      `${bySelf.why} / ${byOther.why}`);
  }
}

// ── 안내는 모델의 말이 아니라 **도구 호출**이다(§6.1). 로그에서 유도하고 따로 안 쌓는다.
{
  const port = new FakeTranscript({ s: [] });
  const read = new ReadTranscript(port);
  read.attach('s');
  const chat = new FakeChat(port, { sessionId: 's', delay: -1 });
  const q = new Quote({ slideId: 's4', slideNo: 4, shapeId: 'sh1', name: '제목',
    type: 'TextBox', text: '3분기 매출 전망과 지역별 분해' });
  // **번호는 가짜가 준다.** 여기서 `'m1'` 이라 적으면 시험이 가짜의 번호 규칙을 베낀 것이고,
  // 규칙이 바뀌어도 초록이다. 받아서 그대로 되민다.
  const mid = await chat.submit(promptOf('줄여줘', [q]));
  chat.reply(mid, promptOf('줄여줘', [q]));
  ok('답이 물음과 같은 번호로 선다',
    read.view.rows.some((r) => r.kind === 'model' && r.messageId === mid), mid);

  const fold = foldAdvice(read.view.rows);
  ok('안내가 도구 호출에서 나온다', fold.items.length === 2, String(fold.items.length));
  ok('안내가 인용한 도형을 가리킨다', fold.items[0].shapeIds[0] === 'sh1');
  ok('가짜 답도 모델의 말로 선다',
    read.view.rows.some((r) => r.kind === 'model' && r.text.includes('상자 폭')));
  ok('턴이 끝난 것이 줄로 남는다', read.view.rows.some((r) => r.kind === 'turn'));

  // 걷으면 걷힌다. 쌓아 두는 상태가 아니라 **로그를 접은 결과**라 다시 붙어도 같다.
  const cleared = foldAdvice([...read.view.rows,
    { kind: 'tool', tool: 'mcp__ppt__clear_advice', args: {} }]);
  ok('걷으면 없어진다', cleared.items.length === 0);

  // 서버 이름은 설정값이다. 이름이 다르면 포스트잇이 **한 장도 안 붙는데**, 그걸 조용히
  // 끝내면 설정 한 줄이 기능 하나를 지운 것이 화면 어디에도 안 남는다.
  const other = foldAdvice([{ kind: 'tool', tool: 'mcp__powerpoint__advise', callId: 'c9',
    args: { items: [{ message: '여기' }] } }]);
  ok('남의 서버 안내는 안 붙인다', other.items.length === 0);
  ok('안 붙인 이유를 값에 싣는다', other.strays[0] === 'mcp__powerpoint__advise');
  const empty = foldAdvice([{ kind: 'tool', tool: 'mcp__ppt__advise', callId: 'c8',
    args: { items: [{ slideId: 's1' }] } }]);
  ok('말 없는 안내는 안 붙이고 센다', empty.items.length === 0 && empty.dropped === 1);

  // **걷은 뒤의 못 붙인 셈.** 위 두 경우가 각각 「걷힘」과 「셈」을 재는데, 현장에서 흔한 것은
  // 둘이 겹친 이 길이다 — 안내가 왔고 그 중 하나가 말이 없었고 모델이 다 걷었다. 셈이 안 걷히면
  // 없어진 안내를 두고 "몇 건은 못 붙였다"는 쪽지가 남고, 화면은 목록과 쪽지가 **둘 다** 비어야
  // 안내 층을 숨기므로 다 걷은 판이 계속 서 있는다.
  const clearedAfterDrop = foldAdvice([
    { kind: 'tool', tool: 'mcp__ppt__advise', callId: 'c1',
      args: { items: [{ slideId: 's1', message: '여기' }, { slideId: 's1' }] } },
    { kind: 'tool', tool: 'mcp__powerpoint__advise', callId: 'c2',
      args: { items: [{ message: '남의 것' }] } },
    { kind: 'tool', tool: 'mcp__ppt__clear_advice', args: {} },
  ]);
  ok('걷으면 못 붙인 셈도 같이 걷힌다',
    clearedAfterDrop.items.length === 0 && clearedAfterDrop.dropped === 0,
    `items=${clearedAfterDrop.items.length} dropped=${clearedAfterDrop.dropped}`);
  // 그런데 **설정이 어긋났다는 사실은 안 걷는다.** 우리 `clear_advice` 는 남의 서버 판을 걷지도
  // 못하고, 걷었다고 이름이 맞아지지도 않는다. 이 비대칭이 다음 사람 손에 지워지지 않게 못 박는다.
  ok('남의 서버 이름은 걷어도 남는다',
    clearedAfterDrop.strays[0] === 'mcp__powerpoint__advise', String(clearedAfterDrop.strays));

  // 이름이 글이 아닌 도구 호출 하나가 오면 접다가 터진다 — 그러면 프레임 **한 장이**
  // 안내 층 전체를 끈다. `Transcript` 가 이름을 못 믿을 땐 null 로 눕힌다.
  const nameless = new Transcript();
  nameless.append({ seq: 1, type: 'part.appended', data: { messageId: 'm1',
    part: { kind: 'tool-call', toolCall: { name: 7, callId: 'c1', args: {} } } } });
  ok('이름이 글이 아니면 도구 이름을 안 만든다', nameless.rows[0].tool === null);
  let threw = null;
  try { foldAdvice(nameless.rows); } catch (e) { threw = e; }
  ok('이름 없는 호출이 안내 층을 안 끈다', threw === null, String(threw));
}
// ── **한 값으로만 불린 인자**가 가리킨 자리. 돌연변이 계측이 구조적으로 못 보는 종류다 —
// 안 밟는 가지에는 뒤집을 연산자가 없어서 무엇을 뒤집어도 초록이 안 흔들린다. 대신 싸게 셀 수
// 있는 것이 있다: 스위트 전체에서 그 인자에 **몇 가지 값이 갔는가**. 세어 보니 `FakeChat.reply`
// 의 둘째 인자는 값이 하나뿐이었다 — 늘 인용이 박힌 글이었다. 즉 **인용 없이 낸 턴을 이 스위트가
// 한 번도 안 밟았다.** 창에서 사람이 그냥 타이핑하면 나오는 가장 흔한 길인데도 그렇다.
//
// ⚠ 그 계측에도 눈먼 자리가 있다. 자유 함수는 **이 파일이 부른 것만** 세어져서, `promptOf` 도
// 「인용 하나뿐」으로 나왔는데 실은 `SendTurn` 을 지나는 시험이 빈 인용으로 이미 밟고 있었다
// (`filter` 를 빼 보면 그 시험이 먼저 빨개진다). 그러니 이 셈은 **후보를 주지 결론을 안 준다** —
// 아래 두 줄은 그래서 「메운 구멍」이 아니라 계약을 직접 이름 붙여 두는 값이다.
{
  const q = new Quote({ slideId: 's4', slideNo: 4, shapeId: 'sh1', name: '제목',
    type: 'TextBox', text: '3분기 매출 전망' });
  // `filter(length > 0)` 가 있는 이유가 이 두 줄이다. 없으면 인용 없는 글에 빈 줄 둘이 앞서고,
  // 모델이 받는 첫 글자가 개행이 된다.
  ok('인용이 없으면 앞에 빈 줄이 안 붙는다', promptOf('줄여줘', []) === '줄여줘');
  ok('글이 비면 인용만 간다', promptOf('', [q]) === q.toPrompt());

  const port = new FakeTranscript({ s2: [] });
  const read = new ReadTranscript(port);
  read.attach('s2');
  const chat = new FakeChat(port, { sessionId: 's2', delay: -1 });

  // 먼저 인용을 실은 턴 하나. 뒤엣것과 **비교할 앞**이 있어야 「안 낳는다」와 「지운다」가 갈린다.
  const mid1 = await chat.submit(promptOf('줄여줘', [q]));
  chat.reply(mid1, promptOf('줄여줘', [q]));
  const before = foldAdvice(read.view.rows);
  ok('앞 턴이 안내를 낳았다', before.items.length === 2, String(before.items.length));

  const mid2 = await chat.submit('그냥 줄여줘');
  chat.reply(mid2, '그냥 줄여줘');
  ok('두 턴의 번호가 다르다', mid1 !== mid2, `${mid1} / ${mid2}`);
  const after = foldAdvice(read.view.rows);
  ok('인용 없는 턴은 되묻는다',
    read.view.rows.some((r) => r.kind === 'model' && r.text.includes('「선택 인용」')));
  ok('되물어도 턴은 끝난다', read.view.rows.filter((r) => r.kind === 'turn').length === 2);
  // **안 낳는 것과 지우는 것은 다르다.** 안내 층은 로그를 접은 결과지 쌓아 둔 상태가 아니라,
  // 안내 없는 턴이 하나 지나가도 앞의 포스트잇은 그 자리에 있어야 한다. 여기서 0 이 나오면
  // 화면은 사람이 안 걷은 안내를 조용히 걷은 것이고, 4 가 나오면 같은 안내를 두 번 붙인 것이다.
  ok('앞 턴의 안내가 그대로 서 있다', after.items.length === 2, String(after.items.length));
  ok('못 붙인 셈이 생기지 않았다', after.dropped === 0 && after.strays.length === 0);
}

// ── 끝난 턴에 검증 딱지가 실려 온다(`TurnFinishedData.Unverified`). 「고쳤다」와 「고쳤다는데
// 아무도 못 봤다」가 같은 종류로 오므로, 안 실으면 화면에서 둘이 똑같이 생긴다.
{
  const t = new Transcript();
  t.append({ seq: 1, type: 'turn.finished', data: { usage: {} } });
  t.append({ seq: 2, type: 'turn.finished',
    data: { unverified: true, reason: 'no independent run passed' } });
  ok('보통 끝에는 딱지가 없다', t.rows[0].unverified === false);
  ok('검증 못 한 끝은 그렇다고 실린다',
    t.rows[1].unverified === true && t.rows[1].reason === 'no independent run passed');
}

// ── 「가리킬 곳」 한 줄. 못 얻은 번호와 아직 안 물어본 번호를 **화면이 갈라야 한다**
// (`DeckPort.slideNumbers` 가 빈 Map 대신 null 을 고른 이유가 그 갈림이다). 넷이 다 다른 글이
// 아니면 그 계약은 값에만 있고 사람에겐 없는 것이다.
{
  const a = new Advice({ message: '넘칩니다', slideId: 's7', shapeIds: ['sh1'] });
  const asked = targetLabel(a, new Map([['s7', 7]]), true);
  const pending = targetLabel(a, null, false);
  const cantNumber = targetLabel(a, null, true);
  const gone = targetLabel(a, new Map([['s9', 9]]), true);
  ok('번호를 얻으면 번호로 적는다', asked === '슬라이드 7 · sh1', asked);
  ok('안 물어본 것과 못 얻은 것이 다른 글', pending !== cantNumber, `${pending} / ${cantNumber}`);
  ok('답 전에는 확인 중이라고 적는다', pending.includes('확인 중'), pending);
  ok('못 주는 호스트는 그렇다고 적는다', cantNumber.includes('못 줍니다'), cantNumber);
  ok('답에 없는 슬라이드는 낡은 안내라고 적는다', gone.includes('덱에 없습니다'), gone);
  ok('어느 글에나 도형 id 는 남는다',
     everyOf([asked, pending, cantNumber, gone], (t) => t.endsWith('sh1')));

  // 안 눌리는 항목에도 **사유가 값에 실린다**. 누를 수 없으니 `PointAtAdvice` 의 사유는 영영
  // 화면에 못 온다 — 목록이 그 자리에서 적어야 하고, 두 자리가 같은 문장이어야 한다.
  const blind = new Advice({ message: '어딘지 안 실림' });
  ok('가리킬 곳이 없으면 사유가 있다', typeof blind.unpointableReason === 'string');
  ok('가리킬 수 있으면 사유가 없다', a.unpointableReason === null);
  const blindRun = await point.run(blind);
  ok('누를 때 사유와 목록의 사유가 같다', blindRun.reason === blind.unpointableReason);
  // 사유만 보고 `ok` 를 안 봤다. 성공으로 읽히면 화면은 캔버스가 따라간 줄 알고 아무 말도
  // 안 하는데, 실제로는 아무 데도 안 갔다.
  ok('가리킬 곳이 없으면 성공이 아니다', blindRun.ok === false, String(blindRun.ok));

  // 1.8 아래 호스트 흉내. **빈 Map 이 아니라 null 이다.**
  const d = new FakeDeck(structuredClone(fixture));
  d.numbering = false;
  ok('번호를 못 주는 덱은 null 을 준다', (await d.slideNumbers()) === null);

  // 계측기 자신이 내는 **거짓 초록**. 앞의 구독을 끊는 손이 뒤엣것의 귀를 막으면, 그 뒤로
  // 아무 이벤트도 안 오는데 시험에는 「아무 일도 안 일어났다」로 보인다.
  const ft = new FakeTranscript({ A: [] });
  const heard = [];
  const off1 = ft.subscribe('A', -1, { onEvent: () => {}, onRestart() {}, onEnd() {} });
  ft.subscribe('A', -1, { onEvent: (e) => heard.push(e), onRestart() {}, onEnd() {} });
  off1();
  ft.push({ sessionId: 'A', type: 'x' });
  ok('앞엣것을 끊어도 뒤엣것 귀는 안 막는다', heard.length === 1, String(heard.length));

  // 번호 없는 슬라이드를 표에 실으면 값이 `undefined` 인 칸이 생기고, `targetLabel` 이
  // 그 칸을 못 알아봐 「지금 덱에 없습니다」로 샌다 — 표에 있는데 없다고 적는 것이다.
  // **번호가 붙은 슬라이드를 하나 같이 둔다.** 안 두면 표가 통째로 비고, 비면 아래 두 줄이
  // 「하나도 안 틀렸다」가 아니라 「볼 것이 없었다」로 초록이 된다. 앞 판본이 그랬다 —
  // `typeof v === 'number'` 를 상수 거짓으로 바꿔도 스위트가 통과하는 것으로 쟀다. 그래서
  // 표가 실제로 뭔가를 담았다는 것을 먼저 묻는다. 그게 아래 둘을 떠받치는 줄이다.
  const noNo = new FakeDeck({ slides: [
    { id: 'sx', title: '번호 없음', shapes: [] },
    { id: 's9', title: '번호 있음', no: 9, shapes: [] },
  ] });
  const m = await noNo.slideNumbers();
  ok('표에 앉은 것이 실제로 있다', m.size === 1, JSON.stringify([...m]));
  ok('번호 없는 슬라이드는 표에 안 앉는다', m.has('sx') === false, JSON.stringify([...m]));
  ok('표의 값은 전부 숫자다', everyOf([...m.values()], (v) => typeof v === 'number'));

  // ── 답이 **언제 것인가**. 「물어본 적 있다」를 「이 안내에 답을 받았다」로 쓰면, 첫 답 뒤에
  // 도착한 안내가 낡은 스냅숏에 없다는 이유로 「지금 덱에 없습니다」가 된다 — 덱에는 있고
  // 우리가 안 물어봤을 뿐이다.
  const sn = new SlideNumbers();
  sn.note('s7');
  ok('묻기 전에는 답이 아니다', sn.answered('s7') === false);
  const t1 = sn.ask();
  ok('묻는 중에도 아직 답이 아니다', sn.answered('s7') === false);
  sn.answer(t1, new Map([['s7', 7]]));
  ok('답이 오면 답이다', sn.answered('s7') === true);

  // 그 뒤에 온 안내. 같은 답이 앉아 있지만 **이 id 를 물어본 적은 없다.**
  sn.note('s9');
  ok('첫 답 뒤에 온 id 는 아직 답을 못 받았다', sn.answered('s9') === false);
  const lateAdvice = new Advice({ message: '나중에 온 안내', slideId: 's9', shapeIds: ['sh2'] });
  const lateLabel = targetLabel(lateAdvice, sn.map, sn.answered('s9'));
  ok('안 물어본 슬라이드를 「덱에 없다」고 단정하지 않는다',
    !lateLabel.includes('없습니다') && lateLabel.includes('확인 중'), lateLabel);
  const t2 = sn.ask();
  sn.answer(t2, new Map([['s7', 7]]));
  ok('물어본 뒤에도 없으면 그때는 없다고 적는다',
    targetLabel(lateAdvice, sn.map, sn.answered('s9')).includes('없습니다'));

  // 그릴 때마다 다시 묻는다 — 왕복 둘이 겹치면 앞엣것이 뒤늦게 돌아온다. 낡은 번호는 없는
  // 번호보다 나쁘다는 것이 `OfficeDeck` 이 캐시를 안 두는 이유고, 화면이 뒤집으면 그 결정은
  // 없는 것과 같다.
  const t3 = sn.ask();
  const t4 = sn.ask();
  sn.answer(t4, new Map([['s7', 4]]));
  ok('늦게 온 옛 답은 안 앉는다', sn.answer(t3, new Map([['s7', 3]])) === false);
  ok('새 답이 그대로 있다', sn.map.get('s7') === 4);

  // 아무것도 안 돌려주는 포트. `undefined` 가 그대로 앉으면 `nos === null` 이 빗나가
  // 「지금 덱에 없습니다」로 샌다 — 안 준 것을 없다고 적는 자리다.
  const sn2 = new SlideNumbers();
  sn2.note('s7');
  sn2.answer(sn2.ask(), undefined);
  ok('안 준 답을 null 로 눕힌다', sn2.map === null);
  ok('그래서 「못 줍니다」로 적힌다',
    targetLabel(a, sn2.map, sn2.answered('s7')).includes('못 줍니다'),
    targetLabel(a, sn2.map, sn2.answered('s7')));
}

// ── 「글이 없다」와 「글을 못 읽었다」. `OfficeDeck.selection` 의 두 번째 왕복이 죽으면 신원만
// 살고 텍스트가 빈 문자열이 되는데, 그대로 두면 **빈 상자와 값이 같다.** 인용은 모델에게 가는
// 말이라 그 거짓은 화면이 아니라 프롬프트에서 값을 치른다(자른 것을 적는 이유와 같다).
{
  const empty = new Quote({ slideId: 's1', shapeId: 'sh1', type: 'TextBox', text: '' });
  const unread = new Quote({ slideId: 's1', shapeId: 'sh1', type: 'TextBox', text: '',
    textUnavailable: true });
  ok('빈 상자는 아무 표시도 안 붙는다', !empty.toPrompt().includes('textUnavailable'));
  ok('못 읽은 글은 그렇다고 실린다', unread.toPrompt().includes('textUnavailable=true'));
  ok('둘은 모델에게 다른 말이다', empty.toPrompt() !== unread.toPrompt());

  // 덱에서 유스케이스를 지나 인용까지 **실려 와야** 한다 — 한 층만 빠져도 값이 도로 같아진다.
  const d2 = new FakeDeck(structuredClone(fixture));
  d2.readText = false;
  d2.click(fixture.slides[0].shapes[0].id, false);
  const got = await new QuoteSelection(d2, new Composer()).run();
  ok('덱이 못 읽었다고 하면 인용까지 실려 온다',
     got.added[0]?.textUnavailable === true, JSON.stringify(got.added[0] ?? null));

  // **옮겨 싣는 칸은 비워도 아무도 안 본다.** 위 한 줄이 따라온 것은 `textUnavailable`
  // 하나고, 나머지 다섯 칸(name·type·text·width·height)은 통째로 `undefined` 를 실어도
  // 스위트가 초록이었다(필드 드롭 계측). 가지는 뒤집으면 누가 알아채는데 옮겨 싣는 칸은
  // 안 그렇다. 그중 `text` 는 **모델에게 가는 몸**이라 비면 인용이 「이 상자」라고만 말한다.
  const src = fixture.slides[0].shapes[0];
  const d3 = new FakeDeck(structuredClone(fixture));
  d3.click(src.id, false);
  const one = (await new QuoteSelection(d3, new Composer()).run()).added[0];
  const carried = ['name', 'type', 'text', 'width', 'height'].filter((k) => one?.[k] === src[k]);
  ok('덱의 도형이 인용에 그대로 실린다', carried.length === 5, carried.join(','));
  // 슬라이드 신원은 도형이 아니라 **선택 전체**에서 온다. 그래서 위 목록에 안 들어가고,
  // 안 들어간 채로 아무도 안 봤다(필드 드롭 계측). 이게 비면 모델이 받는 말이
  // `slide=undefined` 가 되어 **어느 장 얘기인지 모르는 인용**이 되고, 안내가 돌아올 때
  // 도형 id 만으로 장을 되찾아야 한다.
  ok('선택의 슬라이드 신원이 인용에 실린다',
    one.slideId === fixture.slides[0].id && one.toPrompt().includes(`slide=${one.slideId}`),
    `${one.slideId} / ${one.toPrompt().split('\n')[0]}`);
  // **실렸는지만 보면 모델에게 안 가도 초록이다.** 소비자가 묻는 물음으로 한 번 더 묻는다.
  ok('실린 이름과 종류가 모델에게 가는 말에 든다',
    one.toPrompt().includes(`type=${src.type}`) && one.toPrompt().includes(`name="${src.name}"`),
    one.toPrompt().split('\n')[0]);
  ok('실린 글이 모델에게 가는 말에 든다', one.toPrompt().includes(src.text), one.toPrompt());
  // 크기의 소비자는 카드의 치수 한 줄이다. 한 짝만 빠져도 그 줄이 통째로 사라진다.
  ok('실린 크기가 카드의 치수가 된다', one.sizeLabel !== null, String(one.sizeLabel));
}

// ── 선택을 **아예** 못 읽는 날. 위가 반쪽(글만 못 옴)이면 이건 통째다. `run()` 이 던지면
// `onQuote` 는 그걸 안 받으므로 단추가 조용히 죽는다 — 누른 사람에게는 아무 일도 안 일어나고,
// 그건 「안 골랐다」와 화면에서 구별이 안 된다. 사유는 던져지는 게 아니라 실려 올라와야 한다.
{
  const d = new FakeDeck(structuredClone(fixture));
  d.click(fixture.slides[0].shapes[0].id, false);
  const qs = new QuoteSelection(d, new Composer());
  await qs.sampleBeforeFocus();
  d.reading = false;
  const r = await qs.run().catch((e) => ({ threw: String(e) }));
  ok('선택을 못 읽어도 던지지 않는다', !r.threw, r.threw);
  ok('못 읽은 것은 제 이름으로 올라온다', r.reason === 'readFailed', JSON.stringify(r));
  ok('못 읽은 것을 「안 골랐다」로 적지 않는다', r.reason !== 'none' && r.reason !== 'unknown');
  // **사유가 실려도 문이 안 열리면 아무도 안 읽는다.** `onQuote` 는 위 사유 넷을 `if (empty)`
  // 안에서만 본다 — `empty` 가 안 실리면(필드 드롭 계측: 안 실어도 초록이었다) 못 읽은 호출이
  // **인용에 성공한 것처럼** 흘러간다. 0개를 인용하고, 쪽지 한 줄 없이, 사람은 자기가 도형을
  // 안 골랐다고 생각한다. `reason` 을 갈라 놓은 값이 통째로 도달 불가가 되는 자리다.
  ok('못 읽은 호출은 빈 것으로 올라온다', r.empty === true, String(r.empty));

  d.reading = true;
  const ok2 = await qs.run();
  ok('손잡이를 되돌리면 다시 읽는다', ok2.added.length === 1, JSON.stringify(ok2.reason));
}

// ── 그 사유가 **사람에게 무슨 말로 나가는가**. 갈라 놓은 값이 화면에서 한 문장으로 뭉치면
// 갈라 놓은 것이 없는 것과 같다 — `pickNote` 에서 실제로 그렇게 됐다. 여기서는 사유를 손으로
// 적지 않고 **`run()` 이 실제로 낸 것**을 그대로 먹인다. 값의 철자가 한쪽에서만 바뀌면
// (`'readFailed'` → `'read_failed'` 같은) 그건 드리프트지 두 벌이 다 맞는 게 아니다.
{
  const shape = fixture.slides[0].shapes[0];
  const two = { slideId: 's1', slideNo: 1,
    shapes: [shape, fixture.slides[0].shapes[1]] };
  const none = { slideId: 's1', slideNo: 1, shapes: [] };
  const feed = (...answers) => ({ async selection() { return answers.shift() ?? none; } });

  const lost = new QuoteSelection(feed(two, none), new Composer());
  await lost.sampleBeforeFocus();
  const rLost = await lost.run();
  const noneQ = new QuoteSelection(feed(none, none), new Composer());
  await noneQ.sampleBeforeFocus();
  const rNone = await noneQ.run();
  const rUnknown = await new QuoteSelection(feed(none), new Composer()).run();
  const dead = { async selection() { throw new Error('덱이 안 답한다'); } };
  const rRead = await new QuoteSelection(dead, new Composer()).run();

  const say = (r) => quoteNote(r);
  // 덱이 죽은 것을 **사람 탓으로 안 돌린다.** 그리고 안 사라진다 — 다시 누를지 새로고침할지
  // 정하는 사이에 쪽지가 없어지면 사유가 통째로 없어진다.
  ok('못 읽은 것은 붙어 있는다',
    say(rRead).sticky === true && say(rRead).text.includes('덱이 답하지 않았습니다'),
    JSON.stringify(say(rRead)));
  // **수를 싣는다.** 「날아갔다」만으로는 사람이 자기 눈을 안 믿는다.
  ok('날아간 선택은 몇 개였는지까지 적는다',
    say(rLost).text.includes('2개') && rLost.beforeCount === 2, say(rLost).text);
  // 갈라 놓은 값이 여기서 도로 뭉치면 S14 가 안 재진다.
  ok('「모른다」와 「안 골랐다」는 다른 말로 나간다',
    say(rUnknown).text !== say(rNone).text
    && say(rUnknown).text.includes('못 가릅니다'), say(rUnknown).text);
  ok('안 고른 사람에게는 무엇을 할지 알려 준다',
    say(rNone).text.includes('클릭') && say(rNone).sticky === false, say(rNone).text);
  // **다섯째 사유가 생기면 여기서 소리가 난다.** 앞 판본의 `else` 는 그걸 「도형을 클릭한 뒤
  // 다시 눌러 주세요」로 접어 사람 탓으로 바꿔 적었다 — 아무 표시 없이.
  const fifth = quoteNote({ reason: 'lockedByOther', beforeCount: 0 });
  ok('모르는 사유는 사람 탓으로 안 접힌다',
    fifth.text.includes('lockedByOther') && fifth.sticky === true
    && fifth.text !== say(rNone).text, fifth.text);
  // 넷이 서로 다른 말이라는 것 자체가 계약이다. 둘이 같아지면 위 개별 단언 중 하나는 여전히
  // 초록일 수 있다(부분 문자열이라).
  ok('사유마다 다른 말이 나간다',
    new Set([rRead, rLost, rUnknown, rNone].map((r) => say(r).text)).size === 4);
}

// ── 어느 덱에 붙는가. 사유 넷을 **갈라 돌려주는지**를 잰다 — 갈라 놓고 뭉치면 화면이 안 일어난
// 일을 적는다. 여기까지 시험이 하나도 없었고, 그래서 「던진 것을 시한 초과라 적는」 결함이
// 살아 있었다. 밖에서는 Office 가 없으므로 전역 대신 손으로 넣는다.
{
  const host = (h, ms = 0) => ({
    HostType: { PowerPoint: 'PowerPoint' },
    onReady: () => new Promise((r) => setTimeout(() => r({ host: h }), ms)),
  });

  const none = await pickDeck({ office: null });
  ok('Office 가 없으면 가짜로 간다', none.why === 'no-office' && none.deck instanceof FakeDeck);

  const ppt = await pickDeck({ office: host('PowerPoint') });
  ok('PowerPoint 면 진짜 덱이다', ppt.why === null && ppt.deck instanceof OfficeDeck);

  // Office **안**인데 PowerPoint 가 아니다. 옆의 가짜 캔버스가 설명이 못 되는 경우라
  // 무엇에 붙었는지가 값에 실려야 화면이 말할 수 있다.
  const word = await pickDeck({ office: host('Word') });
  ok('PowerPoint 가 아니면 그렇다고 갈라 든다', word.why === 'not-powerpoint');
  ok('무엇이었는지도 같이 든다', word.host === 'Word', String(word.host));

  // **던진 것과 늦은 것은 다른 사실이다.** 앞 판본은 던짐을 `timeout` 으로 적었고, 그때
  // 화면은 「1.5초 안에 안 와」라고 말했다 — 안 일어난 일이다. 게다가 `onReady()` 가 그
  // 자리에서 던지면 늦은 답이 아예 없어 **뒤늦게 바로잡아 줄 것도 없다.**
  const boom = new Error('office.js 를 못 읽었습니다');
  const threw = await pickDeck({ office: { HostType: { PowerPoint: 'PowerPoint' },
    onReady: () => { throw boom; } } });
  ok('던진 것을 시한 초과라 적지 않는다', threw.why === 'threw', threw.why);
  ok('던진 것을 값에 싣는다', threw.error === boom);
  ok('던졌으면 늦은 답도 없다', threw.late === null);

  // 늦은 것은 늦은 것이다 — 그리고 늦은 답을 **계속 듣는다**(화면이 나중에 바로잡는다).
  const slow = await pickDeck({ office: host('PowerPoint', 50), waitMs: 5 });
  ok('시계가 이기면 가짜로 가되 사유가 다르다',
    slow.why === 'timeout' && slow.deck instanceof FakeDeck);
  ok('늦은 답을 계속 듣는다', slow.late !== null);
  ok('늦게 온 답이 진짜로 온다', (await slow.late) === 'PowerPoint');

  // `HostType` 이 없는 판. 호스트를 안 밝힌 답(`null`)과 「PowerPoint 다」를 같다고 세면
  // Word 위에서 진짜 덱을 만든다 — 모르는 둘을 같다고 세는 자리다.
  const blind = await pickDeck({ office: { onReady: async () => ({}) } });
  ok('모르는 둘을 같다고 세지 않는다',
    blind.why === 'not-powerpoint' && blind.deck instanceof FakeDeck, blind.why);

  // 갈라 둔 사유가 **문장에서도** 갈라지는가. 앞 판본은 `not-powerpoint` 하나를 한 문장으로
  // 내보내서, 호스트를 안 밝힌 답에 대고 「PowerPoint 가 아닌 Office 호스트입니다(호스트를
  // 안 밝힘)」이라 적었다 — 브라우저에서 그냥 여는 **가장 흔한 길**에서 없는 호스트를 있다고
  // 말한 것이다. 값이 갈라져 있어도 읽는 쪽이 안 가르면 안 가른 것과 같다.
  ok('안 밝힌 호스트를 「다른 호스트」라고 적지 않는다',
    !pickNote(blind).includes('PowerPoint 가 아닌 Office 호스트'), pickNote(blind));
  ok('밝힌 호스트는 그 이름으로 적는다',
    pickNote(word).includes('Word'), pickNote(word));
  ok('그 둘이 서로 다른 문장이다', pickNote(blind) !== pickNote(word));
  ok('없는 호스트를 괄호로 지어내지 않는다', !pickNote(blind).includes('('), pickNote(blind));

  // 옆에 가짜 캔버스가 떠 설명이 되는 길만 잠잠하다. 나머지 셋은 아무 설명이 없다.
  ok('Office 가 없는 길만 잠잠하다',
    pickNote(none) === null && pickNote(threw) !== null && pickNote(slow) !== null);
  ok('던진 것은 시한 초과와 다른 문장이다', pickNote(threw) !== pickNote(slow));
  ok('던진 사유를 문장이 싣는다', pickNote(threw).includes('office.js 를 못 읽었습니다'));

  // 사유가 **다섯째로 늘어날 때** 이 목록이 같이 자라지 않으면, 갈라 둔 값을 화면이 도로
  // 뭉치는 상태로 조용히 돌아간다. 컴파일러가 안 우는 자리라 문장이 대신 운다.
  const fifth = pickNote({ why: 'quota', host: null, error: null });
  ok('모르는 사유는 잠잠하지 않다', fifth !== null, String(fifth));
  ok('모르는 사유를 아는 사유의 문장으로 적지 않는다',
    fifth !== pickNote(slow) && fifth !== pickNote(threw) && fifth !== pickNote(blind));
  ok('무엇이 모르는 사유였는지 싣는다',
    String(fifth).includes('quota'), String(fifth));

  // **그 그물이 성공을 같이 걷어 올렸다.** `why === null` 은 「사유가 없다」= 진짜 덱에
  // 붙었다는 뜻인데(`pickDeck` 이 `OfficeDeck` 과 함께 돌려주는 값), 위 catch-all 이
  // `why !== 'not-powerpoint'` 로 묻는 바람에 여기로 떨어졌다. 그래서 **진짜 PowerPoint 안에서**
  // 「이 창이 모르는 사유로 가짜 덱에 붙었습니다(null). 이 창을 고쳐야 합니다.」가 떴다.
  // 하필 판 자리(`view.where`)라 창이 사는 내내 남고, 성공 경로에는 `late` 도 없어 덮어써 줄
  // 것도 없다. 이 제품이 존재하는 **유일한 환경에서 화면 맨 위가 거짓말**이었던 셈이다.
  //
  // 사유를 갈라 놓은 값에 시험이 다섯 줄이나 붙어 있었는데도 못 잡은 이유는, 다섯 줄이 전부
  // **가짜로 간 갈래**만 물었기 때문이다. 성공은 아무도 안 물었다(필드 드롭 계측이
  // `host` 를 성공 갈래에서 지워도 조용한 것을 보고 여기를 들여다봤다).
  ok('진짜 덱에 붙은 것은 할 말이 없다',
    pickNote({ why: null, host: 'PowerPoint', error: null }) === null,
    String(pickNote({ why: null, host: 'PowerPoint', error: null })));
  ok('그래도 다섯째 사유는 여전히 운다', pickNote({ why: 'quota' }) !== null);

  // 시한 쪽지는 「PowerPoint 안이라면 새로고침하세요」라는 **조건부**다. 늦게 온 답이 조건을
  // 깨면 그 권유가 틀린 권유가 되므로 늦은 쪽지가 덮어써야 한다.
  ok('늦게 잡은 PowerPoint 는 새로고침하라 한다',
    lateNote('PowerPoint', 'PowerPoint').includes('새로고침하면'));
  ok('늦게 온 딴 호스트는 이름으로 적는다',
    lateNote('Word', 'PowerPoint').includes('Word'));
  ok('늦게 왔는데 안 밝힌 것을 「PowerPoint 가 아니다」라고 단정하지 않는다',
    !lateNote(null, 'PowerPoint').includes('PowerPoint 가 아닙니다'), lateNote(null, 'PowerPoint'));
  // `HostType` 이 없는 판: `want` 도 `null` 이다. 「둘 다 모른다」를 「같다」로 세면 Word 위에서
  // 「PowerPoint 를 늦게 잡았습니다」가 나간다.
  ok('모르는 둘을 늦은 쪽지에서도 같다고 세지 않는다',
    !lateNote(null, null).includes('늦게 잡았습니다'), lateNote(null, null));
  ok('끝내 못 잡은 것은 늦게 온 답과 다른 문장이다',
    lateFailNote(boom).includes('office.js 를 못 읽었습니다')
      && lateFailNote(boom) !== lateNote(null, 'PowerPoint'));

  // **Error 가 아닌 것으로 거절하는 약속을 이 창은 못 건다.** `Office.onReady()` 는 남의
  // 코드고, `Promise.reject('...')` 로 문자열을 던지는 것은 흔하다. `msgOf` 의 `?? String(e)`
  // 가 그날을 위한 줄인데, 스위트 전체에서 이 자리에 간 값이 **Error 하나뿐**이라 그 줄은 한
  // 번도 안 돌았다(인자 값 다양성 계측). 안 돌면 남는 것은 「(undefined)」고, 그것은 사유가
  // 아니라 **사유가 빠졌다는 표시조차 아닌 값**이다.
  ok('Error 가 아닌 것을 던져도 사유가 실린다',
    lateFailNote('boom').includes('boom') && !lateFailNote('boom').includes('undefined'),
    lateFailNote('boom'));
  const wordy = pickNote({ why: 'threw', host: null, error: 'boom' });
  ok('첫 쪽지도 같은 자리에서 안 비어야 한다',
    wordy.includes('boom') && !wordy.includes('undefined'), wordy);
}
// ── §9 의 스파이크 목록은 **여기서 못 재는 것**들이다. 그 사실을 산문으로만 들고 있으면
// 낡는다 ─────────────────────────────────────────────────────────────────────
//
// 문서는 S1…S14 를 "아직 확인 안 된 전제"라고 적어 두고, 어느 것이 왜 아직인지는 여기저기
// 흩어진 문장으로만 들고 있다. 그 상태에서 독자가 할 수 있는 것은 **없다** — 「안 재졌다」와
// 「재려다 막혔다」와 「재는 것을 잊었다」가 화면에서 같게 생겼고, 다음 사람이 새 S 행을 하나
// 더 붙일 때 사유를 안 적어도 아무 데서도 소리가 안 난다.
//
// 그래서 목록을 값으로 옮긴다. 요점은 **시험을 강요하는 것이 아니라 사유를 강요하는 것**이다:
// 못 재는 항목마다 「이 저장소가 무엇을 못 줘서 못 재는가」를 한 줄로 적게 하고, 그 줄이
// **자기가 낡았다고 말하게** 둔다 — 재게 되는 날 그 항목은 `ok()` 딱지에 이름이 실릴 텐데,
// 그 순간 아래 마지막 검사가 「목록이 낡았다」고 운다.
{
  const design = readFileSync(new URL('../../DESIGN.md', import.meta.url), 'utf8');
  const inDoc = [...design.matchAll(/^\| \*\*(S\d+)\*\* \|/gm)].map((m) => m[1]);
  ok('§9 의 표를 읽었다', inDoc.length >= 10, String(inDoc.length));

  // 여기서 **잰** 것. 무엇이 답을 들고 있는지까지 적는다 — 「끝났다」만으로는 다음 사람이
  // 결과를 못 찾고, 못 찾으면 다시 재게 된다.
  const measured = { S9: '§5.0.2 — 켜진 데몬에 런타임으로 붙이는 것이 예상과 다른 이유로 막혔다' };

  // 여기서 **못 재는** 것과, 이 저장소가 무엇을 못 줘서 못 재는지. 「PowerPoint 가 없다」를
  // 열세 번 적으면 목록이 제값을 못 한다 — 항목마다 **없는 것이 실제로 다르다.**
  const cannotMeasureHere = {
    S1: '진짜 PowerPoint 호스트 — stub 은 문서를 읽고 적은 흉내지 호스트가 아니다',
    S2: '사람 눈 — 렌더가 알아볼 만한지는 단언으로 못 적는다',
    S3: 'OS 의 URL 스킴 등록과 애드인이 실제로 도는 웹뷰',
    S4: '편집기의 undo 스택 — 우리 가짜 덱에는 스택이 없다',
    S5: '`exportAsBase64` 가 뱉는 진짜 바이트, 그리고 그것을 도로 받아 줄 호스트',
    S6: '100장짜리 실덱과 진짜 왕복 시간',
    S7: '실덱 100장 + 모델 예산 — 재는 것이 정확도라 흉내로는 수가 안 나온다',
    S8: '동시에 도는 데몬 둘과 워크스페이스 둘',
    S10: '브라우저 엔진의 LNA 게이트 — Node 에는 그 게이트가 아예 없다',
    S11: '저장 안 된 새 덱 둘을 동시에 연 PowerPoint',
    S12: '4:3 · 16:9 · 사용자 지정 크기로 만든 진짜 덱 셋',
    S13: '3번을 보는 채로 5번 도형을 잡아 보는 실제 창',
    S14: '작업창을 클릭하는 사람 손',
  };

  const known = new Set([...Object.keys(measured), ...Object.keys(cannotMeasureHere)]);
  const unaccounted = inDoc.filter((id) => !known.has(id));
  ok('§9 의 모든 항목이 둘 중 한 목록에 있다', unaccounted.length === 0, unaccounted.join(' '));
  const gone = [...known].filter((id) => !inDoc.includes(id));
  ok('없어진 항목을 목록이 붙들고 있지 않다', gone.length === 0, gone.join(' '));
  const both = Object.keys(measured).filter((id) => id in cannotMeasureHere);
  ok('한 항목이 잰 것이면서 못 재는 것일 수 없다', both.length === 0, both.join(' '));

  // **사유가 사유여야 한다.** 빈 줄이나 id 를 되뇌는 줄은 「안 적었다」와 같다.
  const thin = Object.entries(cannotMeasureHere)
    .filter(([id, why]) => why.trim().length < 12 || why.includes(id));
  ok('못 재는 사유가 한 줄이라도 비어 있지 않다',
    thin.length === 0, thin.map(([i]) => i).join(' '));

  // **낡음은 목록이 스스로 말한다.** 어떤 항목을 여기서 재게 되면 그 이름이 `ok()` 딱지에
  // 실릴 텐데, 그러면 「못 잰다」가 거짓이 된 것이다. 이 검사는 그날 운다.
  const self = readFileSync(new URL(import.meta.url), 'utf8');
  const labels = [...self.matchAll(/^\s*ok\('([^']*)'/gm)].map((m) => m[1]).join('\n');
  const aged = Object.keys(cannotMeasureHere)
    .filter((id) => new RegExp(`\\b${id}\\b`).test(labels));
  ok('못 잰다고 적은 것을 재고 있지 않다', aged.length === 0, aged.join(' '));
}

// ── 단추는 조용히 죽지 않는다 ────────────────────────────────────────────────
//
// `View.guard` 는 안쪽 유스케이스의 **약속**(던지는 대신 사유를 값에 싣는다)이 깨진 날을 위해
// 있다. 그런데 그 규칙을 **콜사이트마다 손으로** 지키고 있었고, 그래서 `guard` 자기 주석이
// 이름 댄 셋 중 하나를 빠뜨린 채로 있었다(30599cda). 손으로 지키는 규칙은 자리가 하나 늘 때
// 안 지키는 쪽이 나온다.
//
// 그래서 여기서 **세어 보는 게 아니라 하나도 빠짐없이 분류한다.** 개수 단언은 프록시라, 자리가
// 둘 늘고 둘 줄면 잠잠하다. 분류가 안 되는 자리가 하나라도 있으면 그 줄을 이름 대고 운다 —
// 새로 붙는 리스너는 `guard`/`send` 를 지나거나, 왜 안 지나도 되는지 **여기 적혀야** 한다.
//
// 정규식이 깨지면 자리 0개로 조용히 초록이 되므로, 찾은 자리가 0 이면 그것부터 운다.
{
  const src = readFileSync(new URL('../src/ui/view.js', import.meta.url), 'utf8');
  const lines = src.split('\n');
  // 지나도 되는 자리 — 손 대는 것이 이 창 안의 값뿐이라 던질 것이 없거나, 던지는 것을
  // 제가 잡는 자리. 여는 줄을 그대로 적어 둔다(줄 번호는 움직이므로).
  const allowed = new Map([
    ["$('#quote').addEventListener('pointerenter', () => this.quoteSelection.sampleBeforeFocus());",
      'QuoteSelection.sampleBeforeFocus 가 제 안에서 잡는다 — 사람이 누른 것이 아니라 계측이다'],
    ["input.addEventListener('keydown', (e) => { if (e.key === 'Enter') go(); });",
      'go() 가 this.send 를 지난다'],
    ["b.addEventListener('click', go);", 'go() 가 this.send 를 지난다'],
    ["b.addEventListener('click', () => {", '그만 기다리기 — composer.release 와 다시 그리기뿐'],
    ["x.addEventListener('click', () => {", '인용 빼기 — composer.detach 와 다시 그리기뿐'],
    // 묻는 판의 셋. 하는 일이 **약속 하나를 매듭짓는 것뿐**이라 던질 문이 없다 — 진짜 일은
    // 답을 받은 쪽(`main.js` 의 지우기)이 하고, 그쪽이 제 사유를 적는다.
    ["ok.addEventListener('click', yes);", '묻는 판 — resolve(true) 뿐'],
    ["cancel.addEventListener('click', no);", '묻는 판 — resolve(false) 뿐'],
    ["box.addEventListener('keydown', esc);", '묻는 판 — Escape 는 그만두는 쪽이다'],
  ]);
  const sites = [];
  const stray = [];
  for (let i = 0; i < lines.length; i++) {
    if (!lines[i].includes('addEventListener(')) continue;
    const head = lines[i].trim();
    sites.push(head);
    if (allowed.has(head)) continue;
    // 핸들러가 여러 줄일 수 있어 뒤를 조금 본다. `guard`/`send` 중 하나를 지나야 한다.
    const body = lines.slice(i, i + 8).join('\n');
    if (!body.includes('this.guard(') && !body.includes('this.send(')) {
      stray.push(`${i + 1}: ${head}`);
    }
  }
  ok('view.js 의 리스너 자리를 실제로 찾았다', sites.length > 0, sites.length);
  ok('guard 를 안 지나는 리스너는 여기 사유가 적혀 있다', stray.length === 0, stray.join(' / '));
  // 허용 목록이 **낡는 것**도 잡는다. 지운 자리를 계속 허용해 두면, 나중에 같은 첫 줄을 가진
  // 새 리스너가 아무 심사 없이 통과한다.
  const dead = [...allowed.keys()].filter((k) => !sites.includes(k));
  ok('허용 목록에 없어진 자리가 남아 있지 않다', dead.length === 0, dead.join(' / '));
  // **열쇠가 첫 줄 글자라, 같은 첫 줄을 가진 자리가 둘 되면 하나의 사유로 둘이 통과한다.**
  // 오늘 열한 자리가 다 다른 것은 사실이지 규칙이 아니다 — 규칙을 어길 자리는 보통 **새로
  // 생기는 자리**고, 그게 `b.addEventListener('click', () => {` 처럼 흔한 머리면 아무 심사
  // 없이 들어온다. 그래서 「하나에 하나」를 여기서 못 박는다.
  const doubled = [...allowed.keys()]
    .map((k) => [k, sites.filter((h) => h === k).length])
    .filter(([, n]) => n > 1);
  ok('허용된 첫 줄 하나가 자리 둘을 덮고 있지 않다', doubled.length === 0,
    doubled.map(([k, n]) => `${n}× ${k}`).join(' / '));
}

// ── 화면에 넣는 길은 마크업을 못 읽는 길 하나뿐이다 ──────────────────────────
//
// **숨기는 것과 뭉개는 것이 같은 결함이다.** 인자가 안 실려 칸이 통째로 없어진 자리에서
// 사람이 무엇을 허가하는지 모르고 누르는데(44152fd2), 실린 인자를 마크업으로 읽어
// `rm x && echo <done>` 의 `<done>` 을 삼켜도 **같은 것을 모르고 누른다.** 안 보이는 창이나
// 실제와 다른 글자를 보이는 창이나 결과가 같다. IDE 쪽 창이 스윙 `<html>` 라벨에서 실제로
// 그랬고(44699bd1), 거기서는 이스케이프하는 문을 하나 두고 넷을 다 지나게 해서 닫았다.
//
// 이 창은 오늘 그 자리가 없다 — 넣는 자리가 전부 `textContent` 다. 그런데 **그건 지켜지는
// 규칙이 아니라 습관**이라, 첫 `innerHTML` 이 들어오는 날 아무 데서도 소리가 안 난다.
// 위 리스너 검사가 「손으로 지키는 규칙은 자리가 하나 늘 때 안 지키는 쪽이 나온다」로 세운
// 것과 같은 자리다. 그래서 습관을 문으로 바꾼다: 마크업을 읽는 길은 이 목업에 **없어야**
// 하고, 없다는 것을 매 런이 다시 잰다.
//
// 파일 목록을 손으로 안 적는다 — 규칙을 어길 자리는 보통 **새로 생기는 파일**이고, 적어 둔
// 목록은 그 파일을 안 본다. `taskpane.html` 도 같이 훑는다: 오늘 두 `<script>` 는 둘 다
// `src=` 지만, 인라인 블록이 하나 생기면 그건 `src/` 밖이라 위 훑기에 안 걸린다.
//
// 스캔이 깨지면 자리 0개로 조용히 초록이 되므로, 훑은 파일과 **넣는 자리**를 실제로 찾았는지
// 부터 운다. 뒤엣것이 특히 그렇다: 「마크업 읽는 길이 없다」는 넣는 자리가 하나도 없어도 참이다.
{
  const root = new URL('../src/', import.meta.url);
  const walk = (dir) => readdirSync(dir, { withFileTypes: true }).flatMap((e) =>
    e.isDirectory() ? walk(new URL(`${e.name}/`, dir)) : [new URL(e.name, dir)]);
  const files = [...walk(root).filter((u) => u.pathname.endsWith('.js')),
    new URL('../taskpane.html', import.meta.url)];
  const SINKS = /\b(innerHTML|outerHTML|insertAdjacentHTML|document\.write)\b/;
  const sunk = [];
  let puts = 0;
  for (const f of files) {
    const text = readFileSync(f, 'utf8');
    puts += (text.match(/\.textContent\b/g) ?? []).length;
    text.split('\n').forEach((line, i) => {
      if (SINKS.test(line)) sunk.push(`${f.pathname.split('/addin/')[1]}:${i + 1}`);
    });
  }
  ok('훑을 파일을 실제로 찾았다', files.length > 1, `${files.length} 파일`);
  ok('글을 넣는 자리를 실제로 찾았다', puts > 0, `${puts} 자리`);
  ok('마크업을 읽는 길이 하나도 없다', sunk.length === 0, sunk.join(' / '));
}

// ── 훑는 단언은 빈 것에 초록을 안 준다 ─────────────────────────────────────────
// 위 스캔과 같은 수를 이 파일 자신에게 쓴다. `[].every(f)` 가 늘 참이라 훑을 것이 없는 단언은
// 술어가 무엇이든 통과하는데, 실제로 그런 줄이 하나 서 있었다 — 표가 빈 판에서 표의 값을
// 훑었고, 술어를 상수 거짓으로 바꿔도 스위트가 초록이었다. 여덟 자리를 다 재 보니 나머지
// 일곱은 오늘 안 비어 있다. **그건 규칙이 아니라 운이고, 여덟째가 올 때 아무 데서도 소리가
// 안 난다.** 그래서 `everyOf` 하나로 길을 좁히고, 안 거친 것이 있으면 여기서 이름을 부른다.
//
// 주석 줄은 뺀다. 예외 목록이 아니라 문법 갈래라 늘어날 자리가 없다 — 바로 위 `everyOf` 의
// 설명이 자기 이야기를 하느라 `.every(` 를 적고 있고, 그걸 예외로 적기 시작하면 목록이 산다.
//
// 두 겹인 이유도 같다. 「안 거친 것이 없다」는 훑을 것을 못 찾아도 참이라, 떠받치는 줄은
// 「훑는 자리를 실제로 찾았다」 쪽이다.
{
  // 찾는 글자를 통째로 안 적는다. 적으면 **이 줄이 스스로 걸린다** — 첫 판에서 실제로 그랬고,
  // 세는 자리도 하나 부풀어 「9 자리」라고 적었다(여덟이다). 스캐너가 제 바늘에 걸리는 것을
  // 예외로 빼면 그 예외가 진짜 위반도 같이 가려 준다.
  const CALL = `.${'every'}(`;
  const VIA = `${'every'}Of(`;
  const self = readFileSync(new URL(import.meta.url), 'utf8').split('\n')
    .map((l, i) => [i + 1, l]).filter(([, l]) => !/^\s*(\*|\/\/)/.test(l));
  const sweeps = self.filter(([, l]) => l.includes(VIA)).length;
  const bare = self.filter(([, l]) => l.includes(CALL)).map(([n]) => `smoke.mjs:${n}`);
  ok('훑는 단언 자리를 실제로 찾았다', sweeps > 1, `${sweeps} 자리`);
  ok('훑는 단언이 전부 빈 것을 거르는 길로 간다', bare.length === 0, bare.join(' '));
}

// ── 요구 집합 계측(§12 #4). `OfficeDeck.capabilities()` 를 stub 위에서 실제로 돌린다.
//
// 여기 단언이 있는 이유는 `ok` 가 **불리언이 아니라 셋**이라서다 — 지원/아니오/**물어보다
// 던졌다.** 셋째를 `false` 로 접으면 화면에 ✗ 가 서고, 그건 「호스트가 아니라고 답했다」는
// 말이라 §12 #4 를 없는 실측으로 답해 버린다. 주석으로만 적어 두면 다음 어댑터가 `catch`
// 에서 `false` 를 쓰는 것을 아무도 안 막는다.
{
  const asked = [];
  globalThis.Office = {
    context: {
      requirements: {
        isSetSupported(name, version) {
          asked.push(`${name} ${version}`);
          if (version === '1.7') throw new Error('호스트가 이 물음에 터진다');
          return name === 'SharedRuntime' || version === '1.2' || version === '1.5';
        },
      },
    },
  };
  try {
    const caps = new OfficeDeck().capabilities();
    ok('물어봤으면 쟀다고 말한다', caps.measured === true && caps.note === '', caps.note);
    // 셈이 아니라 **명단**으로 못박는다. 하나가 빠지면 빠진 이름이 diff 에 보인다.
    //
    // **바닥 위(1.9·1.10)도 명단에 있다.** 문서가 「그건 1.10 이라 못 한다」고 적고 있었는데,
    // 그건 스펙을 읽고 적은 것이지 이 호스트에 물어본 것이 아니었다 — 우리 탐침이 1.8 에서
    // 멈춰 있었다. **못 하는 것의 목록은 재 보고 적는다.**
    const want = 'PowerPointApi 1.2,PowerPointApi 1.5,PowerPointApi 1.6,'
      + 'PowerPointApi 1.7,PowerPointApi 1.8,PowerPointApi 1.9,PowerPointApi 1.10,'
      + 'SharedRuntime 1.1';
    const got = caps.sets.map((s) => `${s.name} ${s.version}`).join(',');
    ok('여덟을 요약 없이 그대로 돌려준다', got === want, got);
    ok('물어본 것과 돌려준 것이 같다', asked.join(',') === want, asked.join(','));
    const by = new Map(caps.sets.map((s) => [`${s.name} ${s.version}`, s.ok]));
    ok('그렇다고 답한 집합은 true', by.get('PowerPointApi 1.5') === true);
    ok('아니라고 답한 집합은 false', by.get('PowerPointApi 1.8') === false);
    // 이 한 줄이 이 블록의 값이다: `null` 이지 `false` 가 아니다.
    ok('물어보다 던진 집합은 false 가 아니라 null',
      by.get('PowerPointApi 1.7') === null, String(by.get('PowerPointApi 1.7')));
  } finally {
    delete globalThis.Office;
  }
  // Office 가 없는 곳에서는 **잰 척을 안 한다.** 이 머신이 바로 그런 곳이라 늘 여기로 온다.
  const bare = new OfficeDeck().capabilities();
  ok('Office 가 없으면 안 쟀다고 말한다', bare.measured === false && bare.sets.length === 0);
  ok('안 쟀으면 사유를 댄다', typeof bare.note === 'string' && bare.note.length > 0, bare.note);
}

// ── 돌연변이가 살아남은 자리 ─────────────────────────────────────────────────
//
// 이 블록은 **한 번의 계측에서 나왔다.** 2026-08-29 에 `src/domain/*.js` 와 `src/usecase/*.js`
// 의 판단 연산자를 한 줄에 하나씩 뒤집고(`===`↔`!==`, `<=`→`<`, `&&`↔`||`,
// `return true`↔`return false`) 그때마다 이 파일을 통째로 돌렸다. **74 를 뒤집어 18 이
// 살아남았다** — 211개 단언 중 어느 것도 안 죽는 줄이 18 개였다는 뜻이다.
//
// 살아남은 줄은 둘 중 하나다: 시험이 없거나, **맞는데 맞는 이유가 아무 데도 안 적혀 있거나.**
// 앞은 여기서 메우고, 뒤는 고칠 것이 없으니 결정이 사는 자리에 이유를 적는다(§5.7 의 셋째).
// 뒤에 해당하는 것 둘을 먼저 이름 대 둔다:
//
// - `Cursor.advanced` 의 `seq <= 0` — 그 줄이 지금 이 클라이언트를 안 지킨다. 왜 그런데도
//   두는지는 그 파일 안에 적혀 있다.
// - `WatchPrompt.poll` 의 `wasReachable || !this.saidLost` — 뒤 절이 **판단을 한 번도
//   안 한다.** `saidLost` 가 참인 폴은 끝에서 `reachable` 을 거짓으로 두고, 닿는 폴은
//   `saidLost` 를 거짓으로 되돌리므로 `wasReachable && saidLost` 는 이 클래스 안에서 못
//   나온다. 그래서 `||` 를 `&&` 로 바꿔도 아무 시험이 안 죽는다 — 시험의 구멍이 아니라
//   **불변식이 뒤 절을 삼킨 것**이다. 그 불변식을 아래에서 시험이 붙든다.
{
  // 커서. 이 파일이 `Cursor` 를 직접 부른 적이 없어서 세 줄이 통째로 안 잡혀 있었다.
  const c = new Cursor('sess-a', 12);
  ok('같은 대화면 그 자리부터', c.sinceFor('sess-a') === 12);
  ok('다른 대화면 처음부터', c.sinceFor('sess-b') === -1);
  ok('다른 대화의 커서는 못 쓴다', c.usableFor('sess-b') === false);
  ok('안 읽은 커서도 못 쓴다', new Cursor('sess-a', 0).usableFor('sess-a') === false);
  ok('같은 대화에서 읽은 것이 있으면 쓴다', c.usableFor('sess-a') === true);

  ok('같은 대화에서 뒤로 가는 자리는 안 민다', c.advanced('sess-a', 5) === c);
  ok('같은 자리를 다시 봐도 안 민다', c.advanced('sess-a', 12) === c);
  ok('앞으로 가면 민다', c.advanced('sess-a', 13).seq === 13);
  // **대화가 바뀌면 낮은 자리도 앉는다.** 남의 대화에서 센 12 는 이 대화의 12 와 아무 상관이
  // 없으므로, 여기서 「뒤로 간다」고 막으면 새 대화의 앞부분을 영영 못 읽는다.
  const moved = c.advanced('sess-b', 3);
  ok('대화가 바뀌면 낮은 자리도 앉는다', moved.sessionId === 'sess-b' && moved.seq === 3);
}

{
  // 물음. 종류를 가르는 두 줄과 「1개뿐이면 안 센다」가 안 잡혀 있었다.
  const perm = new Pending({ id: 'c1', kind: 'permission', what: 'bash' });
  const ques = new Pending({ id: 'c2', kind: 'question', what: '어느 쪽?' });
  ok('권한 물음은 권한 물음이다', perm.isPermission === true && perm.isQuestion === false);
  ok('질문은 질문이다', ques.isQuestion === true && ques.isPermission === false);
  ok('모르는 종류는 둘 다 아니다',
    new Pending({ id: 'c3', kind: 'confirm', what: 'x' }).known === false);
  // 하나뿐인 물음에 「1번째 · 모두 1개」를 다는 것은 없는 줄을 세우는 일이다.
  ok('하나뿐이면 자리를 안 적는다',
    new Pending({ id: 'c4', kind: 'question', what: 'x', index: 1, total: 1 }).placement === null);

  // **인자 칸.** 화면은 `if (p.args != null)` 한 줄이라, 안 실리면 칸이 통째로 없었다 —
  // 「권한을 묻고 있습니다 · bash」와 허용/거절 단추만 서고, 사람은 무엇을 허가하는지 모르는
  // 채로 누른다. 위의 `perm` 이 바로 그 모양이다(인자 없는 권한 물음).
  const slot = askArgs(perm);
  ok('인자가 안 실린 권한 물음은 그 사실을 말한다',
    slot?.note != null && slot.args === undefined, JSON.stringify(slot));
  // 소켓의 `Args` 는 `omitempty` 라 「인자 없이 부르는 도구」와 「오다 빠진 인자」가 여기
  // 도착할 때 똑같이 생겼다. 못 가르는 것을 가른 척하면 그게 이 창이 없애려는 뭉갬이다.
  ok('못 가르는 둘을 가른 척하지 않는다', slot.note.includes('못 가릅니다'));
  ok('도구 이름만 보고 누르지 말라고 적는다', slot.note.includes('도구 이름만'));
  // 질문에는 허가할 것이 없다 — 보기와 적는 칸이 그 물음의 내용이다.
  ok('질문에는 안 붙인다', askArgs(ques) === null);

  const real = { cmd: 'rm -rf build' };
  ok('실린 인자는 그대로 나른다',
    askArgs(new Pending({ id: 'c5', kind: 'permission', what: 'bash', args: real }))
      .args === real);
  ok('글로 실린 인자도 그대로 나른다',
    askArgs(new Pending({ id: 'c6', kind: 'permission', what: 'bash', args: 'ls -al' }))
      .args === 'ls -al');

  // **빈 것을 빈 상자로 그리지 않는다.** 빈 `<pre>` 는 「인자가 이렇다」도 「없다」도 아니고
  // 화면이 고장 난 것처럼 보인다. 셋 다 「아무것도 안 실었다」의 다른 철자다.
  const blanks = [{}, [], '   '];
  ok('빈 것은 빈 상자 대신 말로 나간다', everyOf(blanks, (a) =>
    askArgs(new Pending({ id: 'c7', kind: 'permission', what: 'bash', args: a }))?.note
      === '인자 없이 부릅니다.'), JSON.stringify(blanks.map((a) =>
    askArgs(new Pending({ id: 'c7', kind: 'permission', what: 'bash', args: a })))));
  // 「안 실렸다」와 「인자 없이 부른다」는 다른 말이다 — 앞엣것은 이 창이 모른다는 뜻이다.
  ok('안 실린 것과 빈 것은 다른 말이다',
    askArgs(new Pending({ id: 'c8', kind: 'permission', what: 'bash', args: {} })).note
      !== slot.note);
}

{
  // 치수. **둘 중 하나만 없어도 없는 것이다** — 안 그러면 `NaN×3.0cm` 이 화면에 뜬다.
  const both = new Quote({ slideId: 's1', shapeId: 'a', width: 72, height: 72 });
  ok('둘 다 있으면 적는다', both.sizeLabel === '2.5×2.5cm', String(both.sizeLabel));
  ok('높이가 없으면 안 적는다',
    new Quote({ slideId: 's1', shapeId: 'a', width: 72 }).sizeLabel === null);
  ok('폭이 없어도 안 적는다',
    new Quote({ slideId: 's1', shapeId: 'a', height: 72 }).sizeLabel === null);
}

{
  // 카드에 적는 글토막. **스위트가 한 번도 안 불렀다** — 유일한 소비자가 `view.js` 라
  // DOM 이 있어야 도는 자리고, 그래서 길이 제한을 통째로 떨어뜨려도 아무도 안 울었다.
  // 도메인 함수니 DOM 없이 그냥 부르면 된다.
  const q = (text) => new Quote({ slideId: 's1', shapeId: 'a', text });
  ok('짧으면 그대로 둔다', q('3분기 매출').preview() === '3분기 매출');
  // 줄바꿈과 연속 공백이 남으면 카드가 한 줄이 아니게 된다.
  ok('공백은 한 칸으로 접는다', q(' 3분기\n\n 매출  전망 ').preview() === '3분기 매출 전망',
    q(' 3분기\n\n 매출  전망 ').preview());
  // 자른 티가 나야 한다 — 안 나면 사람이 **그게 도형의 전문인 줄 안다**.
  const long = q('가'.repeat(80)).preview();
  ok('길면 자르고 자른 표를 남긴다', long.length === 60 && long.endsWith('…'),
    `${long.length} / ${long.slice(-1)}`);
  ok('제한은 부르는 쪽이 바꾼다', q('가'.repeat(80)).preview(10).length === 10,
    q('가'.repeat(80)).preview(10).length);
  // 경계 한 칸: 딱 제한만큼이면 안 자른다. `>` 를 `>=` 로 고치면 멀쩡한 글에 …가 붙는다.
  ok('딱 제한만큼이면 안 자른다', q('가'.repeat(60)).preview() === '가'.repeat(60));
}

{
  // 인용 빼기. 붙이는 쪽만 잡혀 있었고 빼는 쪽이 통째로 안 잡혀 있었다.
  const cv = new Composer();
  cv.attach(new Quote({ slideId: 's1', shapeId: 'a' }));
  cv.attach(new Quote({ slideId: 's1', shapeId: 'b' }));
  ok('없는 것을 빼면 아무 일도 안 일어난다', cv.detach('zz') === false);
  ok('안 뺐으면 둘 다 그대로', cv.pending.length === 2);
  ok('뺐으면 뺐다고 한다', cv.detach('a') === true);
  ok('지목한 것만 빠진다',
    cv.pending.length === 1 && cv.pending[0].shapeId === 'b',
    cv.pending.map((q) => q.shapeId).join(','));
}

{
  // 번호표의 세대. `note` 가 이미 본 id 를 다시 적으면 그 id 는 **받은 답을 잃는다.**
  const sn = new SlideNumbers();
  sn.note('s7');
  const t = sn.ask();
  ok('답을 앉히면 앉혔다고 한다', sn.answer(t, new Map([['s7', 3]])) === true);
  ok('답을 받은 id 다', sn.answered('s7') === true);
  sn.note('s7');   // 화면은 그릴 때마다 다시 적는다 — 여기서 세대가 밀리면 안 된다
  ok('이미 본 id 를 다시 적어도 답을 안 잃는다', sn.answered('s7') === true);
  // 낡은 답과 같은 물음의 두 번째 답은 **안 앉는다.** 돌려주는 불리언이 「다시 그려라」다.
  ok('같은 물음의 답이 두 번 오면 둘째는 안 앉는다', sn.answer(t, new Map()) === false);
  ok('둘째가 안 앉았으니 답은 그대로', sn.map.get('s7') === 3, String(sn.map.get('s7')));
  ok('낡은 물음의 답은 안 앉는다', sn.answer(t - 1, new Map()) === false);
}

{
  // 안내 접기의 두 관문. 문자열이 아닌 값을 그대로 앉히면 화면이 객체를 슬라이드 id 로 적는다.
  const odd = foldAdvice([{ kind: 'tool', tool: 'mcp__ppt__advise', callId: 'c1',
    args: { items: [{ message: '어딘가', slideId: 7, shapeIds: ['sh1', 9] }] } }]);
  ok('문자열이 아닌 슬라이드 id 는 없는 것이다', odd.items[0].slideId === null,
    String(odd.items[0].slideId));
  ok('문자열이 아닌 도형 id 는 걸러진다',
    odd.items[0].shapeIds.length === 1 && odd.items[0].shapeIds[0] === 'sh1');
  // **한 장을 그냥 펴서 부른 호출도 받는다.** 못 알아들으면 그 안내는 아무 데도 안 남는다.
  const flat = foldAdvice([{ kind: 'tool', tool: 'mcp__ppt__advise', callId: 'c2',
    args: { message: '한 장짜리', slideId: 's1' } }]);
  ok('items 없이 한 장으로 부른 것도 받는다',
    flat.items.length === 1 && flat.items[0].message === '한 장짜리',
    `${flat.items.length}장`);
  // **못 읽은 호출과 빈 목록은 다른 답이다.** 앞은 모델이 말을 빼고 부른 것이라 사람에게
  // 알려야 하고, 뒤는 모델이 「없다」고 말한 것이라 알릴 것이 없다. 접으면 앞이 사라진다.
  ok('항목을 못 꺼내는 호출은 센다',
    foldAdvice([{ kind: 'tool', tool: 'mcp__ppt__advise', callId: 'c3',
      args: { slideId: 's1' } }]).dropped === 1);
  const none = foldAdvice([{ kind: 'tool', tool: 'mcp__ppt__advise', callId: 'c4',
    args: { items: [] } }]);
  ok('빈 목록을 실은 호출은 안 센다', none.dropped === 0 && none.items.length === 0,
    `dropped=${none.dropped}`);
  ok('인자가 아예 없는 호출은 못 읽은 것이다',
    foldAdvice([{ kind: 'tool', tool: 'mcp__ppt__advise', callId: 'c5',
      args: null }]).dropped === 1);

  // **포스트잇의 신원.** `${callId}#${i}` 인데 여태 이 신원을 아무도 안 봤다 — `Transcript`
  // 가 `callId` 를 아예 안 실어도, 여기서 `r.seq` 로만 지어도 스위트가 초록이었다(필드 드롭
  // 계측). 그게 왜 위험한지가 바로 아래 두 줄이다: 도구 호출은 **버스 전용 이벤트**로 와서
  // 로그 자리가 없는 일이 흔하고(`seq === 0`, `Row.positioned` 가 false 인 그 경우),
  // 그러면 다른 호출 둘이 `0#0` 하나를 나눠 갖는다. 캔버스가 신원으로 포스트잇을 세우므로
  // (§6.1), 겹치는 순간 **앞 안내가 뒤 안내 자리에 앉거나 뒤엣것이 아예 안 뜬다.**
  const twoCalls = foldAdvice([
    { kind: 'tool', tool: 'mcp__ppt__advise', seq: 0, callId: 'cA',
      args: { items: [{ message: '가' }, { message: '나' }] } },
    { kind: 'tool', tool: 'mcp__ppt__advise', seq: 0, callId: 'cB',
      args: { items: [{ message: '다' }] } },
  ]);
  ok('포스트잇 신원에 호출 신원이 든다',
    twoCalls.items.map((a) => a.id).join(',') === 'cA#0,cA#1,cB#0',
    twoCalls.items.map((a) => a.id).join(','));
  ok('자리 없는 호출 둘이 같은 신원을 안 쓴다',
    new Set(twoCalls.items.map((a) => a.id)).size === 3);

  // **안 붙은 것들의 사유를 적는 한 줄.** 화면 안에 있어서 여태 한 번도 안 돌았는데, 이 줄은
  // 「설정 한 줄이 기능을 껐다」와 「모델이 말을 빼고 불렀다」를 사람에게 알리는 **유일한**
  // 통로다. 접는 함수가 낸 것을 그대로 먹인다 — 손으로 지어 넣으면 두 함수가 칸 이름을 두고
  // 갈라져도 아무도 안 운다(이제 갈라진 파일 둘에 산다).
  ok('말썽이 없으면 할 말이 없다', adviceNote(foldAdvice([])) === '',
    JSON.stringify(adviceNote(foldAdvice([]))));
  ok('붙은 안내만 있으면 쪽지가 안 선다', adviceNote(foldAdvice([
    { kind: 'tool', tool: 'mcp__ppt__advise', callId: 'cN', args: { message: '괜찮다' } },
  ])) === '');
  // 남의 서버 이름은 **전부** 나와야 한다. 하나만 적으면 사용자는 설정을 한 군데만 고치고
  // 나머지는 여전히 조용히 꺼진 채로 남는다.
  const twoStrays = adviceNote(foldAdvice([
    { kind: 'tool', tool: 'mcp__powerpoint__advise', callId: 'x1', args: { message: '가' } },
    { kind: 'tool', tool: 'mcp__deck__advise', callId: 'x2', args: { message: '나' } },
  ]));
  ok('남의 서버 이름이 다 적힌다',
    twoStrays.includes('mcp__powerpoint__advise') && twoStrays.includes('mcp__deck__advise'),
    twoStrays);
  ok('못 붙인 수가 적힌다',
    adviceNote({ dropped: 2 }) === '안내 2건은 무엇을 말하는지 안 실려 못 붙였습니다.',
    adviceNote({ dropped: 2 }));
  // 둘이 겹치면 **둘 다 남는다.** 뒤엣것이 앞엣것을 덮으면 남의 서버 이름이 화면에서 사라지고,
  // 사용자는 못 붙은 것을 전부 「말이 안 실려서」로 읽는다 — 고칠 데가 아닌 곳을 본다.
  const both = adviceNote(foldAdvice([
    { kind: 'tool', tool: 'mcp__deck__advise', callId: 'y1', args: { message: '가' } },
    { kind: 'tool', tool: 'mcp__ppt__advise', callId: 'y2', args: { slideId: 's1' } },
  ]));
  ok('사유 둘이 겹치면 둘 다 남는다',
    both.includes('mcp__deck__advise') && both.includes('1건') && both.includes(' · '), both);
}

{
  // 접는 종류. 델타로 쌓는 줄만 접고, **messageId 가 있다는 것만으로는 안 접는다** —
  // 같은 메시지에서 도구를 두 번 부르면 두 줄이다.
  const t = new Transcript();
  const call = (name) => ({ type: 'part.appended', seq: 1, data: { messageId: 'm1',
    part: { kind: 'tool-call', toolCall: { name, callId: `c-${name}`, args: {} } } } });
  t.append(call('mcp__ppt__advise'));
  t.append(call('mcp__ppt__set_text'));
  ok('같은 메시지의 도구 호출 둘은 두 줄이다', t.rows.length === 2, `${t.rows.length}줄`);
  ok('둘째 줄이 둘째 도구다', t.rows[1].tool === 'mcp__ppt__set_text', String(t.rows[1].tool));
  // 반대쪽: 델타는 같은 messageId 로 접힌다.
  const t2 = new Transcript();
  t2.append({ type: 'part.delta', seq: 0, data: { messageId: 'm2',
    part: { kind: 'text', text: '가' } } });
  t2.append({ type: 'part.delta', seq: 0, data: { messageId: 'm2',
    part: { kind: 'text', text: '나' } } });
  ok('델타는 한 줄로 접힌다', t2.rows.length === 1 && t2.rows[0].text === '가나',
    `${t2.rows.length}줄 "${t2.rows[0]?.text}"`);

  // callId 는 안내 포스트잇의 신원(`${callId}#${i}`)이 된다. 문자열이 아닌 것을 그대로 실으면
  // 그 자리에서 신원이 객체가 된다.
  const t3 = new Transcript();
  t3.append({ type: 'part.appended', seq: 1, data: { messageId: 'm3',
    part: { kind: 'tool-call', toolCall: { name: 'mcp__ppt__advise', callId: 42, args: {} } } } });
  ok('문자열이 아닌 callId 는 없는 것이다', t3.rows[0].callId === null,
    String(t3.rows[0].callId));
  // **거르는 쪽만 물면 안 싣는 것과 구분이 안 된다.** 위 한 줄은 `callId` 를 아예 안 옮겨
  // 실어도 초록이다(둘 다 `null`). 실제로 안 실은 채로 스위트가 통과했다(필드 드롭 계측).
  // 옮겨 실은 값을 무는 줄이 있어야 그 자리가 있다는 것이 확인된다.
  const t4 = new Transcript();
  t4.append({ type: 'part.appended', seq: 0, data: { messageId: 'm4',
    part: { kind: 'tool-call',
      toolCall: { name: 'mcp__ppt__advise', callId: 'c-42', args: {} } } } });
  ok('문자열 callId 는 줄에 그대로 실린다', t4.rows[0].callId === 'c-42',
    String(t4.rows[0].callId));
  // 자리 없는 이벤트라는 사실도 같이 못박는다 — 위 포스트잇 신원 블록이 기대는 전제다.
  ok('버스 전용 이벤트에는 자리가 없다', t4.rows[0].positioned === false,
    String(t4.rows[0].seq));
}

// ── 늦게 죽은 계측이 남의 세대를 안 건드린다 ──────────────────────────────────
//
// `sampleBeforeFocus` 의 catch 에 있는 `mine === this.epoch` 가 이 두 줄이다. 앞은 「실패한
// 읽기는 낡은 값을 지운다」(안 지우면 「놓쳤습니다」가 거짓으로 뜬다), 뒤는 「지우되 **내 세대의
// 값만**」이다.
{
  const two = { slideId: 's1', slideNo: 1, shapes: [{ id: 'a' }, { id: 'b' }] };
  let mode = 'ok';
  let release;
  const gate = new Promise((r) => { release = r; });
  const deck = {
    async selection() {
      if (mode === 'slow') { await gate; throw new Error('늦게 죽었다'); }
      if (mode === 'fail') throw new Error('못 읽는다');
      return mode === 'empty' ? { slideId: 's1', slideNo: 1, shapes: [] } : two;
    },
  };
  const qs = new QuoteSelection(deck, new Composer());

  await qs.sampleBeforeFocus();
  ok('읽었으면 몇 개였는지 든다', qs.beforeFocus?.count === 2, JSON.stringify(qs.beforeFocus));
  mode = 'fail';
  await qs.sampleBeforeFocus();
  ok('실패한 읽기는 낡은 값을 지운다', qs.beforeFocus === null, JSON.stringify(qs.beforeFocus));
  mode = 'empty';
  const r = await qs.run();
  ok('앞을 모르면 사유가 「모른다」다', r.reason === 'unknown', String(r.reason));

  // 이제 늦게 죽는 읽기. 그 사이에 눌렸고, 눌린 뒤의 읽기가 새 값을 앉혔다.
  mode = 'slow';
  const late = qs.sampleBeforeFocus();
  mode = 'ok';
  await qs.run();                    // 세대가 하나 오른다
  await qs.sampleBeforeFocus();      // 새 세대의 값
  ok('새 세대의 값이 앉았다', qs.beforeFocus?.count === 2, JSON.stringify(qs.beforeFocus));
  release();
  await late;
  ok('늦게 죽은 앞 세대는 남의 값을 안 지운다', qs.beforeFocus?.count === 2,
    JSON.stringify(qs.beforeFocus));
}

// ── 못 닿는다는 말이 한 번뿐인 이유는 불변식이다 ──────────────────────────────
//
// 위 머리글이 이름 댄 `wasReachable || !this.saidLost` 의 뒤 절은 판단을 안 한다. 그게 참인
// 근거가 **「닿으면 `saidLost` 가 거짓으로 돌아간다」** 하나뿐이라, 그 한 줄을 여기서 붙든다.
// 이 시험이 죽는 날은 뒤 절이 살아나는 날이고, 그때 위 머리글의 설명이 틀린 설명이 된다.
{
  const st = new FakeStatus();
  const w = new WatchPrompt(st, {});
  st.reachable = false;
  await w.poll();
  ok('못 닿으면 그렇게 말한다', w.saidLost === true);
  st.reachable = true;
  await w.poll();
  ok('닿으면 말한 것을 잊는다', w.saidLost === false && w.reachable === true);
  ok('닿는 동안 「말했다」가 남아 있지 않다', !(w.reachable && w.saidLost));
}

// ── 계측기가 틀리면 초록이 거짓말이 된다 ──────────────────────────────────────
//
// 위 블록과 같은 계측을 **어댑터에도** 돌렸다(2026-08-29). 26 을 뒤집어 10 이 살아남았는데,
// 그중 다섯은 `OfficeDeck` 의 `selection()` 안이라 이 머신에서 애초에 안 돈다(이 파일 머리에
// 적어 둔 그대로다 — 안 돌려 본 것을 "된다"고 세지 않는다). 남은 다섯은 사정이 다르다:
// **가짜들이 흉내 내는 계약 자체가 아무 데도 안 물려 있었다.**
//
// 이게 위험한 이유는 가짜가 유스케이스의 시험에 쓰이기 때문이다. 가짜가 틀리면 그 위의 시험은
// **틀린 이유로 초록**이 된다 — 이 파일이 §5.7 의 셋째 종을 찾아 나선 그 모양인데, 하필 그것을
// 찾는 도구 쪽에서 나왔다. 그래서 여기서는 유스케이스가 아니라 **가짜의 계약**을 문다.
{
  // 하나 — 범위 **안**의 since 는 거절이 아니다. `since > 0 && since > latest` 의 `&&` 를
  // `||` 로 바꿔도 아무 시험이 안 죽었는데, 그동안 이어 붙는 시험이 전부 `since` 0(처음부터)
  // 이었기 때문이다. 실제 이어 붙기는 **로그 한복판**에서 일어나고, 거기서 거절이 나오면
  // 화면은 이미 본 것을 통째로 다시 그린다.
  const log = [
    { seq: 1, sessionId: 's', type: 'user.prompt' },
    { seq: 2, sessionId: 's', type: 'assistant.part' },
    { seq: 3, sessionId: 's', type: 'assistant.part' },
  ];
  const t = new FakeTranscript({ s: log });
  const got = []; let restart = null;
  t.subscribe('s', 2, { onEvent: (e) => got.push(e.seq), onRestart: (m) => { restart = m; },
    onEnd: () => {} });
  ok('로그 안에 떨어지는 since 는 거절하지 않는다', restart === null, restart);
  // 둘 — `ev.seq <= from` 의 `<=` 를 `<` 로 바꾸면 **커서가 가리키던 그 줄을 다시 보낸다.**
  // 화면에서는 한 줄이 두 번 서는 것으로 보이고, 그건 이 문서가 §5.7 에서 이름 댄 결함이다.
  ok('이어 붙으면 커서 다음부터 온다', JSON.stringify(got) === '[3]', got.join(','));

  const t2 = new FakeTranscript({ s: log });
  let restart2 = null; const got2 = [];
  t2.subscribe('s', 9, { onEvent: (e) => got2.push(e.seq), onRestart: (m) => { restart2 = m; },
    onEnd: () => {} });
  ok('끝을 넘은 since 는 사유를 먼저 내고 처음부터 보낸다',
    typeof restart2 === 'string' && JSON.stringify(got2) === '[1,2,3]',
    `${restart2} / ${got2.join(',')}`);
}
{
  // 셋 — 자리를 안 실은 push 는 **다음 번호**를 받는다. `e.seq > max` 를 재는 줄의 타입 검사를
  // 뒤집으면 max 가 영영 0 이라 둘째 push 도 1 을 받는데, 그러면 두 이벤트가 같은 자리에 앉는다.
  // 커서가 자리로 도는 이상(§5.7) 그건 하나를 영영 못 보는 것과 같다.
  const t = new FakeTranscript({ s: [{ seq: 4, sessionId: 's', type: 'user.prompt' }] });
  t.subscribe('s', 0, { onEvent: () => {}, onRestart: () => {}, onEnd: () => {} });
  const a = t.push({ sessionId: 's', type: 'assistant.part' });
  const b = t.push({ sessionId: 's', type: 'assistant.part' });
  ok('자리 없는 push 는 로그 끝 다음을 받는다', a.seq === 5 && b.seq === 6, `${a.seq},${b.seq}`);
  const z = t.push({ sessionId: 's', type: 'assistant.part.delta', seq: 0 });
  ok('0 을 실어 보내면 0 그대로 둔다', z.seq === 0, z.seq);
}
{
  // 넷 — 슬라이드를 옮기면 선택이 풀린다. 가짜 덱의 `goTo` 가 같은 슬라이드에서 일찍 돌아가는
  // 줄인데, 그 조건을 뒤집으면 **옮겨도 아무 일이 안 일어난다.** 인용은 「지금 슬라이드의 선택」
  // 위에 서 있으므로, 이게 틀리면 옮긴 뒤에도 옛 도형이 골라진 채로 인용된다.
  const d = new FakeDeck(fixture);
  const [s1, s2] = fixture.slides;
  d.goTo(s1.id);
  d.click(s1.shapes[0].id, false);
  ok('고른 것이 하나 있다', d.selected.size === 1, d.selected.size);
  d.goTo(s2.id);
  ok('슬라이드를 옮기면 선택이 풀린다', d.currentSlide === s2.id && d.selected.size === 0,
    `${d.currentSlide} / ${d.selected.size}`);
  let rang = 0;
  const off = d.onChange(() => { rang += 1; });
  d.goTo(s2.id);
  ok('같은 슬라이드로 옮기면 종을 안 친다', rang === 0, rang);
  // **안 친다만 재고 친다를 안 쟀다.** 위 한 줄만 있으면 `onChange` 가 등록을 통째로 버려도
  // 초록이다(인자를 떨어뜨려 봤다 — 살아남았다). 미니 캔버스는 이 종 하나로 다시 그리므로,
  // 안 울리면 화면은 클릭해도 그대로다 — 아무도 안 우는 자리다.
  d.goTo(s1.id);
  ok('슬라이드를 옮기면 종을 친다', rang === 1, rang);
  d.click(s1.shapes[0].id, false);
  ok('도형을 고르면 종을 친다', rang === 2, rang);
  // 뗀 뒤에도 울리면 화면 하나를 닫아도 그 화면이 계속 다시 그린다.
  off();
  d.goTo(s2.id);
  ok('떼면 종이 멎는다', rang === 2, rang);
}
{
  // 다섯 — `pickDeck()` 을 **인자 없이** 부르는 길. 시험은 늘 `office` 를 손으로 넣었고,
  // 그래서 기본값 줄(`typeof Office === 'undefined' ? null : Office`)은 한 번도 안 돌았다.
  // 그 줄이 곧 **제품이 도는 길**이다 — `main.js` 는 인자 없이 부른다. 뒤집으면 Office 가
  // 없는 판에서 `ReferenceError` 가 나고, 그건 창이 아무것도 안 그리는 것으로 보인다.
  const r = await pickDeck();
  ok('Office 없는 판에서 인자 없이 불러도 가짜 덱이 선다',
    r.why === 'no-office' && r.deck && r.late === null, `${r.why} / ${!!r.deck}`);
}

// ── 안 돌아 본 함수의 가지치기만 문다 ────────────────────────────────────────
//
// ⚠ **이 블록은 S13·S14 를 안 닫는다.** 여기서 세우는 `PowerPoint` 는 이 파일이 문서를 읽고
// 적은 흉내지 호스트가 아니다. 흉내가 틀리면 이 시험은 **틀린 것에 대고 초록**이 된다 — 그래서
// 무는 것을 하나로 좁힌다: **우리가 고른 가지**. 「1.8 이 없으면 index 를 아예 안 load 한다」는
// §3.3 의 논증이고, 그 논증이 코드에 그대로 있는지는 호스트 없이도 잰다. 호스트가 실제로 어떻게
// 답하는지는 여전히 안 재 봤고, 이 파일 머리의 그 문장은 그대로 둔다.
//
// 계측이 이 자리를 가리켰다(2026-08-29): `selection()` 안에서 연산자를 뒤집어도 아무것도 안
// 죽는 줄이 다섯이었다. 그중 셋은 `#supports` 라, 뒤집히면 **1.8 을 지원하는 호스트에서도
// 번호를 안 읽는 조용한 퇴화**가 된다(화면은 id 로 적고 아무도 안 운다).
{
  const stub = ({ slide, shapes, textThrows = false, supports = () => true }) => {
    const seen = { slides: null, shapes: null, syncs: 0, asked: [], calls: [] };
    globalThis.Office = { context: { requirements: {
      isSetSupported: (n, v) => { seen.asked.push(`${n} ${v}`); return supports(n, v); },
    } } };
    globalThis.PowerPoint = {
      run: async (cb) => cb({
        presentation: {
          getSelectedSlides: () => ({ items: slide ? [slide] : [],
            load(q) { seen.slides = q; } }),
          getSelectedShapes: () => ({ items: shapes, load(q) { seen.shapes = q; } }),
          setSelectedSlides: (ids) => { seen.calls.push(`고른 슬라이드 ${ids.join('|')}`); },
          slides: {
            getItem: (id) => {
              seen.calls.push(`getItem ${id}`);
              return { setSelectedShapes: (ids) => {
                seen.calls.push(`고른 도형 [${ids.join('|')}]`);
              } };
            },
          },
        },
        sync: async () => {
          seen.syncs += 1;
          seen.calls.push('왕복');
          if (seen.syncs > 1 && textThrows) throw new Error('textFrame 없는 도형이 섞였다');
        },
      }),
    };
    return seen;
  };
  const shape = (id, text) => ({
    id, name: `이름 ${id}`, type: 'GeometricShape', width: 72, height: 36,
    textFrame: { textRange: { load() {}, text } },
  });
  const clear = () => { delete globalThis.Office; delete globalThis.PowerPoint; };

  try {
    // 하나 — 1.8 이 있으면 번호를 묻고, **0-based 를 1-based 로** 바꿔 올린다.
    const src0 = shape('sh1', '가나');
    let seen = stub({ slide: { id: 'sl1', index: 4 }, shapes: [src0] });
    let r = await new OfficeDeck().selection();
    ok('1.8 이면 index 까지 load 한다', seen.slides === 'items/id,items/index', seen.slides);
    // **무엇을 물었는지까지 문다.** 여태 이 블록의 stub 은 전부 상수 함수(`() => true`,
    // `() => false`)라 `#supports` 에 간 인자를 아무도 안 봤다 — 1.8 자리에 1.2 를 적어도
    // 스위트가 초록이었다(인자 드롭 계측). 그런데 여기서 재려는 것이 정확히 **§3.3 이 고른
    // 바닥**이고, 다른 집합을 물으면 1.2 짜리 호스트에서 index 를 load 해 선택을 통째로 잃는다.
    ok('selection 이 묻는 집합은 PowerPointApi 1.8 하나다',
      seen.asked.join(',') === 'PowerPointApi 1.8', seen.asked.join(','));
    ok('번호는 0-based 를 +1 해서 올린다', r.slideNo === 5, r.slideNo);
    ok('신원과 글이 같이 온다',
      r.slideId === 'sl1' && r.shapes[0].id === 'sh1' && r.shapes[0].text === '가나',
      JSON.stringify(r.shapes[0]));
    ok('글을 읽었으면 못 읽었다고 안 적는다', r.shapes[0].textUnavailable === false);
    // **옮겨 싣는 칸을 전수로 문다.** 바로 위 두 줄은 `id` 와 `text` 만 봤는데 이 map 은
    // 여섯 칸을 옮기고, `name`·`type`·`width`·`height` 는 **넷 다 통째로 안 실어도 스위트가
    // 초록**이었다(필드 드롭 계측). 여기가 이 제품에서 제일 조용히 비는 자리다 — 넷은 그대로
    // `Quote` 의 몸이 되어 이름과 종류는 **모델에게 가는 말**에 들고(`Quote.toPrompt`),
    // 치수는 인용 카드에 뜬다. 비어도 화면은 그럴듯하게 그려지고 모델만 덜 받는다.
    const kept = ['id', 'name', 'type', 'width', 'height']
      .filter((k) => r.shapes[0][k] === src0[k]);
    ok('호스트가 준 도형 칸이 그대로 실린다', kept.length === 5, kept.join(','));
    // 그리고 **묻는 것과 싣는 것이 같아야 한다.** load 문자열에서 이름 하나가 빠지면 위
    // 단언은 `undefined === undefined` 로 초록이 될 수 있는데, 그건 호스트가 안 준 것을
    // 안 실었다는 말일 뿐이라 계약이 아니다.
    ok('묻는 속성이 옮겨 싣는 칸과 같다',
      seen.shapes === 'items/id,items/name,items/type,items/width,items/height', seen.shapes);

    // 둘 — 1.8 이 없으면 **묻지도 않는다.** §3.3: 바닥 아래 호스트에서 이 속성을 load 하면
    // sync 가 통째로 실패해 **선택까지 잃는다.** 흉내는 안 터지므로, 여기서 무는 것은
    // 「안 터졌다」가 아니라 **load 문자열에 index 가 없다**는 우리 쪽 가지다.
    seen = stub({ slide: { id: 'sl1', index: 4 }, shapes: [shape('sh1', '가')],
      supports: () => false });
    r = await new OfficeDeck().selection();
    ok('1.8 이 없으면 index 를 안 묻는다', seen.slides === 'items/id', seen.slides);
    ok('안 물었으면 번호를 지어내지 않는다', r.slideNo === null, r.slideNo);

    // 셋 — 「지원한다」는 **`true` 여야 한다.** 옛 호스트가 진리값 비슷한 것을 돌려주는 판을
    // 대비해 `=== true` 로 좁혀 뒀는데, 그 좁힘이 시험에 안 물려 있었다.
    seen = stub({ slide: { id: 'sl1', index: 4 }, shapes: [shape('sh1', '가')],
      supports: () => 'yes' });
    r = await new OfficeDeck().selection();
    ok('true 아닌 답은 지원으로 안 읽는다', seen.slides === 'items/id' && r.slideNo === null,
      `${seen.slides} / ${r.slideNo}`);

    // 셋의 반 — 다른 집합을 다 지원해도 **1.8 만 아니면 안 묻는다.** 위 한 줄이 이름을
    // 못박고, 이 한 줄이 그 이름에 매달린 가지를 못박는다.
    seen = stub({ slide: { id: 'sl1', index: 4 }, shapes: [shape('sh1', '가')],
      supports: (n, v) => !(n === 'PowerPointApi' && v === '1.8') });
    r = await new OfficeDeck().selection();
    ok('1.8 말고 다 된다고 해도 번호는 안 읽는다', seen.slides === 'items/id' && r.slideNo === null,
      `${seen.slides} / ${r.slideNo}`);

    // 넷 — 고른 도형이 없으면 **둘째 왕복을 안 돈다.** 빈 선택에 텍스트를 물으러 가는 것은
    // 사람이 아무것도 안 골랐는데 덱을 한 번 더 건드리는 일이다.
    seen = stub({ slide: { id: 'sl1', index: 0 }, shapes: [] });
    r = await new OfficeDeck().selection();
    ok('빈 선택은 왕복 한 번에 끝난다', seen.syncs === 1 && r.shapes.length === 0, seen.syncs);
    ok('빈 선택에도 슬라이드 신원은 온다', r.slideId === 'sl1' && r.slideNo === 1,
      `${r.slideId} / ${r.slideNo}`);

    // 다섯 — 글 읽기가 통째로 실패해도 **신원은 산다.** 그리고 못 읽었다고 적는다 — 빈
    // 문자열로만 두면 「글이 없는 도형」과 값이 같아진다(`Quote.textUnavailable`).
    seen = stub({ slide: { id: 'sl1', index: 0 },
      shapes: [shape('sh1', '가'), shape('sh2', '나')], textThrows: true });
    r = await new OfficeDeck().selection();
    ok('글을 잃어도 신원 둘은 그대로 온다',
      r.shapes.map((x) => x.id).join(',') === 'sh1,sh2', r.shapes.length);
    ok('못 읽은 것은 못 읽었다고 적는다',
      everyOf(r.shapes, (x) => x.textUnavailable === true && x.text === ''),
      JSON.stringify(r.shapes.map((x) => [x.text, x.textUnavailable])));
    // 여섯 — **`point()` 를 처음으로 돌린다.** 여태 이 함수는 스위트가 한 번도 안 불렀고,
    // 그래서 두 인자를 통째로 떨어뜨려도 아무도 안 울었다(인자 드롭 계측). 여기서 무는 것은
    // 호스트의 대답이 아니라 **함수가 스스로 적어 둔 차례**다: 슬라이드를 먼저 고르고, 왕복
    // 한 번, 그 다음 도형. 흉내가 틀려도 이 차례는 우리 쪽 코드에만 달려 있다.
    //
    // ⚠ **S13·S14 는 그대로 열려 있다.** 안 보고 있는 슬라이드에서 진짜 PowerPoint 가 이
    // 차례를 받아 주는지는 여기서 못 잰다 — 그건 창이 있어야 한다.
    seen = stub({ slide: { id: 'sl1', index: 0 }, shapes: [] });
    await new OfficeDeck().point('sl9', ['shA', 'shB']);
    ok('짚을 때 슬라이드를 먼저 고르고 왕복한 뒤 도형을 잡는다',
      seen.calls.join(' → ')
        === '고른 슬라이드 sl9 → 왕복 → getItem sl9 → 고른 도형 [shA|shB] → 왕복',
      seen.calls.join(' → '));

    // 일곱 — **빈 목록도 끝까지 간다.** 조기 이탈로 고치면 잡은 것이 안 풀려서, 캔버스가
    // 「이 안내는 저 도형에 대한 것」이라는 거짓을 말한다(`point()` 의 주석).
    seen = stub({ slide: { id: 'sl1', index: 0 }, shapes: [] });
    await new OfficeDeck().point('sl9', []);
    ok('빈 목록이면 잡은 것을 놓는다', seen.calls.includes('고른 도형 []'),
      seen.calls.join(' → '));
  } finally {
    clear();
  }
}

// ── 화면이 정하는 것(`screen.js`) ──────────────────────────────────────────────
//
// 왜 이 블록이 생겼나. `view.js` 에 돌연변이 32개를 심었더니 **30개가 살아남았다.** 시험이
// 뷰에서 부를 수 있는 것이 `headOf` 하나뿐이라, 나머지 결정 — 보내는 키가 무엇인가, 쪽지가
// 언제 사라지는가, 안 눌리는 안내에 무엇을 적는가 — 은 아무도 안 보고 있었다. 결정을
// `screen.js` 로 옮겼으니 여기서 **하나씩 잰다.** 옮기기만 하고 안 재면 살아남은 30개는
// 그대로 살아남는다: 답만 맞고 근거는 여전히 없는 것이 된다.
//
// 규칙 하나 — 값을 되뇌지 않는다. 「이 함수가 'x' 를 돌려준다」는 함수를 두 번 적은 것이고,
// 함수가 틀리면 시험도 같이 틀린다. **가르는 것**을 잰다: 둘이 다른가, 어느 쪽이 어느 조건에서 오는가.
{
  // 보내는 키. Enter 혼자로 보내지면 여러 줄을 적을 수 없다 — 줄바꿈이 발송이 된다.
  ok('Cmd/Ctrl 을 짚어야 보낸다',
    isSendKey({ key: 'Enter', metaKey: true }) && isSendKey({ key: 'Enter', ctrlKey: true })
      && !isSendKey({ key: 'Enter' }),
    `plain=${isSendKey({ key: 'Enter' })}`);
  ok('Enter 가 아니면 짚어도 안 보낸다', !isSendKey({ key: 'k', metaKey: true }));

  // 판을 다시 세우면 사람이 적던 답과 포커스가 지워진다. 서명이 같은 동안은 안 세운다.
  ok('서명이 같으면 판을 안 다시 세운다',
    askAction('a', 'a') === 'refresh' && askAction('a', 'b') === 'rebuild'
      && askAction('a', null) === 'rebuild',
    `same=${askAction('a', 'a')} diff=${askAction('a', 'b')} first=${askAction('a', null)}`);

  // 넷은 서로 다른 화면이다. 특히 known/unknown 이 뭉치면 **모르는 종류에 단추가 달린다**(§5.7).
  const kinds = [
    askKind({ reachable: false }),
    askKind({ reachable: true, pending: null }),
    askKind({ reachable: true, pending: { known: true } }),
    askKind({ reachable: true, pending: { known: false } }),
  ];
  ok('물음 판 넷이 안 뭉친다', new Set(kinds).size === 4, kinds.join(' · '));
  ok('못 닿는 것이 물음 없음보다 앞선다', kinds[0] === 'lost' && kinds[1] === 'last');

  // 권한인지 아닌지가 **머리에서** 갈려야 사람이 무게를 안다.
  ok('권한 물음은 머리부터 다르다',
    askHead({ isPermission: true }) !== askHead({ isPermission: false })
      && askHead({ isPermission: true }).includes('권한'),
    askHead({ isPermission: true }));

  // 안 실린 것을 빈 칸으로 두면 「다 읽었다」로 보인다.
  ok('무엇인지 안 실렸으면 그렇다고 적는다',
    whatText({ what: '파일을 지운다' }) === '파일을 지운다'
      && whatText({ what: '' }).includes('안 실렸'),
    whatText({ what: '' }));

  // 글로 온 인자는 그대로, 값으로 온 것은 펴서. 편 것은 여러 줄이라 눈으로 갈린다.
  // `typeof` 를 먼저 본다 — 값 인자가 안 펴진 채 나오면 `.includes` 에서 **터져서** 뒤
  // 단언 수십 개가 안 돈다. 터진 것도 빨갛긴 하지만, 한 자리가 망가진 것과 여러 자리가
  // 망가진 것이 같은 화면이 된다.
  const argsObj = argsText({ args: { path: '/tmp', force: true } });
  ok('글 인자는 안 펴고 값 인자는 편다',
    argsText({ args: 'rm -rf /' }) === 'rm -rf /'
      && typeof argsObj === 'string' && argsObj.includes('\n'),
    typeof argsObj === 'string' ? argsObj.replace(/\n/g, '⏎') : `글이 아님: ${typeof argsObj}`);

  // 자리는 늘 서 있고 말만 없다 — 자리를 없애면 갈아 끼울 데가 없어 판을 다시 세우게 된다.
  ok('다음 물음 안내는 있을 때만 말한다',
    placeLine('뒤에 2개') .hidden === false && placeLine('뒤에 2개').text.includes('뒤에 2개')
      && placeLine(null).hidden === true && placeLine(null).text === '',
    placeLine('뒤에 2개').text);

  // 못 닿는 동안엔 현재형으로 안 적는다 — 근거가 방금 읽은 status 뿐이다.
  ok('못 닿으면 하던 일을 과거형으로 적는다',
    doingLine('빌드 중', true).text === '빌드 중'
      && doingLine('빌드 중', false).text !== '빌드 중'
      && doingLine('빌드 중', false).text.includes('빌드 중'),
    doingLine('빌드 중', false).text);
  ok('하던 일이 없으면 칸이 안 선다',
    doingLine('', true).hidden === true && doingLine('빌드 중', true).hidden === false);

  // 모르는 사유는 조용히 숨는 대신 제 말을 갖고 온다. `show:false` 는 **할 말이 없다**는 뜻뿐.
  ok('직전 물음 줄은 할 말이 없을 때만 안 선다',
    lastAskShape(null).show === false
      && lastAskShape(CLEARED.answered).show === true
      && lastAskShape('무슨-사유인지-모름').show === true,
    lastAskShape('무슨-사유인지-모름').text);

  // 폭이 넓은 결정은 넓게 생겨야 한다 — 문구만으로는 안 읽고 누른다(§5.7).
  const widths = new Set(DECISIONS.map(decisionClass));
  ok('폭이 다른 결정은 단추 모양이 다르다', widths.size === 2, [...widths].join(' | '));
  ok('한 번만 여는 것이 좁은 쪽이다',
    decisionClass({ width: 'call' }) === 'ghost'
      && decisionClass({ width: 'session' }).includes('wide'));

  // 「화면이 지금 거짓말을 하고 있다」는 말은 4초 뒤 없어지면 안 된다.
  const fn = failNote('못 보냈습니다', new Error('문이 닫혔다'));
  ok('터진 사유는 안 사라진다', fn.sticky === true);
  ok('터진 사유는 무엇이 터졌는지와 왜를 같이 적는다',
    fn.text.includes('못 보냈습니다') && fn.text.includes('문이 닫혔다'), fn.text);
  ok('메시지 없는 것도 뭔가는 적는다',
    failNote('x', 'just a string').text.includes('just a string')
      && failNote('x', null).text.includes('null'),
    failNote('x', null).text);

  ok('sticky 쪽지만 수명이 없다',
    noteLife() === 4000 && noteLife({}) === 4000 && noteLife({ sticky: false }) === 4000
      && noteLife({ sticky: true }) === null,
    `기본=${noteLife()} sticky=${noteLife({ sticky: true })}`);

  // 안 잰 것을 잰 것처럼 안 적는다 — 어댑터가 아예 안 답하는 경우까지 값으로 만든다.
  // 없는 문을 부르려 들면 던진다. 그것도 **한 줄로** 운다 — 위와 같은 이유다.
  let mute, muteErr = '';
  try { mute = capsOf({}); } catch (e) { mute = {}; muteErr = `물어보다 터졌다: ${e.message}`; }
  ok('안 답하는 어댑터도 사유를 갖고 온다',
    !muteErr && mute.measured === false && mute.note.length > 0 && Array.isArray(mute.sets),
    muteErr || mute.note);
  const said = { measured: true, note: '', sets: [] };
  ok('답한 것은 그대로 쓴다', capsOf({ capabilities: () => said }) === said);
  ok('안 잰 것은 안 잰 것으로 적는다',
    capsText({ measured: false, note: '가짜 덱' }).includes('가짜 덱')
      && capsText({ measured: false, note: '' }).includes('사유를 안 실었다'),
    capsText({ measured: false, note: '' }));
  // ok 가 null 인 것은 "아니오"가 아니라 **물어보다 던졌다**이므로 셋이 갈려야 한다.
  const capLine = capsText({ measured: true, sets: [
    { name: 'A', version: '1.1', ok: true },
    { name: 'B', version: '1.2', ok: false },
    { name: 'C', version: '1.3', ok: null },
  ] });
  // **있는지가 아니라 어느 것이 어느 것인지**를 잰다. 셋이 다 떠 있기만 하면 ✓ 와 ✗ 가
  // 서로 자리를 바꿔도 잠잠하다 — 실제로 `s.ok === false` 를 뒤집은 변이가 그 틈으로
  // 살아남았고, 그때 화면은 「지원한다」를 「못 물어봤다」로 적고 있었다.
  ok('지원·미지원·못 물어봄이 각자 제 표를 단다',
    capLine.includes('A 1.1 ✓') && capLine.includes('B 1.2 ✗') && capLine.includes('C 1.3 ?'),
    capLine);

  // 문은 깨끗한 끝을 에러로 안 준다 — 이 줄이 없으면 사람은 안 오는 답을 영원히 기다린다.
  ok('조용한 대화와 죽은 스트림이 갈린다',
    streamLine({ live: true }).hidden === true
      && streamLine({ live: false }).hidden === false
      && streamLine({ live: false }).text.includes('끊겼'),
    streamLine({ live: false }).text);
  ok('거절과 끊김은 둘 다 적는다',
    streamLine({ live: false, refusal: '커서가 낡았다' }).text.includes('커서가 낡았다')
      && streamLine({ live: false, refusal: '커서가 낡았다' }).text.includes('끊겼'),
    streamLine({ live: false, refusal: '커서가 낡았다' }).text);
  ok('못 읽은 것이 없으면 칸이 안 선다',
    unknownLine(null).hidden === true && unknownLine(null).text === ''
      && unknownLine('2줄을 못 읽었다').hidden === false);

  // 「글이 없다」와 「글을 못 읽었다」는 다른 문장이다 — 뭉치면 빈 상자를 고치러 간다.
  const q = (o) => ({ preview: () => '앞부분', ...o });
  ok('글 없음과 못 읽음이 안 뭉친다',
    quoteBody(q({ text: '', textUnavailable: true }))
      !== quoteBody(q({ text: '', textUnavailable: false })),
    quoteBody(q({ text: '', textUnavailable: true })));
  ok('글이 있으면 미리보기를 따옴표로 싣는다',
    quoteBody(q({ text: '길다' })).includes('앞부분'), quoteBody(q({ text: '길다' })));
  ok('빈 꼬리표는 구분자만 남기지 않는다',
    quoteMeta({ type: '표', sizeLabel: '' }) === '표'
      && quoteMeta({ type: '표', sizeLabel: '3×4' }).includes(' · '),
    quoteMeta({ type: '표', sizeLabel: '' }));

  // `class="turn turn"` 이 되면 `.turn.turn` 은 CSS 에서 그냥 `.turn` 이라, 그 한 줄에 준
  // 모양이 **모든 줄에** 걸린다. 실제로 사용자 말이 가운데 정렬됐었다.
  ok('종류는 접두사와 함께 적힌다',
    rowClass({ kind: 'turn' }) === 'turn kind-turn'
      && rowClass({ kind: 'turn' }).split(' ')[1] !== 'turn');

  // `⚙` 하나로는 무엇이 슬라이드를 고쳤는지 모른다. **이름은 사람 말로 적는다**(`toolLabel`) —
  // 기계 이름을 그대로 내면 한 턴에 수십 줄을 사람이 매번 번역해서 읽는다.
  ok('도구 줄은 이름까지 적는다',
    rowHead({ kind: 'tool', tool: 'set_text' }).includes('글 바꾸기')
      && rowHead({ kind: 'tool' }).includes('이름 없음'),
    rowHead({ kind: 'tool' }));
  ok('사람과 모델의 말에는 머리가 없다',
    !rowHead({ kind: 'user' }) && !rowHead({ kind: 'assistant' }));
  const shapes = ['tool', 'turn', 'user', 'think'].map((k) => rowShape({ kind: k }));
  ok('도구·끝난 턴·말이 다른 모양으로 그려진다',
    shapes[0] === 'tool' && shapes[1] === 'turn' && shapes[2] === 'text',
    shapes.join(' · '));
  // **혼잣말은 사람 말과 같은 모양이 아니다.** 사용자에게 한 말이 아닌데 답풍선과 같은 자리를
  // 통째로 먹고 있었다 — 도형 하나에 호출 하나인 이 제품에서 그 글은 길다.
  ok('혼잣말은 접히는 모양이다', shapes[3] === 'think', shapes.join(' · '));
  // 요약은 **한 줄이고 첫머리를 미리 보여 준다** — 웹 콘솔과 같은 규칙
  // (`ConversationElement`: `row.reasoning + " · " + oneLine(text, 80)`).
  {
    const long = { kind: 'think', text: '상자 폭 문제로\n  보인다  —\n' + 'x'.repeat(200) };
    const h = thinkHead(long);
    ok('혼잣말 요약이 한 줄이다', !h.includes('\n'), h.slice(0, 40));
    ok('요약이 첫머리를 보여 준다', h.startsWith('혼잣말 · 상자 폭 문제로 보인다'), h.slice(0, 40));
    ok('요약이 길어지지 않는다', h.length <= 90, String(h.length));
    ok('글이 없으면 미리보기도 없다', thinkHead({ kind: 'think', text: '   ' }) === '혼잣말');
    ok('여러 공백이 한 칸이 된다', oneLine('a \n\t b') === 'a b');
  }

  // 「set_text 를 불렀다」는 무엇이 바뀌었는지 안 알려 준다.
  ok('인자 없는 것과 있는 것이 갈린다',
    argsCell({ args: null }).includes('인자 없음')
      && argsCell({ args: { a: 1 } }).includes('"a"'),
    argsCell({ args: { a: 1 } }).replace(/\n/g, '⏎'));
  const long = argsCell({ args: { s: 'x'.repeat(2000) } });
  ok('긴 인자는 잘리고 잘린 표시가 남는다',
    long.length === 300 && long.endsWith('…'), `${long.length}자`);

  // 검증 못 한 착지를 보통 끝처럼 그리지 않는다(`TurnFinishedData`).
  ok('검증 안 된 끝은 보통 끝과 다르게 적힌다',
    endText({ unverified: true }) !== endText({ unverified: false })
      && endText({ unverified: true }).includes('검증'),
    endText({ unverified: true }));
  ok('사유가 있으면 같이 적고 없으면 안 짓는다',
    endText({ unverified: true, reason: '빌드 실패' }).includes('빌드 실패')
      && !endText({ unverified: true }).includes('—'),
    endText({ unverified: true, reason: '빌드 실패' }));
  ok('빈 말줄은 빈 채로 안 둔다',
    bodyText({ text: '' }).includes('글 없음') && bodyText({ text: '안녕' }) === '안녕');

  // 안내가 0개라도 **사유만 있으면 층이 선다** — 아니면 그 말이 갈 곳이 없다.
  ok('안내도 사유도 없을 때만 층이 안 선다',
    adviceBoard([], '').wrapHidden === true
      && adviceBoard([], '2개를 못 읽었다').wrapHidden === false
      && adviceBoard([{}], '').wrapHidden === false);
  ok('사유 줄은 사유가 있을 때만 선다',
    adviceBoard([{}], '').noteHidden === true
      && adviceBoard([{}], '2개를 못 읽었다').noteHidden === false
      && adviceBoard([{}], '2개를 못 읽었다').noteText === '2개를 못 읽었다');

  // 회색으로만 두면 "모델이 어딜 말 안 했다"와 "이 창이 고장났다"가 같은 화면이 된다.
  const pointable = { pointable: true, slideId: 'sl1', shapeIds: ['shA'], unpointableReason: '안 쓰임' };
  const blocked = { pointable: false, slideId: 'sl1', shapeIds: [], unpointableReason: '도형을 안 짚었다' };
  ok('안 눌리는 안내는 왜 안 눌리는지를 그 자리에 적는다',
    adviceTargetText(blocked, new Map(), true) === '도형을 안 짚었다');
  ok('눌리는 안내는 어디를 가리키는지 적는다',
    adviceTargetText(pointable, new Map([['sl1', 3]]), true)
      === targetLabel(pointable, new Map([['sl1', 3]]), true),
    adviceTargetText(pointable, new Map([['sl1', 3]]), true));

  // 못 펴는 것(순환 참조)도 **뭔가는 적는다** — 여기서 던지면 줄 하나가 화면 전체를 없앤다.
  const cyc = {}; cyc.self = cyc;
  ok('못 펴는 값도 뭔가는 적는다', typeof pretty(cyc) === 'string' && pretty(cyc).length > 0,
    pretty(cyc));
  ok('편 값은 여러 줄이 된다', pretty({ a: 1 }).includes('\n'));
  ok('자른 표시까지가 길이다',
    clip('12345', 10) === '12345' && clip('1234567890', 5).length === 5
      && clip('1234567890', 5).endsWith('…'),
    clip('1234567890', 5));
}


// ── 접힌 판이 접힌 채로 거짓말하지 않는가 ────────────────────────────────────
//
// 작업창은 PowerPoint 에서 348×391 이라(MS 애드인 디자인 지침의 크기 표) 세로가 귀하고, 요구
// 집합 여섯 줄은 뭔가 안 될 때만 읽는 값이라 접어 뒀다. 접는 순간 규칙이 하나 생긴다:
// **요약이 사실을 말해야 한다.** 「다 좋다」로 접어 두면 아무도 펴지 않고, 안 쟀다는 사실이
// 화면에서 사라진다.
{
  const all = { measured: true, sets: [{ ok: true }, { ok: true }] };
  const some = { measured: true, sets: [{ ok: true }, { ok: false }, { ok: null }] };
  ok('안 쟀으면 요약이 안 쟀다고 적는다',
    capsSummary({ measured: false, sets: [] }).includes('못 쟀'),
    capsSummary({ measured: false, sets: [] }));
  ok('다 되면 수를 적는다', capsSummary(all).includes('2개'), capsSummary(all));
  ok('빠진 것이 있으면 접힌 줄이 그것을 적는다',
    capsSummary(some).includes('1개 없음') && capsSummary(some).includes('1개 모름'),
    capsSummary(some));
}

// 브랜드 줄(MS 지침이 작업창 아래에 두라고 적은 자리)은 **늘 사실을 적는다.**
{
  ok('안 골랐으면 안 골랐다고 적는다',
    brandState({ companion: null, streamLive: false }) === '컴패니언 미선택');
  ok('붙었으면 어디에·대화가 살아 있는지·손이 몇인지',
    brandState({ companion: 'deck2', streamLive: true, hands: 2 }) === 'deck2 · 대화 연결됨 · 덱 2',
    brandState({ companion: 'deck2', streamLive: true, hands: 2 }));
  ok('대화가 끊기면 그렇게 적는다',
    brandState({ companion: 'deck2', streamLive: false }).includes('대화 끊김'));
}


// ── 붙기 전의 창은 「고장 났다」고 말하지 않는다 ──────────────────────────────
//
// 실물에서 본 화면이 근거다(2026-09-01): 컴패니언을 고르라는 카드 위에 「데몬에 안 닿습니다」와
// 「대화 스트림이 끊겼습니다」가 노란 배너 둘로 겹쳐 떴다. 둘 다 **붙어 있던 것에 대한 말**인데
// 아직 아무 데도 안 붙었으니 참이 아니고, 사람은 고르기도 전에 고장 난 줄 안다.
{
  const notBound = { bound: false, reachable: false, pending: null, live: false };
  ok('안 붙었으면 물음 칸이 아무것도 안 그린다', askKind(notBound) === 'none', askKind(notBound));
  ok('안 붙었으면 스트림 줄이 숨는다', streamLine(notBound).hidden === true);
  // 붙은 뒤에는 **같은 값이 말을 한다** — 조용해지는 것은 붙기 전뿐이다.
  const bound = { bound: true, reachable: false, pending: null, live: false };
  ok('붙은 뒤 못 닿으면 그때는 말한다', askKind(bound) === 'lost');
  ok('붙은 뒤 스트림이 죽으면 그때는 말한다',
    streamLine(bound).hidden === false && streamLine(bound).text.includes('끊겼'));
  // bound 를 안 실어 보내는 옛 호출자도 그대로 돈다 — 없으면 예전처럼 군다.
  ok('bound 를 안 실으면 예전 그대로', askKind({ reachable: false }) === 'lost');
}

// ── 대화 이름은 우리가 짓지 않는다 ────────────────────────────────────────────
//
// 실물에서 본 것이 근거다(2026-09-01): 모델이 `mcp__ppt__` 로 덱의 제목을 **실제로** 고쳤는데
// 작업창은 「보냈습니다 — 로그에 뜨기를 기다립니다」에 멈춘 채였고, 사람이 적은 글도 그대로
// 남아 있었다(메아리가 안 오니 `SendTurn.settle` 이 영영 안 푼다). 창이 지어낸 이름에 붙어
// 있었고, 진짜 이벤트는 `sessionId` 가 달라 **신원 그물에 전부 걸렸다.**
//
// 그 그물은 옳다 — 남의 대화를 조용히 섞지 않는 것이 규칙이다. 틀린 쪽은 **이름을 지어낸
// 호출자**였다. 그래서 여기서 무는 것도 둘이다: 그물이 실제로 거른다는 것과, **조립 자리가
// 이름을 안 짓는다**는 것. 뒤엣것은 앞엣것이 못 잡는다 — 유스케이스는 시키는 대로 했고,
// 틀린 것은 시킨 쪽이었다.
{
  const foreign = { seq: 1, sessionId: '남의-대화', type: 'prompt.submitted', data: { text: '남의 글' } };
  const mine = { seq: 1, sessionId: 's_real', type: 'prompt.submitted', data: { text: '내 글' } };
  const port = new FakeTranscript({ s_real: [] });
  const read = new ReadTranscript(port);
  read.attach('s_real');
  port.push(foreign);
  ok('남의 대화 이벤트는 안 섞인다', read.view.rows.length === 0,
    read.view.rows.map((r) => r.text).join('|'));
  port.push(mine);
  ok('이 대화 이벤트는 선다',
    read.view.rows.some((r) => r.text === '내 글'), `${read.view.rows.length}줄`);

  // **틀린 이름에 붙으면 진짜가 통째로 사라진다** — 위 화면을 그대로 재현한다.
  const port2 = new FakeTranscript({ 'sess-mock': [] });
  const read2 = new ReadTranscript(port2);
  read2.attach('sess-mock');
  port2.push(mine);
  ok('지어낸 이름에 붙으면 진짜 대화가 안 보인다', read2.view.rows.length === 0);
  // 그 화면에서 사람 글이 왜 안 지워지는지까지 같이 적는다 — 메아리를 셀 줄이 0 이다.
  ok('그 화면에서는 메아리를 셀 줄이 없다', logShapeOf(read2.view).userRows === 0);

  // 조립 자리의 규칙: **`attach` 에 지어낸 이름을 넘기는 줄은 가짜 갈래 안에만 있다.**
  const wiring = readFileSync(new URL('../src/main.js', import.meta.url), 'utf8')
    .split('\n')
    .map((l) => l.trim())
    .filter((l) => /readTranscript\.attach\(/.test(l) && !l.startsWith('//') && !l.startsWith('*'));
  ok('조립 자리가 대화에 붙는 줄이 있다', wiring.length > 0);
  ok('지어낸 이름은 가짜 갈래에서만 쓴다',
    everyOf(wiring, (l) => !/\bSESSION\b/.test(l) || /!real/.test(l)), wiring.join(' / '));
  // 진짜 갈래는 컴패니언이 든 이름에 붙는다 — `.sock.session` 이 그 이름의 출처다.
  const wiringSrc = readFileSync(new URL('../src/main.js', import.meta.url), 'utf8');
  ok('진짜 갈래는 컴패니언이 든 이름에 붙는다',
    /listenTo\(companion\.session\)/.test(wiringSrc)
      && /listenTo\(list\?\.bound\?\.session\)/.test(wiringSrc));
}


// ── 호출의 답·허락·판정을 그린다 ─────────────────────────────────────────────
//
// 실물에서 이 창은 자기 입으로 못 그린다고 적고 있었다(2026-09-01): 「이 창이 아직 그릴 줄
// 모르는 이벤트 27건 — context.usage, council.*, part.appended (tool-result),
// permission.decided」. 그중 셋은 이 제품에서 **대화보다 중요한 것**이다 — 도구가 슬라이드를
// 고쳤는지, 그걸 허락했는지, 게이트가 왜 턴을 안 놔주는지.
{
  const call = (callId, name, args) => ({
    seq: 1, sessionId: 'A', type: 'part.appended',
    data: { messageId: 'm1', part: { kind: 'tool-call', toolCall: { callId, name, args } } },
  });
  const result = (callId, content, extra = {}) => ({
    seq: 2, sessionId: 'A', type: 'part.appended',
    data: { messageId: 'm2', part: { kind: 'tool-result', toolResult: { callId, content, ...extra } } },
  });

  // 답은 **호출한 줄에 접힌다.** 따로 세우면 도구가 줄줄이 도는 턴에서 짝이 안 맞는다.
  {
    const t = new Transcript();
    t.append(call('c1', 'mcp__ppt__set_text', { slide: 1 }));
    t.append(result('c1', '슬라이드 1 · 도형 2 글 교체'));
    ok('답은 새 줄을 안 세우고 호출 줄에 붙는다', t.drawnRows.length === 1,
      `${t.drawnRows.length}줄`);
    ok('붙은 답이 그 호출의 것이다', t.drawnRows[0].result?.callId === 'c1');
    ok('그릴 줄 모른다고 안 적는다', t.unknownNote === null, String(t.unknownNote));
  }

  // **한 턴에 같은 도구가 여러 번 도는 것이 이 제품의 보통이다**(도형마다 한 번). 이름으로
  // 짝을 지으면 세 번째 호출의 답이 첫 번째 줄에 붙는다.
  {
    const t = new Transcript();
    t.append(call('c1', 'mcp__ppt__set_text', { slide: 1 }));
    t.append(call('c2', 'mcp__ppt__set_text', { slide: 2 }));
    t.append(result('c2', '슬라이드 2'));
    ok('답은 자기 호출에만 붙는다',
      t.drawnRows[0].result === null && t.drawnRows[1].result?.callId === 'c2');
  }

  // 짝을 못 찾은 답은 **버리지 않는다** — 로그 중간부터 읽기 시작했다는 사실이다.
  {
    const t = new Transcript();
    t.append(result('없는-호출', '뭔가 됐다'));
    ok('짝 없는 답도 줄이 선다', t.drawnRows.length === 1 && t.drawnRows[0].kind === 'result');
    ok('짝 없는 답은 그렇게 적는다', rowHead(t.drawnRows[0]).includes('앞을 못 본'),
      rowHead(t.drawnRows[0]));
  }

  // **`isError` 하나로 ✗ 를 찍지 않는다.** 코어가 `Advisory` 를 따로 둔 이유가 그 필드
  // 주석에 적혀 있다 — 한 일은 했는데 읽을 것이 붙은 호출도 `IsError` 를 세우고, 그래서 창
  // 둘이 성공한 쓰기를 실패로 그렸다.
  {
    const one = (extra) => {
      const t = new Transcript();
      t.append(call('c1', 'mcp__ppt__set_text', {}));
      t.append(result('c1', '했음', extra));
      return resultCell(t.drawnRows[0]);
    };
    ok('된 것은 됐다고', one({}).mark === '✓' && one({}).failed === false);
    ok('못 한 것은 못 했다고', one({ isError: true }).mark === '✗'
      && one({ isError: true }).failed === true);
    ok('했는데 읽을 것이 붙은 것은 실패가 아니다',
      one({ isError: true, advisory: true }).failed === false
        && one({ isError: true, advisory: true }).mark === '⚠',
      one({ isError: true, advisory: true }).mark);
  }

  // 아직 답이 안 온 호출. **「완료」를 미리 적지 않는다.**
  {
    const t = new Transcript();
    t.append(call('c1', 'mcp__ppt__set_text', {}));
    ok('답이 안 온 호출에는 결과 칸이 없다', resultCell(t.drawnRows[0]) === null);
  }

  // 그림은 참조로만 온다(`ImageRef`). 못 그리면 **몇 장인지는 적는다.**
  {
    const t = new Transcript();
    t.append(call('c1', 'mcp__ppt__render_slide', {}));
    t.append(result('c1', '', { images: [{ path: 'a.png', mime: 'image/png' }] }));
    ok('못 그리는 그림도 몇 장인지는 적는다',
      resultCell(t.drawnRows[0]).text.includes('1장'), resultCell(t.drawnRows[0]).text);
  }

  // 허락도 같은 줄에 붙는다. 이 제품에서 그 답은 **덱을 고치게 뒀는가**다.
  {
    const t = new Transcript();
    t.append(call('c1', 'mcp__ppt__set_text', {}));
    t.append({ seq: 3, sessionId: 'A', type: 'permission.decided', data: { callId: 'c1', decision: 'allow' } });
    ok('허락은 호출 줄에 붙는다', t.drawnRows.length === 1 && t.drawnRows[0].permission === 'allow');
    ok('허락을 사람 말로 적는다', permissionText(t.drawnRows[0]).includes('허용'),
      permissionText(t.drawnRows[0]));
    // 모르는 결정을 아는 척 옮기지 않는다 — 글자 그대로 적는다.
    ok('모르는 결정도 뭔가는 적는다',
      permissionText({ permission: 'quarantine' }).includes('quarantine'));
    ok('결정이 없으면 줄이 없다', permissionText({ permission: null }) === '');
  }

  // 종료 게이트. **이 줄이 없으면 사람은 모델이 왜 같은 일을 또 하는지 모른다.**
  {
    const t = new Transcript();
    t.append({ seq: 1, sessionId: 'A', type: 'council.convened',
      data: { round: 1, members: ['Melchior', 'Balthasar', 'Casper'], rule: 'majority' } });
    t.append({ seq: 2, sessionId: 'A', type: 'council.verdict',
      data: { round: 1, member: 'Melchior', lens: 'correctness', decision: 'continue', rationale: '증거가 없다' } });
    t.append({ seq: 3, sessionId: 'A', type: 'council.decided',
      data: { round: 1, decision: 'continue', tally: { done: 0, continue: 3, abstain: 0 },
        feedback: 'council 선언이 없다' } });
    const [conv, verd, dec] = t.drawnRows;
    ok('판정 셋이 다 줄이 된다', t.drawnRows.length === 3 && conv.kind === 'council');
    ok('소집은 누가 판정하는지 적는다',
      rowHead(conv).includes('Melchior') && rowHead(conv).includes('majority'), rowHead(conv));
    ok('한 표는 누가·무엇으로·어떻게 인지 적는다',
      rowHead(verd).includes('Melchior') && rowHead(verd).includes('correctness')
        && rowHead(verd).includes('더 하라'), rowHead(verd));
    ok('그 표의 사유가 몸통에 온다', councilBody(verd) === '증거가 없다');
    ok('결론은 표 수까지 적는다',
      rowHead(dec).includes('더 하라 3') && rowHead(dec).includes('끝났다 0'), rowHead(dec));
    ok('결론의 사유가 몸통에 온다', councilBody(dec).includes('council 선언이 없다'));
    ok('판정은 대화와 다른 모양이다', rowShape(conv) === 'council');

    // **말 없는 표를 「기권했다」로 적지 않는다**(`CouncilVerdictData.Silent`) — 백엔드가
    // 죽었거나 답을 못 읽은 것이라, 판단해서 기권한 것과 다른 사실이다.
    const t2 = new Transcript();
    t2.append({ seq: 1, sessionId: 'A', type: 'council.verdict',
      data: { round: 1, member: 'Casper', decision: 'abstain', silent: true } });
    ok('말 없는 표는 기권이라고 안 적는다',
      rowHead(t2.drawnRows[0]).includes('답이 없었'), rowHead(t2.drawnRows[0]));
  }

  // 안 그리기로 정한 것. **모르는 것과 다른 칸**이라 문장도 따로다.
  {
    const t = new Transcript();
    t.append({ seq: 0, sessionId: 'A', type: 'context.usage', data: { tokens: 1 } });
    t.append({ seq: 0, sessionId: 'A', type: 'council.deliberating', data: { member: 'Casper' } });
    t.append({ seq: 4, sessionId: 'A', type: '알 수 없는 종류', data: {} });
    ok('안 그리기로 한 것은 줄을 안 세운다', t.drawnRows.length === 0, `${t.drawnRows.length}줄`);
    ok('그래도 몇 건인지는 적는다',
      t.skippedNote.includes('2건') && t.skippedNote.includes('context.usage'), t.skippedNote);
    ok('모르는 것은 여전히 따로 센다',
      t.unknownNote.includes('1건') && !t.unknownNote.includes('context.usage'), t.unknownNote);
    ok('두 줄은 서로 다른 문장이다', skippedLine(t.skippedNote).text !== unknownLine(t.unknownNote).text);
    // 대화가 바뀌거나 서버가 커서를 물리면 **두 계수기를 같이** 비운다 — 하나만 비우면
    // 지운 화면에 옛 수가 남는다.
    t.restart('다시');
    ok('다시 그릴 때 두 셈이 같이 비워진다', t.skippedNote === null && t.unknownNote === null);
  }
}


// ── 안내는 도구가 **광고한 철자**로 읽는다 ───────────────────────────────────
//
// 실물에서 본 것이 근거다(2026-09-01): 모델이 `mcp__ppt__advise` 로 슬라이드와 도형을 정확히
// 짚어 안내 둘을 보냈는데, 작업창의 포스트잇은 **하나도 못 눌렸다.** 스키마는 `slide_id` ·
// `shape_ids` 라고 광고하는데 접는 쪽은 `slideId` · `shapeIds` 만 봤기 때문이다. 화면은
// 「어디를 가리키는지 안 실렸습니다」라고 적었고, 그건 **모델을 탓하는 거짓말**이었다.
//
// 그래서 이 시험은 철자를 손으로 안 적는다 — **도구 표에서 뽑는다.** 손으로 적으면 이 파일도
// 두 벌 중 하나가 되어, 스키마가 바뀌는 날 같이 안 바뀐다.
{
  const toolsGo = readFileSync(new URL('../../helper/tools.go', import.meta.url), 'utf8');
  // `[{message, why, slide_id, shape_ids}]` — `advise` 의 `items` 설명문이 광고하는 그것.
  // **`advise` 의 것만 본다.** 파일 안에 같은 모양이 하나 더 있고(`set_table_cells` 의
  // `[{row, column, text}]`), 처음 걸리는 것을 집으면 이 시험은 남의 스키마를 지킨다.
  const adviseBlock = toolsGo.slice(toolsGo.indexOf('Name: "advise"'));
  const advertised = /\[\{([a-z_, ]+)\}\]/.exec(adviseBlock)?.[1]?.split(',').map((s) => s.trim()) ?? [];
  ok('도구 표에서 안내 항목의 철자를 뽑았다',
    advertised.includes('message') && advertised.length >= 4, advertised.join('|'));

  // 광고된 철자 그대로 항목을 지어 먹인다. 무엇이 어느 칸인지는 이름이 말한다.
  const say = (k) => (k === 'message' ? '제목이 깁니다' : k === 'why' ? '읽는 사람이 놓칩니다'
    : k.endsWith('_ids') || k.endsWith('Ids') ? ['2'] : '256#1776505032');
  const item = {};
  for (const k of advertised) item[k] = say(k);

  const t = new Transcript();
  t.append({ seq: 1, sessionId: 'A', type: 'part.appended',
    data: { messageId: 'm1', part: { kind: 'tool-call',
      toolCall: { callId: 'c1', name: 'mcp__ppt__advise', args: { items: [item] } } } } });
  const folded = foldAdvice(t.drawnRows);
  ok('광고된 철자로 부른 안내가 한 장 선다', folded.items.length === 1, `${folded.items.length}장`);
  ok('광고된 철자로 부른 안내는 **눌린다**',
    folded.items[0]?.pointable === true, folded.items[0]?.unpointableReason ?? '');
  ok('그 안내가 짚은 슬라이드와 도형이 실려 온다',
    folded.items[0]?.slideId === '256#1776505032' && folded.items[0]?.shapeIds.length === 1);

  // 낙타등도 계속 받는다 — 목업의 픽스처가 그 철자를 쓰고, 둘을 받는 값이 0 이다.
  const t2 = new Transcript();
  t2.append({ seq: 1, sessionId: 'A', type: 'part.appended',
    data: { messageId: 'm1', part: { kind: 'tool-call', toolCall: { callId: 'c2',
      name: 'mcp__ppt__advise',
      args: { items: [{ message: '옛 철자', slideId: 's1', shapeIds: ['3'] }] } } } } });
  ok('낙타등 철자도 눌린다', foldAdvice(t2.drawnRows).items[0]?.pointable === true);

  // **안 실린 것은 여전히 안 실린 것이다** — 둘 다 없으면 그렇게 적는다.
  const t3 = new Transcript();
  t3.append({ seq: 1, sessionId: 'A', type: 'part.appended',
    data: { messageId: 'm1', part: { kind: 'tool-call', toolCall: { callId: 'c3',
      name: 'mcp__ppt__advise', args: { items: [{ message: '어딘지 안 적음' }] } } } } });
  const bare = foldAdvice(t3.drawnRows).items[0];
  ok('어딘지 안 실린 안내는 안 눌리고 사유가 붙는다',
    bare?.pointable === false && Boolean(bare?.unpointableReason), bare?.unpointableReason ?? '');
}


// ── 막힌 물음은 화면 안으로 끌어온다 ─────────────────────────────────────────
//
// 실물에서 본 것이 근거다(2026-09-01): `--permission ask` 로 띄운 데몬이 `bash` 권한을 물었고
// 판은 정확히 그렸는데, **그 칸이 접힌 자리 밖이라 안 보였다.** 마우스 휠을 굴려야 나왔다.
// §5.7 이 이름 대어 피하려는 「아무도 안 보는 곳에서 대기」가 화면 안에서 그대로 재현된 것이다.
{
  ok('새로 선 권한 물음은 끌어온다', askReveal('known', 'rebuild') === true);
  ok('그릴 줄 모르는 물음도 끌어온다 — 데몬은 똑같이 막혀 있다',
    askReveal('unknown', 'rebuild') === true);
  // **같은 물음을 매초 끌어오지 않는다.** 폴은 1초마다 도는데 그때마다 끌어오면 위로 올려
  // 읽던 것이 매초 도로 내려간다.
  ok('같은 물음은 다시 안 끌어온다', askReveal('known', 'refresh') === false);
  // 사람이 답할 것이 없는 칸은 읽던 자리를 안 뺏는다.
  ok('못 닿는다는 말은 안 끌어온다', askReveal('lost', 'rebuild') === false);
  ok('직전 물음이 내려간 것도 안 끌어온다', askReveal('last', 'rebuild') === false);
  ok('붙기 전에는 끌어올 것이 없다', askReveal('none', 'rebuild') === false);
}

// ── 320px 판에서 긴 토막이 판 밖으로 나가지 않는다 ───────────────────────────
//
// 실물에서 스크롤 영역에 **가로 막대**가 섰다(2026-09-01). 브라우저에서 판 너비를 305px 로
// 줄여 재현했다: `#scroll` 의 scrollWidth 294 · clientWidth 289, 넘긴 것은 혼잣말 줄의 `<p>`
// 였다 — `white-space: pre-wrap` 은 빈칸에서만 접히므로 `mcp__ppt__set_text --slide-id` 같은
// 한 덩어리가 판보다 길면 그대로 나간다.
//
// CSS 는 여기서 못 돌린다. 그래서 **규칙이 그 파일에 서 있는지**를 글자로 문다 — 이 저장소가
// 매니페스트 순서와 오리진에 쓰는 것과 같은 종류의 가드다.
{
  const css = readFileSync(new URL('../taskpane.css', import.meta.url), 'utf8');
  const rule = /\.turn p \{([^}]*)\}/.exec(css)?.[1] ?? '';
  ok('말 줄 규칙을 찾았다', rule !== '', rule);
  ok('긴 토막을 끊는 규칙이 말 줄에 서 있다',
    /overflow-wrap:\s*anywhere|word-break:\s*break-(all|word)/.test(rule), rule.trim());

  // **`hidden` 이 언제나 이겨야 한다.** 속성은 UA 의 `display:none` 으로만 서 있어서, 저자
  // 규칙이 `display:` 를 적는 순간 진다. 실물에서 그렇게 새어 나왔다(2026-09-04): 가이드
  // 편집 칸에 `display:flex` 를 줬더니 판을 열자마자 **안 누른 편집기가 같이 열려** 있었다.
  // 자바스크립트는 `hidden = true` 를 옳게 세우고 있었고 화면만 안 듣고 있었다.
  //
  // 자리마다 예외를 다는 대신 한 줄로 못 박았는지를 묻는다 — 자리마다면 다음에 `display` 를
  // 적는 사람이 그 예외를 안 단다.
  ok('hidden 이 display 를 이긴다',
    /\[hidden\]\s*\{[^}]*display:\s*none\s*!important/.test(css));

  // 그리고 **그 규칙이 실제로 이길 자리에 있는지**도 센다: `display` 를 적는 규칙이 이 파일에
  // 여럿이라(오늘 스물여섯) 한 줄이 없으면 그중 어느 것이든 다음 결함이 된다.
  ok('display 를 적는 규칙이 실제로 여럿이다', (css.match(/display:/g) ?? []).length > 5,
    String((css.match(/display:/g) ?? []).length));
}


// ── 붙어 있던 컴패니언이 다시 뜨면 그렇게 말한다 ─────────────────────────────
//
// 실물에서 본 것이 근거다(2026-09-01): 데몬을 껐다 켰더니 작업창은 「deck2 · 대화 연결됨」을
// 그대로 적고 있었고, 모델에게는 덱 도구가 **하나도** 없었다. 소켓 경로는 워크스페이스에서
// 유도되므로 다시 떠도 같고 dial 도 성공한다 — 그래서 「닿는다」는 참이고 「붙어 있다」는
// 거짓인 상태가 화면에서 구분되지 않았다. 사람은 셸로 우회하려는 모델을 지켜보고 있었다.
{
  // 헬퍼가 판정해서 실어 준다(같은 소켓, 다른 프로세스). 이 층이 하는 일은 **그 사실이
  // 바뀌는 순간에 한 번 종을 치는 것**이다 — 매 폴마다 치면 화면이 매초 다시 세워진다.
  const scripted = (...answers) => ({
    async status() { return answers.shift() ?? answers.at(-1); },
    async answerPermission() {}, async answerQuestion() {},
  });
  // ** 라고 이름 짓지 않는다.** 처음엔 그랬고, 이 블록의 단언이 **한 줄도 안 돌았다** —
  // 지역 이름이 스위트의 단언 함수를 가려서 전부 조용히 삼켜졌고 화면은 초록이었다. 이 파일이
  // 맨 위에 적어 둔 「0개를 본 것과 0개가 틀린 것을 안 가른다」가 제 안에서 한 번 더 났다.
  const st = (extra = {}) => ({ reachable: true, pending: null, doing: '', ...extra });

  {
    const w = new WatchPrompt(scripted(st(), st({ stale: true }), st({ stale: true })));
    let rings = 0;
    w.onChange = () => { rings += 1; };
    await w.poll();
    const quiet = rings;
    await w.poll();
    ok('다시 뜬 것을 값에 싣는다', w.view.stale === true);
    ok('그 사실이 바뀌면 종을 친다', rings === quiet + 1, `${quiet} → ${rings}`);
    await w.poll();
    ok('같은 사실에 매 폴마다 치지는 않는다', rings === quiet + 1, String(rings));
  }

  // 안 실린 것은 **거짓이지 참이 아니다** — 옛 헬퍼가 이 칸을 안 실어 보내도 창이 멀쩡해야 한다.
  {
    const w = new WatchPrompt(scripted(st()));
    await w.poll();
    ok('안 실린 칸은 「다시 떴다」가 아니다', w.view.stale === false);
  }

  // 조립 자리의 규칙: **몰래 다시 붙이지 않는다.** 다시 붙이는 것은 「이 컴패니언에 맡긴다」를
  // 다시 말하는 일이고, 그 말은 사람이 한다(§5.0). 그래서 그 신호가 부르는 것은 `choose` 가
  // 아니라 고르는 판이어야 한다.
  {
    const src = readFileSync(new URL('../src/main.js', import.meta.url), 'utf8');
    const body = /const companionRestarted = \(\) => \{([\s\S]*?)\n    \};/.exec(src)?.[1] ?? '';
    ok('다시 뜬 경우를 다루는 자리가 있다', body !== '');
    ok('그 자리는 고르는 판을 도로 세운다', /showCompanions\(\)/.test(body), body.trim().slice(0, 60));
    ok('그 자리가 몰래 다시 붙이지는 않는다', !/api\.choose/.test(body));
    ok('그 자리는 「붙어 있다」를 내린다', /setBound\(false\)/.test(body));
  }
}


// ── 열면 알아서 붙는다 ──────────────────────────────────────────────────────
//
// 명단 화면은 **이미 데몬이 떠 있는 사람에게만** 뜻이 있다. 메일에서 받은 `.pptx` 를
// 더블클릭한 사람에게는 늘 비어 있고, 「컴패니언을 고르세요」는 그 사람 머릿속에 대응하는
// 개념이 없는 말이다. 이 도구의 목표가 PC 를 잘 다루지 못하는 사람이면 첫 화면이 막다른
// 길이었다 — 그래서 열면 헬퍼가 파워포인트 몫의 컴패니언을 마련한다(`/api/own`).
//
// 여기는 **조립 자리**라 소스를 훑는다. 아래 넷은 전부 「하지 말아야 할 것」이다.
{
  const src = readFileSync(new URL('../src/main.js', import.meta.url), 'utf8');
  const body = /const attachOwn = async \(\) => \{([\s\S]*?)\n    \};/.exec(src)?.[1] ?? '';
  ok('알아서 붙는 자리가 있다', body !== '');
  ok('그 자리가 헬퍼에게 마련을 청한다', /api\.own\(\)/.test(body));

  // **기다리는 동안 말을 한다.** 아무 말 없는 화면은 사람에게 고장으로 읽힌다 — 무엇을
  // 기다리는지도, 다시 눌러야 하는지도 모른다.
  ok('기다리는 동안 사람에게 말한다', /view\.where\(/.test(body) && /초째/.test(body), body.slice(0, 80));

  // **끝없이 기다리지 않는다.** 천장이 없으면 못 뜨는 날 판이 영원히 「준비 중」이다.
  ok('기다림에 천장이 있다', /until/.test(body) && /Date\.now\(\) < until/.test(body));

  // **실패를 조용히 넘기지 않고, 갈 곳을 준다.** 명단이 그 갈 곳이다.
  // **못 했으면 사유와 갈 곳을 남긴다.** 적는 것은 명단을 세운 뒤다 — `pick.render` 가 자식을
  // 갈아 끼우므로 먼저 적으면 지워진다.
  ok('못 했으면 사유를 들고 나온다', /failedWhy = /.test(body), body.slice(0, 60));
  ok('못 했으면 로그 자리도 들고 나온다', /failedLog = r\?\.log/.test(body));
  ok('못 했으면 명단으로 보낸다', /골라 주세요/.test(body));

  // **못 물어본 것과 데몬이 안 뜬 것을 가른다.** 앞 판본은 물어보다 실패해도 마지막에 받은
  // `working` 을 들고 있어서, 헬퍼가 5분 내내 안 답해도 「데몬이 아직 안 떴습니다」로 적었다 —
  // 데몬 이야기를 하는데 사실은 헬퍼 이야기였다.
  ok('물어보다 실패한 것을 따로 센다', /askFailed = String\(/.test(body), body.slice(0, 60));
  ok('그 사유를 사람 말로 적는다', /헬퍼가 답하지 않습니다/.test(body));

  // **됐을 때만 「붙었다」를 올린다.** 이 순서가 뒤집히면 도구가 하나도 없는 채로 화면이
  // 멀쩡하다고 적는다 — 이 저장소가 여러 번 만난 그 모양이다.
  const after = body.slice(body.indexOf("r?.phase !== 'ready'"));
  ok('붙었다고 적는 것은 ready 뒤다', /setBound\(true\)/.test(after), after.slice(0, 60));
  ok('도구 수를 증거로 적는다', /tools\?\.length/.test(after));

  // **이미 붙어 있으면 흔들지 않는다.** 작업창을 껐다 켜면 이 창은 새로 나지만 헬퍼는 살아
  // 있다. 그때 다시 붙이면 첫 등록이 떨어진다(§5.0.1).
  ok('이미 붙어 있으면 다시 안 마련한다', /if \(!bound\) \{/.test(src));

  // ── 준비하는 동안 명단이 뜨면 안 된다 ─────────────────────────────────────
  //
  // 실물에서 봤어야 했는데 못 본 자리다(리뷰가 계산으로 짚었다, 2026-09-02). 명단의 빈 화면에는
  // 「덱이 있는 폴더에서 `magi --daemon` 을 띄운 뒤 새로고침하세요」가 적혀 있다 — 이 판이
  // 없애려던 바로 그 문장이고, PC 를 잘 다루지 못하는 사람에게 터미널 명령을 시키는 말이다.
  // 게다가 그 사이 명단의 단추가 눌리면 사람이 고른 컴패니언이 잠시 뒤 끝난 마련하기에게
  // **말없이 덮인다.**
  {
    ok('명단을 그릴지 말지 고를 수 있다', /const showCompanions = async \(show = true\)/.test(src));
    ok('그릴 때만 그린다', /if \(show\) \{ pick\.render\(list\)/.test(src));
    ok('부팅은 안 그리고 읽기만 한다', /await showCompanions\(false\)/.test(src));
    ok('명단은 자동으로 못 마련했을 때만 선다',
      /if \(!await attachOwn\(\)\) \{\s*\n\s*await showCompanions\(true\)/.test(src),
      src.slice(src.indexOf('if (!await attachOwn'), src.indexOf('if (!await attachOwn') + 80));
    // 못 훑은 경우도 마찬가지 — 준비 중에는 이 화면 자체가 안 나와야 한다.
    const failPath = /\} catch \(e\) \{[\s\S]*?컴패니언을 못 훑었습니다[\s\S]*?\n      \}/.exec(src)?.[0] ?? '';
    ok('못 훑은 것도 그릴 때만 적는다', /if \(show\) \{/.test(failPath), failPath.slice(0, 60));
  }

  // ── 늘 지킬 것 ────────────────────────────────────────────────────────────
  //
  // 「불릿은 한 줄로」, 「강조는 우리 회사 파랑으로」. 이런 것은 부탁이 아니라 **취향이고**
  // **규칙**이라, 대화마다 다시 말하게 하면 사람이 지친다 — 그리고 지치면 안 말하게 되고,
  // 안 말하면 결과가 매번 조금씩 다르다.
  //
  // Claude for PowerPoint 의 「Instructions」와 같은 자리다(2026-09-02 비교). 우리는 그 글을
  // 컴패니언 워크스페이스의 AGENTS.md 로 보내고, magi 가 그것을 매 턴 시스템 프롬프트에 넣는다.
  {
    const html = readFileSync(new URL('../taskpane.html', import.meta.url), 'utf8');
    ok('늘 지킬 것 단추가 있다', /id="rules"/.test(html));
    ok('적는 자리가 있다', /id="rules-text"/.test(html));
    ok('처음에는 접혀 있다', /id="rules-panel"[^>]*hidden/.test(html),
      html.slice(html.indexOf('id="rules-panel"'), html.indexOf('id="rules-panel"') + 60));
    // **어디에 걸리는지 화면이 적는다.** 파워포인트에만 걸린다는 것을 모르면, 사람은 이 글이
    // 자기 저장소 에이전트까지 바꾸는 줄 안다.
    ok('매번 함께 간다고 적는다', /매번/.test(html) && /파워포인트에서만/.test(html));

    const open = /#rules'\)\?\.addEventListener[\s\S]*?\n    \}\);/.exec(src)?.[0] ?? '';
    ok('열면 지금 적힌 것을 읽어 온다', /api\.rules\(\)/.test(open), open.slice(0, 60));
    ok('적혀 있던 것을 그대로 보여 준다', /rulesText\.value = got\?\.text/.test(open));

    // **못 읽었으면 빈 칸을 보여 주지 않는다.** 빈 칸은 「아무것도 안 적혀 있다」는 거짓말이고,
    // 그 위에 저장을 누르면 적어 둔 규칙이 날아간다 — 이 저장소가 최악이라고 적은 그 모양이다.
    ok('못 읽었으면 저장을 막는다', /rulesText\.disabled = true/.test(open), open.slice(-200));
    ok('막은 이유를 적는다', /덮어쓰게 되므로/.test(open));

    const save = /#rules-save'\)\?\.addEventListener[\s\S]*?\n    \}\);/.exec(src)?.[0] ?? '';
    ok('저장하는 자리가 있다', /api\.setRules\(/.test(save), save.slice(0, 60));
    // **언제부터 듣는지 적는다.** 「저장했습니다」만 적으면 사람은 지금 도는 턴에도 걸리는 줄 안다.
    ok('헬퍼가 준 안내를 그대로 적는다', /out\?\.note/.test(save));
  }

  // ── 새 대화로 빠져나갈 길이 있다 ──────────────────────────────────────────
  //
  // 파워포인트 컴패니언은 워크스페이스가 하나라 대화도 하나이고, 그 하나가 **영원히 쌓인다.**
  // 실물에서 봤다(2026-09-02): 한 번 헤맨 대화가 다음 부탁까지 끌고 가서, 사람이 19번 장을
  // 보고 있는데 모델이 8번 장에 정렬을 걸고 6~17번을 헤맸다. 채팅을 쓰는 사람은 누구나 「새
  // 대화」를 알고, PC 를 잘 다루지 못하는 사람에게는 그것이 유일하게 아는 복구 수단이다.
  {
    const html = readFileSync(new URL('../taskpane.html', import.meta.url), 'utf8');
    ok('새 대화 단추가 마크업에 있다', /id="fresh"/.test(html));

    const body = /#fresh'\)\?\.addEventListener[\s\S]*?\n    \}\);/.exec(src)?.[0] ?? '';
    ok('그 단추에 배선이 있다', body !== '');
    ok('헬퍼에게 새 대화를 청한다', /api\.fresh\(\)/.test(body));

    // **창을 새 이름으로 옮겨 앉힌다.** 안 그러면 새 대화의 이벤트가 전부 남의 것으로 걸러져서,
    // 눌렀는데 아무 말도 안 보이는 화면이 된다 — 이 저장소가 실물에서 한 번 겪은 그 모양이다.
    ok('창을 새 대화로 옮겨 앉힌다', /listenTo\(out\?\.session\)/.test(body), body.slice(0, 80));

    // **덱은 안 건드린다는 것을 먼저 말한다.** 슬라이드를 지우는 것으로 읽히면 아무도 못 누른다.
    ok('누르는 순간 덱이 무사하다고 적는다', /슬라이드는 그대로/.test(body), body.slice(0, 80));

    // 못 열었으면 **쓰던 대화는 그대로다.** 그 사실까지 적어야 사람이 자기 대화가 어떻게 됐는지 안다.
    ok('못 열었으면 쓰던 대화가 무사하다고 적는다', /쓰던 대화는 그대로/.test(body));

    // 덱을 고치는 도구를 여기서 부르지 않는다 — 이름 그대로 대화만 바꾼다.
    ok('여기서 덱을 고치지 않는다', !/delete_slide|restore_slide|set_text/.test(body));
  }

  // ── 붙은 뒤에도 명단으로 돌아갈 길이 있다 ────────────────────────────────
  //
  // 명단을 남겨 둔 이유(저장소의 에이전트에게 덱을 맡기기)가 붙고 나면 **화면에서 사라졌다** —
  // `pick.hide()` 뒤로 그것을 다시 세울 방법이 어디에도 없었다. 길이 코드에만 있고 화면에는
  // 없으면 없는 것이다.
  {
    const html = readFileSync(new URL('../taskpane.html', import.meta.url), 'utf8');
    ok('돌아갈 자리가 마크업에 있다', /id="repick"/.test(html));
    ok('붙기 전에는 안 보인다', /id="advanced"[^>]*hidden/.test(html), html.slice(html.indexOf('id="advanced"'), html.indexOf('id="advanced"') + 60));

  // ── 가끔 쓰는 넷은 **늘 닿는 자리**에 ────────────────────────
  //
  // 스크롤 영역 안 맨 위에 있던 시절, 누르려면 대화를 끝까지 거슬러 올라가야 했다 — 대화가
  // 길수록 멀어지는 단추였다(2026-09-04에 지적받았다). **자리로** 잰다: 스크롤 닫는 자리보다
  // 뒤에 있어야 고정이다.
  ok('넷은 스크롤 밖이다',
    html.indexOf('id="advanced"') > html.indexOf('/#scroll'),
    `advanced=${html.indexOf('id="advanced"')} scroll끝=${html.indexOf('/#scroll')}`);
  // **펴지는 것은 편 손 옆에 선다.** 컴포저 위에 뒀더니 누른 자리와 열리는 자리 사이에 컴포저와
  // 계획 판이 통째로 끼어서 무엇이 열렸는지 눈이 못 따라갔다 — 손잡이 바로 위가 그 자리다.
  ok('펴지는 줄이 손잡이 바로 위다',
    html.indexOf('id="advanced"') > html.indexOf('class="composer"')
    && html.indexOf('id="advanced"') < html.indexOf('<footer class="brand">'),
    `composer=${html.indexOf('class="composer"')} advanced=${html.indexOf('id="advanced"')} footer=${html.indexOf('<footer class="brand">')}`);
  // 그렇다고 늘 펴 두지는 않는다 — 48px 는 대화에서 빼 오는 것이다. 손잡이는 **이미 고정이고
  // 글자만 있던 줄**에 얹는다.
  ok('손잡이는 브랜드 줄에 있다',
    /<footer class="brand">[\s\S]*?id="more"[\s\S]*?<\/footer>/.test(html));
  ok('손잡이도 붙기 전에는 안 보인다', /id="more"[^>]*hidden/.test(html));
  ok('무엇을 펴는지 낭독기에 말한다', /id="more"[\s\S]{0,400}aria-controls="advanced"/.test(html));
  {
    const m = readFileSync(new URL('../src/main.js', import.meta.url), 'utf8');
    // 펴짐을 **`aria-expanded` 로도** 말한다 — 화면을 안 보는 손에게는 그 값이 전부다.
    ok('펴짐을 낭독기에도 알린다', /more\.setAttribute\('aria-expanded'/.test(m));
    // 문만 감추고 판을 펴 둔 채로 두면 **닫을 손이 없는 줄**이 남는다.
    ok('문을 감출 때 판도 닫는다', /if \(!on && advanced\) advanced\.hidden = true/.test(m));
    // 여는 단추가 화면 아래라, 안 데려오면 판은 대화 저 위에서 열린다 — 사람이 보기에는
    // **아무 일도 안 일어난 것**이다.
    ok('아래에서 연 판을 데려온다',
      /rulesPanel\.hidden = false;[\s\S]{0,200}view\.reveal\(rulesPanel\)/.test(m)
      && /gPanel\.hidden = false;[\s\S]{0,120}view\.reveal\(gPanel\)/.test(m));
  }

  // ── 도는 중이라는 것 하나 ───────────────────────────────────
  //
  // 도형마다 호출 하나인 제품이라 사람이 읽을 글은 몇 분에 한 줄뿐이다. 그 사이 화면은 「멈춘
  // 것」과 구별되지 않고, 실제로 그 물음을 받았다(2026-09-04).
  //
  // **상태를 따로 묻지 않고 로그에서 유도한다** — 적어 두면 조건이 사라져도 남고, 유도하면
  // 같이 사라진다.
  ok('말을 냈고 끝난 턴이 없으면 도는 중', turnRunning([{ kind: 'user' }]) === true);
  ok('끝난 턴이 뒤에 있으면 아니다', turnRunning([{ kind: 'user' }, { kind: 'turn' }]) === false);
  // 도구가 줄줄이 도는 동안에도 도는 중이다 — 그 구간이 바로 이 막대가 있어야 하는 자리다.
  ok('도구가 도는 동안에도 도는 중',
    turnRunning([{ kind: 'user' }, { kind: 'tool' }, { kind: 'model' }]) === true);
  // 앞 턴이 끝난 뒤 새 말을 내면 다시 도는 중이다.
  ok('다음 말에 다시 선다',
    turnRunning([{ kind: 'user' }, { kind: 'turn' }, { kind: 'user' }]) === true);
  ok('사람 말이 없으면 아니다', turnRunning([{ kind: 'model' }]) === false);
  ok('빈 로그에서도 안 터진다', turnRunning(undefined) === false);
  {
    const paneCss = readFileSync(new URL('../taskpane.css', import.meta.url), 'utf8');
    // 진척을 모르므로 **무한**이다. %를 적으면 그건 지어낸 숫자다.
    ok('무한 막대다', /\.busy-bar \{[^}]*animation:[^}]*infinite/.test(paneCss));
    ok('굵기는 4 다', /\.busy \{[^}]*height:\s*4px/.test(paneCss), 'M3 linear progress');
    // 움직임을 줄이라고 한 사람에게는 **안 움직이되 사라지지도 않는다** — 이 막대가 나르는
    // 것은 장식이 아니라 사실이다.
    const reduce = /@media \(prefers-reduced-motion: reduce\) \{([\s\S]*?)\n\}/.exec(paneCss)?.[1] ?? '';
    ok('움직임 줄이기를 존중한다', /animation:\s*none/.test(reduce), reduce.trim().slice(0, 60));
    ok('그래도 안 사라진다', !/display:\s*none/.test(reduce), reduce.trim().slice(0, 60));
    // **스크롤 영역 밖이다.** 안에 두면 대화가 밀릴 때 같이 밀려서, 정작 「도는 중」을 알아야
    // 하는 긴 턴에서 사라진다. 재는 것은 「위냐 아래냐」가 아니라 **밀려나지 않는가**다.
    ok('막대가 스크롤 밖이다',
      html.indexOf('id="busy"') > html.indexOf('/#scroll'),
      `busy=${html.indexOf('id="busy"')} scroll끝=${html.indexOf('/#scroll')}`);
    // **계획과 컴포저 사이다.** 눈이 이미 가 있는 자리 — 무엇을 하는 중인지와 무엇을 시킬지
    // 사이에 「지금 하는 중」이 놓인다.
    ok('계획과 컴포저 사이다',
      html.indexOf('id="plan"') < html.indexOf('id="busy"')
      && html.indexOf('id="busy"') < html.indexOf('class="composer"'),
      `plan=${html.indexOf('id="plan"')} busy=${html.indexOf('id="busy"')} composer=${html.indexOf('class="composer"')}`);
    ok('낭독기에 진행이라고 말한다', /id="busy"[^>]*role="progressbar"/.test(html));
  }

  // ── 1.10 이 여는 것들 ───────────────────────────────────────
  //
  // 요구 집합 1.10 을 통째로 읽고 나서 붙인 것들이다(2026-09-04). 그 전에는 「없다」고 적어 둔
  // 자리가 셋 있었고, 셋 다 있었다 — `SlideBackground.reset`·`Shape.rotation`·배경 그라데이션.
  // **있는 것을 없다고 적으면 아무도 안 불러 보므로 조용히 남는다.**
  {
    const h = readFileSync(new URL('../src/adapter/OfficeHand.js', import.meta.url), 'utf8');
    const t = readFileSync(new URL('../../helper/tools.go', import.meta.url), 'utf8');
    // 접근성 셋 — 가이드가 요구하면서 도구가 없던 자리다.
    for (const [arg, member] of [['alt_title', 'altTextTitle'], ['alt_text', 'altTextDescription'],
      ['decorative', 'isDecorative'], ['rotation', 'rotation'], ['visible', 'visible']]) {
      ok(`${arg} 가 ${member} 로 간다`, h.includes(`['${arg}', '${member}']`));
      ok(`${arg} 를 광고한다`, t.includes(`Name: "${arg}"`));
    }
    // 없는 호스트에서 조용히 넘어가면 「했습니다」 하고 안 바뀐다.
    ok('1.10 짜리는 사유를 들고 거절한다',
      /alt_title[\s\S]{0,600}supports\('PowerPointApi', '1\.10'\)[\s\S]{0,200}throw new Error/.test(h));
    // 하위 불릿은 들여쓰기 단계로 만든다 — 이것이 없어서 글머리가 한 단계만 됐다.
    ok('들여쓰기 단계가 있다', /paragraphFormat\.indentLevel = lv/.test(h));
    // 배경 채움이 넷이다. 단색만 되던 시절이 visual-deck 에서 스타일 열한 개를 뺀 근거였다.
    ok('그라데이션 배경', /setGradientFill\(\{ type:/.test(h));
    ok('패턴 배경', /setPatternFill\(\{/.test(h));
    // 그리고 되돌리기는 **있다** — fill 이 아니라 그 부모에.
    ok('배경 되돌리기가 있다', /slide\.background\.reset\(\)/.test(h));
    ok('없다고 적지 않는다', !/되돌리는 문이 Office\.js 에 없습니다/.test(h));
  }

  // ── 테마는 **층**을 골라 바꾼다 ─────────────────────────────
  //
  // 장 단위로만 바꾸던 시절 「이 바꿈이 어디까지 번지는가」를 못 재고 결과에 「모른다」를 적어
  // 뒀다. 층을 고를 수 있으면 그 물음이 사라진다 — 마스터에 주면 그 마스터를 쓰는 장 전부이고,
  // 그건 짐작이 아니라 층의 뜻이다.
  {
    const h = readFileSync(new URL('../src/adapter/OfficeHand.js', import.meta.url), 'utf8');
    const t = readFileSync(new URL('../../helper/tools.go', import.meta.url), 'utf8');
    ok('층 셋을 다 고를 수 있다',
      /slideMaster\.themeColorScheme/.test(h) && /slide\.layout\.themeColorScheme/.test(h)
      && /return slide\.themeColorScheme/.test(h));
    ok('모르는 층은 거절한다', /scope 는 slide·layout·master 중 하나입니다/.test(h));
    // 읽는 쪽도 같은 층을 읽어야 한다 — 다른 층을 읽고 바꾸면 「안 바뀌었다」로 보인다.
    ok('읽기도 층을 고른다', /read_theme_colors[\s\S]{0,600}Name: "scope"/.test(t));
    // **「모른다」를 더 안 적는다.** 층이 답하므로 그 문장은 이제 거짓이다.
    ok('번짐을 모른다고 안 적는다', !/다른 장에도 걸리는지는 안 재 봤습니다/.test(h));
    // 도형 하나만 뜨는 길 — render_slide 는 이 도구들 중 제일 비싸다.
    ok('도형만 뜨는 길이 있다', /shape\.getImageAsBase64\(/.test(h));
    ok('없는 호스트엔 사유를 준다', /도형 하나만 뜨는 것은 PowerPointApi 1\.10/.test(h));
    ok('읽기 전용으로 광고한다', /Name: "render_shape"[\s\S]{0,700}ReadOnly: true/.test(t));
  }

  // ── 글머리 기호 ─────────────────────────────────────────────
  //
  // 오래 「못 하는 것」으로 알고 있었는데 안 찾아본 것이었다. `bulletFormat.visible` 은 **1.4** 라
  // 이 애드인의 바닥(1.8)보다 아래다 — 게이트가 필요 없다. `type`·`style` 만 1.10 이다.
  {
    const h = readFileSync(new URL('../src/adapter/OfficeHand.js', import.meta.url), 'utf8');
    // ⚠ **사이에 무엇이 끼었는지까지 본다.** 앞 판본은 두 낱말 사이를 느슨하게 물어서, 그 사이에
    // 게이트를 끼워 넣는 변이가 조용히 지나갔다(2026-09-04).
    const span = /args\.bullet !== undefined([\s\S]{0,160}?)bullets\.visible = Boolean/.exec(h)?.[1];
    ok('켜고 끄는 자리를 찾았다', span !== undefined);
    ok('켜고 끄기에 게이트가 없다', span !== undefined && !/supports\(/.test(span), String(span).slice(0, 80));
    // 없는 호스트에서 조용히 넘어가면 「했습니다」 하고 안 바뀐다 — 사유를 들고 거절한다.
    ok('type·style 은 사유를 들고 거절한다',
      /bullet_type[\s\S]{0,400}supports\('PowerPointApi', '1\.10'\)[\s\S]{0,200}throw new Error/.test(h));
    // **style 목록을 우리가 안 든다.** 레퍼런스가 나열한 41개 밖의 이름도 실제로 받는다
    // (`bulletChromaDot`). 목록을 들면 늙고, 늙은 목록은 되는 값을 거절한다.
    ok('style 을 우리가 검사하지 않는다', !/BulletStyle|ArabicNumeralPeriod'/.test(h));
  }

  // ── 못 닿는 데를 말한다 ─────────────────────────────────────
  //
  // 덱 글꼴을 바꾸는 도구는 **테마 글꼴을 못 바꾼다** — Office.js 에 글꼴 스킴이 없다(어느
  // API 집합에도, 프리뷰에도). 그래서 이 도구가 하는 일은 글자마다 글꼴을 주는 것이고, 새로
  // 만드는 장과 차트·표는 여전히 테마 글꼴로 선다. **그 사실을 답이 적어야 한다** — 안 적으면
  // 사람은 「덱 글꼴을 바꿨다」고 믿고 다음 장에서 딴 글꼴을 본다.
  {
    const h = readFileSync(new URL('../src/adapter/OfficeHand.js', import.meta.url), 'utf8');
    const body = /#deckFont\(args\) \{([\s\S]*?)\n  \}/.exec(h)?.[1] ?? '';
    ok('덱 글꼴 갈래를 찾았다', body !== '');
    ok('못 바꾸는 것을 답에 적는다', /테마 글꼴은 안 바뀝니다/.test(body), body.slice(-120));
    // 자리표시자만이 아니라 **모든 글 있는 도형**을 훑는다 — 그게 apply_style 과 갈리는 이유다.
    ok('도형을 전부 훑는다', /shapes;[\s\S]{0,200}for \(const sh of box\.items\)/.test(body));
    // 글이 없는 도형에 쓰면 그 왕복 전체가 던져서 **한 장이 통째로 안 바뀐다.**
    ok('글 없는 도형에서 안 죽는다', /catch \{ skipped \+= 1; \}/.test(body));
    ok('건너뛴 수를 센다', /skipped/.test(body) && /skipped \?/.test(body));
  }

  // ── 세우는 손 ───────────────────────────────────────────────
  //
  // `/api/interrupt` 는 처음부터 있었고 어댑터에도 있었는데 **아무도 안 불렀다** — 문은 만들어
  // 두고 손잡이를 안 단 것이다. 진행 막대를 붙이고 나서 더 분명해졌다: 「도는 중」이라고 적으면서
  // 세울 길이 없는 화면이었다.
  {
    const m = readFileSync(new URL('../src/main.js', import.meta.url), 'utf8');
    const v = readFileSync(new URL('../src/ui/view.js', import.meta.url), 'utf8');
    ok('세우는 손이 있다', /id="stop"/.test(html));
    ok('그 손이 문을 부른다', /#stop[\s\S]{0,400}api\.interrupt\(\)/.test(m));
    // **도는 동안에만 선다.** 누를 것이 없는 단추는 「지금 뭔가 돌고 있나」를 되묻게 만든다.
    ok('처음엔 안 보인다', /id="stop"[^>]*hidden/.test(html));
    ok('도는 동안에만 보인다', /stop\.hidden = !\(running && this\.canStop\)/.test(v));
    // 부를 문이 없는 갈래(목업)에서는 손잡이도 없다.
    ok('문이 없으면 손잡이도 없다', /view\.canStop = true/.test(m) && /if \(api\) \{/.test(m));
    // **세운 것은 실패가 아니다.** 한 일이 남아 있다고 말해 주지 않으면 되돌려진 줄 안다.
    ok('세운 뒤 남아 있다고 말한다', /세웠습니다[\s\S]{0,40}그대로 남아/.test(m));
  }

  // ── **없는 문은 광고하지 않는다** ───────────────────────────
  //
  // 1.9·1.10 짜리 도구는 호스트가 그것을 지원할 때만 손이 내놓는다. 없는데 목록에 실으면 모델이
  // 부르고, 부르면 「했습니다」 하고 안 바뀐다 — 이 클라이언트가 최악이라고 적어 둔 실패다.
  {
    const h = readFileSync(new URL('../src/adapter/OfficeHand.js', import.meta.url), 'utf8');
    const gated = [['1.10', 'set_background'], ['1.10', 'set_theme_colors'],
      ['1.10', 'read_theme_colors'], ['1.9', 'format_table_cells']];
    ok('새 도구는 능력 뒤에 선다',
      everyOf(gated, ([ver, name]) => new RegExp(`supports\\('PowerPointApi', '${ver}'\\)[\\s\\S]{0,220}'${name}'`).test(h)),
      gated.filter(([ver, name]) => !new RegExp(`supports\\('PowerPointApi', '${ver}'\\)[\\s\\S]{0,220}'${name}'`).test(h)).map(([, n]) => n).join(', '));
    // 그리고 **부를 자리도 있어야 한다.** 목록에만 있고 갈래가 없으면 「모르는 조작」으로 떨어진다.
    ok('부를 자리가 다 있다',
      everyOf(gated, ([, name]) => h.includes(`case '${name}':`)),
      gated.filter(([, name]) => !h.includes(`case '${name}':`)).map(([, n]) => n).join(', '));
  }

  // ── 잰 것은 **헬퍼도 안다** ─────────────────────────────────
  //
  // 요구 집합은 창 안에서만 잴 수 있다. 그런데 창에만 두면 **사람이 화면을 읽어야만 아는 값**이
  // 되고, 그러면 「그건 1.10 이라 못 한다」가 문서에 적힌 채 아무도 다시 안 잰다 — 실제로
  // 그랬고, 다시 재 보니 **지원됐다**(2026-09-04).
  {
    const m = readFileSync(new URL('../src/main.js', import.meta.url), 'utf8');
    const v = readFileSync(new URL('../src/ui/view.js', import.meta.url), 'utf8');
    const a = readFileSync(new URL('../src/adapter/helperApi.js', import.meta.url), 'utf8');
    ok('헬퍼에 넘기는 문이 있다', /caps\(body\) \{ return this\.#send\('\/api\/caps'/.test(a));
    ok('잰 자리에서 넘긴다', /this\.tellCaps\?\.\(capsOf\(this\.deck\)\)/.test(v));
    // 화면이 먼저다 — 넘기다 터져도 요구 집합 줄은 그리던 대로 그린다.
    ok('넘기다 실패해도 화면은 그린다', /try \{ this\.tellCaps[\s\S]{0,80}catch/.test(v));
    // 가짜 갈래엔 헬퍼가 없다. 없는 곳으로 보내지 않는다.
    ok('헬퍼가 없으면 안 보낸다', /if \(api\) view\.tellCaps =/.test(m));
  }

  // ── 켜고 끄는 것은 **스위치**다 ──────────────────────────────
  //
  // M3 가 셋을 갈라 두는 기준이 명시적이다: 체크박스는 목록에서 여럿, 라디오는 하나,
  // **스위치는 독립적인 설정**. 가이드는 서로 무관하고 하나씩, 저장 없이 즉시 먹는다.
  // 앞 판본의 `◉`/`○` 는 읽는 사람에게 라디오의 모양이라 「이 중 하나만」으로 읽혔다.
  {
    const m = readFileSync(new URL('../src/main.js', import.meta.url), 'utf8');
    ok('가이드 토글이 스위치다', /toggle\.setAttribute\('role', 'switch'\)/.test(m));
    // 켜진 **설정**이지 눌린 **단추**가 아니다 — 낭독기에게 그 둘은 다른 말이다.
    ok('상태를 aria-checked 로 말한다', /toggle\.setAttribute\('aria-checked'/.test(m));
    ok('눌린 단추라고 말하지 않는다', !/toggle\.setAttribute\('aria-pressed'/.test(m));
    // 네이티브 단추라야 Space·Enter 가 공짜다(M3 가 스위치에 요구하는 키 경로).
    ok('키보드가 공짜인 자리에 얹는다', /const toggle = document\.createElement\('button'\)/.test(m));
    ok('라디오 글리프를 더 안 그린다', !/i-on|i-off/.test(html));
  }
  {
    const paneCss = readFileSync(new URL('../taskpane.css', import.meta.url), 'utf8');
    const sw = /\.switch \{([^}]*)\}/.exec(paneCss)?.[1] ?? '';
    ok('스위치 규칙을 찾았다', sw !== '');
    // 값은 스펙에서 그대로 옮긴 것이다 — 트랙 32×52, 외곽선 2, 모서리 Full.
    ok('트랙이 52×32 다', /width:\s*52px/.test(sw) && /height:\s*32px/.test(sw), sw.trim());
    ok('외곽선이 2 다', /border:\s*2px/.test(sw), sw.trim());
    ok('모서리가 Full 이다', /border-radius:\s*9999px/.test(sw), sw.trim());
    // 32 짜리 트랙 밖으로 8 씩 — 타겟 48. 아이콘 단추와 같은 규칙이다.
    ok('타겟이 48 이다', /\.switch::before \{[^}]*inset:\s*-8px/.test(paneCss));
    // **핸들이 커지는 것**이 색 말고 다른 한 속성이다 — M3 접근성 문서가 스위치에 대해 이름
    // 대어 요구하는 신호가 그것이고, 자리(왼쪽/오른쪽)와 합쳐 둘이 된다.
    const off = /\.switch-handle \{([^}]*)\}/.exec(paneCss)?.[1] ?? '';
    const on = /\.switch\.on \.switch-handle \{([^}]*)\}/.exec(paneCss)?.[1] ?? '';
    ok('핸들 규칙 둘을 찾았다', off !== '' && on !== '');
    ok('꺼짐 핸들이 16 이다', /width:\s*16px/.test(off), off.trim());
    ok('켜짐 핸들이 24 로 커진다', /width:\s*24px/.test(on), on.trim());
    ok('눌리면 28 이다', /\.switch:active \.switch-handle \{[^}]*width:\s*28px/.test(paneCss));
    ok('자리도 같이 옮긴다', /left:\s*22px/.test(on), on.trim());
    // ⚠ 트랙 안에 글자를 넣지 않는다 — 그 크기의 폰트는 접근성 미달이라고 같은 문서가 적는다.
    ok('트랙에 글자를 안 넣는다', !/switch-handle[\s\S]{0,200}textContent/.test(readFileSync(new URL('../src/main.js', import.meta.url), 'utf8')));
  }

  // ── 정렬은 취향이 아니라 **폭**으로 정한다 ───────────────────
  //
  // `.advanced` 는 넷이 줄을 거의 채워서 양끝이 곧 줄의 양끝이다. `.rules-row` 는 둘뿐이라
  // 양쪽정렬로 두면 한 묶음인 두 손이 판 양끝으로 벌어진다 — 묶음은 붙어 있어야 묶음이다.
  {
    const paneCss = readFileSync(new URL('../taskpane.css', import.meta.url), 'utf8');
    const rr = /\.rules-row \{([^}]*)\}/.exec(paneCss)?.[1] ?? '';
    ok('좁은 손 줄 규칙을 찾았다', rr !== '');
    ok('좁으면 오른쪽에 모은다', /justify-content:\s*flex-end/.test(rr), rr.trim());
    ok('양쪽정렬이 아니다', !/space-between/.test(rr), rr.trim());
    ok('간격은 여전히 16 이다', /gap:\s*16px/.test(rr), rr.trim());
    // 넓은 줄은 그대로 양쪽정렬이다 — 같은 기준의 반대쪽이라 같이 못 박는다.
    ok('넓은 손 줄은 양쪽정렬이다', /\.advanced \{[^}]*justify-content:\s*space-between/.test(paneCss));
  }

  // ── 혼잣말은 **접힌 상자**로 선다 ───────────────────────────
  //
  // 앞 판본은 답풍선과 같은 자리에 펴 놓았다. 사용자에게 한 말이 아닌 글이 348×391 판에서
  // 답을 밀어냈다. 도구 줄과 **같은 손잡이 하나짜리 모양**으로 맞춘다 — 규칙이 하나면 사람이
  // 무엇이 어디 접혀 있는지 안 외운다.
  {
    const v = readFileSync(new URL('../src/ui/view.js', import.meta.url), 'utf8');
    const branch = /if \(shape === 'think'\) \{([\s\S]*?)\n    \}/.exec(v)?.[1] ?? '';
    ok('혼잣말 갈래를 찾았다', branch !== '');
    ok('접히는 상자로 짓는다', /createElement\('details'\)/.test(branch), branch.slice(0, 60));
    ok('손잡이에 요약을 적는다', /thinkHead\(r\)/.test(branch));
    // **기본은 접힘이다.** 펴 두면 앞 판본과 같아진다.
    ok('펴 놓지 않는다', !/\.open = true/.test(branch), branch.slice(0, 60));
    // 혼잣말은 모델이 자기에게 쓴 글이라 줄바꿈이 뜻을 갖는다.
    const paneCss = readFileSync(new URL('../taskpane.css', import.meta.url), 'utf8');
    ok('줄바꿈을 살린다', /\.turn-think \{[^}]*white-space:\s*pre-wrap/.test(paneCss));
    // 답풍선과 **다르게 보여야 한다** — 사용자에게 한 말이 아니다.
    ok('답과 다른 색으로 적는다', /\.turn-think \{[^}]*color:\s*var\(--muted\)/.test(paneCss));
  }

  // ── 아래에 고정된 띠들 사이에는 여백이 없다 ──────────────────
  //
  // 컴포저 · 가끔 쓰는 넷 · 브랜드 줄은 셋 다 제 테두리와 바탕을 가진 띠라, 사이에 12px 이
  // 들어가면 리듬이 아니라 **띠 사이에 낀 흰 조각**으로 보인다. `⋯` 로 넷을 폈을 때 회색 띠
  // 둘 사이에 흰 줄이 서 있는 것을 실물에서 봤다(2026-09-04에 지적받았다).
  //
  // 선택자가 `#pane` 를 지나는 것까지 잰다 — 세로 리듬 규칙이 id 를 물어 (1,0,1) 이라,
  // 클래스만으로 적은 `margin-top: 0` 은 조용히 진다.
  {
    const paneCss = readFileSync(new URL('../taskpane.css', import.meta.url), 'utf8');
    const zeroRule = /#pane > \.advanced[^{]*\{[^}]*margin-top:\s*0[^}]*\}/.exec(paneCss)?.[0] ?? '';
    ok('아래 띠들의 여백 규칙을 찾았다', zeroRule !== '');
    // **막대와 컴포저는 한 묶음이다.** 둘 사이에 여백이 들어가면 막대가 컴포저에 붙은 것이
    // 아니라 떠 있는 줄로 보이고, 그 막대가 말하는 「이 입력칸이 낸 일이 돌고 있다」가 흐려진다.
    ok('막대와 컴포저 사이엔 여백이 없다',
      /#pane > \.busy \+ \.composer \{[^}]*margin-top:\s*0/.test(paneCss));
    // 컴포저를 0 으로 못박지 않는다 — 막대가 없을 때 그 여백을 물려받아야 하고, 리듬 규칙이
    // `[hidden]` 을 건너뛰므로 저절로 그렇게 된다.
    ok('컴포저는 0 으로 못박지 않는다',
      !/#pane > [^{]*\.composer[^+{]*\{[^}]*margin-top:\s*0/.test(paneCss.replace(/#pane > \.busy \+ \.composer \{[^}]*\}/g, '')));
    // ⚠ **이름을 세지 않고 자리를 센다.** 처음엔 셋을 이름으로 적었고, 진행 막대를 넣으면서
    // 하나를 빠뜨려 12px 이 다시 생겼다 — 사람이 보고 물었다(2026-09-04). 이름을 대는 검사는
    // 안 댄 자리를 못 본다. 그래서 스크롤 밖에 서는 띠를 **전부** 세어 맞춘다.
    const after = html.slice(html.indexOf('/#scroll'));
    const bars = [...after.matchAll(/<(?:div|footer|details)[^>]*class="([a-z-]+)"/g)]
      .map((m) => m[1])
      .filter((c) => ['composer', 'advanced', 'brand', 'busy', 'plan'].includes(c));
    ok('스크롤 밖의 띠를 실제로 찾았다', bars.length >= 4, bars.join(', '));
    // `plan` 은 접히는 판이라 위 여백이 있어도 띠 사이에 흰 조각으로 안 보인다 — 나머지를 센다.
    // `plan` 은 접히는 판이고, `busy`·`composer` 는 위에서 따로 잰다(묶음 규칙).
    const need = bars.filter((c) => !['plan', 'busy', 'composer'].includes(c));
    ok('띠마다 여백이 없다',
      everyOf(need, (c) => zeroRule.includes(`#pane > .${c}`)),
      need.filter((c) => !zeroRule.includes(`#pane > .${c}`)).join(', '));
    // 그 리듬 규칙이 여전히 id 를 지나는지도 같이 본다 — 저쪽이 약해지면 이 규칙은 필요 없어진
    // 것이 아니라 **이유가 사라진 채로 남는다.**
    ok('세로 리듬은 여전히 id 를 지난다', /#pane > \*:not\(\[hidden\]\) \+ \*:not\(\[hidden\]\) \{[^}]*margin-top/.test(paneCss));
  }

  // ── 자주 쓰는 손이 되돌릴 수 없는 손 옆에 안 선다 ────────────
  //
  // 가이드 줄의 스위치는 **맨 오른쪽**이다. 켜고 끄는 것이 이 목록에서 가장 자주 하는 일인데,
  // 그것이 지우기 옆에 있으면 자주 가는 손이 되돌릴 수 없는 손과 이웃한다.
  {
    const m = readFileSync(new URL('../src/main.js', import.meta.url), 'utf8');
    const order = /el\.append\(([^)]*)\);/.exec(m)?.[1] ?? '';
    ok('가이드 줄의 차례를 찾았다', order.includes('toggle'), order);
    const parts = order.split(',').map((x) => x.trim());
    ok('스위치가 지우기보다 뒤다', parts.indexOf('toggle') > parts.indexOf('del'), order);
    ok('스위치가 고치기보다 뒤다', parts.indexOf('toggle') > parts.indexOf('edit'), order);
  }

  // ── 물러나는 손이 왼쪽, 저지르는 손이 오른쪽 ─────────────────
  //
  // 오른쪽에 모으고 나면 **순서가 뜻을 갖는다.** 줄 끝에 있는 것이 가장 마지막에 읽히고 가장
  // 쉽게 눌리므로, 거기 서는 것은 사람이 하려던 일이어야 한다. 물러나는 손이 그 자리에 있으면
  // 다 읽고 손이 가는 자리가 「안 함」이다. M3 의 다이얼로그 액션도 확인이 오른쪽이다.
  //
  // 앞 판본은 셋이 거꾸로였다 — 저장이 왼쪽, 닫기가 오른쪽(2026-09-04에 지적받았다).
  {
    const rows = [...html.matchAll(/<div class="(rules-row|confirm-row)">([\s\S]*?)<\/div>/g)];
    ok('손 줄들을 실제로 찾았다', rows.length >= 4, rows.length);
    const wrong = rows
      .map((m) => m[2])
      .map((body) => [...body.matchAll(/<button[^>]*id="([^"]+)"[^>]*class="([^"]*)"/g)]
        .map((b) => ({ id: b[1], primary: /icon-primary|text-danger/.test(b[2]) })))
      .filter((btns) => btns.length >= 2 && !btns[btns.length - 1].primary)
      .map((btns) => btns.map((b) => b.id).join('+'));
    ok('저지르는 손이 줄 끝이다', wrong.length === 0, wrong.join(' / '));
    const heads = rows
      .map((m) => m[2])
      .map((body) => [...body.matchAll(/<button[^>]*id="([^"]+)"[^>]*class="([^"]*)"/g)])
      .filter((b) => b.length >= 2)
      .filter((b) => /icon-primary|text-danger/.test(b[0][2]))
      .map((b) => b[0][1]);
    ok('물러나는 손이 줄 머리다', heads.length === 0, heads.join(' / '));
  }

  // ── 되돌릴 수 없는 것은 **판 안에서** 묻는다 ─────────────────
  //
  // `window.confirm` 을 쓰던 자리다. 그게 이 판에서 위험한 이유는 **안 뜰 수 있어서**인데,
  // 안 뜨면 `undefined` 가 돌아오고 부르는 쪽은 그것을 「아니오」로 읽는다 — 지우기가 거절도
  // 실패도 아닌 채로 조용히 아무 일도 안 하는 단추가 된다.
  {
    const m = readFileSync(new URL('../src/main.js', import.meta.url), 'utf8');
    const v = readFileSync(new URL('../src/ui/view.js', import.meta.url), 'utf8');
    // ⚠ **부르는 꼴로 잰다.** 이름만 찾으면 그걸 왜 안 쓰는지 적은 주석이 걸린다 — 오늘
    // 이 파일에서 같은 덫을 세 번째로 밟았다.
    ok('브라우저 판정에 안 맡긴다', !/globalThis\.confirm\?\.\(|\bwindow\.confirm\(/.test(m + v));
    ok('지우기가 판 안의 물음을 지난다', /view\.ask\('delete-guide'/.test(m));
    // 글을 못 지으면 **묻지 않고 거절한다** — 참을 돌려주면 안 물어보고 지운다.
    ok('물을 말이 없으면 거절이다', /if \(!box \|\| !text\) return Promise\.resolve\(false\)/.test(v));
    // 포커스는 **덜 위험한 쪽**에 준다. 판이 뜨자마자 엔터를 치는 손이 지우면 안 된다.
    ok('포커스가 그만두는 쪽에 간다', /cancel\.focus\(\)/.test(v));
    ok('Escape 도 그만두는 쪽이다', /Escape' \) *\{? *done\(false\)|Escape'\) done\(false\)/.test(v.replace(/\s+/g, ' ')) || /if \(e\.key === 'Escape'\) done\(false\)/.test(v));
    // 되돌릴 수 없는 일이므로 **끄는 길이 옆에 있다**를 같이 말한다. 그 말이 없으면 「잠깐
    // 안 쓰려고」 지우는 사람이 생기고, 그건 되돌릴 곳이 없다.
    const t = confirmAsk('delete-guide', 'design-guide');
    ok('무엇을 지우는지 이름을 댄다', t.head.includes('design-guide'));
    ok('되돌릴 수 없다고 적는다', t.body.includes('되돌릴 수 없습니다'));
    ok('끄는 길을 같이 권한다', t.body.includes('스위치를 끄'));
    ok('모르는 물음은 안 짓는다', confirmAsk('무엇인지 모름', 'x') === null);
    // 다이얼로그의 손은 **글자 단추**다(M3: action = label-large). 아이콘 둘로 두면 되돌릴 수
    // 없는 쪽과 그만두는 쪽이 그림으로 안 갈린다.
    ok('손이 글자다', /id="confirm-ok"[^>]*class="text-btn/.test(html) && /id="confirm-cancel"[^>]*class="text-btn/.test(html));
    ok('낭독기에 경고 판이라고 말한다', /role="alertdialog"[\s\S]{0,120}aria-modal="true"/.test(html));
  }
  {
    const paneCss = readFileSync(new URL('../taskpane.css', import.meta.url), 'utf8');
    // 값은 M3 basic dialog 에서 옮긴 것이다.
    const box = /\.confirm-box \{([^}]*)\}/.exec(paneCss)?.[1] ?? '';
    ok('묻는 판 규칙을 찾았다', box !== '');
    ok('모서리가 28 이다', /border-radius:\s*28px/.test(box), box.trim());
    ok('패딩이 24 다', /padding:\s*24px/.test(box), box.trim());
    ok('폭 천장이 560 이다', /max-width:\s*560px/.test(box), box.trim());
    ok('버튼 사이가 8 이다', /\.confirm-row \{[^}]*gap:\s*8px/.test(paneCss));
    // 덮는 판이 상자를 못 덮으면 뒤가 눌린다.
    ok('덮을 상자가 기준점이다', /#pane \{[^}]*position:\s*relative/.test(paneCss));
  }

  // ── 아래에서 연 것은 **화면 안에** 선다 ──────────────────────
  //
  // 이 판들은 스크롤 영역 **안**에 서는데 그것을 여는 손은 화면 아래(브랜드 줄 → `⋯`)에 있다.
  // 대화가 길면 판이 저 위에 열리고, 사람이 보기에는 **단추를 눌렀는데 아무 일도 안 일어난
  // 것**이다. 실물에서 두 번 봤다(2026-09-04: 편집 칸 `top` 803 / 창 673, 그리고 명단).
  //
  // ⚠ **이름을 대지 않고 자리를 센다.** 처음엔 「늘 지킬 것」과 「가이드」만 이름 대어 고쳤고,
  // **명단을 빠뜨린 것을 사람이 발견했다**. 이름을 대는 검사는 안 댄 자리를 못 본다 — 그래서
  // 여기서는 판을 여는 **모든 자리**를 세고, 하나라도 안 데려오면 운다.
  {
    const m = readFileSync(new URL('../src/main.js', import.meta.url), 'utf8');
    // 스크롤 영역 안에 서는 판들. 이 밖의 것(`advanced` 등)은 고정 줄이라 데려올 것이 없다.
    const inScroll = ['pick', 'rules-panel', 'guides-panel', 'guides-edit'];
    const markup = readFileSync(new URL('../taskpane.html', import.meta.url), 'utf8');
    const scroll = markup.slice(markup.indexOf('id="scroll"'), markup.indexOf('/#scroll'));
    ok('스크롤 영역을 찾았다', scroll.length > 200, String(scroll.length));
    ok('세는 판이 전부 스크롤 안에 있다',
      everyOf(inScroll, (id) => scroll.includes(`id="${id}"`)),
      inScroll.filter((id) => !scroll.includes(`id="${id}"`)).join(', '));
    // 판을 펴는 자리 = `<무엇>.hidden = false`. **그 뒤에 같은 것을 데려오는 줄**이 와야 한다 —
    // 이름이 같은지까지 본다. 아무 `reveal` 이나 세면 옆 판을 데려오는 줄이 이 자리를 덮는다.
    const opens = [...m.matchAll(/([A-Za-z$][\w$]*)\.hidden = false/g)]
      .map((x) => ({ who: x[1], after: m.slice(x.index, x.index + 220) }));
    ok('판을 여는 자리를 실제로 찾았다', opens.length >= 4, String(opens.length));
    ok('여는 자리마다 그 판을 데려온다',
      everyOf(opens, (o) => o.after.includes(`view.reveal(${o.who})`)),
      opens.filter((o) => !o.after.includes(`view.reveal(${o.who})`)).map((o) => o.who).join(' | '));
    // 명단은 그 중에서도 **가장 먼저** 열리는 판이라 따로 못 박는다 — 붙기 전 화면이다.
    ok('명단도 데려온다', /pick\.render\(list\)[\s\S]{0,120}view\.reveal/.test(m));
  }

  // ── 이름을 두 번 안 적는다 ───────────────────────────────────
  //
  // 작업창 머리는 PowerPoint 가 그리고 거기 애드인 이름이 이미 서 있다. 그 아래 한 줄 더
  // 적으면 같은 말이 두 번이고, 348×391 에서 그 줄은 대화에서 빼 온 것이다.
  {
    const head = /<header[^>]*>([\s\S]*?)<\/header>/.exec(html)?.[1] ?? '';
    ok('머리 줄을 찾았다', head !== '');
    ok('머리에 이름을 안 적는다', !/MAGI/.test(head), head.trim().slice(0, 60));
    // 남은 말은 **예상 밖일 때만** 서는 것 하나뿐이라, 그것이 없으면 줄도 없어야 한다 —
    // 빈 테두리 한 줄이 28px 를 먹는다.
    ok('할 말이 없으면 줄도 없다', /id="head"[^>]*hidden/.test(html));
    ok('브랜드는 아래에 남는다', /<footer class="brand">[\s\S]*?MAGI/.test(html));
  }
  {
    const v = readFileSync(new URL('../src/ui/view.js', import.meta.url), 'utf8');
    ok('말이 없으면 줄을 지운다', /head\.hidden = label === ''/.test(v));
  }
    ok('누르면 명단이 선다', /#repick[\s\S]{0,120}showCompanions\(true\)/.test(src));
    ok('붙고 나면 그 줄이 뜬다', /pick\.hide\(\);\s*\n\s*offerRepick\(true\)/.test(src));
    ok('명단이 떠 있는 동안에는 안 뜬다', /pick\.render\(list\); offerRepick\(false\)/.test(src));
  }

  ok('고르는 길이 남아 있다', /api\.choose\(/.test(src) && /mountPick\(/.test(src));

  // **붙어 있으면 고르는 화면을 접는다.** 실물에서 봤다(2026-09-02): 브랜드 줄은 「대화
  // 연결됨」인데 위쪽에는 「어느 컴패니언에 붙일까요」가 떠 있었다 — 다 된 화면이 아직 뭔가
  // 해야 한다고 말하는 꼴이다. 고르고 붙은 길은 접는데 **물려받은 길만** 안 접고 있었다.
  {
    const inherit = /const sock = saidStale[\s\S]*?\n        \}/.exec(src)?.[0] ?? '';
    ok('물려받는 자리가 있다', inherit !== '');
    ok('물려받아도 고르는 화면을 접는다', /pick\.hide\(\)/.test(inherit), inherit.slice(0, 80));
    ok('물려받으면 「붙어 있다」를 올린다', /setBound\(true\)/.test(inherit));
  }
}

// ── 예상 밖일 때만 적는다 ────────────────────────────────────────────────────────
//
// 작업창은 348×391 이라 한 줄이 비싸다. 그런데 **줄일 수 있는 줄과 없는 줄이 갈린다**: 사람이
// 이미 보고 있는 것을 되풀이하는 줄은 지워도 되고, 사람이 달리 알 길이 없는 줄은 못 지운다.
{
  ok('진짜 호스트면 어댑터 이름을 안 적는다',
    adapterText({ label: 'PowerPoint (Office.js)', isHost: true }) === '');
  ok('가짜 덱은 반드시 적는다',
    adapterText({ label: '가짜 덱 (PowerPoint 없이)', isHost: false }) === '가짜 덱 (PowerPoint 없이)');
  // 모르는 어댑터를 진짜로 읽으면 그 화면은 조용히 진짜인 척한다.
  ok('모르는 어댑터도 적는다', adapterText({}) === 'unknown');
  ok('어댑터가 없어도 안 터진다', adapterText(null) === 'unknown');

  ok('안 붙었으면 「시키면 된다」를 안 적는다', readyText(null, 0) === '');
  ok('붙었고 대화가 비면 적는다', readyText('deck2', 0).includes('바로 시키시면'));
  // 첫 줄이 서는 순간 그 문장은 증명된 것이라 자리만 먹는다.
  ok('대화가 서면 사라진다', readyText('deck2', 1) === '');
}

// ── 가이드 판 ────────────────────────────────────────────────────────────────────
{
  const g = guideBoard({ guides: [
    { name: 'design-guide', description: '사내 표준', enabled: true, chars: 1200 },
    { name: 'deck-design', description: '', enabled: false, chars: 4000 },
  ] });
  ok('켜진 수를 센다', g.headText === '가이드 2벌 · 켜짐 1', g.headText);
  // 꺼 둔 것이 사라지면 다시 켤 길이 없다.
  ok('꺼 둔 것도 목록에 남는다', g.rows.length === 2 && g.rows[1].enabled === false);
  ok('꺼짐/켜짐을 글로 적는다', g.rows[0].toggleText === '켜짐' && g.rows[1].toggleText === '꺼짐');
  // **아이콘 단추의 툴팁은 동작을 적는다 — 아이콘 이름이 아니라**(M3 icon-buttons).
  ok('툴팁이 동작을 적는다', g.rows[0].toggleTip.includes('끕니다') && g.rows[1].toggleTip.includes('켭니다'),
    g.rows[0].toggleTip);
  ok('지우기는 되돌릴 수 없다고 적는다', g.rows[0].deleteTip.includes('되돌릴 수 없습니다'));
  // 켜짐/꺼짐을 **색 하나로 말하지 않는다** — 글리프가 갈린다.
  ok('상태를 글리프로도 가른다', g.rows[0].toggleIcon === '◉' && g.rows[1].toggleIcon === '○');
  // 설명은 모델이 부를지 정하는 글이라, 비어 있다는 것 자체가 고칠 거리다.
  ok('설명이 없으면 없다고 적는다', g.rows[1].descMissing && g.rows[1].descText.includes('설명이 없습니다'));

  ok('하나도 없으면 그렇게 적는다', guideBoard({ guides: [] }).headText === '아직 가이드가 없습니다');
  // 「아직 없다」와 「못 읽었다」는 다른 말이다.
  const bad = guideBoard({ error: '권한이 없습니다' });
  ok('못 읽은 것은 사유와 함께', bad.failed && bad.note === '권한이 없습니다');
  // 적어 두고 다 꺼 놓으면 모델은 아무것도 안 읽는다 — 흔한 상태라 화면이 말해야 한다.
  const allOff = guideBoard({ guides: [{ name: 'a', description: 'x', enabled: false, chars: 1 }] });
  ok('전부 꺼진 것을 말한다', allOff.note.includes('아무것도 안 읽습니다'));
}

// ── 계획 판 ──────────────────────────────────────────────────────────────────────
{
  const b1 = planBoard([
    { content: '레이아웃 확인', status: 'completed' },
    { content: '자료 조사', status: 'in_progress' },
    { content: '장 만들기', status: 'pending' },
  ]);
  ok('머리에 진척을 적는다', b1.headText === '계획 1/3', b1.headText);
  // 목록을 접어 둬도 이 한 줄은 남아야 「멈춘 것」과 「도는 중」이 갈린다.
  ok('지금 하는 것을 머리에 적는다', b1.doneText === '자료 조사', b1.doneText);
  ok('상태를 부호로 가른다', b1.rows.map((r) => r.mark).join('') === '✓▸·', b1.rows.map(r=>r.mark).join(''));
  ok('pending 은 아는 상태다', b1.rows[2].known === true);

  // 계획이 없으면 판이 아예 안 선다 — 빈 판은 「계획을 못 읽었다」처럼 보인다.
  ok('없으면 안 그린다', planBoard([]).hidden === true && planBoard(undefined).hidden === true);

  // **다 끝나면 사라진다.** 접는 것으로는 모자란다 — 접힌 판도 이 크기에서 한 줄을 계속 먹는다.
  ok('다 끝나면 사라진다', planBoard([{ content: 'A', status: 'completed' }]).hidden === true);
  // 취소도 끝이다 — 「할 일이 남았는가」에 답하는 판이다.
  ok('취소도 끝으로 센다', planBoard([{ content: 'A', status: 'cancelled' }]).hidden === true);
  // 하나라도 남아 있으면 **끝까지 서 있어야 한다.**
  ok('하나 남으면 계속 선다',
    planBoard([{ content: 'A', status: 'completed' }, { content: 'B', status: 'pending' }]).hidden === false);

  // **모르는 상태를 완료로 읽지 않는다** — 지어내면 다 된 것처럼 보인다.
  const odd = planBoard([{ content: 'A', status: 'blocked' }]);
  ok('모르는 상태는 완료가 아니다', odd.rows[0].mark === '·' && odd.rows[0].known === false);
  ok('모르는 상태는 완료로 안 세어진다', odd.headText === '계획 0/1', odd.headText);
}

// 계획은 **쌓지 않고 갈아 끼운다** — 계약이 매번 전량이다.
{
  const port = new FakeTranscript({ A: [] });
  const read = new ReadTranscript(port);
  read.attach('A');
  port.push({ seq: 1, sessionId: 'A', type: 'todos.changed', data: { todos: [{ content: 'A', status: 'pending' }] } });
  port.push({ seq: 2, sessionId: 'A', type: 'todos.changed', data: { todos: [{ content: 'A', status: 'completed' }, { content: 'B', status: 'pending' }] } });
  ok('마지막 계획만 남는다', read.view.todos.length === 2 && read.view.todos[0].status === 'completed');
  // 판으로 그리는 것은 **대화 줄이 아니다.**
  ok('계획은 대화 줄로 안 선다', read.view.rows.length === 0);
  // 그리고 「그릴 줄 모르는 것」으로도 세면 안 된다 — 그건 고칠 것이 있다는 뜻의 줄이다.
  ok('모르는 것으로 안 센다', !read.view.unknownNote);
  // 모양이 달라진 것으로 있던 계획을 덮지 않는다.
  port.push({ seq: 3, sessionId: 'A', type: 'todos.changed', data: {} });
  ok('빈 모양은 계획을 안 지운다', read.view.todos.length === 2);
}

// ── 인자는 대화 줄에 안 적고, 물음에는 다 적는다 ────────────────────────────────
//
// 좁은 판(348×391)에서 호출 하나가 예닐곱 줄을 먹었다. 무엇이 바뀌었는지는 결과가 한국어로
// 적으므로 인자는 같은 말을 기계 모양으로 한 번 더 하는 것이다. **다만 물음은 다르다** —
// 누르기 전에는 결과가 없고, 그때가 사람이 무엇을 허락하는지 알 수 있는 유일한 순간이다.
{
  const src = readFileSync(new URL('../src/ui/view.js', import.meta.url), 'utf8');
  // 머리 자체가 손잡이라 접혀 있을 때 자리를 안 먹는다.
  ok('도구 줄이 인자를 접어 둔다', /turn-fold[\s\S]*?createElement\('summary'\)/.test(src));
  // **기본이 접힘이다** — 열어 두면 줄인 뜻이 없다.
  ok('기본은 접힘이다', !/fold\.open\s*=\s*true/.test(src));
  // 접힌 것을 색이 아니라 글리프로 가른다(못 가리는 사람이 있다).
  const paneCss = readFileSync(new URL('../taskpane.css', import.meta.url), 'utf8');
  ok('접힘을 글리프로 알린다', /\.turn-fold > summary::after/.test(paneCss));

  // **아이콘 줄은 양쪽정렬이되 간격에 바닥이 있다.** `space-between` 만 두면 판이 좁아질 때
  // 간격이 0 으로 가고, 32짜리 단추의 48 타겟이 서로 겹쳐 가장자리를 누른 사람이 옆 단추를 누른다.
  const row = /\.advanced \{([^}]*)\}/.exec(paneCss)?.[1] ?? '';
  ok('아이콘 줄 규칙을 찾았다', row !== '');
  ok('양쪽정렬이다', /justify-content:\s*space-between/.test(row), row.trim());
  ok('간격에 바닥이 있다', /gap:\s*16px/.test(row), row.trim());

  // **필드+단추 한 줄은 필드 높이의 세로 가운데다 — 바닥정렬 금지.** 이 집의 규칙이라
  // 가이드에는 없다. 여러 줄을 적어 칸이 늘어나도 단추가 아래로 끌려가면 안 된다.
  const composer = /\.buttons \{([^}]*)\}/.exec(paneCss)?.[1] ?? '';
  ok('컴포저 줄 규칙을 찾았다', composer !== '');
  ok('세로 가운데다', /align-items:\s*center/.test(composer), composer.trim());
  ok('바닥정렬이 아니다', !/align-items:\s*(flex-)?end|baseline/.test(composer), composer.trim());
  // 셋이 한 줄에 있어야 그 정렬이 뜻을 갖는다.
  const html2 = readFileSync(new URL('../taskpane.html', import.meta.url), 'utf8');
  // **컴포저 줄부터 컴포저 끝까지**를 잡는다. 안쪽에 상자가 생기면(`.btn-col`) 게으른 `</div>`
  // 는 거기서 멈추고, 그러면 이 검사는 「입력칸이 없다」고 **틀린 이유로 붉어진다** — 실제로
  // 그랬다(2026-09-04). 닫는 짝을 세지 않을 것이면 **바깥 경계를 이름으로** 잡는다.
  const line = /<div class="buttons">([\s\S]*?)<\/div>\s*<\/div><!-- \/composer -->/.exec(html2)?.[1] ?? '';
  ok('컴포저 줄을 찾았다', line !== '');
  ok('인용·입력칸·보내기가 한 줄이다',
    /id="quote"/.test(line) && /<textarea/.test(line) && /id="send"/.test(line), line.slice(0, 60));
  // **검토는 인용 아래다** — 한 줄에 넷이면 입력칸이 220px 에서 172px 로 준다.
  const col = /<div class="btn-col">([\s\S]*?)<\/div>/.exec(line)?.[1] ?? '';
  ok('집어 오는 손 둘이 한 칸에', /id="quote"/.test(col) && /id="review"/.test(col), col.slice(0, 60));
  ok('그 칸은 세로다', /\.btn-col \{[^}]*flex-direction:\s*column/.test(paneCss));
  ok('입력칸과 보내기는 그 칸 밖이다', !/<textarea/.test(col) && !/id="send"/.test(col));

  // **계획은 반대다 — 기본이 펼침이다.** 도는 동안 계속 보는 값이라 접어 두면 답을 못 본다.
  ok('계획은 펴진 채로 선다', /if \(!this\.planShown\)[\s\S]{0,80}el\.open = true/.test(src));
  // 다만 **처음 설 때 한 번만** — 매 변화마다 열면 사람이 접어 둔 것이 토큰마다 다시 열린다.
  ok('계획을 매번 다시 펴지 않는다', /this\.planShown = true/.test(src));
  // 계획이 끝나면 그 기억도 지운다 — 다음 계획은 다시 펴져야 한다.
  ok('다음 계획은 다시 펴진다', /this\.planShown = false/.test(src));

  // ── 계획 목록이 **바뀐 자리를 따라간다** ──────────────────────
  //
  // 목록은 96px 이라 항목이 예닐곱을 넘으면 지금 도는 것이 밖으로 밀린다. 그러면 판을 펴 두고도
  // 「지금 어디까지 왔나」를 못 본다 — 이 판이 존재하는 이유가 그 물음이다.
  {
    const b3 = planBoard([
      { content: '자료 찾기', status: 'completed' },
      { content: '뼈대 잡기', status: 'in_progress' },
      { content: '차트 넣기', status: 'pending' },
    ]);
    const a3 = planAnchor(b3);
    ok('도는 것을 잡는다', a3.index === 1, JSON.stringify(a3));
    // 하나를 끝내고 다음을 아직 안 고른 사이가 실제로 있다(`doneText` 가 그 자리를 적는다).
    // 그때 방금 바뀐 자리는 **끝난 쪽**이다.
    const b4 = planBoard([
      { content: 'a', status: 'completed' },
      { content: 'b', status: 'completed' },
      { content: 'c', status: 'pending' },
    ]);
    ok('도는 것이 없으면 마지막으로 끝난 것', planAnchor(b4).index === 1, JSON.stringify(planAnchor(b4)));
    const b5 = planBoard([{ content: 'a', status: 'pending' }, { content: 'b', status: 'pending' }]);
    ok('아무것도 안 끝났으면 첫 줄', planAnchor(b5).index === 0);
    ok('계획이 없으면 잡을 자리도 없다', planAnchor({ rows: [] }) === null);
    // **키가 요점이다.** 로그는 글자 한 조각마다 뛰므로, 매번 끌면 사람이 목록을 제 손으로
    // 넘겨 볼 수가 없다. 같은 상태면 같은 키라야 안 끈다.
    ok('안 바뀌었으면 같은 키', planAnchor(b3).key === planAnchor(planBoard([
      { content: '자료 찾기', status: 'completed' },
      { content: '뼈대 잡기', status: 'in_progress' },
      { content: '차트 넣기', status: 'pending' },
    ])).key);
    // 항목이 **제자리에서 고쳐 쓰이는** 경우가 있다 — 자리가 같아도 글이 다르면 다른 줄이다.
    const b6 = planBoard([
      { content: '자료 찾기', status: 'completed' },
      { content: '뼈대 다시 잡기', status: 'in_progress' },
      { content: '차트 넣기', status: 'pending' },
    ]);
    ok('글이 바뀌면 다른 키', planAnchor(b3).key !== planAnchor(b6).key);
  }
  ok('바뀐 자리로만 민다', /at\.key !== this\.planAt/.test(src));
  // **`scrollIntoView` 를 안 쓴다.** 그건 조상 스크롤러까지 민다 — 여기서 그 조상은 대화
  // 영역이라, 계획 한 줄을 보이게 하려다 **읽던 대화가 튄다.** 방금 고친 바닥 고정과 정면으로
  // 부딪히는 동작이다.
  // ⚠ **금지가 아니라 자리를 잰다.** 물음 판은 스스로 보이게 하려고 `scrollIntoView` 를 쓰고
  // 있고 그건 정당하다(막힌 데몬의 물음은 보여야 한다). 여기서 막는 것은 **이 함수 안**이다.
  // 그리고 이름이 아니라 **부르는 꼴**로 재는데, 이름만 찾으면 그걸 왜 안 쓰는지 적은 바로 위
  // 주석이 걸린다 — 오늘 그렇게 한 번 붉었다(이 파일에서 같은 덫을 두 번째로 밟았다).
  {
    const body = /scrollWithin\(box, el\) \{([\s\S]*?)\n  \}/.exec(src)?.[1] ?? '';
    ok('상자 안에서만 미는 함수를 찾았다', body !== '');
    ok('조상 스크롤을 안 건드린다', !/\.scrollIntoView\(/.test(body), body.trim().slice(0, 60));
    ok('미는 것은 그 상자다', /box\.scrollTop =/.test(body));
  }
  // `offsetTop` 은 offsetParent 기준이다 — 상자가 그게 아니면 **판 전체 기준**이 되어 엉뚱한
  // 데로 민다. 그래서 `position` 이 계약이다.
  ok('계획 목록이 offsetParent 다', /\.plan-list \{[^}]*position:\s*relative/.test(paneCss));
  ok('스크롤 영역도 offsetParent 다', /#pane > \.scroll \{[^}]*position:\s*relative/.test(paneCss));

  // ── 바닥 고정은 **모든 판이 선 뒤에** ────────────────────────
  //
  // 재고 붙이는 것이 `renderRows` 안에 있던 시절, 그 뒤에 서는 계획 판이 `#scroll` 을 줄이면
  // 붙여 둔 바닥이 그대로 풀렸다 — 실물에서 그 화면을 봤다(2026-09-04). 순서를 지키라고
  // 적는 것보다 **뒤에 못 오게 만드는 것**이 싸므로, 감싸는 모양으로 두고 그것을 잰다.
  ok('바닥 고정이 감싸는 모양이다', /keepingEnd\(draw\) \{[\s\S]{0,200}const stick = this\.atEnd\(\);[\s\S]{0,120}draw\(\);[\s\S]{0,120}if \(stick\) this\.toEnd\(\);/.test(src));
  ok('줄 그리기는 더 이상 스스로 안 붙인다',
    !/renderRows\(rows\) \{[\s\S]{0,600}?scrollTop =/.test(src));
  {
    // **감싼 것 안에 판이 다 들어 있는가.** 하나라도 밖으로 나가면 그것이 선 뒤에 바닥이
    // 안 붙는다 — 오늘 고친 것이 정확히 그 모양이었다.
    const wrap = /this\.keepingEnd\(\(\) => \{([\s\S]*?)\}\);/.exec(src)?.[1] ?? '';
    ok('감싼 자리를 찾았다', wrap !== '');
    const panels = ['renderStream', 'renderRows', 'renderPlan', 'renderReady', 'renderUnknown',
      'renderAdviceFrom', 'renderSent'];
    ok('높이를 바꾸는 판이 전부 그 안에 있다',
      everyOf(panels, (n) => wrap.includes(`this.${n}(`)),
      panels.filter((n) => !wrap.includes(`this.${n}(`)).join(', '));
    // 물음 판도 `#scroll` 안에 선다 — 서고 나면 바닥이 밀린다.
    ok('물음 판도 같이 감싼다', /renderAsk\(\) \{ this\.keepingEnd\(\(\) => this\.drawAsk\(\)\); \}/.test(src));
  }

  // ── 「지금 이 장을 봐 달라」 ──────────────────────────────────
  //
  // 모델은 장을 **번호로** 짚는다(`read_slide {"slide": 5}`). 번호가 없으면 이 부탁은 가리키는
  // 데가 없는 말이라, 「지금 보고 있는 장」 따위로 갈음하지 않는다 — 그 말을 받은 모델은 자기가
  // 마지막으로 만진 장을 고르고 그건 사람이 보는 장이 아니다.
  ok('번호가 있으면 그 번호로 짓는다', reviewAsk({ slideNo: 5 }).text.startsWith('5번 슬라이드'));
  ok('번호가 없으면 안 짓는다', reviewAsk({ slideNo: null }).text === '');
  ok('안 지은 이유를 적는다', reviewAsk({ slideNo: null }).note.includes('못 읽었습니다'));
  ok('선택 자체가 없어도 안 터진다', reviewAsk(undefined).text === '');
  ok('0번은 없는 번호다', reviewAsk({ slideNo: 0 }).text === '');
  ok('번호가 정수가 아니면 안 짓는다', reviewAsk({ slideNo: 2.5 }).text === '');
  // **고치라고는 안 시킨다.** 검토를 부탁했는데 덱이 바뀌어 있으면 그건 검토가 아니다.
  ok('보기부터 시킨다', reviewAsk({ slideNo: 3 }).text.includes('제가 시킨 뒤에'));
  // **적던 글을 안 지운다.** 단추 하나가 쓰던 문단을 날리면 그 단추는 다시 안 눌린다.
  ok('빈 칸이면 그대로 넣는다', appendAsk('', 'X') === 'X');
  ok('공백만 있어도 그대로 넣는다', appendAsk('  \n ', 'X') === 'X');
  ok('적던 글 아래에 붙인다', appendAsk('색 대비를 봐 줘', 'X') === '색 대비를 봐 줘\nX');
  ok('적던 글을 안 지운다', appendAsk('내 글', 'X').startsWith('내 글'));
  ok('붙일 것이 없으면 그대로 둔다', appendAsk('내 글', '') === '내 글');
  // **안 보낸다 — 채워만 둔다.** 누름 하나로 나가면 사람이 안 읽은 말이 나간다.
  ok('검토는 컴포저를 채우기만 한다',
    /async onReview\(\)[\s\S]{0,700}input\.value = appendAsk/.test(src)
    && !/async onReview\(\)[\s\S]{0,700}this\.sendTurn/.test(src));
  // 그리고 물음은 그대로 다 편다.
  ok('물음은 인자를 다 편다', argsText({ args: { slide: 3, text: 'x' } }).includes('"slide": 3'));
  ok('물음의 인자 칸이 뷰에 서 있다', /argsText\(slot\)/.test(src));
}

// ── 물음은 「모르는 것」이 아니다 ────────────────────────────────────────────────
//
// 물음 판이 `status` 를 폴해서 그린다(매뉴얼 §7.1) — 대화 스트림으로 오는 같은 사건은 여기서
// 그릴 것이 아니다. 앞 판본은 그것을 `unknown` 으로 세어 화면 아래에 「이 창이 아직 그릴 줄
// 모르는 이벤트 2건 — permission.requested」를 띄웠다. 그 줄의 뜻은 **「이 창을 고쳐야 한다」**
// 인데 고칠 것이 없었다. 그런 줄이 늘 떠 있으면 진짜로 못 그리는 것이 왔을 때 같이 안 읽힌다.
{
  const port = new FakeTranscript({ A: [] });
  const read = new ReadTranscript(port);
  read.attach('A');
  port.push({ seq: 0, sessionId: 'A', type: 'permission.requested', data: { callId: 'c1' } });
  port.push({ seq: 0, sessionId: 'A', type: 'question.requested', data: {} });
  ok('물음을 「모른다」고 안 적는다', !read.view.unknownNote, read.view.unknownNote ?? '(없다)');
  ok('물음이 대화 줄로도 안 선다', read.view.rows.length === 0);
  // 그리고 **자리 없는 사건이라 커서를 안 민다**(§5.7 — `seq > 0` 만 민다). 붙을 때의 커서는
  // -1(전량)이고, 자리 없는 사건이 지나가도 그대로여야 한다 — 0 으로 밀리면 그건 이 문의
  // 계약에서 「전부 다시」라 화면이 두 벌이 된다.
  ok('자리 없는 물음은 커서를 안 민다', read.cursor.seq === -1, String(read.cursor.seq));
}

// ── 판본이 낀 주소 ───────────────────────────────────────────────────────────────
//
// Office 는 작업창 자산을 자기 캐시에 물고 그 캐시는 헤더로 못 끈다(TESTING §5.1.3 실측).
// 캐시가 주소로 도는 이상 답은 주소를 바꾸는 것이고, `?v=` 로는 안 된다 — 페이지가 부르는 것은
// 진입점 하나뿐이고 나머지 모듈 스무 개는 그 안의 `import` 로 오므로 옛 주소 그대로 온다.
{
  const html = readFileSync(new URL('../taskpane.html', import.meta.url), 'utf8');
  // 페이지는 상대 주소를 그대로 적는다 — 판본은 **헬퍼가 낀다**(page.go 의 versionAssets).
  ok('진입점이 하나다', (html.match(/<script type="module"/g) ?? []).length === 1);
  ok('진입점 주소가 상대다', /src="src\/main\.js"/.test(html));
  ok('스타일 주소가 상대다', /href="taskpane\.css"/.test(html));
  // 모듈이 서로를 상대 경로로 부르는 것이 이 수가 서는 이유다: 접두사 하나가 전부에 걸린다.
  const main = readFileSync(new URL('../src/main.js', import.meta.url), 'utf8');
  ok('모듈은 상대 경로로 서로를 부른다', /from '\.\//.test(main));
  ok('절대 주소로 우리 모듈을 안 부른다', !/from '\/(src|ui)/.test(main));
}

// ── 아이콘 단추는 반드시 말을 단다 ──────────────────────────────────────────────
//
// 아이콘만 두면 무슨 단추인지 모른다. M3 가 그것을 거동으로 못 박았다 — 「hover 에 **동작을
// 설명하는** 툴팁을 띄운다, **아이콘의 이름이 아니라**」. 낭독기에는 `aria-label` 이 그 몫이다.
{
  const html = readFileSync(new URL('../taskpane.html', import.meta.url), 'utf8');
  const btns = [...html.matchAll(/<button[^>]*class="[^"]*icon-btn[^"]*"[^>]*>/g)].map((m) => m[0]);
  // **훑을 것을 실제로 찾았는가** — 0개를 훑고 초록인 것과 다 통과한 것은 글자가 같다(§9).
  ok('아이콘 단추를 찾았다', btns.length >= 8, String(btns.length));
  // `everyOf` 는 **빈 것에 참을 안 준다**(§4.1) — `every` 로 적으면 0개를 훑고도 초록이다.
  ok('전부 툴팁이 있다', everyOf(btns, (b) => /title="[^"]{4,}"/.test(b)),
    btns.find((b) => !/title="[^"]{4,}"/.test(b)) ?? '');
  ok('전부 낭독기 이름이 있다', everyOf(btns, (b) => /aria-label="[^"]{4,}"/.test(b)),
    btns.find((b) => !/aria-label="[^"]{4,}"/.test(b)) ?? '');
  // 스프라이트는 파일 안에 있다 — 남의 주소에서 아이콘을 부르면 LNA·혼합 콘텐츠가 다시
  // 걸린다(§5.5). **글자가 아니라 주소를 센다**: 이 파일에는 그 이유를 적은 주석도 있어서
  // 낱말로 재면 제 주석에 제가 걸린다(실제로 걸렸다).
  const outside = [...html.matchAll(/(?:src|href)="(https?:\/\/[^"]+)"/g)].map((m) => m[1]);
  ok('아이콘이 파일 안에 있다', /<svg width="0"/.test(html));
  ok('밖에서 받아오는 것은 office.js 뿐이다',
    everyOf(outside, (u) => u.startsWith('https://appsforoffice.microsoft.com/')), outside.join(' '));

  // **권한 단추는 아이콘으로 안 바꾼다.** 여는 폭을 문구에 적어야 하는 자리라(§8), 「이번
  // 호출만」과 「이 세션의 set_text 전부」가 아이콘으로는 안 갈린다.
  const view = readFileSync(new URL('../src/ui/view.js', import.meta.url), 'utf8');
  ok('권한 단추는 글자로 남는다', /askAction/.test(view) && !/askAction[\s\S]{0,200}icon\(/.test(view));
}

// ── 답도 접힌다 ─────────────────────────────────────────────────────────────────
//
// 실물 스크린샷에서 판이 답으로 찼다(2026-09-04): `read_slide` 한 번의 JSON 이 `revision`·
// `shapes` 를 펴서 화면을 덮었다. 인자는 접어 뒀는데 **답은 안 접고 있었다.**
//
// 늘 보여야 하는 것은 **됐는가**(머리줄의 ✓/✗/⚠)이고, **무엇이 어떻게 됐는가**는 여는 값이다.
{
  const view = readFileSync(new URL('../src/ui/view.js', import.meta.url), 'utf8');
  const block = /if \(shape === 'tool' && r\.kind === 'tool'\) \{([\s\S]*?)\n      return el;/.exec(view)?.[1] ?? '';
  ok('도구 줄 렌더러를 찾았다', block !== '');
  // **이름과 결과가 같은 줄**이다 — 손잡이가 하나여야 무엇이 어디 접혀 있는지를 안 외운다.
  ok('이름과 결과가 한 줄이다', /sum\.append\(name, mark\)/.test(block));
  // 그리고 그 한 줄을 누르면 **보낸 것과 받은 것이 같이** 펴진다.
  ok('한 손잡이가 인자와 답을 같이 편다',
    /turn-args[\s\S]*?turn-result/.test(block) && (block.match(/createElement\('details'\)/g) ?? []).length === 1);
  ok('바뀐 줄도 같이 접힌다', /fold\.append\(d\)/.test(block));
  ok('기본은 접힘이다', !/fold\.open\s*=\s*true/.test(block));
  // 답이 아직 없으면 **미리 「완료」를 적지 않는다.**
  ok('안 온 답을 지어내지 않는다', /⋯ 대기/.test(block));

  // `changed` 를 못 뽑아도 **지어내지 않는다** — 빈 배열이면 접힘만 남는다.
  ok('못 뽑으면 빈 배열', changedLines({ result: { content: '이건 JSON 이 아니다' } }).length === 0);
  ok('뽑으면 그 줄만', changedLines({ result: { content: JSON.stringify({ changed: ['슬라이드 3 · 제목'], shapes: [1, 2] }) } })
    .join('') === '슬라이드 3 · 제목');
}

// ── 도구는 사람 말로 뜬다 ────────────────────────────────────────────────────────
//
// `mcp__ppt__set_text` 가 아니라 「글 바꾸기」. 한 턴에 그 줄이 수십 개라 기계 이름을 그대로
// 내면 사람이 매번 번역해서 읽는다.
{
  ok('덱 도구는 사람 말로', toolLabel('mcp__ppt__set_text') === '글 바꾸기');
  ok('접두사 없이도 같은 이름', toolLabel('set_text') === '글 바꾸기');
  ok('덱 밖 도구도 사람 말로', toolLabel('websearch') === '웹 검색');
  // **모르는 것은 지어내지 않는다** — 지어낸 이름은 아는 도구와 모르는 도구를 같아 보이게 한다.
  ok('모르는 도구는 받은 이름 그대로', toolLabel('mcp__zzz__nope') === 'mcp__zzz__nope');
  ok('이름이 없으면 없다고 적는다', toolLabel('') === '(이름 없음)');

  // ⚠ **이 표는 카탈로그와 갈릴 수 있다.** 도구가 늘면 여기도 늘어야 하고, 안 늘면 새 도구만
  // 기계 이름으로 뜬다 — 조용히. 그래서 카탈로그를 읽어 빠진 것을 세운다.
  const goSrc = readFileSync(new URL('../../helper/tools.go', import.meta.url), 'utf8');
  const names = [...goSrc.matchAll(/^\t\t\tName: *"([a-z_]+)",$/gm)].map((m) => m[1]);
  ok('카탈로그를 읽었다', names.length > 30, String(names.length));
  const labelled = new Set(labelledTools());
  const missing = names.filter((n) => !labelled.has(n));
  ok('카탈로그의 모든 도구에 표시 이름이 있다', missing.length === 0, missing.join(', '));
}

// ── 목업은 「열린 덱」으로 세어지지 않는다 ──────────────────────────────────────
//
// 실물에서 값을 치렀다(2026-09-04, 웨이브 5). 브라우저로 이 페이지를 열면 덱은 가짜인데 손은
// 헬퍼에 등록됐고, 그래서 모델에게 **열린 덱이 둘**로 보였다 — 둘 중 하나가 가짜인 것을 알
// 길이 없다. 탭을 새로고침할 때마다 등록이 새 번호를 받아, 도는 모델이 방금 받은 문서 번호로
// 부를 때마다 「그런 덱은 없다」를 받았다(한 판에 여섯 번).
{
  const main = readFileSync(new URL('../src/main.js', import.meta.url), 'utf8');
  ok('손을 내놓는 자리를 찾았다', /new ServeHand\(/.test(main));
  // **진짜 호스트일 때만 내놓는다.**
  ok('목업은 손을 안 내놓는다', /if \(deck\.isHost\) \{[\s\S]{0,200}new ServeHand\(/.test(main));
  // 다만 **화면 안에서 쓰는 손은 그대로** — 제안 카드는 브라우저에서 눌러 봐야 한다.
  ok('화면은 여전히 손을 쓴다', /view\.useHand\(hand\)/.test(main));
  ok('가짜 손을 여전히 만든다', /new FakeHand\(/.test(main));
}

console.log(failed ? `\n${failed} 실패` : '\n전부 통과');
process.exit(failed ? 1 : 0);