import { OfficeDocument } from './OfficeDocument.js';
import { FakeDocument } from './FakeDocument.js';
import { fixture } from '../ui/docFixture.js';

/**
 * 어느 문서에 붙는가 — Office 가 있나, Word 인가, 늦나. 가짜로 갈 때는 **사유를 남긴다**(엑셀 판 pickBook 과 같은 규칙).
 */
export const TIMED_OUT = Symbol('timed-out');

export async function pickDoc({
  office = typeof Office === 'undefined' ? null : Office,
  waitMs = 1500,
  doc = () => new FakeDocument(fixture),
} = {}) {
  if (!office) {
    return { doc: doc(), why: 'no-office', host: null, late: null, error: null, office: null };
  }
  let ready = null;
  try {
    ready = office.onReady().then((info) => info?.host ?? null);
    const host = await Promise.race([
      ready,
      new Promise((r) => setTimeout(() => r(TIMED_OUT), waitMs)),
    ]);
    const want = office.HostType?.Word ?? null;
    if (want !== null && host === want) {
      return { doc: new OfficeDocument(), why: null, host, late: null, error: null, office };
    }
    if (host !== TIMED_OUT) {
      return { doc: doc(), why: 'not-word', host, late: null, error: null, office };
    }
  } catch (e) {
    return { doc: doc(), why: 'threw', host: null, late: null, error: e, office };
  }
  return { doc: doc(), why: 'timeout', host: null, late: ready, error: null, office };
}

export function pickNote({ why, host, error } = {}) {
  switch (why) {
    case 'timeout':
      return 'Office 응답이 1.5초 안에 안 와 가짜 문서로 갔습니다. Word 안이라면 새로고침하세요.';
    case 'threw':
      return `Office 를 부르다 던졌습니다(${msgOf(error)}). 가짜 문서로 계속합니다 — 새로고침해도 같은 자리일 수 있습니다.`;
    case null: return null;
    case 'no-office': return null;
    case 'not-word': break;
    default:
      return `이 창이 모르는 사유로 가짜 문서에 붙었습니다(${why}). 이 창을 고쳐야 합니다.`;
  }
  if (host === null || host === undefined) {
    return 'Office.js 는 떴는데 어느 호스트인지 안 밝혔습니다 — 브라우저에서 그냥 열면 그렇습니다. '
      + '이 창은 Word 안에서만 진짜 문서에 붙습니다.';
  }
  return `Word 가 아닌 Office 호스트입니다(${host}). 이 창은 Word 에서만 진짜 문서에 붙습니다.`;
}
export function lateNote(lateHost, want) {
  if (want != null && lateHost === want) return 'Word 를 늦게 잡았습니다. 새로고침하면 진짜 문서에 붙습니다.';
  if (lateHost === null || lateHost === undefined) {
    return 'Office 가 늦게 답했지만 어느 호스트인지 안 밝혔습니다. 새로고침해도 같은 자리입니다.';
  }
  return `Office 가 늦게 답했는데 Word 가 아닙니다(${lateHost}). 새로고침해도 같은 자리입니다.`;
}
export function lateFailNote(e) {
  return `Office 를 끝내 못 잡았습니다(${msgOf(e)}). 가짜 문서로 계속합니다.`;
}
function msgOf(e) { return e?.message ?? String(e); }
