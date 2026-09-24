import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';
import { Hand } from '../core/mcpserver';
import { Ide, callHand, handTools, inside, replaceText, resolvePath, HAND_NAME } from '../core/hand';

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

/**
 * ★ An optional argument the description does not mention is one the model cannot use.
 *
 * A tool's description is the contract the model reads. A REQUIRED argument is unavoidable — the
 * schema forces it — but an optional one is invisible unless the words say it is there and what it
 * does. Two were invisible (measured 2026-09-10 by comparing the two clients' hands field by field):
 *
 * - `apply_edit.replaceAll` — and it is not a convenience. Both hands REFUSE an edit whose `old`
 *   appears more than once unless it is passed ("narrow it, or pass replaceAll"), so a model that
 *   does not know the flag learns of it only by failing first.
 * - `problems.path` — omitting it returns diagnostics for every file the editor knows. The
 *   JetBrains hand has said so since it was written; this one did not, and the whole point of the
 *   tool is to replace running a build and reading the words.
 */
test('every optional argument is explained in the tool that offers it', () => {
  const tools = handTools();
  assert.ok(tools.length >= 3, `only ${tools.length} tools — the hand is not where this guard looks`);

  for (const t of tools) {
    const schema = t.schema as { properties?: Record<string, unknown>; required?: string[] };
    const props = Object.keys(schema.properties ?? {});
    assert.ok(props.length > 0, `${t.name} offers no arguments — the scan is reading nothing`);
    const optional = props.filter((p) => !(schema.required ?? []).includes(p));
    for (const arg of optional) {
      assert.ok(t.description.includes(arg),
        `${t.name} takes an optional \`${arg}\` and never mentions it — the schema offers what the ` +
        'words hide, and a model only uses what it is told');
    }
  }

  // The two that were hidden, named outright so a reader sees the claim without re-deriving it.
  const edit = tools.find((t) => t.name === 'apply_edit')!;
  assert.match(edit.description, /replaceAll/, 'the flag that decides whether an ambiguous edit is refused');
  const problems = tools.find((t) => t.name === 'problems')!;
  assert.match(problems.description, /[Oo]mit path/, 'that omitting path asks for every file');
});

/**
 * ★ An empty `old` is refused before the editor sees it.
 *
 * The schema makes `old` required and an empty string satisfies required — `need` is what rejects
 * it, by treating an empty string as missing. Nothing said so, and the rule was ALSO written in the
 * editor layer where it could never run.
 *
 * It is pinned here because of what it prevents, measured on the JVM for the sibling client (which
 * had no check at all): an empty needle "matches" between every character, so a replaceAll with it
 * rewrites the file with the new text wedged between every character — and the tool reports that as
 * a success. A change to `need` that let an empty string through would open exactly that.
 */
test('an empty old never reaches the editor', async () => {
  const ide = new FakeIde();
  const r = await callHand(ide, 'apply_edit', { path: 'a.ts', old: '', new: 'X', replaceAll: true });
  assert.equal(r.error, true, `an empty old was not refused: ${r.text}`);
  assert.match(r.text, /old is required/);
  assert.equal(ide.replaced, null, 'an empty old reached the editor — that is where the file is shredded');
});

/** Empty, not blank: replacing four spaces with a tab is a real edit. */
test('a whitespace-only old is not refused', async () => {
  const ide = new FakeIde();
  const r = await callHand(ide, 'apply_edit', { path: 'a.ts', old: '    ', new: '\t', replaceAll: true });
  assert.ok(!r.error, `an indentation edit was refused: ${r.text}`);
  assert.deepEqual(ide.replaced, ['a.ts', '    ', '\t', true]);
});

