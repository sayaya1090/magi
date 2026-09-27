#!/usr/bin/env node
// 오피스 사용 시나리오 실행기 — 붙어 있는 실물 Excel·Word·PowerPoint 에 긴 흐름을 걸고, 답과 되읽기로 잰다.
//
//   node clients/office/scenarios/run.mjs [--app xl|word|ppt|all] [--only <id>] [--origin https://127.0.0.1:26411] [--com] [--verbose]
//
// 무엇을 재는지는 README.ko.md 가 시나리오마다 적는다. 기능을 고치면 이것을 돌린다 — 스윕(도구 하나씩)이 못 보는 것,
// **도구와 도구 사이**(표를 만든 뒤의 정렬, 추적을 켠 뒤의 편집, 장을 복제한 뒤의 순서)를 여기서 본다.
//
// 약속:
// - 시나리오는 자기가 만든 것만 만지고, 끝에 그것을 지운다 — 실패해도 정리는 돈다. 사람의 내용은 안 건드린다.
// - 도구가 「했다」고 답한 것을 믿지 않는다: 되읽는 도구로 다시 보고, `--com` 이면 Windows 에서 파일을 COM 으로 직접 읽는다.
//   (2026-09-26 실물: PowerPoint 2021 의 add_shape 는 별을 부르면 네모를 세우고 「추가했습니다」라고 답했다.)
// - 닿지 못한 것은 통과가 아니다 — 연결이 안 되면 거절을 기대하던 단계도 실패로 센다.
import { readdirSync, writeFileSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { execFileSync } from 'node:child_process';

process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0'; // 헬퍼의 자가 서명 인증서 — 시험 도구에서만
const here = dirname(fileURLToPath(import.meta.url));

const opt = { app: 'all', only: '', origin: 'https://127.0.0.1:26411', com: false, verbose: false };
for (let i = 2; i < process.argv.length; i++) {
  const a = process.argv[i];
  if (a === '--com') opt.com = true;
  else if (a === '--verbose') opt.verbose = true;
  else if (a.startsWith('--') && i + 1 < process.argv.length) opt[a.slice(2)] = process.argv[++i];
}
if (opt.com && process.platform !== 'win32') { console.log('--com 은 Windows 에서만 됩니다 — COM 대조 없이 돈다'); opt.com = false; }

// ── 헬퍼에 닿기 ──

async function connect(app) {
  const base = `${opt.origin}/${app}`;
  let page;
  try { page = await (await fetch(`${base}/taskpane.html`)).text(); }
  catch (e) { return { why: `헬퍼(${base})에 못 닿았습니다 — ${e.cause?.code ?? e.message}` }; }
  const tok = page.match(/"token":"([a-f0-9]+)"/)?.[1];
  if (!tok) return { why: `헬퍼 페이지에서 토큰을 못 찾았습니다(${base})` };
  const headers = { authorization: 'Bearer ' + tok, 'content-type': 'application/json' };
  const docs = (await (await fetch(`${base}/api/documents`, { headers })).json()).documents ?? [];
  if (!docs.length) return { why: `${app} 에 붙은 작업창이 없습니다 — 그 프로그램에서 magi 작업창을 여세요` };
  return { base, headers, doc: docs[0] };
}

// ── 시나리오가 쓰는 도구 ──

class Scenario {
  constructor(link, meta) {
    this.link = link; this.meta = meta;
    this.pass = 0; this.fail = 0; this.cleanups = [];
  }
  async #rpc(name, args) {
    // 문서를 주소에 싣는다 — 열린 문서가 둘이면 헬퍼가 「어느 것인가」로 거절한다. 인자의 document 가 더 세다(mcp.go).
    const r = await fetch(`${this.link.base}/mcp?deck=${encodeURIComponent(this.link.doc.document)}`, { method: 'POST', headers: this.link.headers,
      body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name, arguments: args } }) });
    const body = await r.json();
    if (body.error) return { error: `rpc: ${JSON.stringify(body.error)}` };
    const res = body.result ?? {};
    const txt = (res.content ?? []).filter((c) => c.type === 'text').map((c) => c.text).join('');
    let x; try { x = JSON.parse(txt); } catch { x = { _raw: txt }; }
    if (opt.verbose) console.log(`       ↳ ${name} ${JSON.stringify(args).slice(0, 200)}\n         ${txt.replace(/\s+/g, ' ').slice(0, 900)}`);
    return res.isError ? { error: txt } : { value: x };
  }
  /** 도구 하나 — 실패하면 단계가 실패하고 null. */
  async call(name, args = {}, label = '') {
    let got;
    try { got = await this.#rpc(name, args); } catch (e) { got = { error: `닿지 못함: ${e.message}` }; }
    const tag = `${name}${label ? ' · ' + label : ''}`;
    if (got.error) { this.#record(false, tag, got.error); return null; }
    const said = Array.isArray(got.value.changed) && got.value.changed.length ? got.value.changed.join(' | ') : '';
    this.#record(true, tag, said + (got.value.via ? ` [${got.value.via}]` : ''));
    return got.value;
  }
  /** 거절돼야 하는 호출 — 거절문에 match 가 들어 있어야 한다. 닿지 못한 것은 거절이 아니다. */
  async refuse(name, args, match, label = '') {
    let got;
    try { got = await this.#rpc(name, args); } catch (e) { this.#record(false, `${name} 거절 기대`, `닿지 못함: ${e.message}`); return; }
    const tag = `${name} 거절${label ? ' · ' + label : ''}`;
    if (!got.error) { this.#record(false, tag, '거절돼야 하는데 통과했다: ' + JSON.stringify(got.value.changed ?? got.value).slice(0, 160)); return; }
    const ok = !match || (match instanceof RegExp ? match.test(got.error) : got.error.includes(match));
    this.#record(ok, tag, ok ? got.error.slice(0, 120) : `거절 사유가 다르다: ${got.error.slice(0, 160)}`);
  }
  check(label, cond, detail = '') { this.#record(Boolean(cond), label, cond ? '' : String(detail)); }
  note(msg) { console.log(`       ${msg}`); }
  cleanup(fn) { this.cleanups.push(fn); }
  /** Windows 에서 PowerShell 로 파일을 직접 읽는다. 스크립트는 JSON 한 덩어리를 내야 한다. --com 이 없으면 null. */
  com(script) {
    if (!opt.com) return null;
    const dir = mkdtempSync(join(tmpdir(), 'magi-scn-'));
    const f = join(dir, 's.ps1');
    // BOM — PowerShell 5.1 은 BOM 없는 파일을 ANSI(한국어 Windows 면 CP949)로 읽어 한글을 깨뜨린다.
    writeFileSync(f, '﻿[Console]::OutputEncoding = [Text.Encoding]::UTF8\n$ErrorActionPreference = "Stop"\n' + comPrelude + '\n' + script, 'utf8');
    try {
      const out = execFileSync('powershell', ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', f], { encoding: 'utf8', windowsHide: true });
      return JSON.parse(out.trim().split('\n').filter(Boolean).pop());
    } catch (e) {
      this.#record(false, 'COM 되읽기', (e.stderr || e.message).toString().slice(0, 200));
      return null;
    } finally { rmSync(dir, { recursive: true, force: true }); }
  }
  get document() { return this.link.doc; }
  #record(ok, label, detail) {
    if (ok) this.pass++; else this.fail++;
    console.log(`  ${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ' — ' + String(detail).replace(/\s+/g, ' ').slice(0, 170) : ''}`);
  }
}

// COM 되읽기가 같이 쓰는 함수들 — 작업창이 붙은 **그 문서**를 표식으로 찾는다.
const comPrelude = String.raw`
function Get-MagiWordDoc([string]$key) {
  $id = $key -replace '^wd-',''
  $wd = [Runtime.InteropServices.Marshal]::GetActiveObject('Word.Application')
  foreach ($d in $wd.Documents) {
    $p = $d.CustomDocumentProperties
    $n = [System.__ComObject].InvokeMember('Count','GetProperty',$null,$p,$null)
    for ($i = 1; $i -le $n; $i++) {
      $it = [System.__ComObject].InvokeMember('Item','GetProperty',$null,$p,@($i))
      if ([System.__ComObject].InvokeMember('Name','GetProperty',$null,$it,$null) -eq 'MAGI.DOC' -and
          [System.__ComObject].InvokeMember('Value','GetProperty',$null,$it,$null) -eq $id) { return $d }
    }
  }
  throw "표식 $id 인 Word 문서가 없습니다"
}
function Get-MagiWorkbook([string]$name) {
  $xl = [Runtime.InteropServices.Marshal]::GetActiveObject('Excel.Application')
  foreach ($w in $xl.Workbooks) { if ($w.Name -eq $name) { return $w } }
  if ($xl.Workbooks.Count -eq 1) { return $xl.Workbooks.Item(1) }
  throw "통합 문서 $name 을 못 찾았습니다"
}
function Get-MagiDeck([string]$label) {
  $pp = [Runtime.InteropServices.Marshal]::GetActiveObject('PowerPoint.Application')
  foreach ($p in $pp.Presentations) { if ($p.Name -eq $label) { return $p } }
  if ($pp.Presentations.Count -eq 1) { return $pp.Presentations.Item(1) }
  throw "프레젠테이션 $label 을 못 찾았습니다"
}
`;

// ── 돌리기 ──

const files = readdirSync(here).filter((f) => /^[a-z]+-.*\.mjs$/.test(f) && f !== 'run.mjs').sort();
let totalPass = 0, totalFail = 0; const unreachable = []; const summary = [];
for (const f of files) {
  const mod = (await import(pathToFileURL(join(here, f)).href)).default;
  if (opt.app !== 'all' && mod.app !== opt.app) continue;
  if (opt.only && mod.id !== opt.only) continue;
  console.log(`\n■ ${mod.id} — ${mod.title}`);
  // COM 전용 도구(헬퍼가 Windows 에서만 광고한다)를 재는 시나리오는 다른 OS 에서 건너뛴다 — 실패가 아니라 그 도구가 없는 것이다.
  if (mod.windowsOnly && process.platform !== 'win32') { console.log('  건너뜀: Windows 전용(COM)'); unreachable.push(`${mod.id}: Windows 전용`); continue; }
  const link = await connect(mod.app);
  if (link.why) { console.log(`  건너뜀: ${link.why}`); unreachable.push(`${mod.id}: ${link.why}`); continue; }
  console.log(`  문서 ${link.doc.label || '(저장 안 함)'} · ${link.doc.document}${opt.com ? ' · COM 대조' : ''}`);
  const s = new Scenario(link, mod);
  const t0 = Date.now();
  try { await mod.run(s); }
  catch (e) { s.check('시나리오가 끝까지 돌았다', false, e.stack?.split('\n').slice(0, 2).join(' ') ?? e); }
  finally {
    if (s.cleanups.length) console.log('  ── 정리');
    for (const fn of s.cleanups.reverse()) {
      try { await fn(); } catch (e) { s.check('정리', false, e.message); }
    }
  }
  totalPass += s.pass; totalFail += s.fail;
  summary.push(`${s.fail ? 'FAIL' : ' ok '} ${mod.id.padEnd(22)} 통과 ${s.pass} · 실패 ${s.fail} · ${((Date.now() - t0) / 1000).toFixed(1)}초`);
}
console.log('\n══ 요약');
for (const l of summary) console.log('  ' + l);
for (const u of unreachable) console.log('  건너뜀 ' + u);
console.log(`  합계: 통과 ${totalPass} · 실패 ${totalFail}${unreachable.length ? ` · 건너뜀 ${unreachable.length}` : ''}`);
process.exit(totalFail ? 1 : 0);
