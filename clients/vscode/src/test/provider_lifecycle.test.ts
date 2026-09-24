import { test } from 'node:test';
import * as assert from 'node:assert/strict';
const Module = require('module');

// Stub 'vscode' module before importing providers
let textDocuments: any[] = [];
let closeDocListeners: ((doc: any) => void)[] = [];
let tabChangeListeners: (() => void)[] = [];
let closeListenerDisposedCount = 0;
let tabListenerDisposedCount = 0;
let tabGroupsAll: any[] = [];
let executedCommands: { command: string; args: any[] }[] = [];

class MockTabInputText {
  constructor(public readonly uri: any) {}
}

class MockTabInputTextDiff {
  constructor(public readonly original: any, public readonly modified: any) {}
}

const vscodeMock = {
  TabInputText: MockTabInputText,
  TabInputTextDiff: MockTabInputTextDiff,
  EventEmitter: class {
    event = () => ({ dispose() {} });
    fire() {}
    dispose() {}
  },
  Disposable: class {
    static from(..._disposables: any[]) {
      return { dispose() {} };
    }
    dispose() {}
  },
  Uri: {
    file: (f: string): any => ({
      fsPath: f,
      scheme: 'file',
      path: f,
      authority: '',
      query: '',
      fragment: '',
      with: () => ({} as any),
      toJSON: () => ({}),
      toString: () => `file://${f}`,
    }),
    parse: (u: string): any => {
      const scheme = u.split(':/')[0];
      return {
        scheme,
        toString: () => u,
        path: u.slice(scheme.length + 1),
        fsPath: u,
        authority: '',
        query: '',
        fragment: '',
        with: () => ({} as any),
        toJSON: () => ({}),
      };
    },
  },
  workspace: {
    get textDocuments() {
      return textDocuments;
    },
    onDidCloseTextDocument: (fn: any) => {
      closeDocListeners.push(fn);
      return {
        dispose() {
          closeListenerDisposedCount++;
          const idx = closeDocListeners.indexOf(fn);
          if (idx >= 0) closeDocListeners.splice(idx, 1);
        },
      };
    },
    openTextDocument: async (uri: any) => ({
      uri,
      lineCount: 1,
      getText: () => '',
    }),
  },
  window: {
    tabGroups: {
      get all() {
        return tabGroupsAll;
      },
      onDidChangeTabs: (fn: any) => {
        tabChangeListeners.push(fn);
        return {
          dispose() {
            tabListenerDisposedCount++;
            const idx = tabChangeListeners.indexOf(fn);
            if (idx >= 0) tabChangeListeners.splice(idx, 1);
          },
        };
      },
    },
    showTextDocument: async (doc: any) => ({ document: doc }),
  },
  languages: {
    setTextDocumentLanguage: async (doc: any, lang: string) => {
      return { ...doc, languageId: lang };
    },
  },
  commands: {
    executeCommand: async (cmd: string, ...args: any[]) => {
      executedCommands.push({ command: cmd, args });
    },
  },
};

const origLoad = (Module as any)._load;
(Module as any)._load = function (request: string, parent: any, isMain: boolean) {
  if (request === 'vscode') {
    return vscodeMock;
  }
  return origLoad.apply(this, [request, parent, isMain]);
};

import { DiffProvider, openApprovalDiff } from '../ide/diff';
import { OutputProvider, openOutputDocument } from '../ide/output';
import { ProviderLifecycle } from '../ide/provider_lifecycle';

function resetMockState() {
  textDocuments = [];
  closeDocListeners = [];
  tabChangeListeners = [];
  closeListenerDisposedCount = 0;
  tabListenerDisposedCount = 0;
  tabGroupsAll = [];
  executedCommands = [];
}

