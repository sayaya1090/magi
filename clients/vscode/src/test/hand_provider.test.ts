import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as http from 'http';
const Module = require('module');

// Mock vscode module for host testing EditorHand
const documents = new Map<string, { body: string }>();
let applyEditCalls: any[] = [];
let applyEditResult = true;

class MockPosition {
  constructor(public line: number, public character: number) {}
}

class MockRange {
  constructor(public start: MockPosition, public end: MockPosition) {}
}

class MockWorkspaceEdit {
  entries: { uri: any; range: MockRange; newText: string }[] = [];
  replace(uri: any, range: MockRange, newText: string) {
    this.entries.push({ uri, range, newText });
  }
}

const vscodeMock = {
  Uri: {
    file: (f: string) => ({ fsPath: f, scheme: 'file', path: f, toString: () => `file://${f}` }),
  },
  Position: MockPosition,
  Range: MockRange,
  WorkspaceEdit: MockWorkspaceEdit,
  workspace: {
    async openTextDocument(uri: any) {
      const p = uri.fsPath || uri.path || String(uri);
      if (!documents.has(p)) {
        throw new Error(`cannot open ${p}`);
      }
      const data = documents.get(p)!;
      return {
        uri,
        getText: () => data.body,
        positionAt: (offset: number) => {
          const before = data.body.slice(0, offset);
          const lines = before.split('\n');
          return new MockPosition(lines.length - 1, lines[lines.length - 1].length);
        },
      };
    },
    async applyEdit(edit: MockWorkspaceEdit) {
      applyEditCalls.push(edit);
      if (applyEditResult) {
        for (const entry of edit.entries) {
          const p = entry.uri.fsPath || entry.uri.path;
          if (documents.has(p)) {
            documents.set(p, { body: entry.newText });
          }
        }
      }
      return applyEditResult;
    },
    asRelativePath: (uri: any) => uri.fsPath || uri.path,
  },
  window: {
    async showTextDocument() {},
  },
  languages: {
    getDiagnostics: () => [],
  },
  DiagnosticSeverity: { Error: 0, Warning: 1 },
};

const origLoad = (Module as any)._load;
(Module as any)._load = function (request: string, parent: any, isMain: boolean) {
  if (request === 'vscode') {
    return vscodeMock;
  }
  return origLoad.apply(this, [request, parent, isMain]);
};

import { EditorHand } from '../ide/hand';
import { callHand } from '../core/hand';
import { Hand } from '../core/mcpserver';

function resetHost(docs: Record<string, string> = {}) {
  documents.clear();
  for (const [k, v] of Object.entries(docs)) {
    documents.set(k, { body: v });
  }
  applyEditCalls = [];
  applyEditResult = true;
}

function postJson(url: string, headers: Record<string, string>, body: any): Promise<{ status: number; data: any }> {
  return new Promise((resolve, reject) => {
    const u = new URL(url);
    const data = JSON.stringify(body);
    const req = http.request(
      {
        hostname: u.hostname,
        port: u.port,
        path: u.pathname,
        method: 'POST',
        headers: {
          'content-type': 'application/json',
          'content-length': Buffer.byteLength(data),
          ...headers,
        },
      },
      (res) => {
        let text = '';
        res.on('data', (chunk) => {
          text += chunk;
        });
        res.on('end', () => {
          try {
            resolve({ status: res.statusCode ?? 0, data: JSON.parse(text) });
          } catch {
            resolve({ status: res.statusCode ?? 0, data: text });
          }
        });
      }
    );
    req.on('error', reject);
    req.write(data);
    req.end();
  });
}

