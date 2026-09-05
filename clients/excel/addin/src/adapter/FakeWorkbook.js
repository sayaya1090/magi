import { WorkbookPort } from '../port/WorkbookPort.js';
import { SAMPLE_ROWS, SAMPLE_COLS } from './OfficeWorkbook.js';
import { parseAddress, cellName } from './a1.js';

/** 가짜 통합 문서 — 브라우저 목업과 시험용. 진짜와 같은 문(WorkbookPort)으로 답한다. */
export class FakeWorkbook extends WorkbookPort {
  constructor(model) {
    super();
    this.model = model;
    this.currentSheet = model.active ?? model.sheets[0].name;
    this.selected = 'A1';
    this.listeners = new Set();
    this.naming = true;    // false = 시트 목록을 못 주는 호스트 흉내
    this.reading = true;   // false = 선택을 못 읽는 호스트 흉내
  }
  capabilities() { return { measured: false, note: '가짜 통합 문서 — 호스트가 없어 잰 것이 없다', sets: [] }; }
  get label() { return '가짜 통합 문서 (Excel 없이)'; }
  onChange(fn) { this.listeners.add(fn); return () => this.listeners.delete(fn); }
  #emit() { for (const fn of this.listeners) fn(); }
  sheet(name) { return this.model.sheets.find((s) => s.name === name); }
  select(address) { this.selected = address; this.#emit(); }
  goTo(name) { if (this.currentSheet !== name) { this.currentSheet = name; this.selected = 'A1'; this.#emit(); } }
  async selection() {
    if (!this.reading) throw new Error('통합 문서가 선택을 안 내줍니다 (가짜 손잡이)');
    const sheet = this.sheet(this.currentSheet);
    const { top, left, rows, cols } = parseAddress(this.selected);
    const r = Math.min(rows, SAMPLE_ROWS); const c = Math.min(cols, SAMPLE_COLS);
    const values = [];
    for (let i = 0; i < r; i += 1) {
      const row = [];
      for (let j = 0; j < c; j += 1) row.push(sheet.cells[cellName(top + i, left + j)]?.v ?? '');
      values.push(row);
    }
    return {
      sheet: sheet.name, sheetIndex: this.model.sheets.indexOf(sheet) + 1, address: this.selected,
      rowCount: rows, columnCount: cols, values, valuesTruncated: r < rows || c < cols, textUnavailable: false,
    };
  }
  async sheetNames() {
    if (!this.naming) return null;
    return new Map(this.model.sheets.map((s, i) => [s.name, i + 1]));
  }
  async point(sheet, address) {
    const target = sheet ? this.sheet(sheet) : this.sheet(this.currentSheet);
    if (!target) throw new Error(`통합 문서에 없는 시트입니다: ${sheet}`);
    if (address) parseAddress(address); // 틀린 주소는 여기서 던진다
    this.currentSheet = target.name;
    if (address) this.selected = address;
    this.#emit();
  }
}
