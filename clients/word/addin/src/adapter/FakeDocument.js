import { DocumentPort } from '../port/DocumentPort.js';
import { SAMPLE_CHARS } from './OfficeDocument.js';

/** 가짜 문서 — 목업 격자가 그리는 메모리 문서(ui/docFixture.js 꼴). 선택은 문단 번호 범위다. */
export class FakeDocument extends DocumentPort {
  constructor(model) {
    super();
    this.model = model;
    this.from = 1; this.to = 1;
    this.listeners = new Set();
    this.counting = true;  // false = 문단 수를 못 주는 호스트 흉내
    this.reading = true;   // false = 선택을 못 읽는 호스트 흉내
  }
  capabilities() { return { measured: false, note: '가짜 문서 — 호스트가 없어 잰 것이 없다', sets: [] }; }
  get label() { return '가짜 문서 (Word 없이)'; }
  onChange(fn) { this.listeners.add(fn); return () => this.listeners.delete(fn); }
  #emit() { for (const fn of this.listeners) fn(); }
  select(from, to = from) { this.from = from; this.to = to; this.#emit(); }
  async selection() {
    if (!this.reading) throw new Error('문서가 선택을 안 내줍니다 (가짜 손잡이)');
    const ps = this.model.paragraphs.slice(this.from - 1, this.to);
    const full = ps.map((p) => p.text).join('\n');
    return { from: this.from, to: this.to, text: full.slice(0, SAMPLE_CHARS), textTruncated: full.length > SAMPLE_CHARS, textUnavailable: false, approx: false };
  }
  async paragraphCount() { return this.counting ? this.model.paragraphs.length : null; }
  async point(paragraph) {
    const n = Number(paragraph);
    if (!(n >= 1 && n <= this.model.paragraphs.length)) throw new Error(`문서에 ${paragraph}번 문단이 없습니다(문단 ${this.model.paragraphs.length}개)`);
    this.from = n; this.to = n; this.#emit();
  }
}
