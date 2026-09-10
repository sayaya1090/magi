import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';

const manifest = JSON.parse(
  fs.readFileSync(path.join(__dirname, '..', '..', 'package.json'), 'utf8'),
) as {
  engines: { vscode: string };
  contributes: {
    viewsContainers: Record<string, { id: string }[]>;
    views: Record<string, { id: string; type?: string }[]>;
    commands?: { command: string }[];
  };
};

/** The release that added `secondarySidebar` to `contributes.viewsContainers` (VS Code 1.106). */
const SECONDARY_SIDEBAR_SINCE = 106;

function minorFloor(range: string): number {
  const m = /^\D*(\d+)\.(\d+)/.exec(range);
  assert.ok(m, `engines.vscode is not a version range: ${range}`);
  assert.equal(m![1], '1', 'this guard assumes the 1.x line');
  return Number.parseInt(m![2], 10);
}

/**
 * Strip comments from a TypeScript source before scanning it for code shapes.
 *
 * Only a block comment that OPENS A LINE counts. Stripping every opener anywhere was measured to
 * eat working code: `chat.ts` builds a glob inside a template literal, and the slash-star in the
 * middle of that string opened a comment the strip closed hundreds of lines later. Everything
 * between went unseen — and a scan that reads nothing passes every check it makes, silently, for
 * as long as nobody looks (2026-09-10). Writing that glob into this very sentence would end the
 * comment here, which is the same fact from the other side.
 */
