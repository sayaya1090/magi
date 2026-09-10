import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';
import { wireRef, globQuote } from '../core/refs';

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
  // ⚠ **The PACKAGE, not one file.** This named `protocol.go` alone, and `RosterRow` lives in
  // `roster.go` — so twenty-six wire names were invisible to this guard, and declaring any of them
  // was reported as "written by nobody". Naming files ages: the JetBrains side of this same check
  // learned it the hard way when a daemon file split into six and its release went red.
  const dir = path.dirname(PROTOCOL_GO);
  const daemon = new Set<string>();
  for (const f of fs.readdirSync(dir)) {
    if (f.endsWith('.go') && !f.endsWith('_test.go')) for (const t of tags(path.join(dir, f))) daemon.add(t);
  }
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

/**
 * ★ The wire is TYPED, so an invented name cannot compile.
 *
 * The durable half of three measured defects. While `Response` said `unknown` for its nested
 * shapes, each reader cast it to a shape of its own and got names wrong — `handover.state` and
 * `handover.error` were never on this wire, so both of their branches were dead and a handover
 * that died read as "working" for the life of the window. Tests then fed the same invented names
 * and stayed green about behaviour that could not happen, three separate times.
 *
 * Most of the enforcement is now the compiler's: untyping a field a reader touches breaks the
 * build. This guard covers what the compiler cannot — a field NOBODY reads yet, which is exactly
 * the state every one of those three was in before somebody wrote the first reader for it.
 */