test('DiffProvider: 일반 문서, 일반 탭, diff original/modified 양면 보호 및 이종 scheme 비보호 검증', () => {
  resetMockState();
  const provider = new DiffProvider(2);

  const diffUri1 = vscodeMock.Uri.parse('magi-diff:/comp/sess/call1/before/file.ts');
  const diffUri2 = vscodeMock.Uri.parse('magi-diff:/comp/sess/call1/after/file.ts');
  const diffUri3 = vscodeMock.Uri.parse('magi-diff:/comp/sess/call2/patch/patch.diff');
  const otherSchemeUri = vscodeMock.Uri.parse('magi-output:/comp/sess/assistant/id1/res.md');

  provider.put(diffUri1, 'before-code');
  provider.put(diffUri2, 'after-code');
  provider.put(diffUri3, 'patch-code');

  // 1. workspace.textDocuments에 열린 경우 보호
  textDocuments = [{ uri: diffUri1 }];
  assert.equal(provider.isOpen(diffUri1.toString()), true, 'workspace.textDocuments에 있으면 open으로 판정');

  // 2. TabInputTextDiff의 original만 열려 있는 경우
  tabGroupsAll = [
    {
      tabs: [
        { input: new MockTabInputTextDiff(diffUri2, vscodeMock.Uri.file('/tmp/other')) },
      ],
    },
  ];
  assert.equal(provider.isOpen(diffUri2.toString()), true, 'TabInputTextDiff original에 일치하면 open으로 판정');

  // 3. TabInputTextDiff의 modified만 열려 있는 경우
  tabGroupsAll = [
    {
      tabs: [
        { input: new MockTabInputTextDiff(vscodeMock.Uri.file('/tmp/other'), diffUri3) },
      ],
    },
  ];
  assert.equal(provider.isOpen(diffUri3.toString()), true, 'TabInputTextDiff modified에 일치하면 open으로 판정');

  // 4. 일반 TabInputText로 열린 경우
  tabGroupsAll = [
    {
      tabs: [{ input: new MockTabInputText(diffUri1) }],
    },
  ];
  assert.equal(provider.isOpen(diffUri1.toString()), true, 'TabInputText에 일치하면 open으로 판정');

  // 5. 다른 scheme(magi-output)이 열려 있어도 DiffProvider에서는 비보호
  tabGroupsAll = [
    {
      tabs: [{ input: new MockTabInputText(otherSchemeUri) }],
    },
  ];
  assert.equal(provider.isOpen(otherSchemeUri.toString()), false, '이종 scheme은 DiffProvider에서 open 대상 아님');

  provider.dispose();
});

test('OutputProvider: 일반 문서/탭 보호, TabInputTextDiff 미보호(경로 차이 보존) 및 이종 scheme 비보호 검증', () => {
  resetMockState();
  const provider = new OutputProvider(2);

  const outUri = vscodeMock.Uri.parse('magi-output:/comp/sess/assistant/id1/answer.md');
  const diffUri = vscodeMock.Uri.parse('magi-diff:/comp/sess/call1/before/file.ts');

  provider.put(outUri, '# Output Answer');

  // 1. 일반 TabInputText로 열린 경우 보호
  tabGroupsAll = [
    {
      tabs: [{ input: new MockTabInputText(outUri) }],
    },
  ];
  assert.equal(provider.isOpen(outUri.toString()), true, 'TabInputText로 열려 있으면 open 판정');

  // 2. OutputProvider는 TabInputTextDiff를 검사하지 않음 (차이점 명시적 보존)
  tabGroupsAll = [
    {
      tabs: [{ input: new MockTabInputTextDiff(outUri, vscodeMock.Uri.file('/tmp/other')) }],
    },
  ];
  assert.equal(provider.isOpen(outUri.toString()), false, 'OutputProvider는 TabInputTextDiff를 지원하지 않음');

  // 3. DiffProvider의 URI는 OutputProvider에서 열려 있어도 비보호
  tabGroupsAll = [
    {
      tabs: [{ input: new MockTabInputText(diffUri) }],
    },
  ];
  assert.equal(provider.isOpen(diffUri.toString()), false, '이종 scheme은 OutputProvider에서 open 대상 아님');

  provider.dispose();
});

