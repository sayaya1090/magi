/**
 * 브라우저 목업의 통합 문서. 진짜 Excel 없이 화면을 눌러 보는 자리다.
 * 셀은 `cells[address] = { v, f?, nf? }` — 값·수식·표시 형식. 제안은 settings 에 산다(진짜 판과 같은 규약).
 */
export const fixture = {
  active: '매출',
  settings: {
    'MAGI.FIX.DEMO1': JSON.stringify({
      what: '합계가 값으로 박혀 있습니다 — 수식으로 바꾸면 위 숫자가 바뀔 때 따라갑니다',
      why: '지금은 B2:B5 를 고쳐도 B6 이 그대로입니다',
      fix: { tool: 'write_range', args: { sheet: '매출', address: 'B6', formulas: [['=SUM(B2:B5)']] } },
    }),
    'MAGI.FIX.DEMO2': JSON.stringify({
      what: '금액에 천 단위 구분이 없습니다',
      why: '4자리 넘는 수는 구분 기호가 있어야 한눈에 읽힙니다',
      fix: { tool: 'set_number_format', args: { sheet: '매출', address: 'B2:B6', format: '#,##0' } },
    }),
  },
  sheets: [
    { name: '매출', cells: {
      A1: { v: '분기' }, B1: { v: '매출' }, C1: { v: '비고' },
      A2: { v: '1분기' }, B2: { v: 12000 },
      A3: { v: '2분기' }, B3: { v: 13500 },
      A4: { v: '3분기' }, B4: { v: 12800 }, C4: { v: '환율 영향' },
      A5: { v: '4분기' }, B5: { v: 15100 },
      A6: { v: '합계' }, B6: { v: 53400 },
    }, tables: [], charts: [] },
    { name: '비용', cells: {
      A1: { v: '항목' }, B1: { v: '금액' },
      A2: { v: '인건비' }, B2: { v: 8000 },
      A3: { v: '임차료' }, B3: { v: 2400 },
    }, tables: [], charts: [] },
  ],
};
