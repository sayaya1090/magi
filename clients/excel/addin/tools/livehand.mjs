// 가짜 손을 **살아 있는 헬퍼**에 붙인다. `TOKEN=… node tools/livehand.mjs [origin]`
//
// Excel 없이 헬퍼↔손 규약(SSE /hand/stream → call → POST /hand/reply)을 실물 헬퍼에 대고 잰다. 손은 FakeHand 라
// 통합 문서는 메모리이지만, 헬퍼·MCP·데몬·모델은 전부 진짜다 — 모델이 `list_sheets` 를 부르면 여기로 온다.
// 토큰은 헬퍼가 내주는 taskpane.html 의 <!--magi:boot--> 에 박혀 있다(curl -k 로 받아 "token" 을 뽑는다).
import https from 'node:https';
import { HelperStream } from '../src/adapter/HelperStream.js';
import { HelperApi } from '../src/adapter/helperApi.js';
import { ServeHand } from '../src/usecase/ServeHand.js';
import { FakeHand } from '../src/adapter/FakeHand.js';
import { fixture } from '../src/ui/bookFixture.js';

process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0'; // 헬퍼의 자가 서명 인증서 — 시험 도구에서만
const origin = process.argv[2] ?? 'https://127.0.0.1:3001';
const token = process.env.TOKEN ?? '';
if (!token) { console.error('TOKEN 이 없다'); process.exit(2); }

/** EventSource 흉내 — 헬퍼의 SSE 를 줄 단위로 읽어 addEventListener(kind) 에 나른다. */
class NodeEventSource {
  constructor(url) {
    this.listeners = new Map();
    this.req = https.get(url, { rejectUnauthorized: false }, (res) => {
      if (res.statusCode !== 200) { console.log(`  stream ${res.statusCode}`); res.resume(); this.onerror?.(); return; }
      let buf = ''; let kind = 'message';
      res.setEncoding('utf8');
      res.on('data', (chunk) => {
        buf += chunk;
        let at;
        while ((at = buf.indexOf('\n')) >= 0) {
          const line = buf.slice(0, at); buf = buf.slice(at + 1);
          if (line.startsWith('event: ')) kind = line.slice(7).trim();
          else if (line.startsWith('data: ')) { const data = line.slice(6); for (const fn of this.listeners.get(kind) ?? []) fn({ data }); kind = 'message'; }
        }
      });
      res.on('end', () => this.onerror?.());
    });
    this.req.on('error', (e) => { console.log(`  stream error: ${e.message}`); this.onerror?.(); });
  }
  addEventListener(kind, fn) { if (!this.listeners.has(kind)) this.listeners.set(kind, new Set()); this.listeners.get(kind).add(fn); }
  close() { this.req.destroy(); }
}

const hand = new FakeHand(structuredClone(fixture), { document: '', label: 'live-fake.xlsx' });
const stream = new HelperStream({ token, label: 'live-fake.xlsx', origin, EventSourceImpl: NodeEventSource }).open();
const api = new HelperApi({ token, origin });
stream.on('hello', (d) => console.log(`  hello — document=${d.document} label=${d.label}`));
stream.on('call', (d) => console.log(`  call ${d.id}: ${d.op} ${JSON.stringify(d.args ?? {}).slice(0, 160)}`));
stream.on('event', (d) => { const e = d ?? {}; const t = e.type ?? e.kind ?? '?'; const body = JSON.stringify(e.data ?? e.text ?? e.payload ?? '').slice(0, 140); console.log(`  event ${e.seq ?? ''} ${t} ${body}`); });
stream.on('stream', (d) => console.log(`  stream live=${d.live}${d.reason ? ' reason=' + d.reason : ''}${d.why ? ' ' + d.why : ''}`));
new ServeHand({ stream, api, hand, onNote: (s) => console.log(`  note: ${s}`) }).start();
console.log(`  붙는 중 — ${origin} (Ctrl-C 로 뗀다)`);
setInterval(() => {}, 1 << 30);