test('문서 및 탭 종료 이벤트 수신 시 추가 put 없이 초과 캐시 즉각 정리, 관계없는 scheme 이벤트 무시', () => {
  resetMockState();
  // 용량 1인 DiffProvider
  const provider = new DiffProvider(1);
  const doc1 = vscodeMock.Uri.parse('magi-diff:/comp/sess/call1/before/1.ts');
  const doc2 = vscodeMock.Uri.parse('magi-diff:/comp/sess/call1/after/2.ts');

  // doc1 탭에 열어두고 저장
  tabGroupsAll = [{ tabs: [{ input: new MockTabInputText(doc1) }] }];
  provider.put(doc1, 'content-1');

  // doc2도 탭에 열어두고 저장 -> 일시 용량 초과 (size=2)
  tabGroupsAll = [{ tabs: [{ input: new MockTabInputText(doc1) }, { input: new MockTabInputText(doc2) }] }];
  provider.put(doc2, 'content-2');
  assert.equal(provider.get(doc1), 'content-1');
  assert.equal(provider.get(doc2), 'content-2');

  // 1. 관계없는 scheme 문서 닫힘 이벤트 -> prune 유발하지 않음
  const unrelatedDoc = { uri: vscodeMock.Uri.file('/path/to/normal.ts') };
  for (const listener of closeDocListeners) {
    listener(unrelatedDoc);
  }
  assert.equal(provider.get(doc1), 'content-1', '관계없는 문서 닫힘은 캐시 정리 유발하지 않음');
  assert.equal(provider.get(doc2), 'content-2');

  // 2. doc1 탭 닫힘: 탭 목록에서 제거 후 onDidCloseTextDocument 발생
  tabGroupsAll = [{ tabs: [{ input: new MockTabInputText(doc2) }] }];
  const closedDoc = { uri: doc1 };
  for (const listener of closeDocListeners) {
    listener(closedDoc);
  }

  // 추가 put 없이 doc1이 즉시 축출되어 용량 1로 복귀
  assert.equal(provider.get(doc1), undefined, '닫힌 doc1이 추가 put 없이 즉시 축출됨');
  assert.equal(provider.get(doc2), 'content-2', '남은 doc2는 유지됨');

  // 3. onDidChangeTabs 이벤트 발생 시에도 미고정 초과 항목 정리
  tabGroupsAll = []; // doc2도 탭에서 닫힘
  for (const listener of tabChangeListeners) {
    listener();
  }
  // doc2가 유일한 항목(용량 1 이내)이므로 아직 축출되지 않음
  assert.equal(provider.get(doc2), 'content-2');

  provider.dispose();
});

test('언어 변경 중 close 이벤트 발생 시에도 임시 보호 유지 및 반환된 TextDocument 표시', async () => {
  resetMockState();
  const provider = new OutputProvider(1);

  // 이벤트 fixture 준비
  const events = [
    {
      seq: 5,
      type: 'part.appended',
      data: {
        part: {
          kind: 'tool-call',
          toolCall: { callId: 'call1', name: 'read' },
        },
      },
    },
    {
      seq: 10,
      type: 'part.appended',
      data: {
        part: {
          kind: 'tool-result',
          toolResult: { callId: 'call1', content: { result: 'ok' } },
        },
      },
    },
  ];

  let lastShownDoc: any = null;
  const origShow = vscodeMock.window.showTextDocument;
  vscodeMock.window.showTextDocument = async (doc: any) => {
    lastShownDoc = doc;
    tabGroupsAll = [{ tabs: [{ input: new MockTabInputText(doc.uri) }] }];
    return { document: doc };
  };

  // 언어 설정 중 이전 문서 닫힘 이벤트가 발생하는 VS Code 환경 모사
  const origSetLang = vscodeMock.languages.setTextDocumentLanguage;
  vscodeMock.languages.setTextDocumentLanguage = async (doc: any, lang: string) => {
    // VS Code가 내부적으로 이전 언어 문서를 닫으며 이벤트 발생
    for (const listener of closeDocListeners) {
      listener({ uri: doc.uri });
    }
    // 임시 보호로 인해 닫힘 이벤트 직후에도 축출되지 않고 보존됨을 실시간 확인
    assert.equal(provider.get(doc.uri), '{\n  "result": "ok"\n}', '언어 변경 도중 닫힘 이벤트에도 임시 보호 유지');
    return { ...doc, languageId: lang, customMarker: 'updated-doc' };
  };

  try {
    const res = await openOutputDocument({
      provider,
      companionKey: 'comp',
      session: 'sess',
      outputId: 'tool:call1:10',
      events: events as any,
    });

    assert.equal(res.opened, true, '문서가 성공적으로 열려야 함');
    assert.equal(lastShownDoc?.customMarker, 'updated-doc', 'setTextDocumentLanguage 반환 문서로 실제 표시');
    assert.equal(provider.get(res.uri!), '{\n  "result": "ok"\n}', '표시 완료 후에도 탭에 열려 있으므로 보존');
  } finally {
    vscodeMock.languages.setTextDocumentLanguage = origSetLang;
    vscodeMock.window.showTextDocument = origShow;
    provider.dispose();
  }
});

