import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';
import { BOARD_CONTEXT, nextBoard } from '../core/board';

/**
 * The chat stands on the right, and the MAGI board shares that one place: the chat by default, the
 * board behind the settings (gear) button, and the same button brings the chat back (user decision,
 * 2026-11-22 — before, the chat was in the bottom panel and the board alone on the right).
 *
 * VS Code cannot swap one view's content for another's, so both views live in the same right-hand
 * container and a context key hides one of them: `when` clauses that are each other's negation.
 */
const root = path.join(__dirname, '..', '..');
const manifest = JSON.parse(fs.readFileSync(path.join(root, 'package.json'), 'utf8'));

test('the chat and the board share one container in the secondary sidebar, chat first', () => {
  const where = manifest.contributes.viewsContainers as Record<string, { id: string }[]>;
  assert.deepEqual(Object.keys(where), ['secondarySidebar'], 'the chat still has a container elsewhere');
  const side = where.secondarySidebar.map((c) => c.id);
  const views = manifest.contributes.views as Record<string, { id: string; when?: string }[]>;
  const home = Object.keys(views).find((k) => views[k].some((v) => v.id === 'magi.chat'));
  assert.ok(home && side.includes(home), `the chat is not in the secondary sidebar (in ${home})`);
  assert.deepEqual(views[home!].map((v) => v.id), ['magi.chat', 'magi.plan']);
  const chat = views[home!][0], plan = views[home!][1];
  assert.equal(chat.when, `!${BOARD_CONTEXT}`, 'the chat is not hidden while the board shows');
  assert.equal(plan.when, BOARD_CONTEXT, 'the board is not hidden while the chat shows');
});

test('a gear button on both title bars swaps the place', () => {
  const cmd = (manifest.contributes.commands as { command: string; icon?: string }[])
    .find((c) => c.command === 'magi.toggleBoard');
  assert.equal(cmd?.icon, '$(arrow-swap)', 'the swap is not a toggle button');
  const nav = (manifest.contributes.menus['view/title'] as { command: string; when: string; group?: string }[])
    .filter((m) => m.command === 'magi.toggleBoard' && (m.group ?? '').startsWith('navigation'));
  for (const v of ['magi.chat', 'magi.plan']) {
    assert.ok(nav.some((m) => m.when === `view == ${v}`), `no gear on ${v}'s title bar`);
  }
});

test('the swap flips the key and focuses whichever view now shows', () => {
  assert.deepEqual(nextBoard(false), { board: true, focus: 'magi.plan.focus' });
  assert.deepEqual(nextBoard(true), { board: false, focus: 'magi.chat.focus' });
});

test('the extension wires the swap, starts on the chat, and opening the chat brings it back', () => {
  const src = fs.readFileSync(path.join(root, 'src', 'ide', 'extension.ts'), 'utf8');
  assert.match(src, /registerCommand\('magi\.toggleBoard'/);
  assert.match(src, /setContext', BOARD_CONTEXT, false/, 'the window does not start on the chat');
  const focus = src.slice(src.indexOf("registerCommand('magi.focusChat'"));
  assert.match(focus.slice(0, 300), /showBoard\(false\)/, 'opening the chat leaves it hidden behind the board');
});
