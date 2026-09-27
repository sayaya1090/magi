// PowerPoint — 분기 보고 덱을 덱 끝에 세 장 짓고 다듬는다. README.ko.md 「PP-1」.
//
// 보는 것은 도구 사이다: 개요로 여러 장 → 표·차트·도형 → 복제와 순서 → 스냅숏 뒤 망가뜨리고 되돌리기(새 id) → 노트·애니메이션·제안.
// 장은 **id 로만** 부른다 — 순서를 바꾸면 번호가 다른 장을 가리킨다. 2021(COM 손)과 365(작업창 손) 어느 쪽이 붙어도 같은 흐름이다.
const TITLES = ['3분기 보고(시나리오)', '핵심 지표', '다음 분기 계획'];

const idsOf = (x) => (x?.slides ?? x?.made ?? []).map((r) => String(r.slide_id ?? r.id ?? ''));
const shapesOf = (x) => x?.shapes ?? [];
const titleOf = (x) => shapesOf(x).find((sh) => /title/i.test(String(sh.placeholder ?? '')))?.text;

export default {
  id: 'ppt-quarterly',
  app: 'ppt',
  title: '분기 보고 덱 — 개요·표·차트·도형·복제·순서·스냅숏·노트·애니메이션',
  async run(s) {
    const before = await s.call('list_slides', {}, '처음');
    const base = Number(before?.count ?? before?.total ?? (before?.slides ?? []).length);
    const made = await s.call('add_slides', { slides: [
      { layout: '제목 슬라이드', title: TITLES[0], body: '경영지원팀' },
      { layout: '제목 및 내용', title: TITLES[1], body: '매출 12% 증가\n비용 3% 감소\n신규 고객 40곳' },
      { layout: '제목만', title: TITLES[2] },
    ] });
    let ids = idsOf(made);
    if (ids.length !== 3) { // 답에 id 가 안 실렸으면 끝의 세 장을 읽는다
      const ls = await s.call('list_slides', {}, '만든 뒤');
      ids = (ls?.slides ?? []).slice(-3).map((r) => String(r.slide_id));
    }
    s.check('세 장이 덱 끝에 생겼다', ids.length === 3 && ids.every(Boolean), JSON.stringify(made).slice(0, 200));
    const live = new Set(ids); // 정리할 장 — 복제·되돌리기로 id 가 바뀌면 여기서 고친다
    s.cleanup(async () => {
      for (const id of live) await s.call('delete_slide', { slide_id: id }, '정리');
      const after = await s.call('list_slides', {}, '정리 뒤');
      const n = Number(after?.count ?? after?.total ?? (after?.slides ?? []).length);
      s.check(`정리 뒤 장 수가 처음과 같다(${base})`, n === base, String(n));
    });
    const [s1, s2, s3] = ids;
    for (const [i, id] of ids.entries()) {
      const rs = await s.call('read_slide', { slide_id: id }, `${i + 1}장 되읽기`);
      s.check(`${i + 1}장 제목이 개요대로다`, titleOf(rs) === TITLES[i], titleOf(rs));
    }

    // 1. 본문 글자 서식 — 한 낱말만 굵게
    const body2 = shapesOf(await s.call('read_slide', { slide_id: s2 })).find((sh) => /12%/.test(sh.text ?? ''));
    if (body2) {
      await s.call('format_text', { slide_id: s2, shape_id: body2.shape_id, find: '12%', bold: true, color: '#C00000' });
      await s.refuse('format_text', { slide_id: s2, shape_id: body2.shape_id, find: '없는낱말', bold: true }, /./, '없는 낱말');
    }

    // 2. 표 — 만들고, 행 늘리고, 칸 쓰고, 머리 행 서식
    const tb = await s.call('add_table', { slide_id: s3, rows: 3, columns: 3, left: 40, top: 130, width: 420,
      values: [['항목', '3분기', '4분기 목표'], ['매출', '112', '125'], ['고객', '340', '380']] });
    const tid = tb?.shape_id;
    if (tid) {
      await s.call('edit_table', { slide_id: s3, shape_id: tid, add_rows: 1 });
      await s.call('set_table_cells', { slide_id: s3, shape_id: tid, cells: [{ row: 3, column: 0, text: '이익률' }, { row: 3, column: 1, text: '9%' }, { row: 3, column: 2, text: '10%' }] });
      await s.call('format_table_cells', { slide_id: s3, shape_id: tid, row: 0, bold: true, fill: '#1F4E79', color: '#FFFFFF' });
      const rt = shapesOf(await s.call('read_slide', { slide_id: s3 }, '표 되읽기')).find((sh) => sh.shape_id === tid);
      const txt = JSON.stringify(rt ?? {});
      s.check('표에 새 행과 기존 값이 다 있다', txt.includes('이익률') && txt.includes('125') && txt.includes('항목'), txt.slice(0, 240));
    }

    // 3. 차트와 도형 — 없는 도형 이름은 비슷한 이름을 대며 거절(2026-09-26 전에는 네모로 바꿔 세웠다)
    await s.call('add_chart', { slide_id: s3, kind: 'column', categories: ['1분기', '2분기', '3분기'], series: [{ name: '매출', values: [98, 104, 112] }], title: '분기 매출', left: 480, top: 130, width: 420, height: 260 });
    const a = await s.call('add_shape', { slide_id: s1, kind: 'star5', left: 60, top: 400, width: 50, height: 50, fill: '#FFC000' });
    const b = await s.call('add_shape', { slide_id: s1, kind: 'ellipse', left: 140, top: 410, width: 60, height: 40 });
    await s.refuse('add_shape', { slide_id: s1, kind: 'spaceship' }, /./, '모르는 도형');
    if (a && b) {
      await s.call('align_shapes', { slide_id: s1, shape_ids: [a.shape_id, b.shape_id], how: 'top' });
      const g = await s.call('group_shapes', { slide_id: s1, shape_ids: [a.shape_id, b.shape_id] });
      if (g) await s.call('ungroup_shapes', { slide_id: s1, shape_id: g.shape_id ?? g.group_id });
      const kinds = shapesOf(await s.call('read_slide', { slide_id: s1 }, '도형 되읽기')).map((sh) => sh.type);
      s.check('묶었다 푼 뒤 도형이 다시 둘이다(묶음 없음)', !kinds.includes('Group') && kinds.filter((k) => /geometric|autoshape/i.test(String(k))).length >= 2, JSON.stringify(kinds));
    }

    // 4. 복제 → 순서 바꾸기 → 지움. 번호가 아니라 id 로 따라간다
    const dup = await s.call('duplicate_slide', { slide_id: s2 });
    const d = dup?.slide_id && String(dup.slide_id);
    if (d) {
      live.add(d);
      await s.call('reorder_slide', { slide_id: d, to: base + 4 });
      const order = (await s.call('list_slides', {}, '순서'))?.slides?.slice(-4).map((r) => String(r.slide_id)) ?? [];
      s.check('복제본이 맨 끝으로 갔다', JSON.stringify(order) === JSON.stringify([s1, s2, s3, d]), JSON.stringify(order));
      await s.call('delete_slide', { slide_id: d }); live.delete(d);
    }

    // 5. 스냅숏 → 제목 망가뜨림 → 되돌림 — 되돌린 장은 새 id 로 온다
    const snap = await s.call('snapshot_slide', { slide_id: s1 });
    const t1 = shapesOf(await s.call('read_slide', { slide_id: s1 })).find((sh) => /title/i.test(String(sh.placeholder ?? '')));
    if (snap?.snapshot && t1) {
      await s.call('set_text', { slide_id: s1, shape_id: t1.shape_id, text: '망가진 제목' });
      const back = await s.call('restore_slide', { slide_id: s1, snapshot: snap.snapshot });
      const nid = String(back?.slide_id ?? '');
      if (nid) { live.delete(s1); live.add(nid); }
      s.check('되돌린 장이 새 id 로 왔다', nid && nid !== s1, JSON.stringify(back).slice(0, 160));
      const rs = await s.call('read_slide', { slide_id: nid || s1 }, '되돌린 뒤');
      s.check('되돌린 장의 제목이 처음 것이다', titleOf(rs) === TITLES[0], titleOf(rs));
      await s.refuse('read_slide', { slide_id: s1 }, /./, '옛 id');
    }

    // 6. 노트·배경·애니메이션
    await s.call('set_notes', { slide_id: s2, text: '매출 증가는 신규 고객 덕분\n비용은 물류 절감' });
    const notes = await s.call('read_notes', { slide_id: s2 });
    s.check('노트가 되읽힌다', JSON.stringify(notes ?? {}).includes('물류 절감'), JSON.stringify(notes).slice(0, 160));
    await s.call('set_background', { slide_id: s2, color: '#F3F7FB' });
    if (body2) {
      await s.call('animate_slide', { slide_id: s2, steps: [{ shape_id: body2.shape_id, effect: 'fade' }] });
      const an = await s.call('read_animation', { slide_id: s2 });
      s.check('애니메이션 한 단계가 읽힌다', (an?.steps ?? an?.effects ?? []).length === 1, JSON.stringify(an).slice(0, 160));
      // 없는 효과 — 'fly' 는 2021(COM 손)에서 되는 효과가 됐다(2026-09-27). 어느 손에도 없는 이동 경로로 잰다.
      await s.refuse('animate_slide', { slide_id: s2, steps: [{ shape_id: body2.shape_id, effect: 'motion_path' }] }, /./, '없는 효과');
    }

    // 7. 찾기와 제안
    const found = await s.call('find_shapes', { text: '신규 고객' });
    s.check('본문을 글로 찾는다', JSON.stringify(found ?? {}).includes(s2), JSON.stringify(found).slice(0, 160));
    const sg = await s.call('suggest', { slide_id: s3, what: '표 제목을 달자', why: '무슨 표인지 모른다' });
    const rsg = await s.call('read_suggestions', { slide_id: s3 });
    const key = sg?.suggestion ?? rsg?.suggestions?.[0]?.key;
    s.check('제안이 읽힌다', (rsg?.suggestions ?? []).some((r) => r.key === key), JSON.stringify(rsg).slice(0, 160));
    if (key) await s.call('drop_suggestion', { slide_id: s3, key });

    // 8. 파일에서 직접 — 끝의 세 장 제목과 차트
    const com = s.com(`$p = Get-MagiDeck '${s.document.label}'; $n = $p.Slides.Count
      $t = @(); for ($i = [Math]::Max(1, $n - 2); $i -le $n; $i++) { $sl = $p.Slides.Item($i); $t += $(if ($sl.Shapes.HasTitle) { $sl.Shapes.Title.TextFrame.TextRange.Text } else { '' }) }
      $charts = 0; foreach ($sh in $p.Slides.Item($n).Shapes) { if ($sh.HasChart) { $charts++ } }
      @{ count = $n; titles = $t; charts = $charts } | ConvertTo-Json -Compress`);
    if (com) {
      s.check('COM: 끝의 세 장 제목', JSON.stringify(com.titles) === JSON.stringify(TITLES), JSON.stringify(com));
      s.check('COM: 마지막 장에 차트 하나', com.charts === 1, JSON.stringify(com));
    }
  },
};
