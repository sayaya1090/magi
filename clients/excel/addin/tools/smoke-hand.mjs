// 손과 헬퍼 어댑터의 확인. `node tools/smoke-hand.mjs`
//
// `smoke.mjs` 가 **화면과 인용**을 재는 자리라면 이 파일은 **도구가 도는 길**을 잰다:
// 헬퍼가 내려보낸 조작 → 손 → 답. Excel 없이 도는 것이 요점이고, 그래서 여기서 재는 것은
// 「Office.js 가 그렇게 답하는가」가 아니라 **「도구의 계약을 우리가 지키는가」**다.
//
// 갈라 둔 이유는 하나 더 있다. `smoke.mjs` 는 `src/` 전체를 훑어 마크업 싱크를 검사하는데,
// 그 훑기는 이 파일이 더하는 파일들도 **자동으로** 덮는다. 시험을 새 파일에 두는 것이 그
// 그물을 약하게 만들지 않는다.
import { FakeHand } from '../src/adapter/FakeHand.js';
import { ServeHand } from '../src/usecase/ServeHand.js';
import { handRole } from '../src/usecase/HandRole.js';
import { HelperStream } from '../src/adapter/HelperStream.js';
import { HelperApi } from '../src/adapter/helperApi.js';
import { HelperChat, HelperStatus, HelperTranscript, pendingOf } from '../src/adapter/HelperPorts.js';
import { fixture } from '../src/ui/bookFixture.js';

let failed = 0;
const ok = (name, cond, detail = '') => {
  console.log(`${cond ? '  ok  ' : '  FAIL'} ${name}${detail ? ' — ' + detail : ''}`);
  if (!cond) failed++;
};
const threw = async (fn) => {
  try { await fn(); return null; } catch (e) { return e?.message ?? String(e); }
};

const book = () => new FakeHand(structuredClone(fixture));

// ── 손: 계약 넷 ───────────────────────────────────────────────────────────────

{
  const hand = book();
  const out = await hand.run('list_sheets', {});
  ok('목차가 탭 번호를 1 부터 센다', out.result.sheets[0].index === 1,
    JSON.stringify(out.result.sheets.map((s) => s.index)));
  ok('목차가 손댄 문서를 싣는다', out.document === 'book-fake', out.document);
  ok('읽기는 개정 쌍을 안 올린다', out.count === 0, String(out.count));
}

{
  const hand = book();
  const first = await hand.run('describe_sheet', { sheet: '1' });
  ok('1 번은 첫 탭이다', first.result.sheet === fixture.sheets[0].name && first.result.index === 1, first.result.sheet);
  const second = await hand.run('describe_sheet', { sheet: '2' });
  ok('2 번은 둘째 탭이다', second.result.sheet === fixture.sheets[1].name && second.result.index === 2, second.result.sheet);
  // 0-based 로 읽으면 여기서 갈린다. 손이 1 을 첫 탭으로 안 보면 **모델이 보는 세상이 한 장씩 밀린다.**
  ok('못 찾는 번호는 던지고 몇 탭인지 말한다',
    (await threw(() => hand.run('describe_sheet', { sheet: '99' })))?.includes('2개'));
  // **비슷한 것을 찾아 대신 고치지 않는다**(§5.8). 이름이 틀리면 있는 이름을 대고 던진다.
  const why = await threw(() => hand.run('describe_sheet', { sheet: '매출2' }));
  ok('없는 시트는 있는 이름을 대고 던진다', why?.includes('매출2') && why?.includes(fixture.sheets[0].name), why);
}

{
  const hand = book();
  const before = (await hand.run('read_range', { sheet: '매출', address: 'B2' })).result.values[0][0];
  const out = await hand.run('write_range', { sheet: '매출', address: 'B2', values: [[42]] });
  ok('쓰기가 바뀐 곳과 덮어쓴 수를 스스로 싣는다',
    out.changed.length === 1 && out.changed[0].includes('매출!B2') && out.changed[0].includes('덮어썼'),
    out.changed[0]);
  ok('쓰기가 개정 쌍을 올린다', out.count === 1, String(out.count));
  ok('되읽으면 새 값이다', (await hand.run('read_range', { sheet: '매출', address: 'B2' })).result.values[0][0] === 42 && before !== 42);
  const why = await threw(() => hand.run('write_range', { sheet: '없는시트', address: 'A1', values: [[1]] }));
  ok('없는 시트에는 안 쓴다', why?.includes('없는시트'), why);
}

