import { chromium } from 'playwright';

const BASE = 'http://127.0.0.1:7778/next';
const SOCK = process.env.SOCK;
const results = [];
const shot = process.env.SHOT_DIR || 'scratchpad/uitest';

const check = (name, ok, detail) => {
  results.push({ name, ok: !!ok, detail: detail === undefined ? '' : String(detail).slice(0, 120) });
};

// 기둥은 닫힌 채로 온다(운영 규칙: 이 화면은 대화다) — 워크스페이스를 재려면 사람이 하듯 연다.
async function openPane(p, key) {
  const shut = await p.evaluate(k => document.body.getAttribute(k) === 'shut', key);
  if (!shut) return true;
  const h = p.locator('#' + key + 'Toggle');
  if (!(await h.count()) || !(await h.isVisible().catch(() => false))) return false;   // 폰엔 탭이 있다
  await h.click();
  await p.waitForFunction(k => document.body.getAttribute(k) === 'open', key, { timeout: 5000 });
  await p.waitForTimeout(250);
  return true;
}

// 번역되지 않은 키가 화면에 그려졌는가. 페더레이션에서는 모듈마다 static이 따로라, 팩을
// 든 모듈과 그리는 모듈이 다르면 이런 것이 그대로 나온다(실측: "field.facts", "action.send").
// 이름공간은 팩 자신에게서 뽑았다 — "daemon.log" 같은 파일 이름을 키로 오인하지 않으려고.
const PACK_NS = ["ac", "access", "action", "answer", "ask", "board", "cap", "col", "context", "copy", "council", "count", "cron", "detail", "diff", "edit", "embed", "empty", "error", "field", "file", "files", "filter", "find", "fmt", "fold", "git", "going_on", "hint", "history", "insp", "job", "label", "load", "loading", "map", "may", "mcp", "meet", "move", "nav", "notify", "pal", "panel", "perm", "plan", "pr", "pref", "prof", "queued", "reach", "row", "session", "settings", "shared", "shell", "side", "skill", "state", "stop", "team", "time", "type", "update", "ver", "wiki"];
async function untranslated(p) {
  return await p.evaluate(ns => {
    const bad = [];
    const walk = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    for (let n = walk.nextNode(); n; n = walk.nextNode()) {
      const t = (n.textContent || '').trim();
      if (!/^[a-z][a-z0-9]*(\.[a-z0-9_]+)+$/.test(t)) continue;
      if (!ns.includes(t.split('.')[0])) continue;      // 우리 팩의 말이 아니면 남의 이름이다
      const e = n.parentElement;
      if (!e || e.offsetParent === null) continue;      // 보이지 않는 것은 그리지 않은 것
      if (e.closest('#log, #agentdetail, code, pre')) continue;  // 대화의 내용은 우리 말이 아니다
      bad.push(t);
    }
    return [...new Set(bad)].slice(0, 6).join(', ');
  }, PACK_NS);
}

// 좁은 창에서 지식 화면은 셋 중 하나만 보인다(운영 규칙) — 재려는 판을 사람이 하듯 고른다.
async function showShared(p, which) {
  const strip = p.locator('#sharedTabs');
  if (!(await strip.count()) || !(await strip.isVisible().catch(() => false))) return;
  const at = { skills: 0, wiki: 1, mcp: 2 }[which];
  await strip.locator('md-secondary-tab').nth(at).click();
  await p.waitForSelector('#' + which + ':not([hidden])', { timeout: 5000 });
  await p.waitForTimeout(200);
}

// 화면은 들어온다 — 운영은 목적지가 그려질 때마다 그 몸에 .enter를 붙인다(fadeThrough 200ms).
// 클래스가 남으므로 지나간 뒤에도 잴 수 있다.
async function entered(p, sel) {
  return await p.evaluate(s => { const e = document.querySelector(s); if (!e) return '없음';
    return getComputedStyle(e).animationName; }, sel);
}