export function withoutComments(src: string): string {
  return src.replace(/^[ \t]*\/\*[\s\S]*?\*\//gm, '').replace(/^[ \t]*\/\/.*$/gm, '');
}

/**
 * ★ The strip keeps code that merely looks like a comment opener.
 *
 * This is the guard on the guards: two scans below decide what this client reads and what it gates
 * on, and both were blind the same way. The fixture is the real shape from `chat.ts` — a glob in a
 * template literal — with a capability check and a settings read after it, which is exactly what
 * used to disappear.
 */
test('a comment opener inside a string does not swallow the code after it', () => {
  const poisoned = [
    '/** a doc block that really is one */',
    'const g = `**/*${name}*`;',
    "if (caps.has('roster')) ok();",
    "const v = vscode.workspace.getConfiguration('magi').get<boolean>('suggest', true);",
    // A later doc block is what gives the poison something to close on. Without one the runaway
    // comment never terminates and the naive strip removes nothing — which is how a first draft of
    // this fixture passed under the very mutation it exists to catch.
    '/** and a later doc block, as every real file has */',
    "const w = caps.has('transcript');",
  ].join('\n');
  const out = withoutComments(poisoned);
  assert.ok(!out.includes('a doc block that really is one'), 'the line-opening block comment survived');
  assert.match(out, /caps\.has\('roster'\)/, 'the capability check after the glob was eaten');
  assert.match(out, /getConfiguration\('magi'\)/, 'the settings read after the glob was eaten');
});

/**
 * A container declared in a place the declared floor does not have.
 *
 * This is not a style rule. `secondarySidebar` landed in 1.106, and in builds below it the key is
 * still parsed — an extension that ships it with a lower floor was reported moving OTHER
 * extensions' views around and producing "container does not exist"
 * (QwenLM/qwen-code#2432). So the damage is not "our view fails to appear", which somebody would
 * notice; it is somebody else's layout, which they would blame on their own extension.
 *
 * The check is on the manifest rather than on a comment, because the floor and the container are
 * edited in different sittings and only one of them is ever the thing being thought about.
 */
test('a view container is not declared in a place the engine floor lacks', () => {
  const floor = minorFloor(manifest.engines.vscode);
  const where = Object.keys(manifest.contributes.viewsContainers);
  assert.ok(where.length > 0, 'no view containers found — this guard is reading nothing');
  if (where.includes('secondarySidebar')) {
    assert.ok(floor >= SECONDARY_SIDEBAR_SINCE,
      `secondarySidebar needs VS Code 1.${SECONDARY_SIDEBAR_SINCE}+, but engines.vscode allows ` +
      `1.${floor} — on older builds this moves other extensions' views`);
  }
  // Only the three the schema names. `additionalProperties: false` means a fourth (or a
  // mis-cased `secondarySideBar`, which is how it is written in several write-ups) is a schema
  // error the marketplace reports and nothing here would otherwise catch.
  for (const w of where) {
    assert.ok(['activitybar', 'panel', 'secondarySidebar'].includes(w),
      `no such view container location: ${w}`);
  }
});

/**
 * Every view lives in a container that exists.
 *
 * The two are keyed by hand and edited apart — this move renamed the location and left the
 * container id alone, and the reverse mistake produces a view that is simply never drawn, with no
 * error anywhere.
 */
test('every view names a container that is declared', () => {
  const declared = new Set(
    Object.values(manifest.contributes.viewsContainers).flat().map((c) => c.id),
  );
  const keys = Object.keys(manifest.contributes.views);
  assert.ok(keys.length > 0, 'no views found — this guard is reading nothing');
  for (const k of keys) {
    assert.ok(declared.has(k), `views."${k}" has no container by that id (declared: ${[...declared]})`);
  }
});

/**
 * Every test file is named in TESTING.ko.md.
 *
 * The document is the only map of what is measured, and a test that is not on it is one nobody
 * knows to keep. This guard is carried over from the JetBrains client, which has had it for a
 * while — and the first thing it found here was that `webview.test.ts` had never been listed.
 */
test('the testing document names every test file', () => {
  const dir = path.join(__dirname, '..', '..', 'src', 'test');
  const found = fs.readdirSync(dir).filter((f) => f.endsWith('.test.ts'));
  assert.ok(found.length >= 5, `only ${found.length} test files walked — the walk is broken`);
  const doc = fs.readFileSync(path.join(__dirname, '..', '..', 'docs', 'TESTING.ko.md'), 'utf8');
  const missing = found.filter((f) => !doc.includes(f));
  assert.equal(missing.length, 0, `docs/TESTING.ko.md does not name: ${missing.join(', ')}`);
});

/**
 * Every setting the code reads is declared, and every setting declared is read.
 *
 * A key the code reads but the manifest does not declare has no UI, no default from the manifest,
 * and no error — `get(key, fallback)` quietly returns the fallback for ever, so the feature simply
 * never turns on and nothing says why. The reverse is a switch in the settings UI that does
 * nothing, which is worse: a person changes it and concludes the extension is broken.
 */
test('the settings the code reads are exactly the settings declared', () => {
  const dir = path.join(__dirname, '..', '..', 'src');
  // The tests are not the extension. This one in particular quotes the very shapes it looks for,
  // and a scanner that reads its own explanation of itself reports a setting called "key" — which
  // is how this guard failed the first time it ran.
  const walk = (d: string): string[] => fs.readdirSync(d, { withFileTypes: true }).flatMap((e) =>
    e.isDirectory() ? (e.name === 'test' ? [] : walk(path.join(d, e.name)))
      : e.name.endsWith('.ts') ? [path.join(d, e.name)] : []);
  const src = walk(dir);
  assert.ok(src.length >= 10, `only ${src.length} sources walked — the walk is broken`);

  const read = new Set<string>();
  for (const f of src) {
    // Comments stripped for the same reason: a sentence ABOUT a setting is not a read of one.
    // ⚠ Only a block comment that OPENS A LINE is stripped. Stripping every `/*` was measured to
    // eat working code: `chat.ts` builds a glob `**/*<text>*`, and that mid-line `/*` opened a
    // comment the strip closed hundreds of lines later — every setting read below it went unseen,
    // in both directions, and the guard stayed green while blind (2026-09-10).
    const raw = fs.readFileSync(f, 'utf8');
    const body = withoutComments(raw);
    // getConfiguration('magi') … .get<T>('key' …) — the two halves can sit apart, so the section
    // is checked per file rather than per expression.
    const opens = /getConfiguration\(['"]magi['"]\)/;
    // The strip is not allowed to remove the very thing being counted. Without this the blindness
    // above is invisible: a file that reads settings simply stops being a file that reads settings.
    assert.equal(opens.test(body), opens.test(raw),
      `stripping comments changed whether ${path.basename(f)} reads settings — the strip is eating code`);
    if (!opens.test(body)) continue;
    for (const m of body.matchAll(/\.get(?:<[^>]*>)?\(\s*['"]([A-Za-z.]+)['"]/g)) read.add(`magi.${m[1]}`);
  }
  assert.ok(read.size > 0, 'no settings reads found — this guard is reading nothing');

  const manifestKeys = new Set(Object.keys(
    (JSON.parse(fs.readFileSync(path.join(__dirname, '..', '..', 'package.json'), 'utf8')) as
      { contributes: { configuration?: { properties?: Record<string, unknown> } } })
      .contributes.configuration?.properties ?? {},
  ));
  for (const k of read) {
    assert.ok(manifestKeys.has(k), `the code reads ${k}, which package.json does not declare`);
  }
  for (const k of manifestKeys) {
    assert.ok(read.has(k), `package.json declares ${k}, which no code reads`);
  }
});

/**
 * ★ Every capability this client gates on is one the daemon can actually advertise.
 *
 * Thirteen of the daemon's doors carry a capability; thirty-one do not. Gating a command on one of
 * the thirty-one makes it answer "this companion does not offer that" on every build, for ever —
 * and nothing fails, because a client that never calls a door never learns anything about it.
 * Measured 2026-09-09: `compact`, `rewind` and `reload-cron` were gated that way and were dead the
 * whole time they existed.
 *
 * Read from the Go source rather than from a list here, for the reason `wire.test.ts` reads it: a
 * list copied into this repository's other language is the thing that goes stale.
 */
test('every capability the client checks is one the daemon advertises', () => {
  const doors = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'internal', 'adapter', 'daemon', 'doors.go'), 'utf8');
  // `capsOf` puts these two in by hand — they are build-level, not read off the door table.
  const advertised = new Set(['handshake', 'roster', 'transcript']);
  for (const m of doors.matchAll(/cap:\s*"([a-z][a-z0-9-]*)"/g)) advertised.add(m[1]);
  assert.ok(advertised.size >= 12,
    `only ${advertised.size} capabilities found in doors.go — this guard is not reading it`);

  const dir = path.join(__dirname, '..', '..', 'src');
  const walk = (d: string): string[] => fs.readdirSync(d, { withFileTypes: true }).flatMap((e) =>
    e.isDirectory() ? (e.name === 'test' ? [] : walk(path.join(d, e.name)))
      : e.name.endsWith('.ts') ? [path.join(d, e.name)] : []);
  const checked = new Map<string, string>();
  // Not a /g regex: `.test` on a global one carries `lastIndex` from call to call, so the second
  // of the two presence checks below would start mid-file and could answer either way by accident.
  const gate = /(?:caps\.has|\bhas)\(\s*'[a-z][a-z0-9-]*'/;
  for (const f of walk(dir)) {
    const raw = fs.readFileSync(f, 'utf8');
    // Line-opening block comments only — see the note in the settings guard above. A mid-line
    // `/*` inside a glob string swallows the rest of the file, and a scan that reads nothing
    // passes every check it makes.
    const body = withoutComments(raw);
    assert.equal(gate.test(body), gate.test(raw),
      `stripping comments changed whether ${path.basename(f)} checks a capability — the strip is eating code`);
    // caps.has('x'), and the `has('x', …)` helper in doors.ts.
    for (const m of body.matchAll(/(?:caps\.has|\bhas)\(\s*'([a-z][a-z0-9-]*)'/g)) {
      checked.set(m[1], path.relative(dir, f));
    }
  }
  assert.ok(checked.size >= 5, `only ${checked.size} capability checks found — this guard is not reading them`);
  for (const [cap, where] of checked) {
    assert.ok(advertised.has(cap),
      `${where} gates on capability "${cap}", which the daemon never advertises — that command is ` +
      'dead on every build. Call the door instead and show the daemon\'s own refusal.');
  }
});

/**
 * ★ A value goes in the field the door reads, not in a field that merely exists.
 *
 * `cron-set` reads `req.Schedule` — a top-level field. This client sent the schedule as
 * `args.schedule`, and `args` is a field the wire HAS (the `tool` door reads it) so nothing was
 * dropped as unknown and nothing failed: the door simply got an empty schedule. The JetBrains client
 * sent it top-level all along; only this one was wrong.
 *
 * Pinned against the Go source, because what makes this a defect is where the DAEMON looks.
 */
test('a scheduled job sends its schedule where the door reads it', () => {
  const proto = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'internal', 'adapter', 'daemon', 'protocol.go'), 'utf8');
  const req = proto.slice(proto.indexOf('type Request struct'), proto.indexOf('type Response struct'));
  assert.match(req, /Schedule\s+string\s+`json:"schedule/,
    'the wire no longer carries a top-level `schedule` — this client sends one');

  const cmd = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'doors.ts'), 'utf8');
  const call = /call\('cron-set',\s*\{([^}]*)\}/.exec(cmd);
  assert.ok(call, 'the cron-set call is not where this guard looks');
  assert.match(call![1], /\bschedule:/, 'cron-set does not send a top-level schedule');
  assert.ok(!/\bargs:/.test(call![1]), 'cron-set still nests its fields inside args');
});

/**
 * ★ Every `state` message carries the note the panel draws.
 *
 * The words and whether to offer a way out are decided in core, and the webview only draws them — so
 * a `state` post without `note` leaves the panel with nothing to say exactly when there is something
 * to say. A unit test cannot see this: the webview is a string of script, not a module, so the wiring
 * is measured here. Found by mutation: removing `note:` from one of the three posts changed nothing
 * that any test could see.
 */
test('every state message carries its note', () => {
  const body = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'chat.ts'), 'utf8')
    .replace(/\/\*[\s\S]*?\*\//g, '').replace(/^[ \t]*\/\/.*$/gm, '');
  const posts = [...body.matchAll(/\{\s*kind:\s*'state'[^}]*\}/g)].map((m) => m[0]);
  assert.ok(posts.length >= 3, `only ${posts.length} state posts found — this guard is reading nothing`);
  for (const p of posts) {
    assert.match(p, /note:\s*panelNote\(/,
      `a state message goes out without its note, so the panel says nothing:\n  ${p}`);
  }
});

/**
 * ★ And the page READS the note it was sent.
 *
 * The guard above measures the send. It was green for as long as the read was broken: the message
 * puts `note` BESIDE `state`, and the page called `drawState(m.state)` while the drawing function
 * reached for `st.note` — a property `Activity` does not have (`state`, `asking`, `doing`). Always
 * undefined, nothing thrown, and the sentence and the "Start one" button simply never drew. That is
 * the same screen as "everything is fine", shown to the one person for whom nothing is fine: their
 * companion is not running and the button was the way out.
 *
 * Derived from `panelNote`'s own return shape rather than a remembered pair of names, so a key
 * renamed there fails here instead of going quiet again.
 */
test('the panel draws the note it is sent', () => {
  const core = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'core', 'activity.ts'), 'utf8');
  const sig = /export function panelNote\([^)]*\):\s*\{([^}]*)\}/.exec(core);
  assert.ok(sig, 'panelNote no longer declares what it returns — this guard cannot derive its keys');
  const keys = [...sig[1].matchAll(/(\w+)\s*:/g)].map((m) => m[1]);
  assert.ok(keys.length >= 2, `only ${keys.length} key(s) read off panelNote — the scan is dead`);

  const body = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'chat.ts'), 'utf8')
    .replace(/\/\*[\s\S]*?\*\//g, '').replace(/^[ \t]*\/\/.*$/gm, '');
  // The handler hands the drawing function the NOTE, not the state beside it.
  assert.match(body, /kind === 'state'\)\s*drawState\(m\.note\)/,
    'the page is handed the state and reaches inside it for a note that is not there');
  const at = body.indexOf('function drawState(');
  assert.ok(at > 0, 'the drawing function is not where this guard looks for it');
  const whole = body.slice(at, body.indexOf('\nfunction ', at + 1));
  // ⚠ **Past the bail-out.** Reading a key to DECIDE is not drawing it — a mutation proved it:
  // replacing `append(note.text + ' ')` with `append('')` left `!note.text` standing in the early
  // return, and a scan of the whole function found the name and passed while the sentence was gone.
  const bail = whole.indexOf('return;');
  assert.ok(bail > 0, 'the drawing function no longer bails out — re-read this guard');
  const fn = whole.slice(bail);
  for (const k of keys) {
    assert.ok(new RegExp(`\\.${k}\\b`).test(fn),
      `panelNote returns \`${k}\` and the panel never reads it — it crosses to the page and dies there`);
  }
});

/**
 * ★ Every message kind is both sent and received, in both directions, in every webview.
 *
 * A webview is a string of script: nothing type-checks the two halves against each other, and an
 * orphan kind fails the quiet way — the sender posts, nobody listens, and the feature is simply
 * absent. The narrower version of this guard (state messages must carry their note) was added after
 * a mutation proved no unit test could see that wiring at all; this is the general case.
 *
 * Measured clean when written (chat: six kinds each way, plan: one and one). The point is that it
 * stays that way.
 */
test('no webview message is sent to nobody or awaited from nobody', () => {
  const dir = path.join(__dirname, '..', '..', 'src', 'ide');
  const views = fs.readdirSync(dir).filter((f) => f.endsWith('.ts'))
    .map((f) => ({ name: f, body: fs.readFileSync(path.join(dir, f), 'utf8') }))
    .filter((v) => /<script\b/.test(v.body));
  assert.ok(views.length >= 2, `only ${views.length} webviews found — this guard is reading nothing`);

  for (const { name, body } of views) {
    const at = body.search(/<script\b/);
    const ext = body.slice(0, at);
    const web = body.slice(at);
    const kinds = (s: string, re: RegExp): Set<string> =>
      new Set([...s.matchAll(re)].map((m) => m[1]));

    // Extension → webview.
    const sent = kinds(ext, /post(?:Message)?\(\{\s*kind:\s*'([a-z]+)'/g);
    const heard = kinds(web, /m\.kind (?:===|!==) '([a-z]+)'/g);
    assert.ok(sent.size > 0, `${name}: no messages out found — this guard is reading nothing`);
    for (const k of sent) {
      assert.ok(heard.has(k), `${name}: the extension posts "${k}" and the webview never reads it`);
    }
    for (const k of heard) {
      assert.ok(sent.has(k), `${name}: the webview waits for "${k}" and nothing ever posts it`);
    }

    // Webview → extension. A kind the extension does not handle falls into its default and is lost.
    //
    // ⚠ Both shapes count. The chat side switches (`case 'say'`) and the plan side tests
    // (`if (m.kind === 'ready')`) — this guard read only the first and reported the second as an
    // unhandled message, which is a guard failing a build over nothing. A rule that recognises one
    // spelling of a thing is a rule about spelling.
    const back = kinds(web, /vs\.postMessage\(\{\s*kind:\s*'([a-z]+)'/g);
    const taken = new Set([
      ...kinds(ext, /case '([a-z]+)'/g),
      ...kinds(ext, /m\.kind === '([a-z]+)'/g),
    ]);
    // The self-check sits here, where `back` exists: plan.ts sends at least `ready`, so reading none
    // means this guard stopped seeing what it is for.
    assert.ok(back.size > 0, `${name}: no messages back found — this guard is reading nothing`);
    for (const k of back) {
      assert.ok(taken.has(k), `${name}: the webview sends "${k}" and the extension never handles it`);
    }
  }
});

/**
 * ★ Every event name this client branches on is a real event type.
 *
 * A `switch` on `e.type` and an `if (e.type === …)` both fall through silently on a name that does
 * not exist: it compiles, no test touches it, and that one branch simply never runs. The JetBrains
 * client got this guard first, for the same reason and after the same near-miss — the tool that
 * measured it was wrong about `compaction` because its pattern required a dot.
 *
 * ⚠ **Names without a dot are events too** (`compaction`, `error`). They are pinned in the
 * self-check below: a measuring tool that has died always answers "clean".
 */
test('every event name the client branches on exists in the core', () => {
  const ev = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'internal', 'core', 'event', 'event.go'), 'utf8');
  const known = new Set([...ev.matchAll(/Type[A-Za-z]+\s+Type\s*=\s*"([a-z][a-z0-9.]*)"/g)].map((m) => m[1]));
  assert.ok(known.size >= 20, `only ${known.size} event types read from the core — the parser is stale`);

  const dir = path.join(__dirname, '..', '..', 'src');
  const walk = (d: string): string[] => fs.readdirSync(d, { withFileTypes: true }).flatMap((e) =>
    e.isDirectory() ? (e.name === 'test' ? [] : walk(path.join(d, e.name)))
      : e.name.endsWith('.ts') ? [path.join(d, e.name)] : []);
  const named = new Map<string, string>();
  for (const f of walk(dir)) {
    const body = fs.readFileSync(f, 'utf8')
      .replace(/\/\*(?:(?!\*\/)[\s\S])*?\*\//g, '').replace(/^[ \t]*\/\/.*$/gm, '');
    for (const m of body.matchAll(/e\.type (?:===|!==) '([a-z][a-z0-9.]*)'|case '([a-z][a-z0-9.]*)':/g)) {
      const n = m[1] ?? m[2];
      if (n) named.set(n, path.relative(dir, f));
    }
  }
  // The self-check, including the two whose names carry no dot.
  for (const must of ['part.appended', 'turn.finished', 'part.delta', 'todos.changed', 'compaction']) {
    assert.ok(named.has(must), `the guard did not see the branch on "${must}" — it is measuring nothing`);
  }

  // Only dotted names are judged: a bare word in a `case` is as likely to be a part kind or a
  // message kind, and mixing the vocabularies is how a guard reports a defect that is not there.
  for (const [name, where] of named) {
    if (!name.includes('.')) continue;
    assert.ok(known.has(name),
      `${where} branches on event "${name}", which the core does not have — that branch never runs`);
  }
});

/**
 * ★ Every part kind the core can put in a log is one this client draws.
 *
 * The other direction of the event-name guard, and the one that just cost something: a part kind the
 * fold does not name is **not an empty row — it is a row that never existed**, with nothing anywhere
 * saying so. The web console's own comment says that, having hit it with `image` and `error`; this
 * client had `error` and was dropping `image` on the floor.
 *
 * Not every kind belongs on this screen, so the ones deliberately left out are named here with the
 * reason. A kind added to the core lands in neither list and fails — which is the point: the choice
 * gets made rather than defaulting to silence.
 */
test('every part kind the core has is drawn or deliberately not', () => {
  const src = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'internal', 'core', 'session', 'session.go'), 'utf8');
  const kinds = new Set([...src.matchAll(/PartKind = "([a-z-]+)"/g)].map((m) => m[1]));
  assert.ok(kinds.size >= 5, `only ${kinds.size} part kinds read from the core — the parser is stale`);

  const fold = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'core', 'transcript.ts'), 'utf8')
    .replace(/\/\*(?:(?!\*\/)[\s\S])*?\*\//g, '').replace(/^[ \t]*\/\/.*$/gm, '');
  const drawn = new Set([...fold.matchAll(/p\.kind === '([a-z-]+)'/g)].map((m) => m[1]));
  // ⚠ The self-check must not pin the thing under test. It pinned `image` at first, so removing the
  // image branch failed here — with the wrong sentence — instead of at the judgement below, and a
  // future decision to stop drawing images would have been reported as a broken guard.
  assert.ok(drawn.has('text'), 'the guard cannot see the fold it is reading — text is branched on there');
  assert.ok(drawn.size >= 4, `only ${drawn.size} part kinds seen in the fold — the guard is reading nothing`);

  // Left out on purpose, each for a reason a reader can check.
  const skipped: Record<string, string> = {
    // A tool call and its result are drawn from `toolCall`/`toolResult`; the kinds above cover them.
    // These two are the ROLE vocabulary that shares the string space, not part kinds this fold sees.
  };
  for (const k of kinds) {
    if (k in skipped) continue;
    assert.ok(drawn.has(k),
      `the core can log a "${k}" part and this fold never names it — such a row never exists, and ` +
      'nothing says so. Draw it, or add it to `skipped` with the reason.');
  }
});

/**
 * ★ Every row vocabulary the fold produces reaches the screen.
 *
 * A `who` with no rule renders as body text: the fold distinguishes it and the screen does not, so a
 * note about the conversation ("the conversation was folded here") reads as something the companion
 * said. Measured after adding `system` for the fold row — it had no rule at all, while the JetBrains
 * client draws the same rows small, italic and faint.
 *
 * `agent` is the exception and is named here: it IS the body, so having no rule of its own is the
 * whole point. Anything else without one is a vocabulary that stops at the fold.
 */
test('every row kind the fold produces has a style', () => {
  const fold = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'core', 'transcript.ts'), 'utf8');
  const made = new Set([...fold.matchAll(/who: '([a-z]+)'/g)].map((m) => m[1]));
  assert.ok(made.size >= 4, `only ${made.size} row kinds seen in the fold — this guard is reading nothing`);

  const chat = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'chat.ts'), 'utf8');
  const css = chat.slice(chat.indexOf('<style>'), chat.indexOf('</style>'));
  assert.ok(css.length > 100, 'the stylesheet is not where this guard looks');
  const styled = new Set([...css.matchAll(/\.([a-z][a-z-]*)/g)].map((m) => m[1]));

  // Deliberately unstyled, with the reason.
  const body: Record<string, string> = { agent: 'it IS the body text — a rule of its own would say nothing' };
  for (const who of made) {
    if (who in body) continue;
    assert.ok(styled.has(who),
      `the fold makes a "${who}" row and the panel has no rule for it, so it reads as the ` +
      'companion\'s own words. Style it, or add it to `body` with the reason.');
  }
});

