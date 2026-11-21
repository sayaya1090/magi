import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { rows } from '../core/transcript';
import { parseWebviewToHostMessage } from '../core/webview_protocol';
import { chatWebviewSource } from './support/webview_source';

// A call that ran a child names it at the end of its result (the core's "[child <id>, …]" line). The
// row carries it, failed or not, and the webview offers to open it from the row — the subagent strip
// shows a child only while it runs, so after that the row is the way in.
test('a tool row names the child its call ran, and the row offers to open it', () => {
  let seq = 1;
  const pair = (callId: string, isError: boolean, content: string): any[] => [
    { seq: seq++, type: 'part.appended',
      data: { part: { kind: 'tool-call', toolCall: { callId, name: 'dummy_plug_run', args: { goal: 'g' } } } } },
    { seq: seq++, type: 'part.appended',
      data: { part: { kind: 'tool-result', toolResult: { callId, isError, content } } } },
  ];
  const rs: any[] = rows([
    ...pair('c1', false, '35 docs\n\n[child s_kid01, 2 tool call(s)]'),
    ...pair('c2', true, 'boom\n\n[child s_kid02, 1 step(s), stopped short: x]'),
    ...pair('c3', false, 'it mentions [child s_no, 1 step(s)] mid-line'),
  ] as any);
  const tools = rs.filter((r) => r.who === 'tool');
  assert.equal(tools.length, 3);
  assert.deepEqual(tools[0].children, ['s_kid01'], 'a finished call does not name the child it ran');
  assert.deepEqual(tools[1].children, ['s_kid02'], 'a failed call does not name the child it ran');
  assert.equal(tools[2].children, undefined, 'only a line the core wrote names a child');

  const ok: any = parseWebviewToHostMessage({ kind: 'child', session: 's1', child: 's_kid01' });
  assert.equal(ok?.kind, 'child');
  assert.equal(ok?.child, 's_kid01');
  assert.equal(parseWebviewToHostMessage({ kind: 'child', session: 's1' }), undefined, 'a child with no id was let through');

  const view = chatWebviewSource();
  assert.ok(view.includes('child-open-btn'), 'no tool row offers to open its child');
  assert.ok(/actions\.openChild\(/.test(view), 'the button does not ask the host to open the child');
});
