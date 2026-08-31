// 손과 헬퍼 어댑터의 확인. `node tools/smoke-hand.mjs`
//
// `smoke.mjs` 가 **화면과 인용**을 재는 자리라면 이 파일은 **도구가 도는 길**을 잰다:
// 헬퍼가 내려보낸 조작 → 손 → 답. PowerPoint 없이 도는 것이 요점이고, 그래서 여기서 재는 것은
// 「Office.js 가 그렇게 답하는가」가 아니라 **「도구의 계약을 우리가 지키는가」**다.
//
// 갈라 둔 이유는 하나 더 있다. `smoke.mjs` 는 `src/` 전체를 훑어 마크업 싱크를 검사하는데,
// 그 훑기는 이 파일이 더하는 파일들도 **자동으로** 덮는다. 시험을 새 파일에 두는 것이 그
// 그물을 약하게 만들지 않는다.
import { FakeHand } from '../src/adapter/FakeHand.js';
import { ServeHand } from '../src/usecase/ServeHand.js';
import { HelperStream } from '../src/adapter/HelperStream.js';
import { HelperApi } from '../src/adapter/helperApi.js';
import { HelperChat, HelperStatus, HelperTranscript, pendingOf } from '../src/adapter/HelperPorts.js';
import { fixture } from '../src/ui/deckFixture.js';

let failed = 0;
const ok = (name, cond, detail = '') => {
  console.log(`${cond ? '  ok  ' : '  FAIL'} ${name}${detail ? ' — ' + detail : ''}`);
  if (!cond) failed++;
};
const threw = async (fn) => {
  try { await fn(); return null; } catch (e) { return e?.message ?? String(e); }
};

const deck = () => new FakeHand(structuredClone(fixture));

// ── 손: 계약 넷 ───────────────────────────────────────────────────────────────

{
  const hand = deck();
  const out = await hand.run('list_slides', {});
  ok('목차가 위치를 1 부터 센다', out.result.slides[0].slide === 1,
    JSON.stringify(out.result.slides.map((s) => s.slide)));
  ok('목차가 손댄 문서를 싣는다', out.document === 'doc-fake', out.document);
  ok('읽기는 개정 쌍을 안 올린다', out.count === 0, String(out.count));
}

{
  const hand = deck();
  const first = await hand.run('read_slide', { slide: 1 });
  ok('1 번은 첫 장이다', first.result.slide_id === fixture.slides[0].id, first.result.slide_id);
  const second = await hand.run('read_slide', { slide: 2 });
  ok('2 번은 둘째 장이다', second.result.slide_id === fixture.slides[1].id, second.result.slide_id);
  // 0-based 로 읽으면 여기서 갈린다. 헬퍼가 이미 0 을 거절하지만(`args.go`), 손이 1 을 첫 장으로
  // 안 보면 **모델이 보는 세상이 한 장씩 밀린다.**
  ok('못 찾는 위치는 던지고 몇 장인지 말한다',
    (await threw(() => hand.run('read_slide', { slide: 99 })))?.includes('2 장'));
  ok('못 읽는 것을 없는 것으로 안 적는다',
    first.result.unreadable.includes('notes'), JSON.stringify(first.result.unreadable));
}

{
  const hand = deck();
  const target = fixture.slides[0].shapes[0];
  const out = await hand.run('set_text', { slide: 1, shape_id: target.id, text: 'Q3 실적' });
  ok('쓰기가 바뀐 값을 스스로 싣는다',
    out.changed.length === 1 && out.changed[0].includes(target.text) && out.changed[0].includes('Q3 실적'),
    out.changed[0]);
  ok('쓰기가 개정 쌍을 올린다', out.count === 1, String(out.count));

  // **비슷한 것을 찾아 대신 고치지 않는다**(§5.8). 틀린 채로 그럴듯한 것이 제일 나쁘다.
  const why = await threw(() => hand.run('set_text', { slide: 1, shape_id: 'sh-없음', text: 'x' }));
  ok('없는 도형은 던진다', why?.includes('sh-없음'), why);
}