/**
 * ★ The workspace is a trust boundary, and the editor hand is a door into it.
 *
 * The hand resolved whatever path the COMPANION named — absolute as given, relative against the
 * workspace — and opened it. So `show` could open any file on the machine, `apply_edit` could
 * rewrite one, and `problems` could read the diagnostics of one.
 *
 * The sibling client keeps this line and says so: its `find` resolves and then refuses anything not
 * under the project. The daemon confines the companion as well — and a confinement somebody else
 * enforces is not this window's. Two doors into one machine, opened by the same agent.
 */
test('a path outside the workspace is not inside it', () => {
  assert.equal(inside('/w', '/w/src/a.ts'), true);
  assert.equal(inside('/w', 'src/a.ts'), true);
  assert.equal(inside('/w', '.'), true, 'the workspace itself is inside itself');
  // Absolute, elsewhere.
  assert.equal(inside('/w', '/etc/hosts'), false);
  // Relative, walking out.
  assert.equal(inside('/w', '../secrets.env'), false);
  assert.equal(inside('/w', 'src/../../secrets.env'), false);
  assert.equal(inside('/w', '..'), false);
});

/**
 * Compared by path SEGMENTS, not string prefix.
 *
 * A sibling directory whose name merely begins with the workspace's is a different place, and a
 * `startsWith` check would call it inside. That is the classic way this kind of guard is written and
 * the classic way it leaks.
 */
test('a sibling whose name starts the same is not inside', () => {
  assert.equal(inside('/w', '/workspace-other/a.ts'), false);
  assert.equal(inside('/home/me/proj', '/home/me/project2/a.ts'), false);
});

/**
 * ★ A model that writes `"true"` means true, and the two editors must agree about that.
 *
 * This read `args.replaceAll === true`, so a JSON string went through as FALSE — silently. The edit
 * then changed one occurrence, and where `old` appears more than once this tool's own rule REFUSES
 * it, so a call that asked for every occurrence came back as a refusal with nothing saying why.
 *
 * Measured 2026-09-10 by driving the real server over its own HTTP: `true`→true, `"true"`→**false**,
 * `"True"`→false, `1`→false. The JetBrains hand compares the primitive's text, so the same call
 * worked there — one model, two editors, two outcomes.
 *
 * Tolerant in ONE direction: anything that is not the word true stays false. This flag decides
 * whether an edit touches one line or all of them, and guessing "yes" from a number would be the
 * expensive way to be wrong.
 */
test('replaceAll takes the word true in either shape, and nothing else', async () => {
  const ide = new FakeIde();
  const cases: [unknown, boolean][] = [
    [true, true], ['true', true], ['True', true], ['  true  ', true],
    [false, false], ['false', false], ['1', false], [1, false], [undefined, false], ['yes', false],
  ];
  for (const [sent, want] of cases) {
    const args: Record<string, unknown> = { path: 'a.ts', old: 'a', new: 'b' };
    if (sent !== undefined) args.replaceAll = sent;
    await callHand(ide, 'apply_edit', args);
    assert.equal(ide.replaced?.[3], want,
      `replaceAll ${JSON.stringify(sent)} reached the editor as ${ide.replaced?.[3]}, not ${want}`);
  }
});

/**
 * ★ And the sibling reads it the same way, so one model does not get two answers.
 *
 * Read off the Kotlin rather than restated here: the rule is "the primitive's text is the word
 * true", and a copy of that sentence in this language would drift with the thing it is meant to
 * match. If the sibling ever tightens to a JSON-only boolean, this says so instead of leaving the
 * two editors quietly different.
 */
test('the JetBrains hand reads the same flag the same way', () => {
  const kt = fs.readFileSync(path.join(
    __dirname, '..', '..', '..', 'jetbrains', 'plugin', 'core', 'src', 'main', 'kotlin',
    'dev', 'sayaya', 'magi', 'ide', 'usecase', 'Hand.kt'), 'utf8');
  const at = kt.indexOf('"apply_edit" ->');
  assert.ok(at > 0, 'the sibling apply_edit branch was not found — this guard is reading nothing');
  const block = kt.slice(at, kt.indexOf('"problems"', at));
  assert.match(block, /replaceAll[\s\S]{0,80}==\s*"true"/,
    'the sibling no longer takes the WORD true — the two editors now answer one model differently');
});