test('no door we call answers into an untyped hole', () => {
  const proto = fs.readFileSync(OURS, 'utf8');
  const body = proto.slice(proto.indexOf('export interface Response'));
  // ONE scan, used for both the verdict and the check on it. Written as a function because the
  // first cut checked itself with a SECOND regex — so breaking the real one left the self-check
  // green and the verdict silently empty, which is the shape every guard here exists to prevent.
  const scan = (text: string): string[] =>
    [...text.matchAll(/^ {2}([a-zA-Z]+)\?:\s*unknown(\[\])?;/gm)].map((m) => m[1]);

  assert.deepEqual(scan('interface X {\n  a?: unknown;\n  b?: unknown[];\n  c?: string;\n}'), ['a', 'b'],
    'the scan cannot find an untyped field in a sample that has two — it would report none for any reason');

  const holes = scan(body);

  assert.deepEqual(holes, [],
    'these answer into `unknown`, so a reader will cast them to a shape of its own and a wrong ' +
    'name will read as undefined with nothing failing: ' + holes.join(', '));

  // `Request.args` is deliberately open — a TOOL's arguments, spelled by that tool's own schema,
  // which this client neither knows nor should. The scan finds it and this rule does not report it,
  // because the rule is about the ANSWERS: it reads from `Response` down.
  assert.ok(scan(proto).includes('args'), 'the scan no longer sees the one field that stays open');
  assert.ok(/roster\?: RosterRow\[\]/.test(body), 'the roster went back to being untyped');
  assert.ok(/handover\?: \{/.test(body), 'the handover went back to being untyped');
});

/**
 * ★ The declared TYPE of a wire field, not just its name.
 *
 * `wire.test.ts` already checks that the names this client reads are names the daemon writes. It did
 * not check what SHAPE they arrive in, and five fields on `RosterRow` had drifted out of step with
 * the struct (measured 2026-09-10): `hub` was `string` for a bool, `can` was `string[]` for an int,
 * `does` was `string` for a `[]string`, `waiting` was `string` for an int, `handling` was `number`
 * for a bool.
 *
 * TypeScript cannot catch this on its own — JSON crosses the boundary as `unknown`, so the
 * declaration is a promise nobody checks. What a wrong type does do is block the CORRECT read:
 * `r.waiting > 0` was rejected with "Operator '>' cannot be applied to types 'string' and 'number'",
 * which is how a field ends up declared, unread, and quietly absent from the screen.
 */
test('the roster fields are declared with the shapes the daemon sends', () => {
  const go = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'internal', 'adapter', 'daemon', 'roster.go'), 'utf8')
    + fs.readFileSync(
      path.join(__dirname, '..', '..', '..', '..', 'internal', 'adapter', 'daemon', 'protocol.go'), 'utf8');
  const at = go.indexOf('type RosterRow struct');
  assert.ok(at > 0, 'the core no longer has the roster row this guard reads');
  const struct = go.slice(at, go.indexOf('\n}', at));
  const goType = new Map<string, string>();
  for (const m of struct.matchAll(/^\t\w+\s+(\**\[?\]?[\w.[\]]+)\s+`json:"([a-z][a-zA-Z]*)/gm)) {
    goType.set(m[2], m[1].replace(/^\*/, ''));
  }
  assert.ok(goType.size >= 20, `only ${goType.size} roster fields read from the core — the parser is stale`);

  const ts = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'core', 'protocol.ts'), 'utf8');
  // ⚠ **Inline declarations count too.** The first cut of this guard read only `export interface X`,
  // and the nested shapes are declared inline inside `Response` — so `unreadable` sat declared as
  // `boolean` for a wire `string` and this test said the shapes were fine. A guard that can only see
  // half the declarations answers "clean" about the half it cannot see.
  //
  // ⚠ **`unknown` is a wrong shape too.** It reads like caution and behaves like a mistake: nothing
  // can iterate it, so nothing does. `topics` sat `unknown` and the fold count went out with no
  // subjects beside it. Judged here like any other mismatch.
  {
    const pair: Record<string, string> = {
      config: 'ConfigItem', profiles: 'ProfileChoice', cron: 'CronRow',
      handover: 'Handover', jobs: 'Jobs', context: 'ContextState',
    };
    const want: Record<string, string> = {
      string: 'string', int: 'number', int64: 'number', float64: 'number',
      bool: 'boolean', '[]string': 'string[]',
    };
    let judged = 0;
    for (const [key, goName] of Object.entries(pair)) {
      const at = ts.indexOf(`  ${key}?: {`);
      if (at < 0) continue;
      let depth = 0, end = at;
      for (let k = ts.indexOf('{', at); k < ts.length; k++) {
        if (ts[k] === '{') depth++;
        else if (ts[k] === '}' && --depth === 0) { end = k; break; }
      }
      const body = ts.slice(at, end);
      const gAt = go.indexOf(`type ${goName} struct`);
      if (gAt < 0) continue;
      const gBody = go.slice(gAt, go.indexOf('\n}', gAt));
      const gTypes = new Map<string, string>();
      for (const m of gBody.matchAll(/^\t\w+\s+(\**\[?\]?[\w.[\]]+)\s+`json:"([a-z][a-zA-Z]*)/gm)) {
        gTypes.set(m[2], m[1].replace(/^\*/, ''));
      }
      for (const m of body.matchAll(/(\w+)\??:\s*([\w[\]]+)\s*[;,]/g)) {
        const gt = gTypes.get(m[1]);
        if (!gt || !want[gt]) continue;
        judged++;
        assert.equal(m[2], want[gt],
          `${key}.${m[1]} is declared \`${m[2]}\` and the daemon sends \`${gt}\``);
      }
    }
    // A parser that matched nothing would pass every assertion above without making one.
    assert.ok(judged >= 20, `only ${judged} inline fields judged — the parser is reading nothing`);
  }
  // ⚠ Anchor on the DECLARATION, not the first mention. The first cut used `indexOf('RosterRow')`
  // and landed on a usage above it, so the parser read five fields and the guard said so.
  const start = ts.search(/export (?:interface|type) RosterRow\b/);
  assert.ok(start >= 0, 'the roster declaration is not where this guard looks');
  const decl = ts.slice(start, ts.indexOf('\n}', start));
  const tsType = new Map<string, string>();
  for (const m of decl.matchAll(/^\s{2}(\w+)\??:\s*([\w[\]]+);/gm)) tsType.set(m[1], m[2]);
  assert.ok(tsType.size >= 15, `only ${tsType.size} fields read from the declaration — the parser is stale`);

  const want: Record<string, string> = {
    string: 'string', int: 'number', int64: 'number', float64: 'number',
    bool: 'boolean', '[]string': 'string[]',
  };
  // Pinned so a broken parser cannot answer "all fine": these are the shapes that were wrong.
  for (const [f, t] of [['waiting', 'number'], ['handling', 'boolean'], ['can', 'number'], ['does', 'string[]']]) {
    assert.equal(tsType.get(f), t, `\`${f}\` is declared \`${tsType.get(f)}\`, and the daemon sends \`${t}\``);
  }
  for (const [name, gt] of goType) {
    const declared = tsType.get(name);
    if (!declared || !want[gt]) continue;   // not read here, or a shape this guard cannot judge
    assert.equal(declared, want[gt],
      `\`${name}\` is declared \`${declared}\` and the daemon sends \`${gt}\` — a wrong shape here ` +
      'blocks the correct read rather than failing loudly');
  }
});