{
  const hand = deck();
  // 되돌리기: **복원한 슬라이드는 id 가 바뀐다**(§2.1). 그 사실이 결과에 실려야 다음 호출이
  // 낡은 id 로 가지 않는다.
  const snap = await hand.run('snapshot_slide', { slide: 1 });
  await hand.run('set_text', { slide: 1, shape_id: fixture.slides[0].shapes[0].id, text: '바꿔 둠' });
  const back = await hand.run('restore_slide', { snapshot: snap.result.snapshot });
  ok('되돌린 슬라이드는 새 id 를 실어 온다',
    back.result.slide_id !== back.result.replaced && back.result.slide_id.length > 0,
    `${back.result.replaced} → ${back.result.slide_id}`);
  ok('되돌린 것도 changed 로 말한다', back.changed[0].includes('새 id'), back.changed[0]);
  const gone = await threw(() => hand.run('restore_slide', { snapshot: 'snap-없음' }));
  ok('없는 스냅샷은 던진다', gone?.includes('snap-없음'), gone);
}

{
  const hand = deck();
  // 가짜는 **픽셀을 지어내지 않는다.** 없는 증거를 있는 척하는 것이 이 제품이 제일 피하는 것이다.
  const why = await threw(() => hand.run('render_slide', { slide: 1 }));
  ok('가짜 손은 렌더를 못 한다고 말한다', why?.includes('PowerPoint'), why);
  const unknown = await threw(() => hand.run('set_notes', {}));
  ok('모르는 조작은 던진다', unknown?.includes('set_notes'), unknown);
}

{
  const hand = deck();
  const out = await hand.run('advise', { items: [{ message: 'a', why: 'b' }] });
  // **안내는 한 일이 아니라 할 말이다**(§6.1) — `changed` 를 안 싣는 것이 계약이다.
  ok('안내는 덱을 고친 것으로 안 센다',
    out.changed.length === 0 && out.count === 0, JSON.stringify(out.changed));
}

// ── 손 노릇: 스트림에서 받아 답까지 ───────────────────────────────────────────

/** 프레임을 손으로 밀어 넣는 스트림. 시험이 시계를 안 재게. */
class ScriptedStream {
  constructor() { this.handlers = new Map(); this.document = 'doc-fake'; }
  on(kind, fn) {
    if (!this.handlers.has(kind)) this.handlers.set(kind, new Set());
    this.handlers.get(kind).add(fn);
    return () => this.handlers.get(kind).delete(fn);
  }
  push(kind, data) { for (const fn of this.handlers.get(kind) ?? []) fn(data); }
}

/** 올라간 답을 잡아 두는 가짜 API. */
class SpyApi {
  constructor({ fail = false } = {}) { this.replies = []; this.fail = fail; }
  async reply(payload) {
    if (this.fail) throw new Error('올릴 데가 없다');
    this.replies.push(payload);
  }
}

{
  const stream = new ScriptedStream();
  const api = new SpyApi();
  const hand = deck();
  const serve = new ServeHand({ stream, api, hand });
  serve.start();

  stream.push('call', { id: 'r1', op: 'list_slides', args: {} });
  await new Promise((r) => setTimeout(r, 0));
  ok('내려온 조작이 수행되고 답이 올라간다',
    api.replies.length === 1 && api.replies[0].id === 'r1'
      && Array.isArray(api.replies[0].result.slides),
    JSON.stringify(api.replies[0]?.id));
  ok('답이 손댄 문서를 싣는다', api.replies[0].document === 'doc-fake', api.replies[0].document);

  stream.push('call', { id: 'r2', op: 'render_slide', args: { slide: 1 } });
  await new Promise((r) => setTimeout(r, 0));
  const bad = api.replies.find((p) => p.id === 'r2');
  ok('실패는 사유를 실어 올린다', typeof bad?.error === 'string' && bad.error.includes('PowerPoint'),
    bad?.error);
  ok('실패에는 result 를 안 싣는다', bad?.result === undefined, JSON.stringify(bad?.result));
}

