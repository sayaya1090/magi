// Excel — COM 전용 도구(Windows): 목표값 찾기·스파크라인·슬라이서·PDF. README.ko.md 「XL-2」.
//
// Office.js 에 길이 없어 헬퍼가 COM 으로 하는 도구들이다(clients/office/helper/xl_extra.go). Windows 가 아니면 이 도구들은
// 목록에 없으므로 이 시나리오는 건너뛴다.
import { mkdtempSync, readFileSync, rmSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const SHEET = '시나리오-COM';

export default {
  id: 'xl-com',
  app: 'xl',
  title: 'COM 전용 — 목표값 찾기·스파크라인·슬라이서·PDF',
  windowsOnly: true,
  async run(s) {
    const out = mkdtempSync(join(tmpdir(), 'magi-scn-xl-'));
    s.cleanup(() => rmSync(out, { recursive: true, force: true }));
    if (!(await s.call('add_sheet', { name: SHEET, activate: true }))) return;
    s.cleanup(() => s.call('delete_sheet', { sheet: SHEET }, '정리'));

    // 1. 이익 = 매출 − 비용. 이익이 1000 이 되려면 매출이 얼마?
    await s.call('write_range', { sheet: SHEET, address: 'A1:B3', values: [['매출', 1500], ['비용', 800], ['이익', null]] });
    await s.call('write_range', { sheet: SHEET, address: 'B3', formulas: [['=B1-B2']] });
    const g = await s.call('goal_seek', { sheet: SHEET, cell: 'B3', goal: 1000, changing: 'B1' });
    s.check('목표값: 매출 1800 을 찾았고 원래 1500 을 댄다', g?.found === true && Number(g?.changing_value) === 1800 && Number(g?.changing_was) === 1500, JSON.stringify(g).slice(0, 200));
    const back = await s.call('read_range', { sheet: SHEET, address: 'B1:B3' }, '되읽기');
    s.check('통장에도 1800 이 들어가 이익이 1000 이다', JSON.stringify(back?.values) === JSON.stringify([[1800], [800], [1000]]), JSON.stringify(back?.values));
    await s.refuse('goal_seek', { sheet: SHEET, cell: 'B1', goal: 5, changing: 'B2' }, '수식이 없습니다', '수식 아닌 칸');
    await s.refuse('goal_seek', { sheet: SHEET, cell: 'B3', goal: 5, changing: 'B3' }, '같은 칸', '같은 칸');

    // 2. 스파크라인 — 네 달 추세를 칸마다
    await s.call('write_range', { sheet: SHEET, address: 'D1:H4', values: [['지역', '1월', '2월', '3월', '4월'], ['서울', 10, 14, 12, 18], ['부산', 8, 7, 9, 11], ['대구', 5, 6, 4, 7]] });
    const sp = await s.call('add_sparklines', { sheet: SHEET, address: 'I2:I4', source: 'E2:H4', kind: 'line', color: '#1F4E79', markers: true });
    s.check('스파크라인 세 칸', sp?.cells === 3, JSON.stringify(sp).slice(0, 160));
    await s.refuse('add_sparklines', { sheet: SHEET, address: 'I2', source: 'E2:H2', kind: 'pie' }, 'kind 는', '모르는 종류');
    const com1 = s.com(`$w = Get-MagiWorkbook '${s.document.label}'; $sh = $w.Worksheets.Item('${SHEET}')
      @{ groups = $sh.Range('I2:I4').SparklineGroups.Count; markers = $sh.Range('I2').SparklineGroups.Item(1).Points.Markers.Visible } | ConvertTo-Json -Compress`);
    if (com1) s.check('COM: 스파크라인 무리 하나·표식 켜짐', com1.groups === 1 && com1.markers === true, JSON.stringify(com1));
    await s.call('remove_sparklines', { sheet: SHEET, address: 'I2:I4' });
    await s.refuse('remove_sparklines', { sheet: SHEET, address: 'I2:I4' }, '없습니다', '이미 지운 자리');

    // 3. 표 + 슬라이서
    await s.call('add_table', { sheet: SHEET, address: 'D1:H4', name: 'COM표', has_headers: true });
    await s.refuse('add_slicer', { source: 'COM표', field: '지점', sheet: SHEET }, '지역', '없는 열 — 있는 열을 댄다');
    await s.refuse('add_slicer', { source: '없는표', field: '지역', sheet: SHEET }, 'COM표', '없는 표 — 있는 것을 댄다');
    const sl = await s.call('add_slicer', { source: 'COM표', field: '지역', sheet: SHEET, name: '지역슬라이서', left: 420, top: 10 });
    s.check('슬라이서가 이름대로 섰다', sl?.slicer === '지역슬라이서' && sl?.source_kind === 'table', JSON.stringify(sl).slice(0, 160));
    const com2 = s.com(`$w = Get-MagiWorkbook '${s.document.label}'; @{ caches = $w.SlicerCaches.Count; names = @($w.SlicerCaches | ForEach-Object { $_.Slicers | ForEach-Object { $_.Name } }) } | ConvertTo-Json -Compress`);
    if (com2) s.check('COM: 슬라이서가 통장에 있다', [].concat(com2.names).includes('지역슬라이서'), JSON.stringify(com2));
    if (sl) await s.call('remove_slicer', { name: '지역슬라이서' });
    const com3 = s.com(`$w = Get-MagiWorkbook '${s.document.label}'; @{ caches = $w.SlicerCaches.Count } | ConvertTo-Json -Compress`);
    if (com2 && com3) s.check('COM: 지운 뒤 빈 캐시도 안 남았다', com3.caches === com2.caches - 1, `${com2.caches} → ${com3.caches}`);
    await s.refuse('remove_slicer', { name: '지역슬라이서' }, /없습니다/, '이미 지운 것');

    // 4. PDF — 시트 하나
    const pdf = join(out, '보고.pdf');
    const ex = await s.call('export_pdf', { sheet: SHEET, path: pdf });
    s.check('PDF 파일이 진짜 PDF 다', existsSync(pdf) && readFileSync(pdf).subarray(0, 4).toString() === '%PDF' && ex?.what === `시트 ${SHEET}`, JSON.stringify(ex).slice(0, 160));
    await s.refuse('export_pdf', { sheet: SHEET, path: pdf }, 'overwrite', '있는 파일');
    await s.call('export_pdf', { sheet: SHEET, path: pdf, overwrite: true }, '덮어쓰기');
    await s.refuse('export_pdf', { path: 'rel.pdf' }, '전체 경로', '상대 경로');
  },
};