/**
 * ★ Every event the core writes is read by this client, or deliberately not.
 *
 * The mirror of the guard that checks the names we branch on exist. That one catches a branch that
 * can never fire; this one catches the opposite and more expensive mistake — the core writes a
 * fact, nothing here reads it, and the feature is simply absent with nothing saying so.
 *
 * Measured 2026-09-09 by counting the two ports' branches: the JetBrains shaper handled seventeen
 * events, this one eight. Three of the difference were read elsewhere here (`todos.changed`,
 * `context.usage`, `model.changed` have their own readers), and three were read NOWHERE:
 * `council.decided` — so the transcript showed three members voting and never what was decided —
 * and `interjection.deferred` / `interjection.answered`, so a prompt the core had parked drew
 * exactly like one being worked on.
 *
 * "Read" is deliberately the whole client, not this fold: several of these belong to the plan panel
 * or the setup line, and demanding they all be rows would push facts onto the wrong screen.
 */
test('every event the core writes is read somewhere, or deliberately not', () => {
  const core = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'internal', 'core', 'event', 'event.go'), 'utf8');
  const types = new Set([...core.matchAll(/Type[A-Za-z]+\s+Type\s*=\s*"([a-z][a-z.]+)"/g)].map((m) => m[1]));
  assert.ok(types.size >= 20, `only ${types.size} event types read from the core — the parser is stale`);

  // What this client reads, anywhere under src/ (excluding the tests, which name events to build
  // fixtures and would answer this question with themselves).
  const read = new Set<string>();
  const walk = (dir: string): void => {
    for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
      const p = path.join(dir, e.name);
      if (e.isDirectory()) { if (e.name !== 'test') walk(p); continue; }
      if (!e.name.endsWith('.ts')) continue;
      const body = fs.readFileSync(p, 'utf8')
        .replace(/\/\*(?:(?!\*\/)[\s\S])*?\*\//g, '').replace(/^[ \t]*\/\/.*$/gm, '');
      // ⚠ **A dotless name is an event too.** `compaction` and `error` have no dot, and a pattern
      // that required one reported both as unread — the same trap this repository already recorded
      // in the guard one over. Match anything the core's own list can contain.
      for (const m of body.matchAll(/'([a-z][a-z.]*)'/g)) if (types.has(m[1])) read.add(m[1]);
    }
  };
  walk(path.join(__dirname, '..', '..', 'src'));
  assert.ok(read.size >= 8, `only ${read.size} event names seen in the client — the scan is reading nothing`);
  // Pinned because the first cut of this scan could not see them: a dotless name read as no name.
  for (const dotless of ['compaction', 'error']) {
    assert.ok(read.has(dotless), `the scan cannot see "${dotless}" — a dotless event name is still an event`);
  }

  // Left out on purpose, each with the reason a reader can check.
  const skipped: Record<string, string> = {
    'permission.decided': 'not a row — it CLOSES the ask (pendingAsk and touched both read it); the decision itself shows as the ask disappearing',
    'question.answered': 'not a row — it CLOSES the ask (pendingAsk reads it); the answer arrives as the prompt it produced, and a second row would say the person spoke twice',
    // ⚠ These two had each other's reasons. `labels.changed` carries the SESSION's subject labels
    // (`LabelsChangedData.Labels`, "what the agent says this session's work is about", the whole set
    // each time); `user.label.changed` carries the person's display name. The note here described
    // renaming people and was filed under the labels key, and the second key then pointed at it
    // ("same as labels.changed") — so the only reason recorded for either was a statement about the
    // other. A reason that describes the wrong field vouches for a skip nobody has actually judged.
    'labels.changed': 'the session\'s own subject labels. Nothing here draws them: a conversation is '
      + 'named by its title in the list, and `SessionRow.labels` is unread for the same reason',
    // And this one is now exempted for the OPPOSITE reason to the one it used to carry: the client
    // does follow the person's name — `status.user` is read on every poll (Setup.user) — so replaying
    // the event would be a second source for a fact already arriving live. Same rule as
    // `model.changed` and `session.created` above.
    'user.label.changed': 'the person\'s display name, answered LIVE by `status` (Setup.user), so a '
      + 'window attaching mid-conversation gets the current name instead of replaying its history',
    'result.elided': 'a shed tool result is a context-window fact, not a conversation one — the row already says what the call was',
    'workflow.phase': 'this client draws no phase strip; the plan panel shows the todos the phase moves',
    'tool.progress': 'transient and bus-only, so it never reaches a reader of the log; the live note comes from `status.doing`',
    'council.deliberating': 'transient, one per member per round — kept out of the transcript for the reason the core keeps it out of the log',

    'session.created': 'the conversation\'s opening facts. The model on it is answered LIVE by `status` (Setup.model), and a transcript that began at a replayed session.created would name the model it started on rather than the one answering now',
    'model.changed': 'same source, same reason: `status.model` is read on every poll, so a window attaching mid-conversation gets the current model instead of replaying its history',
  };
  // ★ The exemptions that claim a fact arrives LIVE are checked against the code that reads it.
  // A reason is prose and prose does not fail — but THIS reason is a checkable claim: three keys are
  // skipped because `status` answers the same fact on every poll. If that read goes away the
  // exemption becomes a false statement, and nothing else in this file would notice.
  // The fields come from the REASONS, not from a list written here: a reason that says
  // "`status.model`" or "(Setup.user)" is making a claim about a specific read, and this finds every
  // such claim and checks it. Emptying a hand-written list was how the first cut of this went blind —
  // a mutation set it to `[]` and nothing failed.
  const setup = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'core', 'activity.ts'), 'utf8');
  const claimed = new Set<string>();
  for (const why of Object.values(skipped)) {
    for (const m of why.matchAll(/`status\.(\w+)`|\(Setup\.(\w+)\)/g)) claimed.add(m[1] ?? m[2]);
  }
  assert.ok(claimed.size >= 2,
    `only ${claimed.size} live-read claim(s) found in the reasons — the scan is dead`);
  // Read off the REPLY, wherever that lands: `model` and `user` go into Setup, `doing` into the
  // Activity itself. The claim is "this comes live off `status`", and `resp.<field>` is that claim in
  // code — a Setup-shaped check missed `doing` and said so, which is how this scan proved it reads
  // more than the two keys I had in mind.
  for (const field of claimed) {
    assert.ok(new RegExp(`resp\\.${field}\\b`).test(setup),
      `an event is skipped because \`status.${field}\` is read live, and nothing reads it`);
  }

  /**
   * ★ And the exemptions that claim a type is TRANSIENT are checked against the core's own map.
   *
   * Two reasons here rest on "bus-only, never persisted": if such a type were ever promoted to a
   * fact, the exemption would become a false statement and nothing else would notice. That is not
   * hypothetical in this repository — the core says `model.changed` "was declared as a transient for
   * most of its life and this const block is where that mistake was visible", and names what the
   * mistake cost: "three documents copied the header rather than the behaviour, and told outside
   * clients not to expect this line".
   *
   * Read from `transientTypes` rather than from a list here, for the reason the sibling check reads
   * `status.X` off the code: a copy of the answer drifts with the thing it is supposed to catch.
   */
  const trans = new Set<string>();
  {
    const map = /transientTypes = map\[Type\]bool\{([\s\S]*?)\n\}/.exec(core)?.[1] ?? '';
    assert.ok(map, 'the core\'s transient map was not found — this check is reading nothing');
    for (const m of map.matchAll(/(Type[A-Za-z]+):/g)) {
      const v = new RegExp(m[1] + '\\s+Type\\s*=\\s*"([a-z][\\w.]*)"').exec(core);
      if (v) trans.add(v[1]);
    }
    assert.ok(trans.size >= 5, `only ${trans.size} transient types resolved — the scan is broken`);
  }
  const saysTransient = Object.entries(skipped).filter(([, why]) => why.includes('transient'));
  assert.ok(saysTransient.length >= 2,
    `only ${saysTransient.length} transience claim(s) found in the reasons — the scan is dead`);
  for (const [t] of saysTransient) {
    assert.ok(trans.has(t),
      `${t} is skipped because it is "transient", and the core does not list it as one — the ` +
      'reason has aged into a false statement, which is how `model.changed` sat in the wrong block');
  }

  const missed: string[] = [];
  for (const t of types) if (!read.has(t) && !(t in skipped)) missed.push(t);
  assert.deepEqual(missed, [],
    'the core writes these and nothing here reads them, so the feature is absent with nothing ' +
    'saying so: ' + missed.join(', ') + ' — read them, or add each to `skipped` with its reason.');
});

