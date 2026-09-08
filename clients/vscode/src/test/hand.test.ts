import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { Hand } from '../core/mcpserver';
import { Ide, callHand, handTools, HAND_NAME } from '../core/hand';

class FakeIde implements Ide {
  shown: [string, number | undefined] | null = null;
  replaced: [string, string, string, boolean] | null = null;
  asked: string | undefined | false = false;
  async show(path: string, line?: number): Promise<string> { this.shown = [path, line]; return `opened ${path}`; }
  async replace(p: string, o: string, n: string, all: boolean): Promise<string> {
    this.replaced = [p, o, n, all];
    return 'replaced';
  }
  async problems(path?: string): Promise<string> { this.asked = path; return 'no errors'; }
}

async function rpc(hand: Hand, method: string, params?: unknown, token?: string): Promise<{
  status: number; body: Record<string, unknown>;
}> {
  const res = await fetch(hand.url, {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'X-Magi-Hand': token ?? hand.token },
    body: JSON.stringify({ jsonrpc: '2.0', id: 1, method, params }),
  });
  const text = await res.text();
  return { status: res.status, body: text ? (JSON.parse(text) as Record<string, unknown>) : {} };
}

/**
 * The four methods the core's client calls, over the real transport.
 *
 * Measured through HTTP rather than by calling dispatch: the JetBrains hand shipped a 202-vs-204
 * disagreement that only a real client found, and a test that called its own port-of-the-protocol
 * would have passed either way.
 */
test('the hand answers the four methods a magi client calls', async () => {
  const ide = new FakeIde();
  const hand = await Hand.start(ide);
  try {
    const init = await rpc(hand, 'initialize');
    assert.equal((init.body.result as { protocolVersion?: string }).protocolVersion, '2025-06-18');

    // A notification has no reply.
    assert.equal((await rpc(hand, 'notifications/initialized')).status, 204);

    const list = (await rpc(hand, 'tools/list')).body.result as
      { tools: { name: string; inputSchema?: unknown; annotations?: { readOnlyHint?: boolean } }[] };
    assert.deepEqual(list.tools.map((t) => t.name).sort(), ['apply_edit', 'problems', 'show']);
    // A schema on every one, or the model invents arguments.
    assert.ok(list.tools.every((t) => t.inputSchema), 'a tool has no schema');

    const call = (await rpc(hand, 'tools/call', { name: 'show', arguments: { path: 'a.ts', line: 12 } })).body.result as
      { content: { text: string }[]; isError: boolean };
    assert.deepEqual(ide.shown, ['a.ts', 12]);
    assert.equal(call.isError, false);
    assert.match(call.content[0].text, /a\.ts/);
  } finally { hand.close(); }
});

/**
 * ★ The declaration the core reads. Two things hang on it: what gets shed first when the window
 * closes, and — for a tool that names a `path` — whether the call COUNTED as an edit. Leave it out
 * and the protocol's default applies, so opening a file to point at it lands in magi's record as
 * "this turn changed that file".
 */
test('the hand says which of its tools only look', async () => {
  const hand = await Hand.start(new FakeIde());
  try {
    const list = (await rpc(hand, 'tools/list')).body.result as
      { tools: { name: string; annotations?: { readOnlyHint?: boolean } }[] };
    const ro = new Map(list.tools.map((t) => [t.name, t.annotations?.readOnlyHint]));
    assert.equal(ro.get('show'), true, 'show does not change the file and must say so');
    assert.equal(ro.get('problems'), true, 'reading diagnostics changes nothing');
    assert.equal(ro.get('apply_edit'), false, 'apply_edit changes the file — that is how it lands in the record');
  } finally { hand.close(); }
});

/**
 * Loopback is not as narrow as the unix socket the rest of the protocol uses: every process on this
 * machine can reach the port. The token is what makes the caller the daemon we attached to.
 */
test('a caller without the token gets nothing', async () => {
  const hand = await Hand.start(new FakeIde());
  try {
    assert.equal((await rpc(hand, 'tools/list', undefined, 'wrong')).status, 403);
  } finally { hand.close(); }
});

/** An unknown tool is refused. Succeeding quietly lets the agent move on believing it worked. */
test('an unknown tool is refused rather than ignored', async () => {
  const hand = await Hand.start(new FakeIde());
  try {
    const r = (await rpc(hand, 'tools/call', { name: 'rename_everything', arguments: {} })).body.result as
      { content: { text: string }[]; isError: boolean };
    assert.equal(r.isError, true);
    assert.match(r.content[0].text, /no tool called/);
  } finally { hand.close(); }
});

/** A protocol error is not a transport error: a 500 sends somebody looking at the wrong layer. */
test('an unknown method answers a JSON-RPC error with HTTP 200', async () => {
  const hand = await Hand.start(new FakeIde());
  try {
    const r = await rpc(hand, 'tools/nope');
    assert.equal(r.status, 200);
    assert.ok(r.body.error, 'no JSON-RPC error in the body');
  } finally { hand.close(); }
});

/** A missing required argument is told, not guessed at. */
test('a call missing a required argument says which', async () => {
  const ide = new FakeIde();
  const r = await callHand(ide, 'apply_edit', { path: 'a.ts', new: 'x' });
  assert.equal(r.error, true);
  assert.match(r.text, /old is required/);
  assert.equal(ide.replaced, null);
});

/** `line` arrives as a number or as the text of one, depending on the model. */
test('a line number written as text is still a line number', async () => {
  const ide = new FakeIde();
  await callHand(ide, 'show', { path: 'a.ts', line: '12' });
  assert.deepEqual(ide.shown, ['a.ts', 12]);
});

/**
 * The name is fixed. Every `mcp__` tool is dangerous by construction and the only thing that
 * excuses one is an allow rule a person wrote — and that rule keys on this name.
 */
test('the hand has one name', () => {
  assert.equal(HAND_NAME, 'vscode');
  assert.ok(handTools().length >= 3);
});
