import { test } from 'node:test';
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
