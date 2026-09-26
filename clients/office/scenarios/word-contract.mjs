// Word — 계약서 초안을 문서 끝에 써 놓고 검토 한 바퀴를 돈다. README.ko.md 「WD-1」.
//
// 보는 것은 도구 사이다: 추적을 켠 뒤의 고치기와 거부, 댓글에 답하고 닫기, 스냅숏 뒤 지우고 되돌리기, 표를 늘린 뒤 되읽기.
// 문단 번호는 **글로 찾는다** — 표·목록이 끼면 번호가 밀리므로 짐작하지 않는다(2026-09-26 스윕이 빈 문서에서 하나 밀렸다).
const T = {
  title: '물품 공급 계약서(시나리오)',
  h1: '제1조 목적',
  body: '이 계약은 갑이 을에게 물품을 공급하는 조건을 정한다.',
  h2: '제2조 대금',
  pay: '대금은 납품 후 30일 이내에 지급한다.',
  tail: '을: (주)시나리오',
};
const TAG = 'magi-scn-party';

export default {
  id: 'word-contract',
  app: 'word',
  title: '계약서 초안 — 스타일·목록·표·각주·추적·댓글·되돌리기·콘텐츠 컨트롤',
  async run(s) {
    const first = await s.call('list_paragraphs', {}, '처음');
    const base = Number(first?.total ?? 0);
    const baseTables = Number(first?.tables ?? 0);
    const ins = await s.call('insert_paragraphs', { at: 'end', lines: [T.title, T.h1, T.body, T.h2, T.pay, T.tail] });
    if (!ins) return;
    const p1 = ins.from ?? base + 1; // 빈 문서면 1 — Word 가 빈 마지막 문단을 먼저 채운다
    let tableMade = false;
    // 정리는 **처음 모양으로 돌아왔는지까지** 잰다 — 문단 수, 빈 문서였으면 첫 문단의 스타일. 앞 판은 빈 줄 하나와 제목
    // 스타일을 남기고도 「정리」가 다 통과였다(2026-09-27).
    s.cleanup(async () => {
      await s.call('set_track_changes', { mode: 'Off' }, '정리');
      if (tableMade) await s.call('delete_table', { table: baseTables + 1 }, '정리');
      const end = Number((await s.call('list_paragraphs', {}, '정리 전'))?.total ?? 0);
      if (p1 > 1) {
        if (end >= p1) await s.call('delete_paragraphs', { from: p1, to: end }, '정리');
      } else {
        if (end > 1) await s.call('delete_paragraphs', { from: 2, to: end }, '정리(빈 문서)');
        await s.call('replace_paragraph', { paragraph: 1, text: '' }, '정리(빈 문서)');
        await s.call('set_style', { from: 1, builtin: 'Normal' }, '정리(빈 문서)');
      }
      const last = await s.call('list_paragraphs', {}, '정리 뒤');
      s.check(`정리 뒤 문단 수가 처음과 같다(${base})`, Number(last?.total) === base && Number(last?.tables) === baseTables, JSON.stringify({ total: last?.total, tables: last?.tables }));
    });
    const lastPara = async () => Number((await s.call('list_paragraphs', { from: 1, to: 1 }, '끝 번호'))?.total ?? 0);
    const paras = async () => (await s.call('list_paragraphs', { from: p1 }, '되읽기'))?.paragraphs ?? [];
    const at = async (text) => (await paras()).find((p) => p.text === text)?.paragraph;

    // 1. 스타일 — 제목·조 머리, 그리고 없는 스타일은 거절
    await s.call('set_style', { from: p1, builtin: 'Title' });
    await s.call('set_style', { from: p1 + 1, builtin: 'Heading1' });
    await s.call('set_style', { from: p1 + 3, builtin: 'Heading1' });
    await s.refuse('set_style', { from: p1 + 2, style: '없는스타일-시나리오' }, /./, '없는 스타일');
    const styled = await paras();
    s.check('제목·조 머리의 스타일이 되읽힌다', styled[0]?.builtin === 'Title' && styled[1]?.builtin === 'Heading1' && styled[3]?.builtin === 'Heading1', JSON.stringify(styled.slice(0, 4).map((p) => p.builtin)));

    // 2. 번호 목록을 제1조 본문 뒤에
    await s.call('insert_list', { after: await at(T.body), kind: 'numbered', items: ['품목: 사무용품', '수량: 100개', '납기: 10월 31일'] });
    const listed = (await paras()).filter((p) => p.list);
    s.check('목록 셋이 목록으로 읽힌다', listed.length === 3, JSON.stringify(listed.map((p) => p.text)));

    // 3. 대금 표 — 만들고, 머리 행 서식, 열 하나 늘리고, 되읽기
    const t = await s.call('insert_table', { after: await at(T.pay), has_header: true, table_style: 'GridTable4_Accent1', values: [['회차', '금액'], ['1차', '5,000,000'], ['2차', '5,000,000']] });
    tableMade = Boolean(t);
    const tno = baseTables + 1;
    if (t) {
      await s.call('format_table_cells', { table: tno, rows: [0], bold: true, align: 'Centered' });
      // 평평한 목록 하나 = 한 열(2026-09-27 전에는 Office.js 의 InvalidArgument 한 줄) · 열 수와 값 목록 수가 다르면 사유를 댄 거절
    await s.call('edit_table', { table: tno, add_columns: { values: ['지급일', '10/31', '11/30'] } }, '평평한 목록');
    await s.refuse('edit_table', { table: tno, add_columns: { count: 2, values: [['a', 'b', 'c']] } }, '열 2개인데', '열 수 불일치');
      await s.call('add_table_rows', { table: tno, rows: [['합계', '10,000,000', '']] });
      const rt = await s.call('read_table', { table: tno });
      const cells = JSON.stringify(rt ?? {});
      s.check('표가 4행 3열이고 기존 값이 안 덮였다', rt?.rows === 4 && rt?.columns === 3 && cells.includes('지급일') && cells.includes('11/30') && cells.includes('5,000,000') && cells.includes('합계'), cells.slice(0, 240));
      await s.refuse('read_table', { table: tno + 5 }, /./, '없는 표');
    }

    // 4. 각주·책갈피
    const pay = await at(T.pay);
    await s.call('insert_footnote', { paragraph: pay, note: '영업일 기준이다.', text: '30일' });
    const fn = await s.call('read_footnotes', { from: p1, to: await lastPara() });
    s.check('각주가 읽힌다', JSON.stringify(fn ?? {}).includes('영업일 기준'), JSON.stringify(fn).slice(0, 200));
    await s.call('add_bookmark', { from: pay, name: 'scn_pay' });
    s.cleanup(() => s.call('delete_bookmark', { name: 'scn_pay' }, '정리'));

    // 5. 추적 켜고 고치기 → 기록이 남는다 → 거부하면 원래 글로
    await s.call('set_track_changes', { mode: 'TrackAll' });
    await s.call('replace_paragraph', { paragraph: await at(T.body), text: '이 계약은 갑이 을에게 물품을 공급하고 을이 대금을 치르는 조건을 정한다.' });
    const tc = await s.call('read_tracked_changes', { from: p1, to: await lastPara() });
    const nChanges = (tc?.changes ?? tc?.tracked ?? []).length;
    s.check('고친 것이 추적 기록으로 남았다', nChanges > 0, JSON.stringify(tc).slice(0, 200));
    await s.call('review_changes', { from: p1, to: await lastPara(), what: 'reject' });
    s.check('거부 뒤 제1조 본문이 원래 글이다', Boolean(await at(T.body)), '원래 문장을 못 찾음');
    await s.call('set_track_changes', { mode: 'Off' });

    // 6. 댓글 — 달고, 답하고, 닫고, 되읽고, 지운다
    const cm = await s.call('add_comment', { from: await at(T.h2), comment: '지급 기한이 짧지 않은가?' });
    const cid = cm?.id ?? cm?.comment_id;
    if (cid) {
      await s.call('reply_comment', { id: String(cid), text: '30일이 업계 관행입니다.' });
      await s.call('resolve_comment', { id: String(cid), resolved: true });
      const rc = await s.call('read_comments', { from: p1, to: await lastPara() });
      const mine = (rc?.comments ?? []).find((c) => String(c.id) === String(cid));
      s.check('댓글에 답이 달리고 닫혔다', mine && mine.resolved === true && JSON.stringify(mine).includes('업계 관행'), JSON.stringify(mine ?? rc).slice(0, 220));
      await s.call('resolve_comment', { id: String(cid), delete: true }, '지우기');
    }
    await s.refuse('reply_comment', { id: '999999', text: 'x' }, /./, '없는 댓글');

    // 7. 스냅숏 → 조 하나 지움 → 되돌림
    const snap = await s.call('snapshot_paragraphs', { from: p1, to: p1 + 3 });
    await s.call('delete_paragraphs', { from: p1 + 1, to: p1 + 1 }, '일부러 지움');
    s.check('지운 뒤엔 제1조 머리가 없다', !(await at(T.h1)));
    if (snap?.snapshot) await s.call('restore_paragraphs', { snapshot: snap.snapshot });
    s.check('되돌린 뒤 제1조 머리가 다시 있다', Boolean(await at(T.h1)));

    // 8. 바꾸기 — 우리 범위 안에서만
    const end = Number((await s.call('list_paragraphs', {}, '끝'))?.total ?? 0);
    await s.call('replace_all', { find: '갑', replace: '공급자', from: p1, to: end, whole_word: false });
    const f = await s.call('find', { text: '공급자' });
    s.check('바꾼 말이 찾아진다', (f?.hits ?? f?.matches ?? []).length >= 1, JSON.stringify(f).slice(0, 160));

    // 9. 콘텐츠 컨트롤 — 당사자 칸
    await s.call('insert_content_control', { paragraph: await at(T.tail), tag: TAG, title: '당사자', text: '(주)시나리오' }, '글 일부를 감쌈');
    await s.call('set_content_control', { tag: TAG, text: '(주)시나리오상사' });
    const cc = await s.call('read_content_controls', { from: p1, to: await lastPara() });
    s.check('콘텐츠 컨트롤 글이 고친 대로다', JSON.stringify(cc ?? {}).includes('(주)시나리오상사'), JSON.stringify(cc).slice(0, 200));
    await s.call('delete_content_control', { tag: TAG, keep_content: false });

    // 10. 제안 — 붙이고 읽고 뗀다
    const sg = await s.call('suggest', { paragraph: p1, what: '제목을 가운데로', fix: { tool: 'format_paragraph', args: { from: p1, align: 'Centered' } } });
    const rs = await s.call('read_suggestions', {});
    const key = sg?.suggestion ?? sg?.key;
    s.check('제안이 읽힌다', (rs?.suggestions ?? []).some((r) => r.key === key), JSON.stringify(rs).slice(0, 160));
    if (key) await s.call('drop_suggestion', { key });

    // 11. 파일에서 직접 — 추적 기록이 안 남았고 책갈피가 있다
    const com = s.com(`$d = Get-MagiWordDoc '${s.document.document}'
      @{ revisions = $d.Revisions.Count; tracking = $d.TrackRevisions; bookmark = $d.Bookmarks.Exists('scn_pay'); footnotes = $d.Footnotes.Count } | ConvertTo-Json -Compress`);
    if (com) {
      s.check('COM: 남은 추적 기록 0', com.revisions === 0, JSON.stringify(com));
      s.check('COM: 추적 꺼짐', com.tracking === false, JSON.stringify(com));
      s.check('COM: 책갈피 있음', com.bookmark === true, JSON.stringify(com));
    }
  },
};