/**
 * ★ No command id is registered twice.
 *
 * `vscode.commands.registerCommand` is documented in this repo's own typings as an error case:
 * *"Registering a command with an existing command identifier twice will cause an error."* The
 * registrations are built as one array inside `doorCommands()`, so the throw does not lose one
 * button — it aborts the array, `activate()` fails, and **every** command of this extension is
 * gone along with the views that activation wires up.
 *
 * Measured, not imagined: `magi.chooseBackend` was registered twice in `doors.ts` for a day. The
 * second copy came with the info card, whose button needed the command, and nothing noticed —
 * there is no vscode stub here, so no test ever calls `registerCommand`, and the one test that
 * reads command names collects them into a `Set`, where a duplicate disappears by construction.
 * A guard that dedupes cannot see doubling. This one counts.
 */
test('no command id is registered twice', () => {
  const dir = path.join(__dirname, '..', '..', 'src');
  const seen = new Map<string, string[]>();
  const walk = (d: string): void => {
    for (const e of fs.readdirSync(d, { withFileTypes: true })) {
      const p = path.join(d, e.name);
      if (e.isDirectory()) { if (e.name !== 'test') walk(p); continue; }
      if (!e.name.endsWith('.ts')) continue;
      const body = fs.readFileSync(p, 'utf8');
      // Both shapes: the `reg('id', …)` helper and a direct `registerCommand('id', …)`.
      for (const m of body.matchAll(/(?:registerCommand|\breg)\(\s*'(magi\.[A-Za-z]+)'/g)) {
        seen.set(m[1], [...(seen.get(m[1]) ?? []), path.relative(dir, p)]);
      }
    }
  };
  walk(dir);
  // ⚠ **The manifest is a list, and it doubled too.** The same wave that registered the command
  // twice also declared it twice in `contributes.commands`, so the Command Palette listed "magi:
  // Choose the backend" twice — and the first cut of this guard could not see it, because it read
  // the declarations only to look ids up. A `Set` was hiding one duplicate while the test next to
  // it was written about another.
  const declared = (manifest.contributes.commands ?? []).map((c) => c.command);
  const twice = declared.filter((id, i) => declared.indexOf(id) !== i);
  assert.deepEqual(twice, [], `declared more than once: ${twice.join(', ')} — the palette lists it twice`);

  // The floor is DERIVED, not a number somebody remembered: every command the manifest declares is
  // registered somewhere, so a scan that finds fewer has stopped reading a shape. The first cut of
  // this guard read only `registerCommand(`, found the fifteen written that way, cleared a floor of
  // ten — and was blind to all twelve in `doors.ts`, which is exactly where the doubling was.
  for (const id of declared) {
    assert.ok(seen.has(id), `${id} is declared and no registration was found — the scan is missing a shape`);
  }
  for (const [id, where] of seen) {
    assert.equal(where.length, 1,
      `${id} is registered ${where.length} times (${where.join(', ')}) — registering it twice throws, and the throw takes every other command with it`);
  }
});

/**
 * ★ The settings screen reads the two fields that say HOW to ask and WHETHER the answer will stick.
 *
 * `config-get` carries both and the core says why each is on the wire rather than known by each
 * client:
 *
 *  - `profile` marks a key whose value must NAME an `[llm.profiles.*]`. The core's own words:
 *    "Every client that hardcodes which keys are profile-shaped is a copy of a list that lives
 *    here." A free-text box takes any word, and a name that is not a profile is accepted and then
 *    does nothing — the setting reads as changed and the behaviour does not.
 *  - `source` is the layer the CURRENT value comes from, and it can be **"env"**. An environment
 *    variable beats the file this screen writes, so editing one that came from `env` changes
 *    nothing a person can see — the same silence the `unreadable` warning beside it exists to break.
 *
 * Measured 2026-09-10 against a running daemon: five settings came back, two set (`source: global`)
 * and three unset, and one of the two is `autocomplete.code_profile` — a profile-shaped key this
 * screen was asking for as free text. The JetBrains settings screen has drawn both since the fields
 * landed (`MagiConfigurable`: a combo when `item.profile`, and "from <source>" beside the value).
 *
 * ⚠ The screen lost them by RE-DECLARING the reply narrower than `protocol.ts` already types it —
 * a local cast that listed seven of the nine fields. Nothing failed: the two names simply were not
 * there to read. So this pins the shape it reads as the protocol's own.
 */
test('the settings screen asks the way the door says to ask', () => {
  const doors = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'doors.ts'), 'utf8');
  const at = doors.indexOf("call('config-get')");
  assert.ok(at > 0, 'the settings command was not found — this guard is reading nothing');
  const block = doors.slice(at, doors.indexOf("reg('magi.newConversation'", at));
  assert.ok(block.length > 500, 'the settings command body was not read — this guard is reading nothing');

  assert.match(block, /NonNullable<Response\['config'\]>/,
    're-declares the reply shape locally — a narrower copy silently drops fields protocol.ts types');
  // The DESCRIPTION line, not merely a mention of the field. `c.source` also appears where the
  // picked item is built, and a first cut of this assertion matched that instead — the mutation
  // that removed the source from what a person reads walked straight past it.
  const shown = block.slice(block.indexOf('description:'), block.indexOf('detail:'));
  assert.ok(shown.length > 20, 'the description line was not found — this guard is reading nothing');
  assert.match(shown, /c\.source/, 'the value is shown without saying which layer it came from');
  assert.match(block, /=== 'env'/,
    'a value from the environment is edited as if the file would win — it will not');
  assert.match(block, /c\.profile === true/,
    'a profile-shaped key is asked for as free text, so a name that is not a profile is accepted');
  // Asked for, then offered: the door is called and its answer becomes the picker. Matching them
  // in this order is the point — a `showQuickPick` with a list from anywhere else would be the
  // hardcoded copy the core says not to keep.
  const profileBranch = block.slice(block.indexOf('c.profile === true'));
  assert.match(profileBranch, /call\('profiles'\)[\s\S]{0,500}showQuickPick/,
    'the profile-shaped key is not offered the profiles the daemon actually has');
});

