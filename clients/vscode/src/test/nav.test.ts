import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import {
  extractFileNav,
  extractAskFilePath,
  parsePositiveInteger,
  resolveAndOpenFile,
  FileOpener,
} from '../core/nav';
import { AskStore } from '../core/diff';
import { Event } from '../core/protocol';
import { rows } from '../core/transcript';

test('extractFileNav accepts verified IDE companion tools and rejects arbitrary MCP servers', () => {
  // Verified IDE companion tools
  assert.deepEqual(extractFileNav('mcp__vscode__show', { path: 'src/app.ts', line: '50' }), {
    path: 'src/app.ts',
    line: 50,
  });
  assert.deepEqual(extractFileNav('mcp__vscode__apply_edit', { path: 'src/app.ts' }), {
    path: 'src/app.ts',
  });
  assert.deepEqual(extractFileNav('mcp__jetbrains__show', { path: 'src/app.ts', line: 20 }), {
    path: 'src/app.ts',
    line: 20,
  });
  assert.deepEqual(extractFileNav('mcp__jetbrains__apply_edit', { path: 'src/app.ts' }), {
    path: 'src/app.ts',
  });

  // Arbitrary or unknown MCP servers must NEVER be assumed to handle local files
  assert.equal(extractFileNav('mcp__unknown__read', { path: 'src/app.ts', offset: 10 }), undefined);
  assert.equal(extractFileNav('mcp__github__read', { path: 'src/app.ts' }), undefined);
  assert.equal(extractFileNav('mcp__custom__apply_edit', { path: 'src/app.ts' }), undefined);
  assert.equal(extractFileNav('mcp__remote__edit', { path: 'src/app.ts', at: 10 }), undefined);
});

test('parsePositiveInteger handles numbers and strings', () => {
  assert.equal(parsePositiveInteger(1), 1);
  assert.equal(parsePositiveInteger(42), 42);
  assert.equal(parsePositiveInteger('10'), 10);
  assert.equal(parsePositiveInteger(' 99 '), 99);
  assert.equal(parsePositiveInteger(0), undefined);
  assert.equal(parsePositiveInteger(-5), undefined);
  assert.equal(parsePositiveInteger(3.14), undefined);
  assert.equal(parsePositiveInteger('0'), undefined);
  assert.equal(parsePositiveInteger('-1'), undefined);
  assert.equal(parsePositiveInteger('abc'), undefined);
  assert.equal(parsePositiveInteger(null), undefined);
  assert.equal(parsePositiveInteger(undefined), undefined);
});

test('extractFileNav for read tool', () => {
  // With line offset
  assert.deepEqual(extractFileNav('read', { path: 'src/main.ts', offset: 25 }), {
    path: 'src/main.ts',
    line: 25,
  });
  // With string offset
  assert.deepEqual(extractFileNav('read', JSON.stringify({ path: 'docs/가이드.md', offset: '42' })), {
    path: 'docs/가이드.md',
    line: 42,
  });
  // Without offset
  assert.deepEqual(extractFileNav('read', { path: 'src/my folder/my file.txt' }), {
    path: 'src/my folder/my file.txt',
  });
  // With 0 or invalid offset
  assert.deepEqual(extractFileNav('read', { path: 'README.md', offset: 0 }), {
    path: 'README.md',
  });
});

test('extractFileNav for edit tool', () => {
  // Anchored edit with at
  assert.deepEqual(extractFileNav('edit', { path: 'src/index.ts', at: '15', new: 'console.log();' }), {
    path: 'src/index.ts',
    line: 15,
  });
  // Edit with numeric at
  assert.deepEqual(extractFileNav('edit', { path: 'src/index.ts', at: 15 }), {
    path: 'src/index.ts',
    line: 15,
  });
  // Unanchored edit without at
  assert.deepEqual(extractFileNav('edit', { path: 'src/index.ts', old: 'a', new: 'b' }), {
    path: 'src/index.ts',
  });
});

test('extractFileNav for write and multiedit tools', () => {
  assert.deepEqual(extractFileNav('write', { path: 'dist/bundle.js', content: '// code' }), {
    path: 'dist/bundle.js',
  });
  assert.deepEqual(extractFileNav('multiedit', { path: 'C:\\projects\\app\\file.go', edits: [] }), {
    path: 'C:\\projects\\app\\file.go',
  });
  assert.deepEqual(extractFileNav('apply_edit', { path: 'src/file.ts', old: '1', new: '2' }), {
    path: 'src/file.ts',
  });
});

