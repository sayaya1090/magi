// PowerPoint 없이 도는 확인. `node tools/smoke.mjs`
//
// 이게 이 목업에서 **오늘 실제로 검증되는 전부**다. 유스케이스가 Office.js 를 모르기 때문에
// FakeDeck 하나만 갈아 끼우면 흐름이 끝까지 돈다.
//
// `OfficeDeck` 에서 **오늘 도는 것은 `capabilities()` 하나**다. 그건 Office.js 를 호출하지
// 않고 `isSetSupported` 가 답한 것을 나르기만 하는 함수라, 그 자리에 stub 을 세우면 나르는
// 계약(여섯을 요약 안 한다 / 던진 것을 「아니오」로 안 접는다)을 진짜로 잰다. `selection()`
// 과 `point()` 는 `PowerPoint.run` 이 필요해 **여전히 한 번도 안 돌았다** — 안 돌려 본 것을
// "된다"고 세지 않는다.
import { Composer, promptOf } from '../src/domain/Composer.js';
import { Quote } from '../src/domain/Quote.js';
import { Advice, targetLabel, SlideNumbers } from '../src/domain/Advice.js';
import { foldAdvice } from '../src/domain/AdviceBoard.js';
import { FakeDeck } from '../src/adapter/FakeDeck.js';
import { OfficeDeck } from '../src/adapter/OfficeDeck.js';
import { pickDeck, pickNote, lateNote, lateFailNote } from '../src/adapter/pickDeck.js';
import { QuoteSelection } from '../src/usecase/QuoteSelection.js';
import { SendTurn } from '../src/usecase/SendTurn.js';
import { FakeChat } from '../src/adapter/FakeChat.js';
import { PointAtAdvice } from '../src/usecase/PointAtAdvice.js';
import { readFileSync } from 'node:fs';
import { fixture } from '../src/ui/deckFixture.js';
import { headOf } from '../src/ui/view.js';
import { Transcript } from '../src/domain/Transcript.js';
import { FakeTranscript } from '../src/adapter/FakeTranscript.js';
import { ReadTranscript } from '../src/usecase/ReadTranscript.js';
import { FakeStatus } from '../src/adapter/FakeStatus.js';
import { WatchPrompt } from '../src/usecase/WatchPrompt.js';
import { DECISIONS, CLEARED } from '../src/domain/Pending.js';

let failed = 0;
const ok = (name, cond, detail = '') => {
  console.log(`${cond ? '  ok  ' : '  FAIL'} ${name}${detail ? ' — ' + detail : ''}`);
  if (!cond) failed++;
};

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
  const long = new Quote({ slideId: 's1', shapeId: 'sh1', type: 'TextBox', text: '가'.repeat(900) });
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
const good = await point.run(new Advice({ message: '여기가 넘칩니다', slideId: 's4f2a1', shapeIds: ['sh8c30'] }));
ok('가리키기 성공', good.ok === true);
const bad = await point.run(new Advice({ message: '사라진 도형', slideId: 's4f2a1', shapeIds: ['sh-없음'] }));
ok('없는 도형은 대체 없이 실패', bad.ok === false, bad.reason);

// 다른 슬라이드의 도형은 슬라이드까지 옮겨야 가리켜진다.
const cross = await point.run(new Advice({ message: '뒷장', slideId: 's7', shapeIds: ['sh7b'] }));
ok('슬라이드 이동 후 가리킨다', cross.ok === true && deck.currentSlide === 's7');

