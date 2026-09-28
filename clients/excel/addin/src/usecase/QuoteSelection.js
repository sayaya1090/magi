import { Quote } from '../domain/Quote.js';

/**
 * 「선택 인용」 — 사람이 시트에서 잡은 범위를 대화에 붙인다.
 *
 * 파워포인트 판과 다른 점 하나: 엑셀에는 늘 선택이 있다(활성 셀). 그래서 「아무것도 안 잡았다」는 없고,
 * 빈 셀 하나를 잡은 채 누르면 그 빈 셀이 인용된다 — 그것도 사실이다(「B7 은 비었다」를 물을 수 있다).
 * 포커스가 선택을 가져가는 일(파워포인트의 S14)은 엑셀 작업창에서 없다.
 */
export class QuoteSelection {
  constructor(book, conversation) {
    this.book = book;
    this.conversation = conversation;
  }
  async sampleBeforeFocus() { /* 엑셀은 포커스가 선택을 안 뺏는다 — 잴 것이 없다 */ }
  async run() {
    let sel;
    try {
      sel = await this.book.selection();
    } catch {
      return { added: [], skipped: 0, empty: true, reason: 'readFailed', beforeCount: 0 };
    }
    if (!sel || !sel.address) {
      return { added: [], skipped: 0, empty: true, reason: 'none', beforeCount: 0 };
    }
    const q = new Quote({
      sheet: sel.sheet, sheetIndex: sel.sheetIndex, address: sel.address,
      rowCount: sel.rowCount, columnCount: sel.columnCount,
      values: sel.values, valuesTruncated: sel.valuesTruncated, textUnavailable: sel.textUnavailable,
    });
    if (this.conversation.attach(q)) return { added: [q], skipped: 0, empty: false, reason: null, beforeCount: 0 };
    return { added: [], skipped: 1, empty: false, reason: null, beforeCount: 0 };
  }
}

export function quoteNote({ reason } = {}) {
  switch (reason) {
    case 'readFailed':
      return { sticky: true,
        text: '선택을 못 읽었습니다 — 통합 문서가 답하지 않았습니다. 잠시 뒤 다시 누르거나 새로고침하세요.' };
    case 'none':
      return { sticky: false, text: '잡힌 범위가 없습니다 — 시트에서 셀이나 범위를 고른 뒤 다시 눌러 주세요.' };
    default:
      return { sticky: true,
        text: `선택한 내용을 인용하지 못했습니다. 오류 코드: ${reason}. 문제가 계속되면 이 코드를 포함해 문의해 주세요.` };
  }
}
