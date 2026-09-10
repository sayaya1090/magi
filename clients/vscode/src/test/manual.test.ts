import { test } from 'node:test';
import { State, label } from '../core/activity';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';

const JETBRAINS_MANUAL = path.join(__dirname, '..', '..', '..', 'jetbrains', 'docs', 'MANUAL.ko.md');
const DESIGN = path.join(__dirname, '..', '..', 'docs', 'DESIGN.ko.md');

/** Sections that describe how to INSTALL magi rather than what the plugin does. */
const NOT_FEATURES = ['1.', '1a.'];

/**
 * Every bold item the JetBrains manual names, section by section.
 *
 * Two shapes carry a feature there: a table row whose first cell is bold (the plan panel and the
 * entry points), and a bullet with a bold lead (the transcript's row kinds, the composer, the
 * settings). Both are structure rather than prose, which is why they can be read at all.
 */
export function manualItems(src: string): { section: string; item: string }[] {
  const rowBold = /^\|\s*\*\*(.+?)\*\*\s*\|/;
  const bullet = /^\s*[-*]\s+\*\*(.+?)\*\*/;
  const heading = /^#{2,3}\s+(\S+)/;
  const out: { section: string; item: string }[] = [];
  let section = '';
  for (const line of src.split('\n')) {
    const h = heading.exec(line);
    if (h) { section = h[1]; continue; }
    if (NOT_FEATURES.some((s) => section.startsWith(s))) continue;
    const m = rowBold.exec(line) ?? bullet.exec(line);
    if (!m) continue;
    // The same normalisation the design table uses: the "magi: " the IDE needs on a menu item is
    // not part of the feature's name, and a trailing period is punctuation.
    const item = m[1].trim().replace(/^(magi:\s*|Magi\s+)/, '').replace(/\.$/, '').trim();
    if (item) out.push({ section, item });
  }
  return out;
}

/**
 * The design's porting table names every feature the JetBrains manual does.
 *
 * This exists because the first version of that table was written from `plugin.xml` and the UI
 * file names — sixteen entries — while the manual names forty-eight. The difference was every
 * behaviour that has no action of its own: the `@` mention, the reference chips, conversation
 * tabs, copying out of the transcript, "변경 보기". Nobody noticed until somebody asked whether
 * all of it was being ported, and the honest answer was no.
 *
 * So the list stops being something a person remembers. It is derived, here, from the document
 * that already has it — the same discipline `ManualTest` applies on the JetBrains side, for the
 * same reason: a table is prose, and prose does not hold itself up.
 */
test('the porting table names every feature the JetBrains manual names', () => {
  assert.ok(fs.existsSync(JETBRAINS_MANUAL), `the sibling manual is not at ${JETBRAINS_MANUAL}`);
  const items = manualItems(fs.readFileSync(JETBRAINS_MANUAL, 'utf8'));

  // The extraction asserts it found a manual. Reading zero items and reporting zero gaps is the
  // shape this test exists to prevent — and it is what a renamed heading or a reformatted table
  // would produce.
  assert.ok(items.length >= 40,
    `only ${items.length} items read from the manual — the extraction is broken, so this checks nothing`);

  const design = fs.readFileSync(DESIGN, 'utf8');
  const missing = items.filter(({ item }) => !design.includes(item)).map((i) => `§${i.section} ${i.item}`);
  assert.deepEqual([...new Set(missing)], [],
    'the JetBrains manual names these and the porting table does not — decide what happens to them ' +
    '(ported / moved / cannot) rather than letting them fall off:\n  ' + missing.join('\n  '));
});

/** And the extractor is checked, because one that reads nothing reports nothing missing. */
test('the manual extractor sees both shapes a feature takes', () => {
  const sample = [
    '## 5. 진입점',
    '| **magi: 커밋 메시지 생성** | 커밋 칸 | 초안을 짓는다 |',
    '| plain cell | x | y |',
    '## 2.3 입력줄',
    '- **`@` 멘션.** 낱말 시작에서',
    '- 굵지 않은 불릿',
    '## 1a. 안 깔았다면',
    '- **이것은 세지 않는다.** 코어 설치다',
  ].join('\n');
  const got = manualItems(sample);
  assert.deepEqual(got, [
    { section: '5.', item: '커밋 메시지 생성' },   // the "magi: " prefix is stripped
    { section: '2.3', item: '`@` 멘션' },          // the trailing period is stripped
  ], 'the extractor must read table rows and bullets, strip the prefix, and skip §1a');
});

/**
 * ★ Every status string the manual advertises is one this client can actually produce.
 *
 * The manual said `magi: idle` and glossed it "붙었고, 노는 중" — attached and idling. That word was
 * removed from the code earlier because the daemon never says it: the branch is reached when
 * `status` carries neither `waiting` nor `doing`, and that is what an ordinary running turn looks
 * like. The label is `attached` — "it answered and did not say more than that".
 *
 * The code was fixed and the manual was not, so the document went on teaching the exact misreading
 * the fix exists to prevent — worse than a missing sentence, because a person who reads it learns
 * to see "idle" where the screen says something else.
 *
 * Derived, not remembered: the strings come from `label()`, so a renamed state fails here rather
 * than leaving the manual to rot.
 */