/**
 * Literal replacement must not interpret JavaScript replacement patterns ($&, $$, $`, $', $n).
 * Single replacement and replaceAll must preserve the exact text verbatim.
 */
test('literal replacement preserves $, backslashes, Korean, and newlines verbatim', () => {
  const cases = [
    ['$&', 'before $& after'],
    ['$$', 'before $$ after'],
    ['$`', 'before $` after'],
    ["$'", "before $' after"],
    ['$1 and $2', 'before $1 and $2 after'],
    ['\\path\\to\\file.ts', 'before \\path\\to\\file.ts after'],
    ['한글 치환 및 특수기호 $& $$', 'before 한글 치환 및 특수기호 $& $$ after'],
    ['line1\nline2\n\tline3', 'before line1\nline2\n\tline3 after'],
  ];

  for (const [replacement, expected] of cases) {
    // Single replacement
    const single = replaceText('before TOKEN after', 'TOKEN', replacement, false);
    assert.equal(single, expected, `single replacement of TOKEN with ${replacement} failed`);

    // All replacement
    const all = replaceText('before TOKEN after', 'TOKEN', replacement, true);
    assert.equal(all, expected, `all replacement of TOKEN with ${replacement} failed`);
  }

  // Single replacement touches only the first occurrence
  assert.equal(
    replaceText('TOKEN and TOKEN', 'TOKEN', '$&', false),
    '$& and TOKEN'
  );

  // All replacement replaces all occurrences with literal string
  assert.equal(
    replaceText('TOKEN and TOKEN', 'TOKEN', '$&', true),
    '$& and $&'
  );

  // When old is not found, body is returned unchanged
  assert.equal(replaceText('hello world', 'missing', '$&', false), 'hello world');
  assert.equal(replaceText('hello world', 'missing', '$&', true), 'hello world');
});

/**
 * Verify EditorHand adapter wiring: EditorHand.replace must use replaceText rather than body.replace(old, text).
 * And EditorHand.resolve must use resolvePath.
 */
test('EditorHand adapter uses replaceText and resolvePath', () => {
  const ideHandSrc = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'hand.ts'), 'utf8');
  assert.match(ideHandSrc, /replaceText\s*\(\s*body\s*,\s*old\s*,\s*text\s*,\s*all\s*\)/,
    'EditorHand.replace must use replaceText helper to avoid JavaScript replacement patterns');
  assert.ok(!/body\.replace\s*\(\s*old\s*,\s*text\s*\)/.test(ideHandSrc),
    'EditorHand.replace still uses body.replace(old, text) — JavaScript replacement syntax is interpreted');
  assert.match(ideHandSrc, /resolvePath\s*\(\s*this\.workdir\s*,\s*path\s*\)/,
    'EditorHand.resolve must use resolvePath helper');
  assert.ok(!/path\.startsWith\s*\(\s*'\/'\s*\)/.test(ideHandSrc),
    'EditorHand.resolve still uses path.startsWith("/") — Windows absolute paths will be mangled');
});

/**
 * Path resolution under POSIX rules.
 */
test('POSIX path resolution handles relative and absolute paths within workspace', () => {
  const posix = path.posix;
  const workdir = '/workspace';

  // Relative path
  assert.equal(resolvePath(workdir, 'src/main.ts', posix), '/workspace/src/main.ts');
  assert.equal(resolvePath(workdir, './src/main.ts', posix), '/workspace/src/main.ts');

  // Absolute path within workspace
  assert.equal(resolvePath(workdir, '/workspace/src/main.ts', posix), '/workspace/src/main.ts');

  // Korean and spaces
  assert.equal(resolvePath(workdir, '문서/설정 파일.json', posix), '/workspace/문서/설정 파일.json');
  assert.equal(resolvePath(workdir, '/workspace/문서/설정 파일.json', posix), '/workspace/문서/설정 파일.json');

  // Traversal out is rejected
  assert.throws(() => resolvePath(workdir, '../secret.env', posix), /is outside this workspace/);
  assert.throws(() => resolvePath(workdir, 'src/../../secret.env', posix), /is outside this workspace/);

  // External absolute path is rejected
  assert.throws(() => resolvePath(workdir, '/etc/hosts', posix), /is outside this workspace/);

  // Sibling with common prefix is rejected
  assert.throws(() => resolvePath(workdir, '/workspace-other/main.ts', posix), /is outside this workspace/);
});

