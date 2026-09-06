/**
 * 브라우저 목업의 문서. 진짜 Word 없이 화면을 눌러 보는 자리다.
 * 문단은 본문 순서(1부터)이고 표는 `after`(그 문단 뒤) 에 산다. 제안은 settings 에 산다(진짜 판과 같은 규약).
 */
export const fixture = {
  title: '2026 상반기 사업 보고',
  properties: { title: '2026 상반기 사업 보고', author: '기획팀' },
  trackChanges: 'Off',
  paragraphs: [
    { text: '2026 상반기 사업 보고', style: 'Title' },
    { text: '1. 요약', style: 'Heading 1' },
    { text: '상반기 매출은 전년 동기 대비 12% 늘었고, 신규 고객이 전체의 30% 를 차지했다.', style: 'Normal' },
    { text: '비용은 계획 대비 3% 초과했으며, 주된 원인은 물류비 상승이다.', style: 'Normal' },
    { text: '2. 매출', style: 'Heading 1' },
    { text: '분기별 매출은 아래 표와 같다.', style: 'Normal' },
    { text: '주요 성과', style: 'Heading 2' },
    { text: '신규 고객 120곳 확보', style: 'List Paragraph', list: { kind: 'bulleted', level: 0 } },
    { text: '재구매율 68% 로 상승', style: 'List Paragraph', list: { kind: 'bulleted', level: 0 } },
    { text: '3. 하반기 계획', style: 'Heading 1' },
    { text: '하반기에는 물류비를 줄이기 위한 계약 재협상을 진행하고, 신규 시장 두 곳에 진입한다.', style: 'Normal' },
  ],
  tables: [
    { after: 6, hasHeader: true, style: 'GridTable4_Accent1', values: [['분기', '매출', '비고'], ['1분기', '12,000', ''], ['2분기', '15,100', '']] },
  ],
  comments: [],
  bookmarks: {},
  settings: {
    'MAGI.FIX.DEMO1': JSON.stringify({
      what: '「주요 성과」가 제목 2 가 아니라 굵은 본문으로 보입니다 — 스타일로 세우면 목차에 잡힙니다',
      why: '지금은 탐색 창에 안 보입니다',
      fix: { tool: 'set_style', args: { from: 7, to: 7, builtin: 'Heading2' } },
    }),
    'MAGI.FIX.DEMO2': JSON.stringify({
      what: '요약의 둘째 문장이 원인만 말합니다 — 대응을 한 줄 덧붙이면 보고가 닫힙니다',
      why: '읽는 사람이 「그래서?」를 묻습니다',
      fix: { tool: 'replace_paragraph', args: { paragraph: 4, text: '비용은 계획 대비 3% 초과했으며, 주된 원인은 물류비 상승이다. 하반기 계약 재협상으로 대응한다.' } },
    }),
  },
};