test('extractFileNav for show tool', () => {
  assert.deepEqual(extractFileNav('show', { path: 'src/app.ts', line: 100 }), {
    path: 'src/app.ts',
    line: 100,
  });
  assert.deepEqual(extractFileNav('mcp__vscode__show', { path: 'src/app.ts', line: '50' }), {
    path: 'src/app.ts',
    line: 50,
  });
  assert.deepEqual(extractFileNav('show', { path: 'src/app.ts' }), {
    path: 'src/app.ts',
  });
});

test('extractFileNav rejects non-file tools', () => {
  // list has path, but it is a directory
  assert.equal(extractFileNav('list', { path: 'src/' }), undefined);
  // glob has pattern
  assert.equal(extractFileNav('glob', { pattern: '**/*.ts' }), undefined);
  // bash has command
  assert.equal(extractFileNav('bash', { command: 'cat src/main.ts' }), undefined);
  // grep has query and path
  assert.equal(extractFileNav('grep', { query: 'foo', path: 'src/' }), undefined);
  // problems has optional path
  assert.equal(extractFileNav('problems', { path: 'src/main.ts' }), undefined);
  // unknown tool
  assert.equal(extractFileNav('some_tool', { path: 'src/main.ts' }), undefined);
});

test('extractFileNav handles edge cases', () => {
  assert.equal(extractFileNav('read', null), undefined);
  assert.equal(extractFileNav('read', undefined), undefined);
  assert.equal(extractFileNav('read', 'invalid json'), undefined);
  assert.equal(extractFileNav('read', { path: '' }), undefined);
  assert.equal(extractFileNav('read', { path: '   ' }), undefined);
  assert.equal(extractFileNav('read', { path: 123 }), undefined);
});

test('extractAskFilePath extracts path for file tools and rejects others', () => {
  assert.equal(extractAskFilePath({ what: 'write', args: JSON.stringify({ path: 'src/foo.ts' }) }), 'src/foo.ts');
  assert.equal(extractAskFilePath({ what: 'edit', args: { path: 'docs/가이드.md', old: 'x', new: 'y' } }), 'docs/가이드.md');
  assert.equal(extractAskFilePath({ what: 'bash', args: { command: 'echo hello' } }), undefined);
  assert.equal(extractAskFilePath({ what: 'list', args: { path: 'src/' } }), undefined);
  assert.equal(extractAskFilePath({ what: 'glob', args: { pattern: '*.ts' } }), undefined);
});

class FakeOpener implements FileOpener {
  calls: { path: string; line?: number }[] = [];
  mockLineCount: number = 50;

  async openDocument(absPath: string, line?: number): Promise<{ opened: boolean; line?: number }> {
    let actualLine = line;
    if (line !== undefined && line > this.mockLineCount) {
      actualLine = this.mockLineCount;
    }
    this.calls.push({ path: absPath, line: actualLine });
    return { opened: true, line: actualLine };
  }
}

function toolEvent(seq: number, name: string, args: Record<string, unknown>): Event {
  return {
    seq,
    type: 'part.appended',
    data: {
      part: {
        kind: 'tool-call',
        toolCall: {
          callId: `call-${seq}`,
          name,
          args,
        },
      },
    },
  };
}