/**
 * Path resolution under Windows rules (path.win32).
 */
test('Windows path resolution handles drive absolute, relative, UNC, spaces, and Korean', () => {
  const win32 = path.win32;
  const workdir = 'C:\\work';

  // Relative path with backslashes
  assert.equal(resolvePath(workdir, 'src\\main.ts', win32), 'C:\\work\\src\\main.ts');
  assert.equal(resolvePath(workdir, '.\\src\\main.ts', win32), 'C:\\work\\src\\main.ts');

  // Relative path with forward slashes (common from models)
  assert.equal(resolvePath(workdir, 'src/main.ts', win32), 'C:\\work\\src\\main.ts');

  // Windows drive absolute path
  assert.equal(resolvePath(workdir, 'C:\\work\\src\\main.ts', win32), 'C:\\work\\src\\main.ts');
  assert.equal(resolvePath(workdir, 'C:/work/src/main.ts', win32), 'C:\\work\\src\\main.ts');

  // Spaces and Korean
  assert.equal(resolvePath(workdir, '작업 문서\\테스트 파일.ts', win32), 'C:\\work\\작업 문서\\테스트 파일.ts');
  assert.equal(resolvePath(workdir, 'C:\\work\\작업 문서\\테스트 파일.ts', win32), 'C:\\work\\작업 문서\\테스트 파일.ts');

  // UNC paths
  const uncWorkdir = '\\\\server\\share\\work';
  assert.equal(resolvePath(uncWorkdir, 'sub\\main.ts', win32), '\\\\server\\share\\work\\sub\\main.ts');
  assert.equal(resolvePath(uncWorkdir, '\\\\server\\share\\work\\sub\\main.ts', win32), '\\\\server\\share\\work\\sub\\main.ts');

  // Traversal out is rejected
  assert.throws(() => resolvePath(workdir, '..\\secret.env', win32), /is outside this workspace/);
  assert.throws(() => resolvePath(workdir, 'src\\..\\..\\secret.env', win32), /is outside this workspace/);

  // Different drive is rejected
  assert.throws(() => resolvePath(workdir, 'D:\\other\\file.ts', win32), /is outside this workspace/);

  // System folder is rejected
  assert.throws(() => resolvePath(workdir, 'C:\\Windows\\system32', win32), /is outside this workspace/);

  // UNC outside is rejected
  assert.throws(() => resolvePath(uncWorkdir, '\\\\other-server\\share\\file.ts', win32), /is outside this workspace/);

  // Sibling with common prefix is rejected
  assert.throws(() => resolvePath(workdir, 'C:\\work-other\\main.ts', win32), /is outside this workspace/);
});

const CATALOGUE_FIXTURE_PATH = path.resolve(__dirname, '../../../test-fixtures/ide_hand_catalogue.json');

function normalizeSchema(schema: unknown, isRequiredArray = false): unknown {
  if (Array.isArray(schema)) {
    if (isRequiredArray) {
      return [...schema].sort();
    }
    return schema.map((item) => normalizeSchema(item, false));
  }
  if (schema !== null && typeof schema === 'object') {
    const sortedEntries = Object.entries(schema)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([k, v]) => [k, normalizeSchema(v, k === 'required')]);
    return Object.fromEntries(sortedEntries);
  }
  return schema;
}