{
  const hand = book();
  // 되돌리기: 스냅숏은 **이 창이 뜬 뒤 찍은 것만** 안다 — 그 사실이 거절문에 실려야 모델이 옛 id 를 안 든다.
  const snap = await hand.run('snapshot_range', { sheet: '매출', address: 'A1:C6' });
  await hand.run('write_range', { sheet: '매출', address: 'A1', values: [['바꿔 둠']] });
  const back = await hand.run('restore_range', { snapshot: snap.result.snapshot });
  ok('되돌린 범위를 말한다', back.result.address === 'A1:C6' && back.changed[0].includes('되돌렸'), back.changed[0]);
  ok('되돌리면 값이 돌아온다', (await hand.run('read_range', { sheet: '매출', address: 'A1' })).result.values[0][0] === '분기');
  const gone = await threw(() => hand.run('restore_range', { snapshot: 'snap-없음' }));
  ok('없는 스냅숏은 던지고 어디서 얻는지 말한다', gone?.includes('snap-없음') && gone?.includes('snapshot_range'), gone);
}

{
  const hand = book();
  // 가짜는 **픽셀을 지어내지 않는다.** 없는 증거를 있는 척하는 것이 이 제품이 제일 피하는 것이다.
  const why = await threw(() => hand.run('render_range', { sheet: '매출', address: 'A1:C6' }));
  ok('가짜 손은 렌더를 못 한다고 말한다', why?.includes('Excel'), why);
  // **이름은 진짜 도구와 겹치면 안 된다.** 겹치면 그 도구가 생기는 날 이 시험이 조용히
  // 다른 것을 재게 된다 — 파워포인트 판이 set_notes 로 한 번 겪었다.
  const unknown = await threw(() => hand.run('폴더_열기', {}));
  ok('모르는 조작은 던진다', unknown?.includes('폴더_열기'), unknown);
}

{
  const hand = book();
  const out = await hand.run('advise', { items: [{ message: 'a', why: 'b' }] });
  // **안내는 한 일이 아니라 할 말이다**(§6.1) — `changed` 를 안 싣는 것이 계약이다.
  ok('안내는 문서를 고친 것으로 안 센다',
    out.changed.length === 0 && out.count === 0, JSON.stringify(out.changed));
}

// ── 손 노릇: 스트림에서 받아 답까지 ───────────────────────────────────────────

/** 프레임을 손으로 밀어 넣는 스트림. 시험이 시계를 안 재게. */
class ScriptedStream {
  constructor() { this.handlers = new Map(); this.document = 'book-fake'; }
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
  const hand = book();
  const serve = new ServeHand({ stream, api, hand });
  serve.start();

  stream.push('call', { id: 'r1', op: 'list_sheets', args: {} });
  await new Promise((r) => setTimeout(r, 0));
  ok('내려온 조작이 수행되고 답이 올라간다',
    api.replies.length === 1 && api.replies[0].id === 'r1'
      && Array.isArray(api.replies[0].result.sheets),
    JSON.stringify(api.replies[0]?.id));
  ok('답이 손댄 문서를 싣는다', api.replies[0].document === 'book-fake', api.replies[0].document);

  stream.push('call', { id: 'r2', op: 'render_range', args: { sheet: '매출', address: 'A1' } });
  await new Promise((r) => setTimeout(r, 0));
  const bad = api.replies.find((p) => p.id === 'r2');
  ok('실패는 사유를 실어 올린다', typeof bad?.error === 'string' && bad.error.includes('Excel'),
    bad?.error);
  ok('실패에는 result 를 안 싣는다', bad?.result === undefined, JSON.stringify(bad?.result));
}

