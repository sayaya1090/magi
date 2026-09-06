import { DocumentPort } from '../port/DocumentPort.js';

/**
 * 진짜 Word 에 닿는 문서. **이 파일과 WordHand 만 Office 를 안다.**
 *
 * 문서의 안정된 이름은 사용자 지정 속성(customProperties, WordApi 1.3)에 적는다 — 엑셀의 settings(1.4)는 2021 워드에
 * 없다. 값은 255자 제한이라 짧은 id 만 적는다.
 */
export const DOC_PROPERTY = 'MAGI.DOC';
export const SAMPLE_CHARS = 1200;

export async function stableDocId(runner, note) {
  const say = note ?? ((m) => { if (typeof console !== 'undefined') console.warn('[magi] 문서 이름:', m); });
  const run = runner ?? (typeof Word === 'undefined' ? null : Word.run);
  if (!run) return '';
  try {
    const got = await run(async (context) => {
      const props = context.document.properties.customProperties;
      const had = props.getItemOrNullObject(DOC_PROPERTY);
      had.load('value,isNullObject');
      await context.sync();
      if (had.isNullObject !== true && had.value) return String(had.value);
      const made = newDocId();
      props.add(DOC_PROPERTY, made);
      await context.sync();
      return made;
    });
    if (got) return got;
  } catch (e) {
    say(`사용자 지정 속성에 못 적었습니다: ${e?.message ?? e}`);
  }
  return '';
}
function newDocId() {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) return `doc-${crypto.randomUUID()}`;
  return `doc-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

/** 선택의 문단들을 본문 문단 목록에서 **글로** 찾아 번호를 매긴다. 같은 글이 여럿이면 첫 것 — approx 로 그렇다고 말한다. */
export function locate(bodyTexts, selTexts) {
  if (!selTexts.length) return { from: 0, to: 0, approx: false };
  const n = selTexts.length;
  const hits = [];
  for (let i = 0; i + n <= bodyTexts.length; i += 1) {
    let ok = true;
    for (let k = 0; k < n; k += 1) if (bodyTexts[i + k] !== selTexts[k]) { ok = false; break; }
    if (ok) hits.push(i);
  }
  if (hits.length === 0) return { from: 0, to: 0, approx: true };
  return { from: hits[0] + 1, to: hits[0] + n, approx: hits.length > 1 };
}

export class OfficeDocument extends DocumentPort {
  constructor({ run } = {}) {
    super();
    this.run = run ?? ((fn) => Word.run(fn));
  }
  get label() { return 'Word (Office.js)'; }
  get isHost() { return true; }
  capabilities() {
    const req = (typeof Office !== 'undefined') && Office.context && Office.context.requirements;
    if (!req || typeof req.isSetSupported !== 'function') {
      return { measured: false, note: 'Office.context.requirements 가 없다', sets: [] };
    }
    const want = [
      ['WordApi', '1.3'], ['WordApi', '1.4'], ['WordApi', '1.5'], ['WordApi', '1.6'], ['WordApi', '1.7'],
      ['WordApi', '1.8'], ['WordApi', '1.9'], ['WordApiDesktop', '1.1'], ['SharedRuntime', '1.1'],
    ];
    const sets = want.map(([name, version]) => {
      let ok = null;
      try { ok = req.isSetSupported(name, version); } catch { ok = null; }
      return { name, version, ok };
    });
    return { measured: true, note: '', sets };
  }
  async selection() {
    return this.run(async (context) => {
      const body = context.document.body.paragraphs; body.load('items/text');
      const sel = context.document.getSelection(); const ps = sel.paragraphs; ps.load('items/text');
      let textUnavailable = false;
      try { await context.sync(); } catch { textUnavailable = true; }
      if (textUnavailable) return { from: 0, to: 0, text: '', textTruncated: false, textUnavailable: true, approx: false };
      const bodyTexts = body.items.map((p) => p.text); const selTexts = ps.items.map((p) => p.text);
      const { from, to, approx } = locate(bodyTexts, selTexts);
      const full = selTexts.join('\n');
      const text = full.length > SAMPLE_CHARS ? full.slice(0, SAMPLE_CHARS) : full;
      return { from, to, text, textTruncated: full.length > SAMPLE_CHARS, textUnavailable: false, approx };
    });
  }
  async paragraphCount() {
    try {
      return await this.run(async (context) => {
        const ps = context.document.body.paragraphs; ps.load('items'); await context.sync();
        return ps.items.length;
      });
    } catch { return null; }
  }
  async point(paragraph) {
    return this.run(async (context) => {
      const ps = context.document.body.paragraphs; ps.load('items'); await context.sync();
      const p = ps.items[Number(paragraph) - 1];
      if (!p) throw new Error(`문서에 ${paragraph}번 문단이 없습니다(문단 ${ps.items.length}개)`);
      p.select(); await context.sync();
    });
  }
}
