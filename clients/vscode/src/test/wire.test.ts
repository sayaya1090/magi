import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';

const REPO = path.join(__dirname, '..', '..', '..', '..');
const PROTOCOL_GO = path.join(REPO, 'internal', 'adapter', 'daemon', 'protocol.go');
const EVENT_GO = path.join(REPO, 'internal', 'core', 'event', 'event.go');
const OURS = path.join(__dirname, '..', '..', 'src', 'core', 'protocol.ts');

/** Every `json:"name"` tag in a Go file, minus its options. */
function tags(file: string): Set<string> {
  const src = fs.readFileSync(file, 'utf8');
  const out = new Set<string>();
  for (const m of src.matchAll(/json:"([^",]+)/g)) out.add(m[1]);
  return out;
}

/** Every field name our TypeScript interfaces declare. */
function ourFields(): Set<string> {
  const src = fs.readFileSync(OURS, 'utf8');
  const out = new Set<string>();
  // Underscores included. Without them the scanner cannot see `call_id`, which is exactly the
  // shape a wrong name takes — and a scanner that cannot see the defect reports none. Measured:
  // with `[A-Za-z0-9]*` a planted `call_id` passed this test untouched.
  for (const m of src.matchAll(/^\s{2}([A-Za-z][A-Za-z0-9_]*)\??:/gm)) out.add(m[1]);
  return out;
}

/** And the field scanner is checked, because a scanner that reads nothing reports nothing. */
test('the field scanner sees the shapes a wrong name takes', () => {
  const sample = ['interface X {', '  callId?: string;', '  call_id?: string;', '  ok: boolean;', '}'].join('\n');
  const seen = new Set<string>();
  for (const m of sample.matchAll(/^\s{2}([A-Za-z][A-Za-z0-9_]*)\??:/gm)) seen.add(m[1]);
  for (const want of ['callId', 'call_id', 'ok']) {
    assert.ok(seen.has(want), `the scanner does not see ${want} — it would miss a renamed field`);
  }
});

/**
 * The names this client reads are the names the daemon writes.
 *
 * TypeScript drops unknown keys silently and hands back `undefined` for missing ones — the same
 * trap `ignoreUnknownKeys` sets on the Kotlin side, and the JetBrains plugin has a test for it for
 * the same reason. A name that disagrees is not an exception. It is a default: the screen says
 * "nothing" and nothing fails, on every frame, for ever.
 *
 * So this reads the Go source rather than a copy of it. A copy would drift with the thing it is
 * supposed to catch drifting.
 */
test('every field we read exists on the daemon wire', () => {
  assert.ok(fs.existsSync(PROTOCOL_GO), `the daemon protocol is not at ${PROTOCOL_GO}`);
  const daemon = tags(PROTOCOL_GO);
  const events = tags(path.join(REPO, 'internal', 'core', 'event', 'payload.go'));
  const known = new Set([...daemon, ...events, ...tags(EVENT_GO)]);

  // The walk asserts it found a wire at all. Reading zero tags and reporting zero mismatches is
  // the shape this whole test exists to prevent.
  assert.ok(known.size >= 40, `only ${known.size} json tags found in the daemon — the scan is broken`);

  const ours = ourFields();
  assert.ok(ours.size >= 15, `only ${ours.size} fields parsed from protocol.ts — the scan is broken`);

  // Ours that the daemon does not have. `data` is ours: the core carries the event payload as
  // json.RawMessage under that name in the Event struct, and it has no tag of its own to find.
  const mine = new Set(['data']);
  const unknown = [...ours].filter((f) => !known.has(f) && !mine.has(f));
  assert.deepEqual(unknown, [],
    `these are read by this client and written by nobody: ${unknown.join(', ')}`);
});

/** The wire version we speak is the one the daemon declares. */
test('the protocol version matches the daemon', () => {
  const src = fs.readFileSync(PROTOCOL_GO, 'utf8');
  const m = src.match(/ProtoVersion\s*=\s*(\d+)/);
  assert.ok(m, 'the daemon does not declare a ProtoVersion any more — this test is reading nothing');
  const ours = fs.readFileSync(OURS, 'utf8').match(/PROTO_VERSION\s*=\s*(\d+)/);
  assert.ok(ours, 'this client does not declare PROTO_VERSION');
  assert.equal(ours![1], m![1], 'the wire version this client speaks is not the daemon\'s');
});

/** The permission words are the daemon's three, spelled once. */
test('the permission vocabulary is the core one', () => {
  const src = fs.readFileSync(PROTOCOL_GO, 'utf8');
  assert.ok(/allow \| deny \| always/.test(src),
    'the core no longer spells the decision this way — check what it spells now');
  const ours = fs.readFileSync(OURS, 'utf8');
  for (const w of ['allow', 'deny', 'always']) {
    assert.ok(ours.includes(`'${w}'`), `this client does not know the decision "${w}"`);
  }
});