/**
 * ★ A door that refuses without a conversation named is asked with one.
 *
 * `answerContext` reads `req.Session` and answers "no session named" when it is empty. There is no
 * fallback to whatever is current, and the dispatcher never fills the field — the core's own client
 * passes `Session: sid` at every call site that needs it. This client asked bare, so the plan
 * panel's context section drew nothing on every poll, on every build, and said nothing about it
 * (`panel.context` returns '' on a refusal).
 *
 * Measured 2026-09-10 against a freshly built daemon in an isolated config: bare `context` answers
 * `ok:false, "no session named"`; the same call carrying the session answers with `model`, `window`,
 * `used` and `parts`. The JetBrains client has sent it since the door landed.
 *
 * The list of such doors is DERIVED, not written here: an answerer that reads `req.Session` and
 * refuses is the shape, and a second door growing that shape tomorrow must fail here rather than
 * join the first one in silence.
 */
test('every door that refuses without a session is called with one', () => {
  const doors = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'internal', 'adapter', 'daemon', 'doors.go'), 'utf8');
  const byFn = new Map<string, string>();
  for (const m of doors.matchAll(/"([a-z][a-z0-9-]*)":\s*\{[^}]*?run:\s*(answer[A-Za-z]+)/gs)) {
    byFn.set(m[2], m[1]);
  }
  assert.ok(byFn.size >= 15, `only ${byFn.size} doors mapped to answerers — the scan is broken`);

  const strict: string[] = [];
  for (const m of doors.matchAll(
    /func (answer[A-Za-z]+)\(ctx context\.Context, eng Engine, req Request\) Response \{([\s\S]*?)\n\}\n/g)) {
    if (/req\.Session/.test(m[2]) && /no session/i.test(m[2]) && byFn.has(m[1])) strict.push(byFn.get(m[1])!);
  }
  assert.ok(strict.length >= 1, 'no session-strict door found in the core — the scan is dead');

  const src = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'plan.ts'), 'utf8');
  for (const door of strict) {
    const at = src.indexOf(`ask('${door}'`);
    assert.ok(at > 0, `${door} is not asked from the plan panel — has it moved? this guard is reading nothing`);
    // The call, to its closing paren: the session has to be IN it, not merely nearby.
    const call = src.slice(at, src.indexOf(')', src.indexOf('{', at)));
    assert.match(call, /session:/,
      `${door} is asked without naming the conversation, and the core refuses that outright — ` +
      'the section draws nothing, on every poll, with nothing saying why');
  }
});