/**
 * ★ The `@` in the box finds the same files here as it does on the other two surfaces.
 *
 * A typed `@page[1` is a filename, not a character class. Measured against a running daemon
 * (2026-09-10): the tool answers `ok:false, "invalid glob pattern: syntax error in pattern"`, and
 * this client's mention popup reads `out` without ever looking at `ok`, so the refusal arrives as
 * an empty list — the same shape as "no such file". A CLOSED bracket is worse: `pa[nl]el` is valid,
 * so it searches for something nobody typed and presents the result as the answer.
 *
 * The escaped set is read out of `files.go` rather than written here, because three surfaces
 * agreeing today is not the claim — the claim is that they cannot drift apart. A list copied into
 * this language is exactly the thing that goes stale.
 */
test('a typed mention escapes the same glob characters the console escapes', () => {
  const files = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', 'web', 'server', 'files.go'), 'utf8');
  // strings.ContainsRune(`*?[]\`, r) — the backquoted set inside globQuote.
  const set = /func globQuote[\s\S]*?ContainsRune\(`([^`]+)`/.exec(files)?.[1];
  assert.ok(set, 'globQuote was not found in files.go — this guard is reading nothing');
  assert.ok(set.length >= 4, `only ${set.length} characters read from globQuote — the scan is broken`);

  for (const ch of set) {
    assert.equal(globQuote(ch), '\\' + ch, `${ch} reaches the tool unescaped and means itself to the glob`);
  }
  // And the wrapping wildcards the caller adds are NOT the ones being escaped: an ordinary word
  // must come through untouched, or every mention would search for a literal backslash.
  assert.equal(globQuote('panel'), 'panel');
  assert.equal(globQuote('page[1'), 'page\\[1');
});

/**
 * ★ And the mention actually calls it.
 *
 * A quoting function nobody calls quotes nothing. Measured: deleting `globQuote` from the call site
 * (and its now-unused import) left all 204 tests green, because the test above only asks what the
 * function returns. Testing a function and testing the place that calls it are different facts, and
 * this repository has paid for that difference more than once.
 *
 * Read off the source: `chat.ts` imports `vscode`, so no test in this process can load it and press
 * the seam. What can be checked is that the pattern is built from the quoted token.
 */
test('the mention builds its pattern from the quoted token', () => {
  const chat = fs.readFileSync(
    path.join(__dirname, '..', '..', 'src', 'ide', 'chat.ts'), 'utf8');
  const at = chat.indexOf("name: 'glob'");
  assert.ok(at > 0, 'the mention call was not found — this guard is reading nothing');
  // To the end of that statement, so the check is on the pattern and not on the whole file.
  const call = chat.slice(at, chat.indexOf('});', at));
  assert.match(call, /pattern:[^\n]*globQuote\(/,
    'the mention sends the typed token straight to the glob — a bracket in a filename refuses');
});