{
  // 답도 못 올리면 **조용히 넘어가지 않는다** — 그러면 모델은 60초를 기다린다.
  const stream = new ScriptedStream();
  const notes = [];
  const serve = new ServeHand({
    stream, api: new SpyApi({ fail: true }), hand: book(), onNote: (s) => notes.push(s),
  });
  serve.start();
  stream.push('call', { id: 'r1', op: '노트_열기', args: {} });
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
  const asking = { id: 'c1', kind: 'permission', what: 'mcp__xl__write_range', args: { a: 1 }, since: '2026-08-31T00:00:00Z' };
  const { impl } = spyFetch({ '/api/status': { status: 200, body: { reachable: true, doing: '읽는 중', asking, session: 's_live' } } });
  const st = await new HelperStatus(new HelperApi({ origin: '', fetchImpl: impl })).status();
  ok('물음이 값으로 온다', st.pending?.id === 'c1' && st.pending?.kind === 'permission');
  ok('대화 이름이 값으로 온다 — 떨어뜨리면 창은 영영 「아직 안 붙었다」다', st.session === 's_live');
  ok('이름이 안 실려 오면 모름(undefined)이지 빈 이름이 아니다', (await new HelperStatus(new HelperApi({ origin: '', fetchImpl: spyFetch({ '/api/status': { status: 200, body: { reachable: true } } }).impl })).status()).session === undefined);
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
  constructor(url) {
    this.url = url;
    this.listeners = new Map();
    FakeEventSource.last = this;
    // 몇 번 열렸는가. 「한 번만 되살린다」는 이 수로만 잴 수 있다.
    FakeEventSource.opened = (FakeEventSource.opened ?? 0) + 1;
  }
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
    token: 'tok', workbook: 'w1', label: 'q3.xlsx',
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
  src.emit('hello', { document: 'doc-7', label: 'q3.xlsx' });
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
// 조작은 내려갔고, 수행됐고, 답만 길을 잃는다. 파워포인트 판이 처음 붙인 날 실제로 그 모양이 났다
// (2026-09-01). 그래서 이 자리는 `||` 이고, 그 사실을 시험이 문다.
{
  const stream = new ScriptedStream();
  stream.document = 'doc-real-7';
  const api = new SpyApi();
  const hand = book();
  hand.document = '';           // 손이 아직 자기 키를 모르는 상태
  const serve = new ServeHand({ stream, api, hand });
  serve.start();

  stream.push('call', { id: 'r9', op: 'list_sheets', args: {} });
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
  const hand = book();
  hand.document = '';
  const serve = new ServeHand({ stream, api, hand });
  serve.start();
  stream.push('hello', { document: 'doc-hello-3', label: 'q3.xlsx' });
  ok('hello 가 손에게 문서 키를 준다', hand.document === 'doc-hello-3', hand.document);

  stream.push('call', { id: 'r10', op: 'list_sheets', args: {} });
  await new Promise((r) => setTimeout(r, 0));
  ok('그 뒤의 답은 그 키를 싣는다', api.replies[0]?.document === 'doc-hello-3',
    String(api.replies[0]?.document));
}


{
  // **오류는 어느 속성이 거절됐는지까지 싣는다.** Office 는 code·message·errorLocation 을 따로
  // 들고, 이 호스트는 message 가 code 와 같다 — 하나만 올리면 `InvalidArgument` 한 단어가 답이다.
  const stream = new ScriptedStream();
  stream.document = 'doc-err';
  const api = new SpyApi();
  const hand = {
    document: 'doc-err',
    async run() {
      const e = new Error('InvalidArgument');
      e.code = 'InvalidArgument';
      e.debugInfo = { errorLocation: 'Range.numberFormat' };
      throw e;
    },
  };
  const serve = new ServeHand({ stream, api, hand });
  serve.start();
  stream.push('call', { id: 'r11', op: 'set_number_format', args: {} });
  await new Promise((r) => setTimeout(r, 0));
  const why = api.replies[0]?.error ?? '';
  ok('오류가 거절된 속성 이름을 싣는다', why.includes('Range.numberFormat'), why);
  ok('코드와 같은 message 는 한 번만 적는다', why.split('InvalidArgument').length === 2, why);
}

{
  // 볼륨 판(2021)은 메모 스레드를 NotImplemented 로 거절한다 — 코드 한 단어면 모델이 인자를 바꿔 다시 부른다.
  // **이 판이 안 주는 것**이라고 적어야 멈춘다(실물 2026-09-07, add_comment·read_comments·resolve_comment 넷 다).
  const stream = new ScriptedStream();
  stream.document = 'doc-ni';
  const api = new SpyApi();
  const hand = { document: 'doc-ni', async run() { const e = new Error('NotImplemented'); e.code = 'NotImplemented'; e.debugInfo = { errorLocation: 'CommentCollection._OnAccess' }; throw e; } };
  const serve = new ServeHand({ stream, api, hand });
  serve.start();
  stream.push('call', { id: 'r12', op: 'add_comment', args: {} });
  await new Promise((r) => setTimeout(r, 0));
  const why = api.replies[0]?.error ?? '';
  ok('NotImplemented 는 이 Excel 판이 안 주는 것이라고 적는다', why.includes('이 Excel 판이 이 기능을 아직 안 줍니다') && why.includes('Microsoft 365'), why);
  ok('거절된 자리도 그대로 싣는다', why.includes('CommentCollection._OnAccess'), why);
}

// ── 되살아난 것도 사건이다 ───────────────────────────────────────────────────
//
// 이 창은 스트림을 먼저 열고 컴패니언을 나중에 고른다. 그래서 **정상 흐름이 죽은 스트림으로
// 시작한다** — 죽음만 값에 실으면 화면은 붙은 뒤에도 「대화 스트림이 끊겼습니다」를 띄운 채다.
// 실물에서 그 화면을 봤다(2026-09-01): 헬퍼는 live:true 를 보내고 있었다.
{
  const stream = new ScriptedStream();
  const seen = [];
  const tr = new HelperTranscript(stream);
  tr.subscribe("s1", -1, {
    onEvent: () => {}, onRestart: () => {},
    onEnd: () => seen.push("end"),
    onLive: () => seen.push("live"),
  });
  stream.push("stream", { live: false });
  stream.push("stream", { live: true });
  ok("끊김과 되살아남을 둘 다 알린다", seen.join(" ") === "end live", seen.join(" "));
}

// ── 끊긴 스트림을 스스로 되살린다 ───────────────────────────────────────────
//
// 실물에서 본 고장이다(2026-09-02): 헬퍼를 다시 띄웠더니 판은 위쪽에 「붙었습니다 — 도구
// 28개」를 적어 둔 채 손이 죽어 있었고, 모델이 부르는 도구는 전부 「연결된 작업창이 없습니다」로
// 떨어졌다. 살리는 길이 **파워포인트를 통째로 껐다 켜는 것뿐**이었다 — PC 를 잘 다루지 못하는
// 사람에게는 고칠 방법이 없는 고장이고, 화면은 멀쩡하다고 적혀 있으니 신고할 말조차 없다.
//
// 핵심은 **되살릴 수 없는 갈래가 하나 있다**는 것이다. 토큰은 헬퍼가 뜰 때마다 새로 나므로,
// 헬퍼가 다시 뜨면 이 페이지의 토큰은 영영 거부된다. 다시 붙어도 안 되고, 새 토큰을 실은
// 페이지를 받아 오는 수밖에 없다. 그래서 **왜 끊겼는지 묻고 나서** 움직인다.
{
  // **먼저 온 것을 버리지 않는다.** 스트림은 창이 뜨자마자 열리고 전사 읽기는 대화 이름을 안 뒤에
  // 붙는다 — 헬퍼가 되풀이해 준 앞부분(리로드 뒤 과거 대화)이 듣는 이 없이 지나갔다(2026-09-05).
  {
    const s = new HelperStream({ token: 'tok', origin: 'https://127.0.0.1:3000', EventSourceImpl: FakeEventSource }).open();
    const src = FakeEventSource.last;
    src.emit('event', { seq: 1, type: 'prompt.submitted' });
    src.emit('event', { seq: 2, type: 'part.appended' });
    const got = [];
    s.on('event', (ev) => got.push(ev.seq));
    src.emit('event', { seq: 3, type: 'part.appended' });
    ok('청자가 붙기 전에 온 event 프레임은 첫 청자에게 순서대로 간다', got.join(',') === '1,2,3', got.join(','));
    const later = [];
    s.on('event', (ev) => later.push(ev.seq));
    ok('둘째 청자에게는 되풀이하지 않는다', later.length === 0);
  }

  const openStream = ({ answer, reload, waits }) => {
    const s = new HelperStream({
      token: 'tok', origin: 'https://127.0.0.1:3000', EventSourceImpl: FakeEventSource,
      fetchImpl: answer,
      reload,
      wait: async (ms) => { waits.push(ms); },
    }).open();
    return s;
  };

  // ① 토큰이 낡음 — 헬퍼가 다시 뜬 경우. **다시 붙지 말고 페이지를 새로 받는다.**
  {
    const waits = []; let reloaded = 0; const said = [];
    const s = openStream({
      answer: async () => ({ status: 401 }),
      reload: () => { reloaded += 1; }, waits,
    });
    s.on('stream', (d) => said.push(d));
    const first = FakeEventSource.last;
    await first.onerror();
    ok('낡은 토큰이면 창을 다시 불러온다', reloaded === 1, String(reloaded));
    ok('다시 붙어 보지는 않는다 — 붙어도 안 되는 갈래다',
      FakeEventSource.last === first, 'reopened');
    ok('왜 깜빡이는지 사람에게 적는다',
      said.some((d) => d.reason === 'stale' && d.why.includes('헬퍼가 다시 시작됐습니다')),
      JSON.stringify(said));
  }

  // ② 헬퍼는 멀쩡하고 연결만 끊김 — **그냥 다시 붙는다.** 사람이 할 일이 없다.
  {
    const waits = []; let reloaded = 0; const said = [];
    const s = openStream({
      answer: async () => ({ status: 200 }),
      reload: () => { reloaded += 1; }, waits,
    });
    s.on('stream', (d) => said.push(d));
    const first = FakeEventSource.last;
    await first.onerror();
    ok('연결만 끊겼으면 다시 붙는다', FakeEventSource.last !== first, 'not reopened');
    ok('그때는 창을 다시 안 불러온다 — 쓰던 글이 날아간다', reloaded === 0, String(reloaded));
    ok('죽은 연결은 닫는다', first.closed === true, String(first.closed));
    ok('다시 붙었다고 알린다', said.some((d) => d.live === true), JSON.stringify(said));
    ok('멀쩡한 경우는 안 기다린다', waits.length === 0, JSON.stringify(waits));
  }

  // ③ 헬퍼가 안 뜸 — **모르는 것을 「낡았다」로 적지 않는다.**
  //
  // 이 가름이 이 묶음의 요점이다. 못 물어본 것을 낡은 것으로 치면, 헬퍼가 잠깐 바쁜 사이에
  // 창을 다시 불러오고 사람이 적던 글이 날아간다 — 고치려던 것보다 나쁜 고장이다.
  {
    const waits = []; let reloaded = 0; const said = [];
    const s = openStream({
      answer: async () => { throw new Error('ECONNREFUSED'); },
      reload: () => { reloaded += 1; }, waits,
    });
    s.on('stream', (d) => said.push(d));
    const first = FakeEventSource.last;
    await first.onerror();
    ok('못 물어본 것을 낡은 토큰으로 치지 않는다', reloaded === 0, String(reloaded));
    ok('헬퍼가 안 뜬 것을 사실대로 적는다',
      said.some((d) => d.reason === 'down' && d.why.includes('응답하지 않습니다')),
      JSON.stringify(said));
    ok('물러서서 다시 붙는다', waits[0] === 1000 && FakeEventSource.last !== first,
      JSON.stringify(waits));

    // 헛걸음이 이어지면 간격이 자라되 **천장이 있다** — 무한정 늘리면 헬퍼가 돌아와도
    // 한참 뒤에야 붙는다.
    for (let i = 0; i < 8; i += 1) await FakeEventSource.last.onerror();
    ok('물러서는 간격이 자란다', waits[1] === 2000 && waits[2] === 4000, JSON.stringify(waits));
    ok('간격에 천장이 있다', Math.max(...waits) === 15000, JSON.stringify(waits));
  }

  // ④ 겹쳐 돌지 않는다 — 두 번 겹치면 연결이 둘 생기고, 그러면 도구 호출이 두 번 온다.
  {
    const waits = []; const s = openStream({
      answer: async () => { await new Promise((go) => setTimeout(go, 5)); return { status: 200 }; },
      reload: () => {}, waits,
    });
    const first = FakeEventSource.last;
    // 센 값은 시험 전체에 걸쳐 쌓이므로 **여기서부터** 센다.
    const base = FakeEventSource.opened;
    await Promise.all([first.onerror(), first.onerror(), first.onerror()]);
    ok('한 번만 되살린다', FakeEventSource.opened === base + 1,
      String(FakeEventSource.opened - base));
  }
}

// ── 역할: 바닥 아래 호스트에서는 화면만 ────────────────────────────────────────
//
// 파워포인트 판이 실물 LTSC 2021 에서 본 것(2026-09-05): 창이 도구를 다 광고했고, 모델이 부른 첫
// 호출이 「'index' 속성을 사용할 수 없습니다」로 돌아왔고, 모델은 「API 가 없다」고 결론짓고 셸로
// 돌아섰다. 엑셀의 바닥은 ExcelApi 1.7 — 2019·2021·365 는 손이고, 2016(1.4) 은 화면이다.
{
  const caps = (ok17) => ({ measured: true, sets: [
    { name: 'ExcelApi', version: '1.4', ok: true }, { name: 'ExcelApi', version: '1.7', ok: ok17 },
    { name: 'ExcelApi', version: '1.9', ok: ok17 }, { name: 'ExcelApi', version: '1.14', ok: false },
    { name: 'SharedRuntime', version: '1.1', ok: true }] });
  ok('1.7 이 있으면 손', handRole({ isHost: true, caps: caps(true) }).role === 'hand');
  const v = handRole({ isHost: true, caps: caps(false) });
  ok('1.7 이 없으면 화면', v.role === 'viewer', v.role);
  ok('사유가 이 호스트의 천장을 말하고 SKU 는 안 말한다',
    v.why.includes('1.4') && !/2016|2021|LTSC|COM/.test(v.why), v.why);
  ok('못 쟀으면 손 — 모르는 것을 없다로 읽지 않는다',
    handRole({ isHost: true, caps: { measured: false, sets: [] } }).role === 'hand');
  ok('가짜 문서는 역할을 안 바꾼다', handRole({ isHost: false, caps: caps(false) }).role === 'hand');
  const ten = handRole({ isHost: true, caps: { measured: true, sets: [
    { name: 'ExcelApi', version: '1.9', ok: true }, { name: 'ExcelApi', version: '1.14', ok: true }] } });
  ok('천장은 수로 잰다 — 1.14 가 1.9 보다 높다', ten.top === '1.14', ten.top);
}
{
  new HelperStream({ token: 'tok', workbook: 'w1', origin: 'https://127.0.0.1:3000', EventSourceImpl: FakeEventSource, role: 'viewer' }).open();
  ok('화면은 role=viewer 로 붙는다', FakeEventSource.last.url.includes('role=viewer'), FakeEventSource.last.url);
  new HelperStream({ token: 'tok', workbook: 'w1', origin: 'https://127.0.0.1:3000', EventSourceImpl: FakeEventSource }).open();
  ok('손은 role 을 안 싣는다', !FakeEventSource.last.url.includes('role='), FakeEventSource.last.url);
}
{
  // 볼 손이 아직 없으면 — 헬퍼는 404 를 주고 EventSource 는 onerror 만 낸다. 왜인지 물어서 사람에게 적고 물러선다.
  const answer = (attached) => async (url) => (String(url).endsWith('/api/documents')
    ? { status: 200, json: async () => ({ attached, documents: [] }) }
    : { status: 200 });
  const waits = []; const said = [];
  const s = new HelperStream({
    token: 'tok', origin: 'https://127.0.0.1:3000', EventSourceImpl: FakeEventSource, role: 'viewer',
    fetchImpl: answer(false), reload: () => {}, wait: async (ms) => { waits.push(ms); },
  }).open();
  s.on('stream', (d) => said.push(d));
  const first = FakeEventSource.last;
  await first.onerror();
  ok('손이 없으면 그렇게 적는다 — 사람이 할 일(다른 판에서 열기)이 있다',
    said.some((d) => d.reason === 'nohand' && d.why.includes('ExcelApi 1.7') && d.why.includes('여세요')), JSON.stringify(said));
  ok('그 문장은 손·화면 구분을 말하지 않는다 — 사람은 구분할 일이 없다',
    !said.some((d) => d.reason === 'nohand' && /화면|viewer/.test(d.why)), JSON.stringify(said));
  ok('물러섰다가 다시 본다', waits.length === 1 && FakeEventSource.last !== first, JSON.stringify(waits));

  const waits2 = []; const said2 = [];
  const s2 = new HelperStream({
    token: 'tok', origin: 'https://127.0.0.1:3000', EventSourceImpl: FakeEventSource, role: 'viewer',
    fetchImpl: answer(true), reload: () => {}, wait: async (ms) => { waits2.push(ms); },
  }).open();
  s2.on('stream', (d) => said2.push(d));
  await FakeEventSource.last.onerror();
  ok('손이 있으면 그냥 다시 붙는다', waits2.length === 0 && !said2.some((d) => d.reason === 'nohand'), JSON.stringify(said2));

  // **못 물은 것은 「없다」가 아니다.** 헬퍼가 잠깐 바쁘거나 답이 이상하면 그냥 다시 붙는다 —
  // 여기서 「없다」로 읽으면 헬퍼가 바쁜 사이에 사람에게 손을 띄우라고 하게 된다.
  for (const [name, answer] of [
    ['비-200', async (url) => (String(url).endsWith('/api/documents') ? { status: 503 } : { status: 200 })],
    ['attached 칸 없음', async (url) => (String(url).endsWith('/api/documents')
      ? { status: 200, json: async () => ({ documents: [] }) } : { status: 200 })],
    ['던짐', async (url) => { if (String(url).endsWith('/api/documents')) throw new Error('끊김'); return { status: 200 }; }],
  ]) {
    const w = []; const said = [];
    const s = new HelperStream({
      token: 'tok', origin: 'https://127.0.0.1:3000', EventSourceImpl: FakeEventSource, role: 'viewer',
      fetchImpl: answer, reload: () => {}, wait: async (ms) => { w.push(ms); },
    }).open();
    s.on('stream', (d) => said.push(d));
    await FakeEventSource.last.onerror();
    ok(`손이 있는지 못 물었으면(${name}) 없다로 안 읽는다`, w.length === 0 && !said.some((d) => d.reason === 'nohand'), JSON.stringify(said));
  }

  // 손(role 없음)은 이 물음을 아예 안 한다 — 앞 판본과 같은 길.
  const asked = [];
  const s3 = new HelperStream({
    token: 'tok', origin: 'https://127.0.0.1:3000', EventSourceImpl: FakeEventSource,
    fetchImpl: async (url) => { asked.push(String(url)); return { status: 200 }; }, reload: () => {}, wait: async () => {},
  }).open();
  s3.on('stream', () => {});
  await FakeEventSource.last.onerror();
  ok('손은 손이 있는지 안 묻는다', !asked.some((u) => u.endsWith('/api/documents')), asked.join(','));
}

console.log(failed ? `\n${failed} 실패` : '\n전부 통과');
process.exit(failed ? 1 : 0);
