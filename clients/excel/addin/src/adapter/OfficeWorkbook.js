import { WorkbookPort } from '../port/WorkbookPort.js';

/**
 * 진짜 Excel 에 붙는 통합 문서 어댑터. **이 파일과 `ExcelHand` 만 Office 를 안다.**
 *
 * 통합 문서의 안정된 이름은 workbook.settings(ExcelApi 1.4)에 적어 둔다 — 파워포인트 판이 태그에 적던 것과
 * 같은 이유: 저장 안 한 새 문서는 이름이 없고, 새 문서 둘이 한 대화에 겹친다. settings 는 파일과 함께 저장되고
 * 사람 눈에는 안 보인다.
 */
export const BOOK_SETTING = 'MAGI.BOOK';
/** 인용 표본의 상한 — 이보다 큰 범위는 잘라 보내고 `valuesTruncated` 를 적는다. */
export const SAMPLE_ROWS = 12;
export const SAMPLE_COLS = 12;

export async function stableBookId(runner, note) {
  const say = note ?? ((m) => { if (typeof console !== 'undefined') console.warn('[magi] 통합 문서 이름:', m); });
  const run = runner ?? (typeof Excel === 'undefined' ? null : Excel.run);
  if (!run) return '';
  try {
    const got = await run(async (context) => {
      const settings = context.workbook.settings;
      const had = settings.getItemOrNullObject(BOOK_SETTING);
      had.load('value,isNullObject');
      await context.sync();
      if (had.isNullObject !== true && had.value) return String(had.value);
      const made = newBookId();
      settings.add(BOOK_SETTING, made);
      await context.sync();
      return made;
    });
    if (got) return got;
  } catch (e) {
    say(`settings 에 못 적었습니다: ${e?.message ?? e}`);
  }
  return '';
}
function newBookId() {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) return `book-${crypto.randomUUID()}`;
  return `book-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

export class OfficeWorkbook extends WorkbookPort {
  constructor({ run } = {}) {
    super();
    this.run = run ?? ((fn) => Excel.run(fn));
  }
  get label() { return 'Excel (Office.js)'; }
  get isHost() { return true; }
  capabilities() {
    const req = (typeof Office !== 'undefined') && Office.context && Office.context.requirements;
    if (!req || typeof req.isSetSupported !== 'function') {
      return { measured: false, note: 'Office.context.requirements 가 없다', sets: [] };
    }
    const want = [
      ['ExcelApi', '1.7'], ['ExcelApi', '1.8'], ['ExcelApi', '1.9'], ['ExcelApi', '1.10'],
      ['ExcelApi', '1.11'], ['ExcelApi', '1.12'], ['ExcelApi', '1.13'], ['ExcelApi', '1.14'],
      ['SharedRuntime', '1.1'],
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
      const range = context.workbook.getSelectedRange();
      range.load('address,rowCount,columnCount');
      const sheet = range.worksheet;
      sheet.load('name,position');
      await context.sync();
      const rows = Math.min(range.rowCount, SAMPLE_ROWS);
      const cols = Math.min(range.columnCount, SAMPLE_COLS);
      const sample = range.getCell(0, 0).getResizedRange(rows - 1, cols - 1);
      sample.load('values');
      let values = []; let textUnavailable = false;
      try { await context.sync(); values = sample.values; } catch { textUnavailable = true; }
      const address = range.address.includes('!') ? range.address.split('!').pop() : range.address;
      return {
        sheet: sheet.name, sheetIndex: sheet.position + 1, address,
        rowCount: range.rowCount, columnCount: range.columnCount,
        values, valuesTruncated: rows < range.rowCount || cols < range.columnCount, textUnavailable,
      };
    });
  }
  async sheetNames() {
    try {
      return await this.run(async (context) => {
        const sheets = context.workbook.worksheets;
        sheets.load('items/name,items/position');
        await context.sync();
        return new Map(sheets.items.map((s) => [s.name, s.position + 1]));
      });
    } catch {
      return null;
    }
  }
  async point(sheet, address) {
    return this.run(async (context) => {
      const ws = sheet ? context.workbook.worksheets.getItem(sheet) : context.workbook.worksheets.getActiveWorksheet();
      ws.activate();
      if (address) ws.getRange(address).select();
      await context.sync();
    });
  }
}
