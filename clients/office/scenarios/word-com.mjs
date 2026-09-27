// Word — COM 전용 도구(Windows): 맞춤법 검사·문서 통계·PDF·문서 비교. README.ko.md 「WD-2」.
//
// Office.js 에 길이 없어 헬퍼가 COM 으로 하는 도구들이다(clients/office/helper/word_extra.go). 비교는 견줄 .docx 를 COM 으로
// 만들어야 해서 --com 일 때만 돈다.
import { mkdtempSync, readFileSync, rmSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const LINES = ['시나리오 교정 점검', 'This sentense has a speling mistake.', '두 번째 문단은 멀쩡하다.'];

export default {
  id: 'word-com',
  app: 'word',
  title: 'COM 전용 — 맞춤법 검사·문서 통계·PDF·문서 비교',
  windowsOnly: true,
  async run(s) {
    const out = mkdtempSync(join(tmpdir(), 'magi-scn-wd-'));
    s.cleanup(() => rmSync(out, { recursive: true, force: true }));
    const first = await s.call('list_paragraphs', {}, '처음');
    const base = Number(first?.total ?? 0);
    const ins = await s.call('insert_paragraphs', { at: 'end', lines: LINES, style: 'Normal' });
    if (!ins) return;
    const p1 = ins.from ?? base + 1;
    const p3 = p1 + LINES.length - 1;
    s.cleanup(async () => {
      const end = Number((await s.call('list_paragraphs', {}, '정리 전'))?.total ?? 0);
      if (p1 > 1) { if (end >= p1) await s.call('delete_paragraphs', { from: p1, to: end }, '정리'); }
      else {
        if (end > 1) await s.call('delete_paragraphs', { from: 2, to: end }, '정리(빈 문서)');
        await s.call('replace_paragraph', { paragraph: 1, text: '' }, '정리(빈 문서)');
      }
      const last = await s.call('list_paragraphs', {}, '정리 뒤');
      s.check(`정리 뒤 문단 수가 처음과 같다(${base})`, Number(last?.total) === base, String(last?.total));
    });

    // 1. 맞춤법 — Word 가 밑줄 친 것을 제안과 함께
    const pr = await s.call('proofread', { from: p1, to: p3 });
    const items = pr?.items ?? [];
    const typo = items.find((i) => i.text === 'sentense');
    s.check('영어 오타를 짚고 제안에 sentence 가 있다', typo?.kind === 'spelling' && typo?.paragraph === p1 + 1 && (typo?.suggestions ?? []).includes('sentence'), JSON.stringify(items).slice(0, 220));
    s.check('오타 둘을 다 짚었다', items.some((i) => i.text === 'speling'), JSON.stringify(items.map((i) => i.text)));
    s.check('답이 무엇을 셌는지 말한다', (pr?.changed ?? []).join(' ').includes('맞춤법'), JSON.stringify(pr?.changed));
    const one = await s.call('proofread', { from: p1 + 2 }, 'from 만 — 그 문단 하나');
    s.check('멀쩡한 문단 하나만 보면 영어 오타는 없다', !(one?.items ?? []).some((i) => i.text === 'sentense'), JSON.stringify(one?.items).slice(0, 160));
    await s.refuse('proofread', { from: 99999 }, '문단', '없는 문단');
    await s.call('replace_all', { find: 'sentense', replace: 'sentence', from: p1, to: p3 }, '짚은 것을 고침');
    const again = await s.call('proofread', { from: p1, to: p3 }, '고친 뒤');
    s.check('고친 뒤에는 그 오타가 없다', !(again?.items ?? []).some((i) => i.text === 'sentense'), JSON.stringify(again?.items).slice(0, 160));

    // 2. 통계
    const st = await s.call('document_stats', {});
    s.check('쪽·단어 수가 선다', st?.pages >= 1 && st?.words >= 10 && st?.paragraphs >= LINES.length, JSON.stringify(st).slice(0, 200));

    // 3. PDF
    const pdf = join(out, '교정.pdf');
    await s.call('export_pdf', { path: pdf });
    s.check('PDF 파일이 진짜 PDF 다', existsSync(pdf) && readFileSync(pdf).subarray(0, 4).toString() === '%PDF');
    await s.refuse('export_pdf', { path: pdf }, 'overwrite', '있는 파일');

    // 4. 비교 — 견줄 판을 COM 으로 만든다(--com 일 때만)
    const other = join(out, '수정판.docx');
    const made = s.com(`$wd = [Runtime.InteropServices.Marshal]::GetActiveObject('Word.Application')
      $d = $wd.Documents.Add(); $d.Content.Text = "시나리오 교정 점검\`rThis sentence has a spelling mistake.\`r두 번째 문단은 고쳤다."
      $d.SaveAs2('${other.replace(/'/g, "''")}'); $d.Close(0); @{ ok = (Test-Path '${other.replace(/'/g, "''")}') } | ConvertTo-Json -Compress`);
    if (made?.ok) {
      const cmp = await s.call('compare_documents', { path: other });
      s.check('비교 결과가 새 문서로 열리고 변경이 있다', cmp?.revisions > 0 && typeof cmp?.result_document === 'string', JSON.stringify(cmp).slice(0, 200));
      if (cmp?.result_document) {
        s.cleanup(() => { s.com(`$wd = [Runtime.InteropServices.Marshal]::GetActiveObject('Word.Application'); foreach ($d in @($wd.Documents)) { if ($d.Name -eq '${cmp.result_document}') { $d.Close(0) } }; @{ ok = $true } | ConvertTo-Json -Compress`); });
      }
      await s.refuse('compare_documents', { path: join(out, '없음.docx') }, '파일이 없습니다', '없는 파일');
    } else {
      s.note('비교는 건너뜀 — 견줄 파일을 만들려면 --com');
    }
  },
};