{
  // 답도 못 올리면 **조용히 넘어가지 않는다** — 그러면 모델은 60초를 기다린다.
  const stream = new ScriptedStream();
  const notes = [];
  const serve = new ServeHand({
    stream, api: new SpyApi({ fail: true }), hand: deck(), onNote: (s) => notes.push(s),
  });
  serve.start();
  stream.push('call', { id: 'r1', op: 'set_notes', args: {} });
  await new Promise((r) => setTimeout(r, 0));
  ok('실패를 알리지도 못하면 그것까지 말한다',
    notes.length === 2 && notes[1].includes('알리지도'), notes.join(' / '));
}

// ── 헬퍼 어댑터 ───────────────────────────────────────────────────────────────

/** 왕복을 잡아 두는 가짜 fetch. */
function spyFetch(answers = {}) {
  const calls = [];
  const impl = async (url, init) => {
    calls.push({ url, init });
    const path = new URL(url, 'https://x').pathname;
    const a = answers[path] ?? { status: 204 };
    return {
      ok: a.status < 400,
      status: a.status,
      headers: { get: () => (a.body === undefined ? '' : 'application/json') },
      json: async () => a.body,
      text: async () => a.text ?? '',
    };
  };
  return { impl, calls };
}

{
  const { impl, calls } = spyFetch({ '/api/submit': { status: 202 } });
  const api = new HelperApi({ token: 'tok', origin: 'https://127.0.0.1:3000', fetchImpl: impl });
  await new HelperChat(api).submit('안녕');
  ok('낸 말이 헬퍼로 간다', calls.length === 1 && calls[0].url.endsWith('/api/submit'), calls[0]?.url);
  ok('토큰은 헤더로 간다',
    calls[0].init.headers.Authorization === 'Bearer tok'
      && !calls[0].url.includes('tok'),
    calls[0].url);
  ok('보낸 것이 글이다', JSON.parse(calls[0].init.body).text === '안녕');
}

{
  const asking = { id: 'c1', kind: 'permission', what: 'mcp__ppt__set_text', args: { a: 1 }, since: '2026-08-31T00:00:00Z' };
  const { impl } = spyFetch({ '/api/status': { status: 200, body: { reachable: true, doing: '읽는 중', asking } } });
  const st = await new HelperStatus(new HelperApi({ origin: '', fetchImpl: impl })).status();
  ok('물음이 값으로 온다', st.pending?.id === 'c1' && st.pending?.kind === 'permission');
  ok('닿았는지가 값으로 온다', st.reachable === true && st.doing === '읽는 중');

  const dead = spyFetch({ '/api/status': { status: 200, body: { reachable: false, why: '못 닿았습니다' } } });
  const st2 = await new HelperStatus(new HelperApi({ origin: '', fetchImpl: dead.impl })).status();
  // **못 닿은 것과 「묻는 게 없다」가 값이 같으면 안 된다**(§5.7).
  ok('못 닿은 것은 물음 없음과 다르다',
    st2.reachable === false && st2.pending === null && st2.why.length > 0, st2.why);
}

{
  // 모르는 종류에 **기본값을 안 준다**(§5.7). `kind ?? 'permission'` 이 목업의 첫 판에 있었다.
  const p = pendingOf({ id: 'c9', what: '뭔가' });
  ok('모르는 종류는 빈 채로 든다', p.kind === '', JSON.stringify(p.kind));
}

/** `EventSource` 를 흉내 낸다. 프레임은 시험이 민다. */
class FakeEventSource {
  constructor(url) { this.url = url; this.listeners = new Map(); FakeEventSource.last = this; }
  addEventListener(kind, fn) {
    if (!this.listeners.has(kind)) this.listeners.set(kind, new Set());
    this.listeners.get(kind).add(fn);
  }
  emit(kind, data) {
    for (const fn of this.listeners.get(kind) ?? []) fn({ data: JSON.stringify(data) });
  }
  close() { this.closed = true; }
}