// 계측이 스스로에 대해 거짓말하지 않는가. 가짜 덱이 `measured:true` 를 내면 화면이 실측처럼
// 보이고, 그러면 §12 #4 를 답해 줄 유일한 줄이 처음부터 못 쓰게 된다.
const caps = deck.capabilities();
ok('가짜 덱은 잰 게 없다고 말한다', caps.measured === false && caps.sets.length === 0);
ok('안 쟀으면 사유가 있다', typeof caps.note === 'string' && caps.note.length > 0);


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

  // 대화가 바뀌면 커서를 버린다. **서버가 못 잡아 주는 자리**라 우리가 메운다.
  ok('대화가 바뀌면 커서를 버린다', read.attach('B') === -1);
  ok('앞 대화가 화면에 안 남는다', read.view.rows.every((r) => r.text !== '키웠습니다'));

  // 거절 프레임. 안 읽으면 보던 대화 뒤에 같은 대화의 처음이 이어 붙는다.
  const port2 = new FakeTranscript({ A: [ev(1, 'prompt.submitted', '첫 줄')] });
  const read2 = new ReadTranscript(port2);
  read2.cursor = read2.cursor.advanced('A', 40);   // 어제 커서를 들고 왔다
  read2.sessionId = 'A';
  read2.attach('A');
  ok('로그 끝을 넘은 커서는 거절당한다', read2.view.refusal !== null);
  ok('거절 뒤 화면은 한 벌뿐이다', read2.view.rows.length === 1, `${read2.view.rows.length}줄`);
  ok('거절당한 커서는 버려진다', read2.cursor.seq === 1);

  // 모르는 종류를 안 버린다. 버리면 화면이 "아무 일도 없었다"처럼 보인다.
  const port3 = new FakeTranscript({ A: [] });
  const read3 = new ReadTranscript(port3);
  read3.attach('A');
  port3.push({ seq: 1, sessionId: 'A', type: 'council.verdict', data: {} });
  port3.push({ seq: 2, sessionId: 'A', type: 'todos.changed', data: {} });
  ok('모르는 종류는 안 그려도 안 사라진다', read3.view.rows.length === 0
    && read3.view.unknownNote !== null, read3.view.unknownNote ?? '(말이 없다)');
  ok('모르는 것도 커서는 민다', read3.cursor.seq === 2);

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
    kindsAnon.length === 2 && kindsAnon.every((k) => k === 'note'), kindsAnon.join('/'));
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
  t5.append(app('m5', { kind: 'tool-result', toolResult: { callId: 'c1' } }));
  ok('못 그린 조각은 조각 이름까지 적는다',
    /part\.appended \(tool-result\)/.test(t5.unknownNote ?? ''), t5.unknownNote ?? '(없음)');
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
  ok('못 닿으면 소리 내어 말한다', w.view.lostNote !== null);
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
  ok('dial 실패도 못 닿음이다', w2.view.reachable === false && w2.view.lostNote !== null);

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
    DECISIONS.every((d) => d.width === 'call' || /세션|계속|설정/.test(d.label)),
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

  const rows = () => read.view.rows.filter((r) => r.kind === 'user').length;
  const r1 = await send.run('제목 줄여줘', { userRows: rows(), live: true });
  ok('보내면 간다', r1.sent === true && chat.sent.length === 1);
  // 여기서 화면에 미리 붙이면 로그가 같은 말을 실어 올 때 두 벌이 된다.
  ok('낸 것을 화면이 미리 안 붙인다',
    read.view.rows.filter((r) => r.kind === 'user').length === 1
    && read.view.rows[0].text.includes('제목 줄여줘'));
  ok('메아리가 오면 컴포저가 빈다',
    send.settle(rows()) === true && comp.pending.length === 0 && comp.waiting === false);

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
  const bad = { async submit() { throw new Error('문이 닫혔습니다'); } };
  const comp3 = new Composer();
  const r3 = await new SendTurn(bad, comp3).run('안 갈 말', { userRows: 0, live: true });
  ok('못 가면 사유가 온다', r3.sent === false && r3.why === 'failed');
  ok('못 갔으면 안 잠긴다', comp3.waiting === false);
}

// ── 안내는 모델의 말이 아니라 **도구 호출**이다(§6.1). 로그에서 유도하고 따로 안 쌓는다.
{
  const port = new FakeTranscript({ s: [] });
  const read = new ReadTranscript(port);
  read.attach('s');
  const chat = new FakeChat(port, { sessionId: 's', delay: -1 });
  const q = new Quote({ slideId: 's4', slideNo: 4, shapeId: 'sh1', name: '제목',
    type: 'TextBox', text: '3분기 매출 전망과 지역별 분해' });
  await chat.submit(promptOf('줄여줘', [q]));
  chat.reply('m1', promptOf('줄여줘', [q]));

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
     [asked, pending, cantNumber, gone].every((t) => t.endsWith('sh1')));

  // 안 눌리는 항목에도 **사유가 값에 실린다**. 누를 수 없으니 `PointAtAdvice` 의 사유는 영영
  // 화면에 못 온다 — 목록이 그 자리에서 적어야 하고, 두 자리가 같은 문장이어야 한다.
  const blind = new Advice({ message: '어딘지 안 실림' });
  ok('가리킬 곳이 없으면 사유가 있다', typeof blind.unpointableReason === 'string');
  ok('가리킬 수 있으면 사유가 없다', a.unpointableReason === null);
  ok('누를 때 사유와 목록의 사유가 같다',
     (await point.run(blind)).reason === blind.unpointableReason);

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
  const noNo = new FakeDeck({ slides: [{ id: 'sx', title: '번호 없음', shapes: [] }] });
  const m = await noNo.slideNumbers();
  ok('번호 없는 슬라이드는 표에 안 앉는다', m.has('sx') === false, JSON.stringify([...m]));
  ok('표의 값은 전부 숫자다', [...m.values()].every((v) => typeof v === 'number'));

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

  d.reading = true;
  const ok2 = await qs.run();
  ok('손잡이를 되돌리면 다시 읽는다', ok2.added.length === 1, JSON.stringify(ok2.reason));
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
    const want = 'PowerPointApi 1.2,PowerPointApi 1.5,PowerPointApi 1.6,'
      + 'PowerPointApi 1.7,PowerPointApi 1.8,SharedRuntime 1.1';
    const got = caps.sets.map((s) => `${s.name} ${s.version}`).join(',');
    ok('여섯을 요약 없이 그대로 돌려준다', got === want, got);
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

console.log(failed ? `\n${failed} 실패` : '\n전부 통과');
process.exit(failed ? 1 : 0);
