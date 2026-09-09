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
    const body = fs.readFileSync(f, 'utf8')
      .replace(/\/\*[\s\S]*?\*\//g, '').replace(/^[ \t]*\/\/.*$/gm, '');
    // getConfiguration('magi') … .get<T>('key' …) — the two halves can sit apart, so the section
    // is checked per file rather than per expression.
    if (!/getConfiguration\(['"]magi['"]\)/.test(body)) continue;
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
  for (const f of walk(dir)) {
    const body = fs.readFileSync(f, 'utf8')
      .replace(/\/\*[\s\S]*?\*\//g, '').replace(/^[ \t]*\/\/.*$/gm, '');
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
