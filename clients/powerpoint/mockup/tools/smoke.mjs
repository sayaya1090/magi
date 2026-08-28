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
import { PointAtAdvice } from '../src/usecase/PointAtAdvice.js';
import { fixture } from '../src/ui/deckFixture.js';

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

console.log(failed ? `\n${failed} 실패` : '\n전부 통과');
process.exit(failed ? 1 : 0);
