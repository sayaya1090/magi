import { Quote } from '../domain/Quote.js';

/** 지금 선택한 문단들을 인용에 담는다. Office 를 모른다 — 문서 포트가 답한 것을 값으로 만든다. */
export class QuoteSelection {
  constructor(doc, conversation) {
    this.doc = doc;
    this.conversation = conversation;
  }
  async sampleBeforeFocus() { /* 워드는 포커스가 선택을 안 뺏는다 — 잴 것이 없다 */ }
  async run() {
    let sel;
    try {
      sel = await this.doc.selection();
    } catch {
      return { added: [], skipped: 0, empty: true, reason: 'readFailed', beforeCount: 0 };
    }
    if (!sel || !(sel.from >= 1)) {
      return { added: [], skipped: 0, empty: true, reason: 'none', beforeCount: 0 };
    }
    const q = new Quote({ from: sel.from, to: sel.to, text: sel.text, textTruncated: sel.textTruncated, textUnavailable: sel.textUnavailable, approx: sel.approx });
    if (this.conversation.attach(q)) return { added: [q], skipped: 0, empty: false, reason: null, beforeCount: 0 };
    return { added: [], skipped: 1, empty: false, reason: null, beforeCount: 0 };
  }
}

export function quoteNote({ reason } = {}) {
  switch (reason) {
    case 'readFailed':
      return { text: '선택을 못 읽었습니다 — 문서가 답하지 않았습니다. 잠시 뒤 다시 누르세요.', sticky: true };
    case 'none':
      return { text: '잡힌 문단이 없습니다 — 본문에서 문단을 고른 뒤 누르세요.', sticky: false };
    default:
      return { text: `이 창이 모르는 사유(${reason})입니다 — 이 창을 고쳐야 합니다.`, sticky: true };
  }
}