interface CatalogueTool {
  name: string;
  readOnly: boolean;
  schema: unknown;
}

function normalizeToolDefinitions(tools: { name: string; readOnly: unknown; schema: unknown }[]): CatalogueTool[] {
  return [...tools]
    .sort((a, b) => a.name.localeCompare(b.name))
    .map((t) => {
      assert.equal(typeof t.readOnly, 'boolean', `tool ${t.name} must have boolean readOnly`);
      return {
        name: t.name,
        readOnly: t.readOnly as boolean,
        schema: normalizeSchema(t.schema),
      };
    });
}

function parseHttpTools(rawTools: unknown[]): { name: string; readOnly: boolean; schema: unknown }[] {
  return rawTools.map((raw) => {
    const t = raw as { name: string; inputSchema: unknown; annotations?: { readOnlyHint?: unknown } };
    assert.ok(
      t.annotations && typeof t.annotations.readOnlyHint === 'boolean',
      `tool ${t.name} must declare boolean annotations.readOnlyHint`
    );
    return {
      name: t.name,
      readOnly: t.annotations.readOnlyHint as boolean,
      schema: t.inputSchema,
    };
  });
}

test('§6.44.2/§6.44.7 Shared catalogue fixture ide_hand_catalogue.json matches handTools() losslessly', () => {
  const raw = fs.readFileSync(CATALOGUE_FIXTURE_PATH, 'utf-8');
  const expected = JSON.parse(raw) as CatalogueTool[];
  const actual = handTools();
  assert.deepEqual(normalizeToolDefinitions(actual), normalizeToolDefinitions(expected));
});

test('§6.44.2/§6.44.7 HTTP tools/list preserves inputSchema and annotations.readOnlyHint against catalogue fixture', async () => {
  const raw = fs.readFileSync(CATALOGUE_FIXTURE_PATH, 'utf-8');
  const expected = JSON.parse(raw) as CatalogueTool[];
  const hand = await Hand.start(new FakeIde());
  try {
    const list = (await rpc(hand, 'tools/list')).body.result as {
      tools: unknown[];
    };
    const actual = parseHttpTools(list.tools);
    assert.deepEqual(normalizeToolDefinitions(actual), normalizeToolDefinitions(expected));
  } finally {
    hand.close();
  }
});