test('열기 실패 시 finally 안전 해제와 기존 오류 안내 및 빈 문자열/만료 구분', async () => {
  resetMockState();
  const provider = new OutputProvider(1);

  // 1. 빈 문자열 문서는 유효한 내용으로 정상 저장 및 만료 에러 없이 반환
  const emptyUri = vscodeMock.Uri.parse('magi-output:/empty/doc');
  provider.put(emptyUri, '');
  assert.equal(provider.provideTextDocumentContent(emptyUri), '', '빈 문자열 문서는 에러 없이 "" 반환');

  // 2. 만료 또는 미존재 자료는 정확한 사용자 메시지와 함께 예외 발생
  const missingUri = vscodeMock.Uri.parse('magi-output:/missing/doc');
  assert.throws(
    () => provider.provideTextDocumentContent(missingUri),
    (err: any) => err.message === `자료를 더 이상 열 수 없습니다: ${missingUri.toString()}`
  );

  // 3. DiffProvider의 만료 메시지도 검증
  const diffProvider = new DiffProvider(1);
  const missingDiffUri = vscodeMock.Uri.parse('magi-diff:/missing/diff');
  assert.throws(
    () => diffProvider.provideTextDocumentContent(missingDiffUri),
    (err: any) => err.message === `승인 스냅샷이 만료되었거나 존재하지 않습니다: ${missingDiffUri.toString()}`
  );

  // 4. showTextDocument 실패 시 finally에서 임시 보호 해제
  const origShow = vscodeMock.window.showTextDocument;
  vscodeMock.window.showTextDocument = async () => {
    throw new Error('시뮬레이션된 에디터 표시 에러');
  };

  try {
    const events = [
      {
        seq: 1,
        type: 'part.appended',
        data: { role: 'assistant', part: { kind: 'text', text: 'Hello' } },
      },
    ];

    const res = await openOutputDocument({
      provider,
      companionKey: 'comp',
      session: 'sess',
      outputId: 'assistant:1',
      events: events as any,
    });

    assert.equal(res.opened, false);
    assert.match(res.error || '', /편집창 열기 실패: 시뮬레이션된 에디터 표시 에러/);
    // 임시 보호가 안전하게 해제되었는지 검증 (미고정 상태)
    assert.equal((provider as any).snapshots.tempPinnedSize, 0, '열기 실패 후 tempPinned가 0으로 안전 해제됨');
  } finally {
    vscodeMock.window.showTextDocument = origShow;
    provider.dispose();
    diffProvider.dispose();
  }
});

