import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';
import { wireRef } from '../core/refs';

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

/**
 * An attachment reaches the door as `refs`, not as words.
 *
 * The defect this pins: the composer's chips were spliced into the head of the person's own text
 * (`path:12-40\n\n` + what they typed), which is the convention `internal/app/refs.go` names in its
 * first paragraph as the one it REPLACED. The `submit` door reads `r.Refs` and the core renders
 * each excerpt inside the workspace jail, caps it (16KB a ref, 64KB the lot) and persists it with
 * the prompt. None of that happens for a path written into a sentence — the agent gets a string and
 * has to go read the file, the transcript cannot show what it was shown, and an attachment that
 * could not be served said nothing at all. The JetBrains client sent the structured shape all
 * along, so this was drift between the two ports, not a missing feature in the core.
 *
 * Read off the source rather than asserted about behaviour, for the reason the tests above are:
 * the seam is one call in a webview message handler, and there is no daemon in this process.
 */
test('the composer sends its attachments as refs, not spliced into the words', () => {
  const chat = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'chat.ts'), 'utf8');
  const say = chat.slice(chat.indexOf("case 'say'"), chat.indexOf("case 'start'"));
  assert.ok(say.length > 100, 'the submit branch was not found — this guard is reading nothing');

  // The door is chosen at runtime now (steer while a turn runs, submit otherwise), so this reads
  // the ask() call by its shape rather than by the literal method name.
  const call = say.slice(say.indexOf('ask(door'));
  assert.ok(call.length > 20, 'the ask() call was not found — this guard is reading nothing');
  assert.ok(call.includes('refs'), "submit does not carry refs — the attachment goes nowhere the core can render it");
  assert.ok(/wireRef/.test(say), 'the chips are not converted to the wire shape ({path, lines})');
  assert.ok(!/lead\s*\+\s*body|refText\(/.test(call),
    'the attachment is still being spliced into the person\'s words — that is the convention refs.go replaced');

  // And the door really does read the field, so this guard cannot outlive the wire it names.
  const doors = fs.readFileSync(path.join(REPO, 'internal', 'adapter', 'daemon', 'doors.go'), 'utf8');
  assert.ok(/Refs:\s*r\.Refs/.test(doors), 'the submit door no longer reads r.Refs — re-read this guard');
});

/**
 * And the shape itself is the one the core parses. `sliceLines` takes "12" or "12-40", one-based
 * and inclusive, and falls back to the WHOLE FILE on anything it cannot parse — which is a silent
 * fallback, so a wrong spelling here reads as "attached the file" rather than as an error.
 */
test('a chip becomes the lines spelling the core parses', () => {
  assert.deepEqual(wireRef({ path: 'a.ts' }), { path: 'a.ts' });
  assert.deepEqual(wireRef({ path: 'a.ts', from: 12 }), { path: 'a.ts', lines: '12' });
  assert.deepEqual(wireRef({ path: 'a.ts', from: 12, to: 12 }), { path: 'a.ts', lines: '12' });
  assert.deepEqual(wireRef({ path: 'a.ts', from: 12, to: 40 }), { path: 'a.ts', lines: '12-40' });
});

/**
 * ★ Every door this client CALLS, it declares the fields of.
 *
 * Undeclared is unreadable. TypeScript hands back `undefined` for a field the interface does not
 * name, so the answer arrives, the screen reads nothing, and nothing fails — which is how three
 * separate facts were lost in one session: `tools` (mcp-attach answers with what it attached, so
 * the screen could only ever say "attached"), `user` (an SSO plugin's username, so every row said
 * "user"), and `council` (whether this companion declares to a council, so it was unknowable).
 *
 * Scoped to doors this client actually calls. A field of a door nobody knocks on is not a defect,
 * and demanding it would grow this type with wire we do not speak.
 */
test('every door we call, we declare the answer of', () => {
  const daemon = fs.readFileSync(
    path.join(REPO, 'internal', 'adapter', 'daemon', 'protocol.go'), 'utf8');
  const doorsGo = fs.readFileSync(
    path.join(REPO, 'internal', 'adapter', 'daemon', 'doors.go'), 'utf8');

  // Response's Go field name → its json tag.
  const struct = /type Response struct \{([\s\S]*?)\n\}/.exec(daemon);
  assert.ok(struct, 'the Response struct was not found — this guard is reading nothing');
  const tag = new Map<string, string>();
  for (const line of struct![1].split('\n')) {
    const m = /^\s*([A-Z]\w*)\s+\S.*?json:"([^",]+)/.exec(line);
    if (m) tag.set(m[1], m[2]);
  }
  assert.ok(tag.size >= 25, `only ${tag.size} Response fields read from the core — the parser is stale`);

  // door → the answer function that serves it.
  const door = new Map<string, string>();
  for (const m of doorsGo.matchAll(/"([a-z][a-z-]*)":\s*\{[^}]*run:\s*(answer\w+)/g)) door.set(m[1], m[2]);
  assert.ok(door.size >= 20, `only ${door.size} doors read from the core — the parser is stale`);

  const fills = (fn: string): string[] => {
    const body = new RegExp(`^func ${fn}\\([\\s\\S]*?\\n\\}`, 'm').exec(doorsGo);
    if (!body) return [];
    // ⚠ Only fields of the RESPONSE. A `Field:` anywhere in the function also matches a nested
    // struct's — `answerJobs` builds `BackgroundJob{Exit: …}`, and reading that as `Response.exit`
    // reported a field this client reads perfectly well (inline, in panel.ts). So: assignments to
    // `resp.X`, plus keys inside a `Response{…}` literal, and nothing else.
    const literals = [...body[0].matchAll(/Response\{([^{}]*)\}/g)].map((m) => m[1]).join(',');
    const out: string[] = [];
    for (const [F, t] of tag) {
      if (t === 'ok' || t === 'error') continue;
      if (new RegExp(`\\bresp\\.${F}\\s*=`).test(body[0]) || new RegExp(`\\b${F}:\\s`).test(literals)) out.push(t);
    }
    return out;
  };
  assert.ok(fills('answerMCPAttach').includes('tools'),
    'the fill-scan cannot see that mcp-attach answers with `tools` — it is reading nothing');

  // What this client calls, and what it declares.
  const src = (p: string): string => fs.readFileSync(path.join(__dirname, '..', '..', 'src', p), 'utf8');
  const calls = new Set<string>();
  const walk = (dir: string): void => {
    for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
      const p = path.join(dir, e.name);
      if (e.isDirectory()) { if (e.name !== 'test') walk(p); continue; }
      if (!e.name.endsWith('.ts')) continue;
      const body = fs.readFileSync(p, 'utf8');
      for (const m of body.matchAll(/\.(?:ask|exchange)\(\s*'([a-z-]+)'/g)) calls.add(m[1]);
      for (const m of body.matchAll(/method:\s*'([a-z-]+)'/g)) calls.add(m[1]);
    }
  };
  walk(path.join(__dirname, '..', '..', 'src'));
  assert.ok(calls.size >= 15, `only ${calls.size} doors called — the scan is reading nothing`);

  const declared = new Set([...src('core/protocol.ts').matchAll(/^ {2}([a-zA-Z]+)\??:/gm)].map((m) => m[1]));
  const missing: string[] = [];
  for (const d of [...calls].sort()) {
    const fn = door.get(d);
    if (!fn) continue;
    for (const t of fills(fn)) if (!declared.has(t)) missing.push(`${d} → ${t}`);
  }
  assert.deepEqual(missing, [],
    'these doors are called and their answer carries a field this client does not declare, so it ' +
    'cannot be read at all and nothing fails: ' + missing.join(', '));
});
