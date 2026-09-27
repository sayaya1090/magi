// PowerPoint — 발표 설정(COM 손, 2021): 화면 전환·숨기기·구역·애니메이션 확장·크기·PDF. README.ko.md 「PP-2」.
//
// Office.js 에 길이 없어 COM 손(hand-com)만 하는 도구들이다. 작업창이 손인 호스트(365·Mac)에서는 목록에 없어 이 시나리오의
// 첫 호출이 「모르는 도구」로 끝난다 — 그 호스트에서는 돌리지 않는다. 덱 전체를 바꾸는 호출(all:true, 크기 바꾸기)은
// 사람의 장까지 건드리므로 이 시나리오는 제 장에만 걸고, 크기는 「이미 그 크기」 no-op 과 거절만 잰다.
import { mkdtempSync, readFileSync, rmSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const TITLES = ['발표 설정 점검(시나리오)', '본론', '예비 자료'];
const countOf = (x) => Number(x?.count ?? (x?.slides ?? []).length);

export default {
  id: 'ppt-show',
  app: 'ppt',
  title: '발표 설정 — 전환·숨기기·구역·애니메이션 확장·크기·PDF (COM 손)',
  windowsOnly: true,
  async run(s) {
    const out = mkdtempSync(join(tmpdir(), 'magi-scn-pp-'));
    s.cleanup(() => rmSync(out, { recursive: true, force: true }));
    const before = await s.call('list_slides', {}, '처음');
    const base = countOf(before);
    const sectionsBefore = (before?.sections ?? []).map((x) => x.name);
    const size = before?.slide_size;
    const made = await s.call('add_slides', { slides: [
      { layout: '제목 슬라이드', title: TITLES[0], body: '시나리오' },
      { layout: '제목 및 내용', title: TITLES[1], body: '첫째 이유\n둘째 이유\n셋째 이유' },
      { layout: '제목 및 내용', title: TITLES[2], body: '질문이 오면 보여 줄 표' },
    ] });
    const ids = (made?.slides ?? []).map((r) => String(r.slide_id));
    if (ids.length !== 3) { s.check('세 장을 만들었다', false, JSON.stringify(made).slice(0, 200)); return; }
    const [s1, s2, s3] = ids;
    s.cleanup(async () => {
      const now = await s.call('list_slides', {}, '정리 전');
      // 뒤에서부터 지운다 — 첫 구역은 뒤에 구역이 있는 동안 PowerPoint 가 못 지운다.
      for (const sec of (now?.sections ?? []).map((x) => x.name).filter((n) => !sectionsBefore.includes(n)).reverse()) {
        await s.call('remove_section', { name: sec }, '정리');
      }
      for (const id of ids) await s.call('delete_slide', { slide_id: id }, '정리');
      const after = await s.call('list_slides', {}, '정리 뒤');
      s.check(`정리 뒤 장 수·구역이 처음과 같다(${base})`, countOf(after) === base && JSON.stringify((after?.sections ?? []).map((x) => x.name)) === JSON.stringify(sectionsBefore),
        JSON.stringify({ n: countOf(after), sections: after?.sections }));
    });

    // 1. 화면 전환 — 제 장에만
    const tr = await s.call('set_transition', { slide_id: s2, effect: 'fade', duration: 0.4 });
    s.check('전환이 무엇을 했는지 말한다', (tr?.changed ?? []).join(' ').includes('fade 0.4초'), JSON.stringify(tr?.changed));
    const r2 = await s.call('read_slide', { slide_id: s2 }, '전환 되읽기');
    s.check('read_slide 가 전환을 싣는다', r2?.transition?.effect === 'fade' && Math.abs(r2?.transition?.duration - 0.4) < 0.01, JSON.stringify(r2?.transition));
    await s.call('set_transition', { slide_id: s2, advance_after: 5 }, '자동 넘김만 더함');
    const r2b = await s.call('read_slide', { slide_id: s2 }, '더한 뒤');
    s.check('효과는 두고 자동 넘김만 붙었다', r2b?.transition?.effect === 'fade' && r2b?.transition?.advance_after === 5, JSON.stringify(r2b?.transition));
    await s.refuse('set_transition', { slide_id: s2, effect: 'morph' }, '중 하나입니다', '모르는 효과');
    await s.refuse('set_transition', { slide_id: s2, on_click: false }, 'advance_after', '클릭도 자동도 없음');

    // 2. 숨기기 — 예비 자료
    await s.call('hide_slide', { slide_id: s3 });
    const ls = await s.call('list_slides', {}, '숨김 되읽기');
    const row3 = (ls?.slides ?? []).find((r) => String(r.slide_id) === s3);
    s.check('목차가 숨긴 장을 표시한다', row3?.hidden === true, JSON.stringify(row3));

    // 3. 구역
    const a = await s.call('add_section', { slide_id: s1, name: '시나리오 본문' });
    await s.call('add_section', { slide_id: s3, name: '시나리오 부록' });
    await s.call('add_section', { slide_id: s3, name: '시나리오 부록(이름 바꿈)' }, '같은 장 — 이름만');
    const ls2 = await s.call('list_slides', {}, '구역 되읽기');
    const secOf = (id) => (ls2?.slides ?? []).find((r) => String(r.slide_id) === id)?.section;
    s.check('장마다 구역이 적힌다', secOf(s1) === '시나리오 본문' && secOf(s2) === '시나리오 본문' && secOf(s3) === '시나리오 부록(이름 바꿈)', JSON.stringify([secOf(s1), secOf(s2), secOf(s3)]));
    s.check('구역을 만들었다고 말한다', a?.created === true, JSON.stringify(a).slice(0, 160));
    await s.refuse('remove_section', { name: '없는 구역' }, '있는 구역', '없는 구역');

    // 4. 애니메이션 — 들어오기·강조·끝내기(COM 손만)
    const body = (await s.call('read_slide', { slide_id: s2 }, '도형'))?.shapes?.find((sh) => /둘째/.test(sh.text ?? ''));
    if (body) {
      await s.call('animate_slide', { slide_id: s2, steps: [
        { shape_id: body.shape_id, effect: 'fly', paragraphs: 'each' },
        { shape_id: body.shape_id, effect: 'bold_flash' },
        { shape_id: body.shape_id, effect: 'fade_out' },
      ] });
      const an = await s.call('read_animation', { slide_id: s2 });
      const kinds = [...new Set((an?.steps ?? []).map((x) => x.kind))];
      s.check('들어오기·강조·끝내기가 다 되읽힌다', ['entrance', 'emphasis', 'exit'].every((k) => kinds.includes(k)) && an?.all_known === true, JSON.stringify(an?.steps).slice(0, 220));
      await s.refuse('animate_slide', { slide_id: s2, steps: [{ shape_id: body.shape_id, effect: 'motion_path' }] }, '끝내기', '이동 경로');
    }

    // 5. 크기 — 덱 전체라 바꾸지 않고, 같은 크기 no-op 과 거절만
    if (size) {
      const same = await s.call('set_slide_size', { width: size.width, height: size.height }, '같은 크기');
      s.check('같은 크기는 안 바꿨다고 말한다', (same?.changed ?? []).join(' ').includes('이미'), JSON.stringify(same?.changed));
    }
    await s.refuse('set_slide_size', { size: '21:9' }, '중 하나입니다', '모르는 크기');

    // 6. PDF — 숨긴 장은 빠진다
    const pdf = join(out, '발표.pdf');
    const ex = await s.call('export_pdf', { path: pdf });
    const bytes = existsSync(pdf) ? readFileSync(pdf) : Buffer.alloc(0);
    s.check('PDF 파일이 진짜 PDF 다', bytes.subarray(0, 4).toString() === '%PDF', `${bytes.length}B`);
    const hidden = (ls2?.slides ?? []).filter((r) => r.hidden).length;
    s.check('숨긴 장을 뺐다고 말한다', ex?.hidden_skipped === hidden, JSON.stringify(ex).slice(0, 160));
    await s.refuse('export_pdf', { path: pdf }, 'overwrite', '있는 파일');

    // 7. 파일에서 직접
    const com = s.com(`$p = Get-MagiDeck '${s.document.label}'
      $sl = @($p.Slides | Where-Object { $_.SlideID -eq ${s2} })[0]; $h = @($p.Slides | Where-Object { $_.SlideID -eq ${s3} })[0]
      @{ effect = [int]$sl.SlideShowTransition.EntryEffect; advance = $sl.SlideShowTransition.AdvanceOnTime; hidden = $h.SlideShowTransition.Hidden; sections = $p.SectionProperties.Count } | ConvertTo-Json -Compress`);
    if (com) {
      s.check('COM: 전환 페이드(ppEffectFadeSmoothly=3849)·자동 넘김·숨김', com.effect === 3849 && com.advance === -1 && com.hidden === -1, JSON.stringify(com));
    }
  },
};