test('§6.47: EditorHand 호스트 연결 및 apply_edit 거절의 도구 오류 전달', async (t) => {
  const workdir = '/test/workspace';
  const dummyCompanion = {
    caps: async () => new Set(['tool-servers']),
    ask: async () => ({ ok: true }),
  } as any;
  const hand = new EditorHand(dummyCompanion, workdir);

  await t.test('1. 본문 abc에 old=missing: error=true, 사유 유지, workspace.applyEdit 호출 0회', async () => {
    resetHost({ '/test/workspace/a.txt': 'abc' });
    const res = await callHand(hand, 'apply_edit', {
      path: 'a.txt',
      old: 'missing',
      new: 'xyz',
    });
    assert.equal(res.error, true, `old 미발견 시 error=true 여야 함: ${JSON.stringify(res)}`);
    assert.match(res.text, /that text is not in \/test\/workspace\/a\.txt/);
    assert.equal(applyEditCalls.length, 0, 'old 미발견 시 applyEdit이 호출되지 않아야 함');
    assert.equal(documents.get('/test/workspace/a.txt')?.body, 'abc', '문서 내용이 보존되어야 함');
  });

  await t.test('2. 본문 x x에 old=x, all=false: error=true, 다중 발견 안내, applyEdit 0회', async () => {
    resetHost({ '/test/workspace/a.txt': 'x x' });
    const res = await callHand(hand, 'apply_edit', {
      path: 'a.txt',
      old: 'x',
      new: 'y',
      replaceAll: false,
    });
    assert.equal(res.error, true, `다중 발견 시 replaceAll=false면 error=true 여야 함: ${JSON.stringify(res)}`);
    assert.match(res.text, /that text appears 2 times in \/test\/workspace\/a\.txt — narrow it, or pass replaceAll/);
    assert.equal(applyEditCalls.length, 0, '다중 발견 거절 시 applyEdit이 호출되지 않아야 함');
    assert.equal(documents.get('/test/workspace/a.txt')?.body, 'x x', '문서 내용이 보존되어야 함');
  });

  await t.test('3. 단일 일치 + applyEdit=false: error=true, 기존 editor refused 문구, API 호출 1회', async () => {
    resetHost({ '/test/workspace/a.txt': 'hello world' });
    applyEditResult = false;
    const res = await callHand(hand, 'apply_edit', {
      path: 'a.txt',
      old: 'world',
      new: 'universe',
    });
    assert.equal(res.error, true, `에디터 거절 시 error=true 여야 함: ${JSON.stringify(res)}`);
    assert.match(res.text, /the editor refused the edit to \/test\/workspace\/a\.txt/);
    assert.ok(!res.text.includes('replaced'), '성공 문구가 포함되지 않아야 함');
    assert.equal(applyEditCalls.length, 1, 'applyEdit이 정확히 1회 호출되어야 함');
  });

  await t.test('4. 단일 일치 및 all=true 다중 일치 성공: error 미설정, WorkspaceEdit 내용 일치, 빈 new, 공백 old, 달러 리터럴 보존', async () => {
    // 4-1. 단일 치환 성공
    resetHost({ '/test/workspace/a.txt': 'hello world' });
    const res1 = await callHand(hand, 'apply_edit', {
      path: 'a.txt',
      old: 'world',
      new: 'universe',
    });
    assert.ok(!res1.error, `정상 치환은 error가 없어야 함: ${JSON.stringify(res1)}`);
    assert.match(res1.text, /replaced 1 occurrence\(s\) in \/test\/workspace\/a\.txt/);
    assert.equal(applyEditCalls.length, 1);
    assert.equal(documents.get('/test/workspace/a.txt')?.body, 'hello universe');

    // 4-2. replaceAll=true 다중 치환 성공
    resetHost({ '/test/workspace/a.txt': 'cat and cat and cat' });
    const res2 = await callHand(hand, 'apply_edit', {
      path: 'a.txt',
      old: 'cat',
      new: 'dog',
      replaceAll: true,
    });
    assert.ok(!res2.error);
    assert.match(res2.text, /replaced 3 occurrence\(s\) in \/test\/workspace\/a\.txt/);
    assert.equal(documents.get('/test/workspace/a.txt')?.body, 'dog and dog and dog');

    // 4-3. 빈 new 삭제
    resetHost({ '/test/workspace/a.txt': 'prefix-REMOVE-suffix' });
    const res3 = await callHand(hand, 'apply_edit', {
      path: 'a.txt',
      old: '-REMOVE',
      new: '',
    });
    assert.ok(!res3.error);
    assert.equal(documents.get('/test/workspace/a.txt')?.body, 'prefix-suffix');

    // 4-4. 공백 old 허용
    resetHost({ '/test/workspace/a.txt': '    indented' });
    const res4 = await callHand(hand, 'apply_edit', {
      path: 'a.txt',
      old: '    ',
      new: '\t',
    });
    assert.ok(!res4.error);
    assert.equal(documents.get('/test/workspace/a.txt')?.body, '\tindented');

    // 4-5. 달러 치환 문자 리터럴 보존 ($&, $$, $1)
    resetHost({ '/test/workspace/a.txt': 'replace TOKEN here' });
    const res5 = await callHand(hand, 'apply_edit', {
      path: 'a.txt',
      old: 'TOKEN',
      new: '$& $$ $1 $\' $`',
    });
    assert.ok(!res5.error);
    assert.equal(documents.get('/test/workspace/a.txt')?.body, 'replace $& $$ $1 $\' $` here');
  });

  await t.test('5. openTextDocument 거절: 기존 예외 메시지가 error=true로 전달됨', async () => {
    resetHost({}); // no files exist
    const res = await callHand(hand, 'apply_edit', {
      path: 'nonexistent.txt',
      old: 'a',
      new: 'b',
    });
    assert.equal(res.error, true, `문서 열기 실패 시 error=true 여야 함: ${JSON.stringify(res)}`);
    assert.match(res.text, /cannot open \/test\/workspace\/nonexistent\.txt/);
    assert.equal(applyEditCalls.length, 0);
  });

  await t.test('6. 실제 Hand.start(EditorHand) HTTP tools/call 경로에서 old 미발견(isError=true) 및 성공(isError=false) 검증', async () => {
    resetHost({ '/test/workspace/file.txt': 'sample text content' });
    const server = await Hand.start(hand);
    try {
      // 6-1. old 미발견 거절 요청 -> HTTP 200, result.isError = true
      const errCall = await postJson(server.url, server.headers, {
        jsonrpc: '2.0',
        id: 101,
        method: 'tools/call',
        params: {
          name: 'apply_edit',
          arguments: {
            path: 'file.txt',
            old: 'nonexistent_needle',
            new: 'replacement',
          },
        },
      });
      assert.equal(errCall.status, 200);
      assert.equal(errCall.data.id, 101);
      assert.ok(errCall.data.result, `result가 존재해야 함: ${JSON.stringify(errCall.data)}`);
      assert.equal(errCall.data.result.isError, true, '거절된 도구 호출의 isError는 true 여야 함');
      assert.match(errCall.data.result.content[0].text, /that text is not in \/test\/workspace\/file\.txt/);

      // 6-2. 성공 치환 요청 -> HTTP 200, result.isError = false
      const okCall = await postJson(server.url, server.headers, {
        jsonrpc: '2.0',
        id: 102,
        method: 'tools/call',
        params: {
          name: 'apply_edit',
          arguments: {
            path: 'file.txt',
            old: 'sample text',
            new: 'modified text',
          },
        },
      });
      assert.equal(okCall.status, 200);
      assert.equal(okCall.data.id, 102);
      assert.ok(okCall.data.result, `result가 존재해야 함: ${JSON.stringify(okCall.data)}`);
      assert.equal(okCall.data.result.isError, false, '성공한 도구 호출의 isError는 false 여야 함');
      assert.match(okCall.data.result.content[0].text, /replaced 1 occurrence\(s\) in \/test\/workspace\/file\.txt/);
      assert.equal(documents.get('/test/workspace/file.txt')?.body, 'modified text content');
    } finally {
      server.close();
    }
  });

  hand.dispose();
});
