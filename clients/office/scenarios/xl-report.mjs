// Excel — 월 매출 보고서를 빈 시트에서 끝까지 만든다. README.ko.md 「XL-1」.
//
// 도구 하나씩은 스윕이 본다. 여기서 보는 것은 **앞 도구가 남긴 것 위에서 뒤 도구가 맞게 도는가** — 표로 만든 뒤의 정렬,
// 정렬한 뒤의 이름 합계, 스냅숏을 뜬 뒤 망가뜨리고 되돌리기, 보호한 시트에 쓰기.
const SHEET = '시나리오-매출';
const rows = [
  ['서울', '1월', 1200, 800],
  ['부산', '1월', 700, 500],
  ['서울', '2월', 1500, 900],
  ['대구', '2월', 400, 450],
  ['부산', '2월', 900, 600],
  ['대구', '1월', 650, 300],
];
const flat = (v) => (Array.isArray(v) ? v.flat(Infinity) : []);
const cellsOf = (r) => r?.values ?? r?.cells ?? r?.rows ?? [];

export default {
  id: 'xl-report',
  app: 'xl',
  title: '월 매출 보고서 — 표·정렬·이름·차트·피벗·보호·되돌리기',
  async run(s) {
    const made = await s.call('add_sheet', { name: SHEET, activate: true });
    if (!made) return;
    s.cleanup(() => s.call('delete_sheet', { sheet: SHEET }, '정리'));

    // 1. 데이터와 수식
    await s.call('write_range', { sheet: SHEET, address: 'A1:D7', values: [['지역', '월', '매출', '비용'], ...rows] });
    await s.call('write_range', { sheet: SHEET, address: 'E1:E7', formulas: [['이익'], ...rows.map((_, i) => [`=C${i + 2}-D${i + 2}`])] });
    const back = await s.call('read_range', { sheet: SHEET, address: 'E2:E7' }, '이익 되읽기');
    s.check('이익 열이 매출−비용으로 계산됐다', JSON.stringify(flat(cellsOf(back)).map(Number)) === JSON.stringify(rows.map((r) => r[2] - r[3])), JSON.stringify(back).slice(0, 200));

    // 2. 표로 만들고 정렬 — 정렬 뒤에도 수식이 자기 행을 따라가야 한다
    await s.call('add_table', { sheet: SHEET, address: 'A1:E7', name: '매출표', has_headers: true, table_style: 'TableStyleMedium2' });
    await s.refuse('add_table', { sheet: SHEET, address: 'B2:C4', name: '겹친표' }, /./, '이미 표인 자리');
    await s.call('sort_range', { table: '매출표', by: [{ column: '매출', ascending: false }] });
    const sorted = await s.call('read_table', { table: '매출표' });
    const srows = sorted?.rows ?? sorted?.values ?? [];
    const firstSales = srows.map((r) => Number(Array.isArray(r) ? r[2] : r['매출']));
    s.check('매출 내림차순으로 섰다', firstSales.length === 6 && firstSales.every((v, i) => i === 0 || firstSales[i - 1] >= v), JSON.stringify(firstSales));
    const profitOk = srows.every((r) => { const a = Array.isArray(r) ? r : [r['지역'], r['월'], r['매출'], r['비용'], r['이익']]; return Number(a[4]) === Number(a[2]) - Number(a[3]); });
    s.check('정렬 뒤에도 이익이 제 행의 매출−비용이다', srows.length === 6 && profitOk, JSON.stringify(srows).slice(0, 200));

    // 3. 행 추가 → 이름 → 이름을 쓰는 수식
    await s.call('add_table_rows', { table: '매출표', rows: [['광주', '2월', 300, 100, null]] });
    await s.call('set_name', { name: '총매출', refers_to: '=SUM(매출표[매출])' });
    s.cleanup(() => s.call('delete_name', { name: '총매출' }, '정리'));
    await s.call('write_range', { sheet: SHEET, address: 'G1:G2', formulas: [['총매출'], ['=총매출']] });
    const total = await s.call('read_range', { sheet: SHEET, address: 'G2' });
    const want = rows.reduce((a, r) => a + r[2], 0) + 300;
    s.check(`이름 합계가 새 행까지 센다(${want})`, Number(flat(cellsOf(total))[0]) === want, JSON.stringify(total).slice(0, 160));
    const trace = await s.call('trace_cell', { sheet: SHEET, address: 'G2', what: 'precedents' });
    s.check('G2 의 선행이 잡힌다', JSON.stringify(trace ?? {}).includes('매출') || JSON.stringify(trace ?? {}).includes('C'), JSON.stringify(trace).slice(0, 160));

    // 4. 서식 — 숫자 형식, 조건부 서식, 유효성
    await s.call('set_number_format', { sheet: SHEET, address: 'C2:E8', format: '#,##0' });
    await s.call('add_conditional_format', { sheet: SHEET, address: 'C2:C8', cf_kind: 'data_bar' });
    const cfs = await s.call('read_conditional_formats', { sheet: SHEET, address: 'C2:C8' });
    s.check('조건부 서식이 하나 이상 읽힌다', JSON.stringify(cfs ?? {}).toLowerCase().includes('databar') || (cfs?.formats ?? cfs?.rules ?? []).length > 0, JSON.stringify(cfs).slice(0, 160));
    await s.call('set_validation', { sheet: SHEET, address: 'B2:B8', validation_kind: 'list', values: ['1월', '2월', '3월'] });
    const val = await s.call('read_validation', { sheet: SHEET, address: 'B2' });
    s.check('유효성 목록이 읽힌다', JSON.stringify(val ?? {}).includes('2월'), JSON.stringify(val).slice(0, 160));

    // 5. 차트 — 만들고, 고치고, 되읽기
    const chart = await s.call('add_chart', { sheet: SHEET, source: 'A1:C8', chart_type: 'ColumnClustered', title: '지역별 매출', name: '매출차트', left: 420, top: 20, width: 360, height: 220 });
    const cname = chart?.chart ?? chart?.name ?? '매출차트';
    await s.call('format_chart', { sheet: SHEET, chart: cname, title: '지역별 매출(원)', legend: 'Bottom', data_labels: true });
    const rc = await s.call('read_chart', { sheet: SHEET, chart: cname });
    s.check('차트 제목이 고친 대로다', JSON.stringify(rc ?? {}).includes('지역별 매출(원)'), JSON.stringify(rc).slice(0, 160));

    // 6. 피벗 — 표 이름을 원본으로. 없는 필드는 있는 머리글을 적어 거절해야 한다(2026-09-27 전에는 ItemNotFound 한 줄)
    await s.refuse('add_pivot', { source: '매출표', destination: `'${SHEET}'!H10`, rows: ['지점'], values: [{ field: '매출' }] }, '지역', '없는 필드');
    const pv = await s.call('add_pivot', { source: '매출표', destination: `'${SHEET}'!H10`, name: '지역피벗', rows: ['지역'], values: [{ field: '매출', function: 'Sum' }] });
    if (pv) {
      const pr = await s.call('read_range', { sheet: SHEET, address: 'H10:I16' }, '피벗 되읽기');
      s.check('피벗에 서울 합계 2700 이 있다', JSON.stringify(pr ?? {}).includes('2700'), JSON.stringify(pr).slice(0, 200));
    }

    // 7. 스냅숏 → 망가뜨림 → 되돌림
    const snap = await s.call('snapshot_range', { sheet: SHEET, address: 'A1:E8' });
    const snapId = snap?.snapshot ?? snap?.id;
    await s.call('clear_range', { sheet: SHEET, address: 'C2:D4', what: 'contents' }, '일부러 지움');
    if (snapId) await s.call('restore_range', { snapshot: snapId });
    const restored = await s.call('read_table', { table: '매출표' }, '되돌린 뒤');
    const rrows = restored?.rows ?? restored?.values ?? [];
    s.check('되돌린 뒤 매출·비용이 비지 않았다', rrows.length === 7 && rrows.every((r) => { const a = Array.isArray(r) ? r : Object.values(r); return a[2] !== '' && a[2] != null && a[3] !== '' && a[3] != null; }), JSON.stringify(rrows).slice(0, 200));

    // 8. 보호 — 잠근 시트는 못 쓰고, 풀면 쓴다
    await s.call('protect_sheet', { sheet: SHEET });
    await s.refuse('write_range', { sheet: SHEET, address: 'K1', values: [['막혀야 함']] }, /./, '보호된 시트');
    await s.call('unprotect_sheet', { sheet: SHEET });
    await s.call('write_range', { sheet: SHEET, address: 'K1', values: [['풀림']] }, '보호 푼 뒤');

    // 9. 찾기·바꾸기 — 표 두 칸과 피벗의 행 이름 한 칸. 피벗 칸은 Excel 이 못 바꾸는데, 앞 판은 그것까지 「바꿨다」고 셌다(2026-09-27).
    //    바꾼 수는 실제로 바뀐 칸이고, 안 바뀐 칸은 이름을 대고, 피벗은 새로 고쳐야 새 이름을 따른다.
    const found = await s.call('find', { sheet: SHEET, text: '대구', whole_cell: true });
    s.check('대구가 표 두 칸 + 피벗 한 칸', (found?.hits ?? []).length === (pv ? 3 : 2), JSON.stringify(found?.hits ?? found).slice(0, 200));
    const rep = await s.call('replace_all', { sheet: SHEET, find: '대구', replace: '대구광역시', whole_cell: true });
    s.check('바꾼 수는 실제로 바뀐 칸(2)', rep?.cells === 2, JSON.stringify(rep?.sheets ?? rep).slice(0, 200));
    if (pv) {
      s.check('안 바뀐 피벗 칸을 이름으로 댄다', JSON.stringify(rep?.sheets ?? []).includes('H12') && (rep?.changed ?? []).join(' ').includes('안 바뀐 칸'), JSON.stringify(rep?.changed));
      await s.call('refresh_pivot', { sheet: SHEET, name: '지역피벗' });
    }
    const after = await s.call('find', { sheet: SHEET, text: '대구광역시', whole_cell: true }, '바꾼 뒤');
    s.check('새 이름이 표와 (새로 고친) 피벗에 있다', (after?.hits ?? []).length === (pv ? 3 : 2), JSON.stringify(after?.hits ?? after).slice(0, 200));

    // 10. 제안 — 붙이고, 읽고, 뗀다
    const sg = await s.call('suggest', { sheet: SHEET, address: 'G2', what: '총매출을 굵게', fix: { tool: 'format_range', args: { sheet: SHEET, address: 'G2', bold: true } } });
    const list = await s.call('read_suggestions', { sheet: SHEET });
    const key = sg?.suggestion ?? sg?.key ?? list?.suggestions?.[0]?.key;
    s.check('제안이 읽힌다', (list?.suggestions ?? []).some((r) => r.key === key), JSON.stringify(list).slice(0, 160));
    if (key) await s.call('drop_suggestion', { key });

    // 11. 파일에서 직접 — COM 으로 값과 표 이름을 대조
    const com = s.com(`$w = Get-MagiWorkbook '${s.document.label}'; $sh = $w.Worksheets.Item('${SHEET}')
      @{ g2 = $sh.Range('G2').Value2; tables = @($sh.ListObjects | ForEach-Object { $_.Name }); k1 = $sh.Range('K1').Text; charts = $sh.ChartObjects().Count } | ConvertTo-Json -Compress`);
    if (com) {
      s.check('COM: G2 합계', Number(com.g2) === want, JSON.stringify(com));
      s.check('COM: 표 이름', [].concat(com.tables).includes('매출표'), JSON.stringify(com));
      s.check('COM: 차트 하나', com.charts === 1, JSON.stringify(com));
    }
  },
};