{
  const stream = new HelperStream({
    token: 'tok', presentation: 'p1', label: 'q3.pptx',
    origin: 'https://127.0.0.1:3000', EventSourceImpl: FakeEventSource,
  }).open();
  const src = FakeEventSource.last;
  ok('스트림만 토큰을 쿼리로 낸다', src.url.includes('token=tok') && src.url.includes('/hand/stream'), src.url);

  const seen = [];
  const tr = new HelperTranscript(stream);
  tr.subscribe('sess-1', -1, {
    onRestart: (why) => seen.push(`restart:${why}`),
    onEvent: (ev) => seen.push(`event:${ev.seq}`),
    onEnd: () => seen.push('end'),
  });
  src.emit('hello', { document: 'doc-7', label: 'q3.pptx' });
  ok('첫 프레임이 문서 키를 준다', stream.document === 'doc-7', stream.document);

  src.emit('restart', { why: '커서가 로그 끝을 넘었습니다' });
  src.emit('event', { seq: 3 });
  src.emit('stream', { live: false });
  ok('사유 프레임이 이벤트보다 먼저 온 순서 그대로 나른다',
    seen.join(' ') === 'restart:커서가 로그 끝을 넘었습니다 event:3 end', seen.join(' '));

  // **끊김은 에러가 아니다**(문이 그렇게 적어 뒀다) — `onEnd` 에 사유 인자가 없다.
  ok('끊김을 에러로 위장하지 않는다', tr.subscribe.length === 3, String(tr.subscribe.length));
}


// ── 빈 문자열은 값이 아니다 ───────────────────────────────────────────────────
//
// 손이 문서를 **빈 문자열**로 돌려줄 때 `??` 는 그것을 값으로 치고 그대로 싣는다. 그러면 헬퍼가
// 라우팅할 키가 없어 답이 404 로 떨어지고 **모델은 45초를 기다린 뒤 타임아웃을 받는다** —
// 조작은 내려갔고, 수행됐고, 답만 길을 잃는다. PowerPoint 에 처음 붙인 날 실제로 그 모양이 났다
// (2026-09-01). 그래서 이 자리는 `||` 이고, 그 사실을 시험이 문다.
{
  const stream = new ScriptedStream();
  stream.document = 'doc-real-7';
  const api = new SpyApi();
  const hand = deck();
  hand.document = '';           // 손이 아직 자기 키를 모르는 상태
  const serve = new ServeHand({ stream, api, hand });
  serve.start();

  stream.push('call', { id: 'r9', op: 'list_slides', args: {} });
  await new Promise((r) => setTimeout(r, 0));
  ok('빈 문서 키는 스트림이 준 키로 대체된다',
    api.replies[0]?.document === 'doc-real-7', String(api.replies[0]?.document));
}

{
  // 그리고 `hello` 가 오면 **손이 자기 키를 갖는다** — 그래야 결과가 「실제로 손댄 문서」를
  // 스스로 실을 수 있다(§6).
  const stream = new ScriptedStream();
  stream.document = null;
  const api = new SpyApi();
  const hand = deck();
  hand.document = '';
  const serve = new ServeHand({ stream, api, hand });
  serve.start();
  stream.push('hello', { document: 'doc-hello-3', label: 'q3.pptx' });
  ok('hello 가 손에게 문서 키를 준다', hand.document === 'doc-hello-3', hand.document);

  stream.push('call', { id: 'r10', op: 'list_slides', args: {} });
  await new Promise((r) => setTimeout(r, 0));
  ok('그 뒤의 답은 그 키를 싣는다', api.replies[0]?.document === 'doc-hello-3',
    String(api.replies[0]?.document));
}

console.log(failed ? `\n${failed} 실패` : '\n전부 통과');
process.exit(failed ? 1 : 0);