test('§6.44.2/§6.44.7 Catalogue comparison fails on mutations (missing, extra prop, wrong req, inverted ro, additionalProps, enum, minimum, missing ro)', () => {
  const raw = fs.readFileSync(CATALOGUE_FIXTURE_PATH, 'utf-8');
  const expected = JSON.parse(raw) as CatalogueTool[];

  // 1. Missing tool
  const missing = expected.filter((t) => t.name !== 'show');
  assert.throws(() => assert.deepEqual(normalizeToolDefinitions(missing), normalizeToolDefinitions(expected)));

  // 2. Inverted readOnly
  const inverted = expected.map((t) => (t.name === 'apply_edit' ? { ...t, readOnly: true } : t));
  assert.throws(() => assert.deepEqual(normalizeToolDefinitions(inverted), normalizeToolDefinitions(expected)));

  // 3. Wrong required
  const wrongReq = expected.map((t) =>
    t.name === 'apply_edit'
      ? { ...t, schema: { ...(t.schema as object), required: ['path', 'old'] } }
      : t
  );
  assert.throws(() => assert.deepEqual(normalizeToolDefinitions(wrongReq), normalizeToolDefinitions(expected)));

  // 4. Extra property
  const extraProp = expected.map((t) => {
    if (t.name !== 'show') return t;
    const s = t.schema as { properties: Record<string, unknown> };
    return {
      ...t,
      schema: { ...s, properties: { ...s.properties, extra: { type: 'string' } } },
    };
  });
  assert.throws(() => assert.deepEqual(normalizeToolDefinitions(extraProp), normalizeToolDefinitions(expected)));

  // 5. schema.additionalProperties = false 추가 (§6.44.7)
  const extraAdditionalProps = expected.map((t) =>
    t.name === 'show' ? { ...t, schema: { ...(t.schema as object), additionalProperties: false } } : t
  );
  assert.throws(() => assert.deepEqual(normalizeToolDefinitions(extraAdditionalProps), normalizeToolDefinitions(expected)));

  // 6. path.enum 추가 (§6.44.7)
  const pathEnum = expected.map((t) => {
    if (t.name !== 'show') return t;
    const s = t.schema as { properties: Record<string, unknown> };
    return {
      ...t,
      schema: {
        ...s,
        properties: {
          ...s.properties,
          path: { ...(s.properties.path as object), enum: ['a.ts', 'b.ts'] },
        },
      },
    };
  });
  assert.throws(() => assert.deepEqual(normalizeToolDefinitions(pathEnum), normalizeToolDefinitions(expected)));

  // 7. line.minimum 추가 (§6.44.7)
  const lineMinimum = expected.map((t) => {
    if (t.name !== 'show') return t;
    const s = t.schema as { properties: Record<string, unknown> };
    return {
      ...t,
      schema: {
        ...s,
        properties: {
          ...s.properties,
          line: { ...(s.properties.line as object), minimum: 1 },
        },
      },
    };
  });
  assert.throws(() => assert.deepEqual(normalizeToolDefinitions(lineMinimum), normalizeToolDefinitions(expected)));

  // 8. apply_edit readOnly / readOnlyHint 누락 시 실패 검증 (§6.44.7, §6.44.10)
  const missingReadOnly = expected.map((t) =>
    t.name === 'apply_edit' ? { ...t, readOnly: undefined as unknown as boolean } : t
  );
  assert.throws(() => normalizeToolDefinitions(missingReadOnly));

  // HTTP annotations.readOnlyHint 누락 시 실패 검증 (parseHttpTools 공통 함수 사용)
  const httpMissingHint = expected.map((t) => {
    if (t.name !== 'apply_edit') {
      return { name: t.name, inputSchema: t.schema, annotations: { readOnlyHint: t.readOnly } };
    }
    return { name: t.name, inputSchema: t.schema, annotations: {} };
  });
  assert.throws(() => parseHttpTools(httpMissingHint));

  // §6.44.10 readOnly 및 readOnlyHint boolean 타입 변이 검증 (문자열 "true"/"false", 숫자 0/1, null, 누락)
  for (const invalid of ['true', 'false', 0, 1, null, undefined]) {
    // 1) catalogue / handTools readOnly
    const invalidReadOnly = expected.map((t) =>
      t.name === 'apply_edit' ? { ...t, readOnly: invalid as unknown as boolean } : t
    );
    assert.throws(() => normalizeToolDefinitions(invalidReadOnly));

    // 2) HTTP annotations.readOnlyHint
    const invalidHttpHint = expected.map((t) => {
      if (t.name !== 'apply_edit') {
        return { name: t.name, inputSchema: t.schema, annotations: { readOnlyHint: t.readOnly } };
      }
      return { name: t.name, inputSchema: t.schema, annotations: { readOnlyHint: invalid } };
    });
    assert.throws(() => parseHttpTools(invalidHttpHint));
  }

  // true / false 는 정상 통과
  for (const validBool of [true, false]) {
    const validReadOnly = expected.map((t) =>
      t.name === 'apply_edit' ? { ...t, readOnly: validBool } : t
    );
    assert.doesNotThrow(() => normalizeToolDefinitions(validReadOnly));

    const validHttpHint = expected.map((t) => {
      if (t.name !== 'apply_edit') {
        return { name: t.name, inputSchema: t.schema, annotations: { readOnlyHint: t.readOnly } };
      }
      return { name: t.name, inputSchema: t.schema, annotations: { readOnlyHint: validBool } };
    });
    assert.doesNotThrow(() => parseHttpTools(validHttpHint));
  }
});


