/**
 * 인용 — 사람이 시트에서 잡은 범위 하나를 대화에 붙인 것.
 *
 * 값은 **표본**이다(어댑터가 12×12 안쪽으로 자른다). 모델이 받는 것은 `toPrompt()` 가 짓는 글이고,
 * 범위 주소가 들어 있으니 모델은 필요하면 `read_range` 로 전부 읽는다.
 */
export class Quote {
  constructor({ sheet, sheetIndex, address, rowCount, columnCount, values, valuesTruncated = false,
                textUnavailable = false }) {
    this.sheet = sheet ?? '';
    this.sheetIndex = sheetIndex ?? null;
    this.address = address ?? '';
    this.rowCount = rowCount ?? 0;
    this.columnCount = columnCount ?? 0;
    this.values = Object.freeze((values ?? []).map((r) => Object.freeze([...r])));
    this.valuesTruncated = Boolean(valuesTruncated);
    this.textUnavailable = Boolean(textUnavailable);
    Object.freeze(this);
  }
  /** 화면의 첫 줄: `2분기!B2:E9`. */
  get headline() { return this.sheet ? `${this.sheet}!${this.address}` : this.address; }
  get where() { return this.sheet ? `시트 ${this.sheet}` : '시트 ?'; }
  /** 셀 하나·범위 하나를 가르는 열쇠 — 같은 범위를 두 번 인용하지 않게. */
  get key() { return `${this.sheet}!${this.address}`; }
  /** 첫 칸부터 이어 붙인 미리보기. */
  preview(limit = 60) {
    const flat = [];
    for (const row of this.values) for (const v of row) if (v !== '' && v != null) flat.push(String(v));
    const t = flat.join(' | ').replace(/\s+/g, ' ').trim();
    return t.length > limit ? t.slice(0, limit - 1) + '…' : t;
  }
  get text() { return this.preview(400); }
  get sizeLabel() { return `${this.rowCount}×${this.columnCount}`; }
  toPrompt(limit = 1200) {
    const head = `[인용] sheet="${this.sheet}" range=${this.address} size=${this.rowCount}x${this.columnCount}`;
    if (this.textUnavailable) return `${head} valuesUnavailable=true`;
    if (this.values.length === 0) return head;
    const rows = this.values.map((r) => r.map((v) => (v == null ? '' : String(v))).join('\t'));
    let body = rows.join('\n');
    const cut = body.length > limit;
    if (cut) body = body.slice(0, limit);
    return `${head}${this.valuesTruncated || cut ? ' valuesTruncated=true' : ''}\n${body}`;
  }
}