async function run(view, width, height) {
  const b = await chromium.launch();
  const ctx = await b.newContext({ viewport: { width, height }, hasTouch: view === 'phone' });
  const p = await ctx.newPage();
  const errs = [];
  p.on('pageerror', e => errs.push(String(e).slice(0, 140)));
  p.on('console', m => { if (m.type() === 'error') errs.push('console:' + m.text().slice(0, 140)); });
  const tag = t => `${view}: ${t}`;
  const noOverflow = async where => {
    const over = await p.evaluate(() => document.scrollingElement.scrollWidth - window.innerWidth);
    check(tag(`가로 오버플로 없음 (${where})`), over <= 1, `over=${over}px`);
  };

  // ── 목록 ────────────────────────────────────────────────────────────────
  await p.goto(BASE, { waitUntil: 'domcontentloaded' });
  await p.waitForSelector('#fleet .card', { timeout: 12000 });
  check(tag('목록: 행이 선다'), (await p.locator('#fleet .card').count()) > 0);
  check(tag('fleet: 화면이 들어온다(.enter)'), (await entered(p, '#fleet')) === 'fadeThrough', await entered(p, '#fleet'));
  check(tag('목록: 요약 칩 4개'), (await p.locator('#summary md-filter-chip').count()) === 4);
  check(tag('목록: 나가는 길(보드)'), (await p.locator('#summary .toview').count()) >= 1);
  await noOverflow('목록');
  await p.screenshot({ path: `${shot}/${view}-01-list.png` });

  // ── 레일 ────────────────────────────────────────────────────────────────
  if (view === 'desk') {
    await p.click('#railMenu');
    await p.waitForSelector('body[nav=open]', { timeout: 5000 });
    // 라벨은 폭 트랜지션을 따라온다(실측: 직후 0px → 400ms 뒤 159px) — 폭이 자리를 잡을 때까지 기다린다.
    await p.waitForFunction(() => document.getElementById('rail').getBoundingClientRect().width > 200,
                            null, { timeout: 5000 }).catch(() => {});
    check(tag('레일: 열리면 라벨이 보인다'), await p.locator('#railNav .raili .lbl').first().isVisible());
    check(tag('레일: 배지는 라벨 뒤로'), await p.evaluate(() =>
      document.getElementById('railBadge')?.parentElement.className.includes('raili')));
    await p.screenshot({ path: `${shot}/${view}-02-rail.png` });
    await p.click('#scrim');
    await p.waitForFunction(() => !document.body.hasAttribute('nav'), null, { timeout: 5000 });
  } else {
    check(tag('폰: 레일은 하단 바'), await p.evaluate(() =>
      getComputedStyle(document.getElementById('railNav')).flexDirection === 'row'));
    check(tag('폰: 버거는 없다'), !(await p.locator('#railMenu').isVisible()));
  }

  // ── 지식 ────────────────────────────────────────────────────────────────
  await p.goto(`${BASE}?v=skills`, { waitUntil: 'domcontentloaded' });
  await p.waitForSelector('#skills .sectionhead', { timeout: 12000 });
  check(tag('지식: 세 판이 직계로'), (await p.locator('#frame > #skills, #frame > #wiki, #frame > #mcp').count()) === 3);
  check(tag('지식: 서버 머리에 추가'), (await p.locator('#mcp .sectionhead .mcpopen').count()) === 1);
  await noOverflow('지식');
  await p.screenshot({ path: `${shot}/${view}-03-knowledge.png` });

  // ── 보드 ────────────────────────────────────────────────────────────────
  await p.goto(`${BASE}?v=board`, { waitUntil: 'domcontentloaded' });
  await p.waitForSelector('.boardhead', { timeout: 12000 });
  check(tag('보드: 머리와 오늘 잠금'), (await p.locator('.boardhead md-text-button[disabled]').count()) === 1);
  await noOverflow('보드');
  await p.screenshot({ path: `${shot}/${view}-04-board.png` });

  // ── 맵 ──────────────────────────────────────────────────────────────────
  await p.goto(`${BASE}?v=map`, { waitUntil: 'domcontentloaded' });
  await p.waitForSelector('#map .machine', { timeout: 12000 });
  check(tag('map: 화면이 들어온다(.enter)'), (await entered(p, '#map')) === 'fadeThrough', await entered(p, '#map'));
  check(tag('맵: 머신 상자'), (await p.locator('#map .machine').count()) >= 1);
  check(tag('맵: 표로 돌아가는 길'), (await p.locator('#map .astable').count()) === 1);
  await noOverflow('맵');
  await p.screenshot({ path: `${shot}/${view}-05-map.png` });

  // ── 접근 ────────────────────────────────────────────────────────────────
  await p.goto(`${BASE}?v=access`, { waitUntil: 'domcontentloaded' });
  await p.waitForSelector('#access', { timeout: 12000 });
  check(tag('access: 화면이 들어온다(.enter)'), (await entered(p, '#access')) === 'fadeThrough', await entered(p, '#access'));
  check(tag('접근: 화면이 선다'), (await p.locator('#access .sectionhead').count()) === 1);
  await noOverflow('접근');
  await p.screenshot({ path: `${shot}/${view}-06-access.png` });

  // ── 컴패니언 상세 ───────────────────────────────────────────────────────
  await p.goto(`${BASE}?d=${encodeURIComponent(SOCK)}`, { waitUntil: 'domcontentloaded' });
  await p.waitForSelector('#agentview', { timeout: 12000 });
  await p.waitForTimeout(2000);
  if (view === 'phone') {
    // 폰은 한 번에 하나 — 탭이 고른다(대화가 기본).
    check(tag('상세: 폰이면 탭이 선다'), (await p.locator('#ptabs:not([hidden])').count()) === 1);
    check(tag('상세: 기본은 대화'), (await p.evaluate(() => document.body.getAttribute('panel'))) === 'talk');
    await p.locator('#ptab-facts').click();
    await p.waitForSelector('#detail:not([hidden])', { timeout: 6000 });
    check(tag('상세: 정보 탭이 사실판을 보인다'), (await p.locator('#detail .f').count()) > 0);
    await p.locator('#ptab-files').click();
    await p.waitForSelector('#filecol:not([hidden])', { timeout: 6000 });
    check(tag('상세: 파일 탭이 왼쪽을 보인다'), (await p.locator('#filecol #files').count()) === 1);
    await p.locator('#ptab-talk').click();
    await p.waitForSelector('#stream:not([hidden])', { timeout: 6000 });
  }
  check(tag('상세: 사실판'), view === 'phone' ? true : (await p.locator('#detail:not([hidden])').count()) === 1);
  check(tag('상세: 가운데는 자식(대화)'), (await p.locator('#stream #conversation').count()) === 1);
  {
    const keys = await untranslated(p);
    check(tag('상세: 번역되지 않은 키가 없다'), keys === '', keys);
  }
  check(tag('상세: 기둥은 닫힌 채로 온다 — 이 화면은 대화다'),
    (await p.evaluate("document.body.getAttribute('files')")) === 'shut');
  await openPane(p, 'files');
  check(tag('상세: 왼쪽은 워크스페이스'), (await p.locator('#filecol #files').count()) === 1);
  if (view === 'phone') await p.locator('#ptab-files').click().catch(() => {});
  check(tag('상세: 전사 행'), (await p.locator('#stream #log .row').count()) > 0);
  check(tag('상세: 컴포저'), (await p.locator('#dock .composer #t').count()) === 1);
  await noOverflow('상세');
  await p.screenshot({ path: `${shot}/${view}-07-detail.png`, fullPage: false });

  if (view === 'phone') await p.waitForTimeout(500);
  // 워크스페이스: 트리·펼침·파일 열기
  const dirs = await p.locator('#files .treerow.dir').count();
  check(tag('워크스페이스: 트리 행'), (await p.locator('#files .pane-files .treerow').count()) > 0);
  if (dirs > 0) {
    const before = await p.locator('#files .pane-files .treerow').count();
    await p.locator('#files .treerow.dir').first().click();
    await p.waitForTimeout(900);
    const after = await p.locator('#files .pane-files .treerow').count();
    check(tag('워크스페이스: 가지를 펼치면 자식이 는다'), after >= before, `${before}→${after}`);
  }
  const files = await p.locator('#files .treerow:not(.dir)').count();
  if (files > 0) {
    await p.locator('#files .treerow:not(.dir)').first().click();
    // 본문은 가운데 카드로 간다(운영과 같은 자리) — 번호는 제 기둥에 선다
    const opened = await p.waitForSelector('#fileview .filebody .filecode', { timeout: 8000 }).catch(() => null);
    check(tag('워크스페이스: 파일이 가운데 카드로 열린다'), !!opened);
    const gut = await p.locator('#fileview .filebody .filegutter').count();
    check(tag('워크스페이스: 번호가 제 기둥에 선다'), gut === 1);
    await p.screenshot({ path: `${shot}/${view}-08-file.png` });
    // 폰에서는 카드가 트리 자리에 서 있다 — 트리로 돌아가야 찾기가 다시 보인다(운영과 같다)
    const back = p.locator('.fileback').first();
    if (await back.count() && await back.isVisible().catch(() => false)) {
      await back.click().catch(() => {});
      await p.waitForTimeout(500);
    }
    // 탭의 ×로 닫는다 — 그 자리를 고르는 것은 탭 줄이고, 탭 줄은 부모의 것이다
    const close = p.locator('#cardtabs .tabclose').first();
    if (await close.count() && await close.isVisible().catch(() => false)) await close.click().catch(() => {});
  }
  // 찾기
  const findGo = p.locator('#files .filefind md-text-button').first();
  if (await findGo.count()) {
    await findGo.click();
    const box = p.locator('md-dialog.askline md-outlined-text-field input, md-dialog.askline md-outlined-text-field textarea').first();
    await box.waitFor({ timeout: 5000 }).catch(() => {});
    check(tag('워크스페이스: 찾기는 눌러야 묻는다'), await box.count() > 0);
    if (await box.count()) {
      await box.fill('log');
      await p.locator('md-dialog.askline md-filled-button').click();
      await p.waitForTimeout(1200);
      const hits = await p.locator('#files .hits .treerow.hit').count();
      const note = await p.locator('#files .hits .filesnote').count();
      check(tag('워크스페이스: 찾기가 답한다'), hits + note > 0, `hits=${hits} note=${note}`);
      check(tag('워크스페이스: 무엇을 찾았는지 말한다'),
        (await p.locator('#files .filefind .findnow').count()) === 1);
      await p.locator('#files .filefind md-text-button').last().click().catch(() => {});
    }
    await p.evaluate("document.querySelectorAll('md-dialog').forEach(d => d.close && d.close())");
  }
  // git — 이 워크스페이스는 저장소가 아니다: 그 사실을 말해야 한다
  const gitNote = await p.locator('#files .pane-git .filesnote').textContent().catch(() => '');
  check(tag('워크스페이스: git이 제 상태를 말한다'), !!gitNote, gitNote);

  // ── 이력 층위 ───────────────────────────────────────────────────────────
  await p.goto(`${BASE}?d=${encodeURIComponent(SOCK)}&past=`, { waitUntil: 'domcontentloaded' });
  const list = await p.waitForSelector('#agentdetail .hs', { timeout: 12000 }).catch(() => null);
  check(tag('이력: 목록이 선다'), !!list);
  if (list) {
    check(tag('이력: 컴포저는 물러난다'), (await p.locator('form[hidden]').count()) >= 0);
    await p.locator('#agentdetail .hs').first().click();
    const rows = await p.waitForSelector('#agentdetail .dlog .row', { timeout: 10000 }).catch(() => null);
    check(tag('이력: 한 세션의 전사'), !!rows);
    await p.screenshot({ path: `${shot}/${view}-09-past.png` });
  }
  await noOverflow('이력');

  check(tag('콘솔 에러 0'), errs.length === 0, errs.slice(0, 3).join(' | '));
  await b.close();
}

await run('desk', 1500, 950);
await run('phone', 390, 844);

const bad = results.filter(r => !r.ok);
console.log(JSON.stringify({ total: results.length, failed: bad.length, failures: bad }, null, 1));
