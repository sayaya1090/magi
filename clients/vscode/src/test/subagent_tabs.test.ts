import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { nextTabs, sessionLabel, type ChildJobLike } from '../core/subagent_tabs';

// The same rule as JetBrains' SubagentTabs (user decision, 2026-11-23): the first tab is the
// conversation's title, a running subagent gets a tab beside it, and a finished one closes unless
// it is the tab being looked at — then it stays until the person moves to another tab.
const job = (id: string, task: string | undefined, running: boolean): ChildJobLike => ({ id, task, running });

test('each running subagent gets a tab named by the first line of its task', () => {
  const tabs = nextTabs([], [job('s_aaaaaa1', 'read docs\nmore', true), job('s_bbbbbb2', undefined, true)], null);
  assert.deepEqual(tabs.map((t) => [t.id, t.label]), [['s_aaaaaa1', 'read docs'], ['s_bbbbbb2', '·bbbbb2']]);
});

test('a finished subagent closes unless it is the tab being looked at', () => {
  const open = nextTabs([], [job('a', 'A', true), job('b', 'B', true)], null);
  const done = [job('a', 'A', false), job('b', 'B', false)];
  const kept = nextTabs(open, done, 'b');
  assert.deepEqual(kept.map((t) => [t.id, t.running]), [['b', false]]);
  assert.deepEqual(nextTabs(kept, done, null), []);
});

test('a child gone from the list counts as finished; tabs keep the order they appeared in', () => {
  const open = nextTabs([], [job('a', 'A', true)], null);
  const more = nextTabs(open, [job('c', 'C', true), job('a', 'A', true)], null);
  assert.deepEqual(more.map((t) => t.id), ['a', 'c']);
  assert.deepEqual(nextTabs(more, [job('c', 'C', true)], null).map((t) => t.id), ['c']);
});

test('the session tab is the title\'s first line, clipped, or a fallback', () => {
  assert.equal(sessionLabel('  고쳐 줘\n둘째 줄', '채팅'), '고쳐 줘');
  assert.equal(sessionLabel('   ', '채팅'), '채팅');
  assert.equal(sessionLabel(undefined, '채팅'), '채팅');
  assert.equal(sessionLabel('x'.repeat(50), '채팅'), 'x'.repeat(31) + '…');
});

test('the host asks the conversation (not the screen) for its subagents and the webview draws the row', () => {
  const fs = require('fs') as typeof import('fs');
  const path = require('path') as typeof import('path');
  const root = path.join(__dirname, '..', '..', 'src');
  const chat = fs.readFileSync(path.join(root, 'ide', 'chat.ts'), 'utf8');
  assert.match(chat, /ask\('jobs', \{ session: home \}\)/, 'the tab row is not asked of the home conversation');
  assert.match(chat, /case 'tab':/, 'a picked tab is not handled');
  assert.match(chat, /this\.showTab\(m\.child\)/, 'opening a child row forgets the conversation');
  const html = fs.readFileSync(path.join(root, 'web', 'chat_html.ts'), 'utf8');
  assert.match(html, /id="tabrow"/);
  const view = fs.readFileSync(path.join(root, 'web', 'chat_view.ts'), 'utf8');
  assert.match(view, /onTabs:drawTabs/);
  assert.match(view, /kind: 'tab', id/);
});
