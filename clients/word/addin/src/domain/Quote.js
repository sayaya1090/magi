/**
 * 인용 한 조각 — 문단 번호 범위와 그 글. 값 객체다(freeze).
 */
export class Quote {
  constructor({ from, to, text, textTruncated = false, textUnavailable = false, approx = false }) {
    this.from = from ?? 0;
    this.to = to ?? this.from;
    this.text = text ?? '';
    this.textTruncated = Boolean(textTruncated);
    this.textUnavailable = Boolean(textUnavailable);
    this.approx = Boolean(approx);
    Object.freeze(this);
  }
  /** 화면의 첫 줄: `문단 3` · `문단 3–5`. */
  get headline() { return this.from === this.to ? `문단 ${this.from}` : `문단 ${this.from}–${this.to}`; }
  get where() { return this.approx ? '문단 (글로 찾음)' : '문단'; }
  /** 같은 범위를 두 번 인용하지 않게 하는 열쇠. */
  get key() { return `${this.from}-${this.to}`; }
  preview(limit = 60) {
    const t = this.text.replace(/\s+/g, ' ').trim();
    return t.length > limit ? t.slice(0, limit - 1) + '…' : t;
  }
  get sizeLabel() { const n = this.to - this.from + 1; return n === 1 ? '문단 1개' : `문단 ${n}개`; }
  toPrompt(limit = 1200) {
    const head = `[인용] paragraphs=${this.from}-${this.to}${this.approx ? ' approx=true' : ''}`;
    if (this.textUnavailable) return `${head} textUnavailable=true`;
    if (!this.text) return head;
    let body = this.text;
    const cut = body.length > limit;
    if (cut) body = body.slice(0, limit);
    return `${head}${this.textTruncated || cut ? ' textTruncated=true' : ''}\n${body}`;
  }
}