test('the manual advertises only status words this client can say', () => {
  const manual = fs.readFileSync(path.join(__dirname, '..', '..', 'docs', 'MANUAL.ko.md'), 'utf8');
  const advertised = [...manual.matchAll(/`(?:\$\([\w~-]+\) )?magi: ([a-z ]+?)(?: ·|`)/g)]
    .map((m) => m[1].trim());
  assert.ok(advertised.length >= 5,
    `only ${advertised.length} status words read from the manual — the extraction is broken, and a ` +
    'broken extraction reports no drift at all');

  // What the code can actually say, for every state and both shapes of the two that take a tail.
  const sayable = new Set<string>();
  for (const st of Object.values(State)) {
    sayable.add(label({ state: st as State }));
    sayable.add(label({ state: st as State, doing: 'x' }).replace(/ · x$/, ''));
    sayable.add(label({ state: st as State, asking: 'x' }).replace(/ · x$/, ''));
  }
  assert.ok(sayable.has('attached') && sayable.has('cannot say'),
    'the label table is not where this guard thinks it is');

  for (const word of new Set(advertised)) {
    assert.ok(sayable.has(word),
      `the manual advertises "magi: ${word}" and no state produces it — the document is teaching a ` +
      `screen that does not exist. This client can say: ${[...sayable].sort().join(', ')}`);
  }
});

/**
 * ★ Every command a person can run is NAMED in this client's manual.
 *
 * Ten of twenty-six were not, and the palette shows the English TITLE — so somebody who found
 * "magi: Choose the approval mode" there had no way to look it up: the manual covered the feature
 * under a Korean heading and never spelled the words the palette had just shown them.
 *
 * The title, not the id. The id is what the code says; the title is what the person read.
 */
test('the manual names every command the palette offers', () => {
  const root = path.join(__dirname, '..', '..');
  const manifest = JSON.parse(fs.readFileSync(path.join(root, 'package.json'), 'utf8')) as
    { contributes: { commands?: { command: string; title: string }[] } };
  const cmds = manifest.contributes.commands ?? [];
  assert.ok(cmds.length >= 20, `only ${cmds.length} commands read from the manifest — the scan is dead`);
  const doc = fs.readFileSync(path.join(root, 'docs', 'MANUAL.ko.md'), 'utf8');
  for (const c of cmds) {
    assert.ok(doc.includes(c.title),
      `the palette offers "${c.title}" and the manual never says those words — it cannot be looked up`);
  }
});

/**
 * ★ A ported row may not name a command that is not there.
 *
 * The porting table opens by saying it "is not written by hand — it is verified automatically". The
 * automation checks that every feature the sibling manual NAMES appears in the table. It never checks
 * whether a ✓ is true, and four rows carried claims that were not: a dedicated row-copy command, the
 * `(failed)`/`(running)` words in copied text, folded blocks unfolding on copy, and a diff viewer
 * opened through `vscode.diff`. This client has no copy path at all and calls no diff command.
 *
 * Only command-shaped literals are checked, and that is a deliberate limit measured rather than
 * guessed: of the 18 backticked literals in ✓/↷ rows, a general check flags five and three of those
 * are fine — an API constant reached through the typed API (`quickfix`), a sibling's class name. The
 * four command-shaped ones flag exactly the one that is wrong. A guard with three false alarms in
 * five would be turned off; this one has none.
 */
test('a ported row names no command this client does not have', () => {
  const design = fs.readFileSync(DESIGN, 'utf8');
  const rows = design.split('\n').filter((l) => l.startsWith('|') && (l.includes('✓') || l.includes('↷')));
  assert.ok(rows.length >= 20, `only ${rows.length} ported rows read — the scan is dead`);
  // ⚠ `__dirname` is `out/test` at runtime, so `..` is the COMPILED tree and holds no `.ts` at all.
  // The first cut of this guard read nothing and still passed its floor — the floor counts claims
  // from the document, not source — and it took a real failure to notice. Read the source tree.
  const srcDir = path.join(__dirname, '..', '..', 'src');
  const manifest = fs.readFileSync(path.join(__dirname, '..', '..', 'package.json'), 'utf8');
  const src = readAll(srcDir) + manifest;
  assert.ok(src.length > 50_000, `only ${src.length} characters of source read — the scan is dead`);
  const claims: [string, string][] = [];
  for (const row of rows) {
    const what = row.split('|')[1].trim();
    for (const m of row.matchAll(/`([a-z][a-zA-Z]*\.[a-zA-Z][\w.]*)`/g)) claims.push([what, m[1]]);
  }
  assert.ok(claims.length >= 3, `only ${claims.length} command claims found — the scan is dead`);
  for (const [what, cmd] of claims) {
    // `contributes.x` names a MANIFEST section, not a command — checked against the manifest's own
    // keys rather than as a string somebody would have typed.
    if (cmd.startsWith('contributes.')) {
      const key = cmd.slice('contributes.'.length);
      const has = (JSON.parse(manifest) as { contributes?: Record<string, unknown> }).contributes ?? {};
      assert.ok(key in has,
        `the porting table says "${what}" is done through \`${cmd}\`, and the manifest has no such section`);
      continue;
    }
    assert.ok(src.includes(cmd),
      `the porting table says "${what}" is done through \`${cmd}\`, and nothing in this client names it`);
  }
});

/** Read every non-test source file under a directory, for guards that ask "is this anywhere". */
function readAll(dir: string): string {
  let out = '';
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    if (e.name === 'test') continue;
    const p = path.join(dir, e.name);
    if (e.isDirectory()) out += readAll(p);
    else if (e.name.endsWith('.ts')) out += fs.readFileSync(p, 'utf8');
  }
  return out;
}
