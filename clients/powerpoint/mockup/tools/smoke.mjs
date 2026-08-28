// PowerPoint 없이 도는 확인. `node tools/smoke.mjs`
//
// 이게 이 목업에서 **오늘 실제로 검증되는 전부**다. 유스케이스가 Office.js 를 모르기 때문에
// FakeDeck 하나만 갈아 끼우면 흐름이 끝까지 돈다. OfficeDeck 은 여기 안 들어온다 — 이 머신에
// PowerPoint 가 없고, 안 돌려 본 것을 "된다"고 세지 않는다.
import { Conversation } from '../src/domain/Conversation.js';
import { Quote } from '../src/domain/Quote.js';
import { Advice } from '../src/domain/Advice.js';
import { FakeDeck } from '../src/adapter/FakeDeck.js';
import { QuoteSelection } from '../src/usecase/QuoteSelection.js';
import { SendTurn } from '../src/usecase/SendTurn.js';
import { FakeChat } from '../src/adapter/FakeChat.js';
import { PointAtAdvice } from '../src/usecase/PointAtAdvice.js';
import { fixture } from '../src/ui/deckFixture.js';
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
const conv = new Conversation();
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

  const lost = new QuoteSelection(scripted(one, none), new Conversation());
  await lost.sampleBeforeFocus();
  const rl = await lost.run();
  ok('포커스가 가져간 선택은 lostFocus',
     rl.reason === 'lostFocus' && rl.beforeCount === 1, rl.reason);

  const empty2 = new QuoteSelection(scripted(none, none), new Conversation());
  await empty2.sampleBeforeFocus();
  ok('원래 빈 선택은 none', (await empty2.run()).reason === 'none');

  // 단축키·키보드로 누르면 포인터가 단추에 들어온 적이 없다 → 앞 읽기가 아예 없다.
  const blind = new QuoteSelection(scripted(none), new Conversation());
  ok('앞 읽기가 없으면 unknown', (await blind.run()).reason === 'unknown');

  // 앞 읽기는 한 번 쓰고 버린다. 안 버리면 두 번째 누름이 낡은 값으로 lostFocus 를 지어낸다.
  const stale = new QuoteSelection(scripted(one, none, none), new Conversation());
  await stale.sampleBeforeFocus();
  await stale.run();
  ok('낡은 앞 읽기는 다음 누름에 안 샌다', (await stale.run()).reason === 'unknown');

  // 인용에 성공한 길에도 사유 칸이 있고, 거기엔 사유가 없다.
  const okrun = new QuoteSelection(scripted(one), new Conversation());
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
    const q = new QuoteSelection(laggy, new Conversation());
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

// 보내면 pending 이 턴으로 넘어간다.
conv.say('이 두 개를 한 줄로 붙여줘');
ok('보내면 pending 이 빈다', conv.pending.length === 0 && conv.turns.length === 1);
ok('턴이 인용을 들고 간다', conv.turns[0].quotes.length === 2);

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

  // 끊김. 문은 깨끗한 끝을 에러로 안 준다 — 그래서 조용한 대화와 죽은 스트림이 똑같이 생겼다.
  ok('붙어 있는 동안은 살아 있다', read3.view.live === true);
  port3.drop();
  ok('끊기면 화면이 그걸 안다', read3.view.live === false);

  // 연결이 둘이라는 사실(§5.7 — `transcript` 는 연결을 통째로 가져가므로 헬퍼는 두 번 붙는다).
  // 요청 쪽이 멀쩡히 도는 것이 스트림이 살아 있다는 증거가 아니다. 그래서 제출이 성공해도
  // `live` 가 되살아나면 안 된다 — 되살아나면 화면은 죽은 스트림을 살아 있다고 그린다.
  const chat = new FakeChat();
  const sent = await new SendTurn(chat, new Conversation()).run('제목 줄여줘');
  ok('스트림이 죽어도 제출은 간다', sent !== null && sent.text === '제목 줄여줘');
  ok('제출 성공이 스트림을 되살리지 않는다', read3.view.live === false);
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

console.log(failed ? `\n${failed} 실패` : '\n전부 통과');
process.exit(failed ? 1 : 0);
