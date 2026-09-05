import { OfficeWorkbook } from './OfficeWorkbook.js';
import { FakeWorkbook } from './FakeWorkbook.js';
import { fixture } from '../ui/bookFixture.js';

/**
 * 어느 통합 문서에 붙을지 고른다 — Excel 안이면 진짜, 아니면 가짜(브라우저 목업).
 *
 * 파워포인트 판의 pickDeck 과 같은 규칙: Office.onReady 를 1.5초까지 기다리고, 그 안에 Excel 이라고 답하면
 * 진짜, 다른 호스트면 가짜, 시간이 지나면 가짜로 가되 늦게 온 답을 `late` 로 남겨 말만 한다(덱을 뒤에서
 * 바꾸지 않는다 — 화면이 반은 진짜 반은 가짜가 된다).
 */
export const TIMED_OUT = Symbol('timed-out');

export async function pickBook({
  office = typeof Office === 'undefined' ? null : Office,
  waitMs = 1500,
  book = () => new FakeWorkbook(fixture),
} = {}) {
  if (!office) {
    return { book: book(), why: 'no-office', host: null, late: null, error: null, office: null };
  }
  let ready = null;
  try {
    ready = office.onReady().then((info) => info?.host ?? null);
    const host = await Promise.race([
      ready,
      new Promise((r) => setTimeout(() => r(TIMED_OUT), waitMs)),
    ]);
    const want = office.HostType?.Excel ?? null;
    if (want !== null && host === want) {
      return { book: new OfficeWorkbook(), why: null, host, late: null, error: null, office };
    }
    if (host !== TIMED_OUT) {
      return { book: book(), why: 'not-excel', host, late: null, error: null, office };
    }
  } catch (e) {
    return { book: book(), why: 'threw', host: null, late: null, error: e, office };
  }
  return { book: book(), why: 'timeout', host: null, late: ready, error: null, office };
}

export function pickNote({ why, host, error } = {}) {
  switch (why) {
    case 'timeout':
      return 'Office 응답이 1.5초 안에 안 와 가짜 통합 문서로 갔습니다. Excel 안이라면 새로고침하세요.';
    case 'threw':
      return `Office 를 부르다 던졌습니다(${msgOf(error)}). 가짜 통합 문서로 계속합니다 — 새로고침해도 같은 자리일 수 있습니다.`;
    case null: return null;
    case 'no-office': return null;
    case 'not-excel': break;
    default:
      return `이 창이 모르는 사유로 가짜 통합 문서에 붙었습니다(${why}). 이 창을 고쳐야 합니다.`;
  }
  if (host === null || host === undefined) {
    return 'Office.js 는 떴는데 어느 호스트인지 안 밝혔습니다 — 브라우저에서 그냥 열면 그렇습니다. '
      + '이 창은 Excel 안에서만 진짜 통합 문서에 붙습니다.';
  }
  return `Excel 이 아닌 Office 호스트입니다(${host}). 이 창은 Excel 에서만 진짜 통합 문서에 붙습니다.`;
}

export function lateNote(lateHost, want) {
  if (want != null && lateHost === want) return 'Excel 을 늦게 잡았습니다. 새로고침하면 진짜 통합 문서에 붙습니다.';
  if (lateHost === null || lateHost === undefined) {
    return 'Office 가 늦게 답했지만 어느 호스트인지 안 밝혔습니다. 새로고침해도 같은 자리입니다.';
  }
  return `Office 가 늦게 답했는데 Excel 이 아닙니다(${lateHost}). 새로고침해도 같은 자리입니다.`;
}
export function lateFailNote(e) {
  return `Office 를 끝내 못 잡았습니다(${msgOf(e)}). 가짜 통합 문서로 계속합니다.`;
}
function msgOf(e) { return e?.message ?? String(e); }