test('dispose 2회 호출 시 리스너가 정확히 1회씩만 해제되고, 지연 콜백은 무동작', () => {
  resetMockState();
  const provider = new DiffProvider(10);

  assert.equal(closeDocListeners.length, 1, 'close 리스너 1개 등록됨');
  assert.equal(tabChangeListeners.length, 1, 'tab change 리스너 1개 등록됨');

  // 지연 실행될 콜백 보관
  const lateCloseCallback = closeDocListeners[0];
  const lateTabCallback = tabChangeListeners[0];

  // 1회차 dispose
  provider.dispose();
  assert.equal(closeListenerDisposedCount, 1, 'close 리스너 dispose 1회 실행');
  assert.equal(tabListenerDisposedCount, 1, 'tab change 리스너 dispose 1회 실행');

  // 2회차 dispose (중복 호출)
  provider.dispose();
  assert.equal(closeListenerDisposedCount, 1, '중복 dispose 시 리스너 dispose 추가 호출 없음');
  assert.equal(tabListenerDisposedCount, 1, '중복 dispose 시 리스너 dispose 추가 호출 없음');

  // 지연된 콜백이 호출되어도 무동작 (prune 미실행, 에러 없음)
  let pruneCalled = false;
  (provider as any).prune = () => {
    pruneCalled = true;
    return 0;
  };

  lateCloseCallback({ uri: vscodeMock.Uri.parse('magi-diff:/test') });
  lateTabCallback();
  assert.equal(pruneCalled, false, 'dispose 후 늦게 도착한 콜백은 prune을 실행하지 않음');
});

test('openApprovalDiff: sides 및 patch 모드에서 DiffProvider 임시 보호 및 vscode.diff 명령 연동', async () => {
  resetMockState();
  const provider = new DiffProvider(1);

  // 1. sides 모드
  const askSides = {
    kind: 'permission' as const,
    callId: 'call-sides-1',
    what: 'edit',
    args: JSON.stringify({ path: 'src/main.ts', old: 'const a = 1;', new: 'const a = 2;' }),
  };

  const resSides = await openApprovalDiff(provider, 'comp1', 'sess1', askSides as any);
  assert.equal(resSides, true, 'sides 모드 diff 열기 성공');
  assert.equal(executedCommands.length, 1);
  assert.equal(executedCommands[0].command, 'vscode.diff');
  assert.match(executedCommands[0].args[0].toString(), /magi-diff:\/comp1\/sess1\/call-sides-1\/before\/main\.ts/);
  assert.match(executedCommands[0].args[1].toString(), /magi-diff:\/comp1\/sess1\/call-sides-1\/after\/main\.ts/);

  // 2. patch 모드
  executedCommands = [];
  const askPatch = {
    kind: 'permission' as const,
    callId: 'call-patch-2',
    what: 'bash',
    diff: '--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-old\n+new\n',
  };

  const resPatch = await openApprovalDiff(provider, 'comp1', 'sess1', askPatch as any);
  assert.equal(resPatch, true, 'patch 모드 diff 열기 성공');
  const patchUri = vscodeMock.Uri.parse('magi-diff:/comp1/sess1/call-patch-2/patch/magi-%EC%8A%B9%EC%9D%B8-changes-atch-2.diff');
  assert.equal(provider.provideTextDocumentContent(patchUri), askPatch.diff);

  provider.dispose();
});

test('ProviderLifecycle: scheme 및 prune 콜백 직접 제어와 isDisposed 상태 검증', () => {
  resetMockState();
  let pruneCount = 0;
  const lifecycle = new ProviderLifecycle({
    scheme: 'custom-scheme',
    onPrune: () => {
      pruneCount++;
    },
    supportsDiffTabs: false,
  });

  assert.equal(lifecycle.scheme, 'custom-scheme');
  assert.equal(lifecycle.isDisposed, false);

  // 무관한 scheme 닫힘 -> pruneCount 불변
  for (const listener of closeDocListeners) {
    listener({ uri: vscodeMock.Uri.parse('other-scheme:/doc1') });
  }
  assert.equal(pruneCount, 0);

  // 대상 scheme 닫힘 -> pruneCount 증가
  for (const listener of closeDocListeners) {
    listener({ uri: vscodeMock.Uri.parse('custom-scheme:/doc1') });
  }
  assert.equal(pruneCount, 1);

  // 탭 변경 -> pruneCount 증가
  for (const listener of tabChangeListeners) {
    listener();
  }
  assert.equal(pruneCount, 2);

  lifecycle.dispose();
  assert.equal(lifecycle.isDisposed, true);
  assert.equal(lifecycle.isOpen('custom-scheme:/doc1'), false, 'disposed 상태에서는 항상 false');
});