test('adapter test: tool row click navigates to file and line in workspace', async () => {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-test-nav-'));
  try {
    const srcDir = path.join(tmp, 'src');
    fs.mkdirSync(srcDir);
    const mainFile = path.join(srcDir, 'main.ts');
    fs.writeFileSync(mainFile, 'line 1\nline 2\nline 3\n');

    const opener = new FakeOpener();
    const notes: string[] = [];
    const asks = new AskStore(10);
    const events: Event[] = [toolEvent(1, 'read', { path: 'src/main.ts', offset: 2 })];

    const ok = await resolveAndOpenFile({
      m: { seq: 1 },
      session: 'sess-1',
      companionWorkdir: tmp,
      asks,
      events,
      postNote: (t) => notes.push(t),
      opener,
    });

    assert.equal(ok, true);
    assert.equal(opener.calls.length, 1);
    assert.equal(opener.calls[0].path, mainFile);
    assert.equal(opener.calls[0].line, 2);
    assert.equal(notes.length, 0);
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
});

test('adapter test: approval card click opens current workspace file', async () => {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-test-nav-'));
  try {
    const koreanDir = path.join(tmp, '한글 경로');
    fs.mkdirSync(koreanDir);
    const docFile = path.join(koreanDir, '문서.md');
    fs.writeFileSync(docFile, '# 한글 문서\n내용입니다.');

    const opener = new FakeOpener();
    const notes: string[] = [];
    const asks = new AskStore(10);
    asks.record(
      {
        callId: 'perm-1',
        kind: 'permission',
        what: 'write',
        args: JSON.stringify({ path: '한글 경로/문서.md', content: 'new content' }),
      },
      tmp,
      'sess-1',
    );

    const ok = await resolveAndOpenFile({
      m: { callId: 'perm-1' },
      session: 'sess-1',
      companionWorkdir: tmp,
      asks,
      events: [],
      postNote: (t) => notes.push(t),
      opener,
    });

    assert.equal(ok, true);
    assert.equal(opener.calls.length, 1);
    assert.equal(opener.calls[0].path, docFile);
    assert.equal(opener.calls[0].line, undefined);
    assert.equal(notes.length, 0);
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
});

test('adapter test: non-existent file reports error and does not create file', async () => {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-test-nav-'));
  try {
    const missingFile = path.join(tmp, 'missing.ts');
    assert.equal(fs.existsSync(missingFile), false);

    const opener = new FakeOpener();
    const notes: string[] = [];
    const asks = new AskStore(10);
    const events: Event[] = [toolEvent(1, 'read', { path: 'missing.ts' })];

    const ok = await resolveAndOpenFile({
      m: { seq: 1 },
      session: 'sess-1',
      companionWorkdir: tmp,
      asks,
      events,
      postNote: (t) => notes.push(t),
      opener,
    });

    assert.equal(ok, false);
    assert.equal(opener.calls.length, 0);
    assert.equal(fs.existsSync(missingFile), false); // must NOT create file
    assert.ok(notes.some((n) => n.includes('존재하지 않습니다')));
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
});

test('adapter test: directory path is rejected', async () => {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-test-nav-'));
  try {
    const subDir = path.join(tmp, 'subdir');
    fs.mkdirSync(subDir);

    const opener = new FakeOpener();
    const notes: string[] = [];
    const asks = new AskStore(10);
    const events: Event[] = [toolEvent(1, 'write', { path: 'subdir' })];

    const ok = await resolveAndOpenFile({
      m: { seq: 1 },
      session: 'sess-1',
      companionWorkdir: tmp,
      asks,
      events,
      postNote: (t) => notes.push(t),
      opener,
    });

    assert.equal(ok, false);
    assert.equal(opener.calls.length, 0);
    assert.ok(notes.some((n) => n.includes('디렉터리')));
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
});

test('adapter test: out-of-workspace boundary path is rejected', async () => {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-test-nav-'));
  try {
    const opener = new FakeOpener();
    const notes: string[] = [];
    const asks = new AskStore(10);
    const events: Event[] = [toolEvent(1, 'read', { path: '../outside.ts' })];

    const ok = await resolveAndOpenFile({
      m: { seq: 1 },
      session: 'sess-1',
      companionWorkdir: tmp,
      asks,
      events,
      postNote: (t) => notes.push(t),
      opener,
    });

    assert.equal(ok, false);
    assert.equal(opener.calls.length, 0);
    assert.ok(notes.some((n) => n.includes('외부 경로')));
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
});

test('adapter test: stale click from previous session is rejected', async () => {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-test-nav-'));
  try {
    const file = path.join(tmp, 'file.ts');
    fs.writeFileSync(file, 'code');

    const opener = new FakeOpener();
    const notes: string[] = [];
    const asks = new AskStore(10);
    // Recorded in session sess-old
    asks.record(
      {
        callId: 'perm-old',
        kind: 'permission',
        what: 'edit',
        args: JSON.stringify({ path: 'file.ts', old: 'c', new: 'd' }),
      },
      tmp,
      'sess-old',
    );

    // Current session is sess-new
    const ok = await resolveAndOpenFile({
      m: { callId: 'perm-old' },
      session: 'sess-new',
      companionWorkdir: tmp,
      asks,
      events: [],
      postNote: (t) => notes.push(t),
      opener,
    });

    assert.equal(ok, false);
    assert.equal(opener.calls.length, 0);
    assert.ok(notes.some((n) => n.includes('이전 세션')));
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
});

test('adapter test: remote companion reports unsupported reason', async () => {
  const opener = new FakeOpener();
  const notes: string[] = [];
  const asks = new AskStore(10);

  const ok = await resolveAndOpenFile({
    m: { seq: 1 },
    session: 'sess-1',
    companionWorkdir: '/workspace',
    companionState: 'remote',
    asks,
    events: [],
    postNote: (t) => notes.push(t),
    opener,
  });

  assert.equal(ok, false);
  assert.equal(opener.calls.length, 0);
  assert.ok(notes.some((n) => n.includes('원격 컴패니언')));
});

test('adapter test: line clamping beyond file length reports adjusted position', async () => {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-test-nav-'));
  try {
    const file = path.join(tmp, 'app.ts');
    fs.writeFileSync(file, 'line 1\nline 2\n');

    const opener = new FakeOpener();
    opener.mockLineCount = 2; // file has 2 lines
    const notes: string[] = [];
    const asks = new AskStore(10);
    const events: Event[] = [toolEvent(1, 'read', { path: 'app.ts', offset: 99 })];

    const ok = await resolveAndOpenFile({
      m: { seq: 1 },
      session: 'sess-1',
      companionWorkdir: tmp,
      asks,
      events,
      postNote: (t) => notes.push(t),
      opener,
    });

    assert.equal(ok, true);
    assert.equal(opener.calls.length, 1);
    assert.equal(opener.calls[0].line, 2); // clamped to 2
    assert.ok(notes.some((n) => n.includes('마지막 줄(2행)로 이동했습니다')));
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
});

test('adapter test: Windows drive path rules with path.win32', async () => {
  const win32 = path.win32;
  const workdir = 'C:\\workspace\\project';
  const opener = new FakeOpener();
  const notes: string[] = [];
  const asks = new AskStore(10);
  const events: Event[] = [toolEvent(1, 'edit', { path: 'src\\components\\view.tsx', at: 10 })];

  const fakeFs = {
    existsSync(p: string) {
      return p === 'C:\\workspace\\project\\src\\components\\view.tsx';
    },
    statSync(_p: string) {
      return { isDirectory: () => false };
    },
  };

  const ok = await resolveAndOpenFile({
    m: { seq: 1 },
    session: 'sess-1',
    companionWorkdir: workdir,
    asks,
    events,
    postNote: (t) => notes.push(t),
    opener,
    pathLib: win32,
    fsLib: fakeFs,
  });

  assert.equal(ok, true);
  assert.equal(opener.calls.length, 1);
  assert.equal(opener.calls[0].path, 'C:\\workspace\\project\\src\\components\\view.tsx');
  assert.equal(opener.calls[0].line, 10);
});

test('adapter test: multi-workspace isolation uses origin companion workdir', async () => {
  const tmpA = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-test-wsA-'));
  const tmpB = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-test-wsB-'));
  try {
    const fileA = path.join(tmpA, 'file.ts');
    fs.writeFileSync(fileA, 'content A');
    const fileB = path.join(tmpB, 'file.ts');
    fs.writeFileSync(fileB, 'content B');

    const opener = new FakeOpener();
    const notes: string[] = [];
    const asks = new AskStore(10);
    // Ask originated in workspace A
    asks.record(
      {
        callId: 'perm-wsA',
        kind: 'permission',
        what: 'write',
        args: JSON.stringify({ path: 'file.ts', content: 'A' }),
      },
      tmpA,
      'sess-common',
    );

    // Current companion is on workspace B
    const ok = await resolveAndOpenFile({
      m: { callId: 'perm-wsA' },
      session: 'sess-common',
      companionWorkdir: tmpB,
      asks,
      events: [],
      postNote: (t) => notes.push(t),
      opener,
    });

    assert.equal(ok, true);
    assert.equal(opener.calls.length, 1);
    // Verified resolved to workspace A, not workspace B!
    assert.equal(opener.calls[0].path, fileA);
  } finally {
    fs.rmSync(tmpA, { recursive: true, force: true });
    fs.rmSync(tmpB, { recursive: true, force: true });
  }
});

test('adapter test: late tool row click from session A does not open session B file even with identical seq', async () => {
  const tmpA = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-test-sessA-'));
  const tmpB = fs.mkdtempSync(path.join(os.tmpdir(), 'magi-test-sessB-'));
  try {
    const fileA = path.join(tmpA, 'file_a.ts');
    fs.writeFileSync(fileA, 'content A');
    const fileB = path.join(tmpB, 'file_b.ts');
    fs.writeFileSync(fileB, 'content B');

    // Both sessions have a tool call at seq: 1 with different files and callIds
    const eventsB: Event[] = [
      toolEvent(1, 'read', { path: 'file_b.ts' }),
    ];

    const opener = new FakeOpener();
    const notes: string[] = [];
    const asks = new AskStore(10);

    // Case 1: Late click from session A carrying session identifier 'sess-A'
    const ok1 = await resolveAndOpenFile({
      m: { seq: 1, session: 'sess-A', callId: 'call-1' },
      session: 'sess-B',
      companionWorkdir: tmpB,
      asks,
      events: eventsB,
      postNote: (t) => notes.push(t),
      opener,
    });

    assert.equal(ok1, false, 'late click from session A must be rejected on session B');
    assert.equal(opener.calls.length, 0, 'session B file must NOT be opened');
    assert.ok(notes.some((n) => n.includes('이전 세션')), 'note explains previous session rejection');

    // Case 2: Late click with mismatched callId from session A
    notes.length = 0;
    const ok2 = await resolveAndOpenFile({
      m: { seq: 1, callId: 'call-different-from-session-A' },
      session: 'sess-B',
      companionWorkdir: tmpB,
      asks,
      events: eventsB,
      postNote: (t) => notes.push(t),
      opener,
    });

    assert.equal(ok2, false, 'click with mismatched callId must be rejected');
    assert.equal(opener.calls.length, 0, 'session B file must NOT be opened');
    assert.ok(notes.some((n) => n.includes('이전 세션')), 'note explains tool request rejection');
  } finally {
    fs.rmSync(tmpA, { recursive: true, force: true });
    fs.rmSync(tmpB, { recursive: true, force: true });
  }
});

test('edit old/new and read offset/limit are preserved in r.args alongside r.fileNav', () => {
  // Edit tool with path, at, old, new
  const editEvents: Event[] = [
    toolEvent(1, 'edit', { path: 'src/main.ts', at: 10, old: 'const x = 1;', new: 'const x = 2;' }),
  ];
  const editRows = rows(editEvents);
  assert.equal(editRows.length, 1);
  const editRow = editRows[0];
  assert.deepEqual(editRow.fileNav, { path: 'src/main.ts', line: 10 });
  assert.ok(editRow.args, 'args must not be empty');
  assert.ok(editRow.args.includes('const x = 1;'), 'args preserves old');
  assert.ok(editRow.args.includes('const x = 2;'), 'args preserves new');

  // Read tool with path, offset, limit
  const readEvents: Event[] = [
    toolEvent(2, 'read', { path: 'src/app.ts', offset: 15, limit: 30 }),
  ];
  const readRows = rows(readEvents);
  assert.equal(readRows.length, 1);
  const readRow = readRows[0];
  assert.deepEqual(readRow.fileNav, { path: 'src/app.ts', line: 15 });
  assert.ok(readRow.args, 'args must not be empty');
  assert.ok(readRow.args.includes('15'), 'args preserves offset');
  assert.ok(readRow.args.includes('30'), 'args preserves limit');

  // Write tool with only path
  const writeEvents: Event[] = [
    toolEvent(3, 'write', { path: 'src/index.ts' }),
  ];
  const writeRows = rows(writeEvents);
  assert.equal(writeRows.length, 1);
  const writeRow = writeRows[0];
  assert.deepEqual(writeRow.fileNav, { path: 'src/index.ts' });
  assert.equal(writeRow.args, 'src/index.ts', 'single path argument is cleanly preserved');
});

