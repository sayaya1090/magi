import { test } from 'node:test';
import * as assert from 'node:assert/strict';
const Module = require('module');

// Stub 'vscode' module before importing OutputProvider and Chat
let registeredProviders = new Map<string, any>();
let openedDocs: any[] = [];
let shownDocs: any[] = [];
let closeDocListeners: ((doc: any) => void)[] = [];
let textDocuments: any[] = [];

const vscodeMock = {
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
    file: (f: string) => ({ fsPath: f, scheme: 'file', path: f, toString: () => `file://${f}` }),
    parse: (u: string) => {
      const scheme = u.split(':/')[0];
      return {
        scheme,
        toString: () => u,
        path: u.slice(scheme.length + 1),
      };
    },
    joinPath: (base: any, ...paths: string[]) => ({
      fsPath: [base.fsPath, ...paths].join('/'),
      scheme: 'file',
      path: [base.path, ...paths].join('/'),
    }),
  },
  workspace: {
    textDocuments,
    registerTextDocumentContentProvider: (scheme: string, provider: any) => {
      registeredProviders.set(scheme, provider);
      return {
        dispose() {
          registeredProviders.delete(scheme);
        },
      };
    },
    openTextDocument: async (uriOrOptions: any) => {
      const uri = uriOrOptions?.scheme ? uriOrOptions : uriOrOptions?.content ? { scheme: 'untitled' } : uriOrOptions;
      const doc = { uri, lineCount: 5, getText: () => '' };
      openedDocs.push(doc);
      return doc;
    },
    onDidCloseTextDocument: (fn: any) => {
      closeDocListeners.push(fn);
      return {
        dispose() {
          const idx = closeDocListeners.indexOf(fn);
          if (idx >= 0) closeDocListeners.splice(idx, 1);
        },
      };
    },
    getConfiguration: () => ({ get: () => true }),
  },
  window: {
    tabGroups: {
      all: [] as any[],
      onDidChangeTabs: () => ({ dispose() {} }),
    },
    showTextDocument: async (doc: any, opts: any) => {
      shownDocs.push({ doc, opts });
      return { document: doc };
    },
    createTextEditorDecorationType: () => ({ dispose() {} }),
  },
  languages: {
    setTextDocumentLanguage: async (doc: any, lang: string) => {
      (doc as any).languageId = lang;
      return doc;
    },
  },
  ThemeColor: class {
    constructor(public id: string) {}
  },
  OverviewRulerLane: {
    Left: 1,
    Center: 2,
    Right: 4,
    Full: 7,
  },
  commands: {
    executeCommand: async () => {},
  },
};

const origLoad = (Module as any)._load;
(Module as any)._load = function (request: string, parent: any, isMain: boolean) {
  if (request === 'vscode') {
    return vscodeMock;
  }
  return origLoad.apply(this, [request, parent, isMain]);
};

import { OutputProvider, openOutputDocument } from '../ide/output';
import { Event } from '../core/protocol';
import { Chat } from '../ide/chat';

function resetMocks() {
  registeredProviders.clear();
  openedDocs.length = 0;
  shownDocs.length = 0;
  closeDocListeners.length = 0;
  textDocuments.length = 0;
}

test('OutputProvider scheme registration and basic snapshot storage', () => {
  resetMocks();
  const provider = new OutputProvider(50);
  assert.equal(OutputProvider.scheme, 'magi-output');

  const uri = { scheme: 'magi-output', toString: () => 'magi-output:/workspace/s1/assistant/id1/res.md' } as any;
  provider.put(uri, '# Title\nHello world');

  const content = provider.provideTextDocumentContent(uri);
  assert.equal(content, '# Title\nHello world');

  provider.dispose();
});

test('OutputProvider.provideTextDocumentContent throws error on expired or missing snapshot (never returns empty document)', () => {
  resetMocks();
  const provider = new OutputProvider(50);
  const uri = { scheme: 'magi-output', toString: () => 'magi-output:/workspace/s1/assistant/non-existent/res.md' } as any;

  assert.throws(
    () => provider.provideTextDocumentContent(uri),
    /자료를 더 이상 열 수 없습니다/,
    'must throw explicit error when snapshot is expired or absent, rather than degrading to empty file'
  );
  provider.dispose();
});

test('openOutputDocument opens assistant answer verbatim in native editor and protects during open', async () => {
  resetMocks();
  const provider = new OutputProvider(10);
  const events: Event[] = [
    {
      seq: 10,
      type: 'part.appended',
      data: {
        role: 'assistant',
        part: { kind: 'text', text: 'Verbatim content line 1\nLine 2\n\n' },
      },
    },
  ];

  const res = await openOutputDocument({
    provider,
    companionKey: '/workspace/magi',
    session: 'sess-1',
    outputId: 'assistant:10',
    events,
    preserveFocus: false,
  });

  assert.equal(res.opened, true);
  assert.ok(res.uri);
  assert.match(res.uri.toString(), /^magi-output:\//);
  assert.ok(res.uri.toString().includes('assistant%3A10'));

  // Document was opened and shown via VS Code APIs
  assert.equal(openedDocs.length, 1);
  assert.equal(shownDocs.length, 1);
  assert.equal(shownDocs[0].opts?.preserveFocus, false);

  // Content was stored in provider and matches verbatim
  assert.equal(provider.provideTextDocumentContent(res.uri), 'Verbatim content line 1\nLine 2\n\n');

  provider.dispose();
});

test('openOutputDocument opens tool result (structured JSON) with JSON formatting and language hint', async () => {
  resetMocks();
  const provider = new OutputProvider(10);
  const toolPayload = { ok: true, found: ['a.go', 'b.go'] };
  const events: Event[] = [
    {
      seq: 100,
      type: 'part.appended',
      data: {
        role: 'assistant',
        part: { kind: 'tool-call', toolCall: { callId: 'c100', name: 'find_files' } },
      },
    },
    {
      seq: 105,
      type: 'part.appended',
      data: {
        role: 'tool',
        part: { kind: 'tool-result', toolResult: { callId: 'c100', content: toolPayload } },
      },
    },
  ];

  const res = await openOutputDocument({
    provider,
    companionKey: '/workspace/magi',
    session: 'sess-1',
    outputId: 'tool:c100:105',
    events,
  });

  assert.equal(res.opened, true);
  assert.ok(res.uri);
  assert.ok(res.uri.toString().includes('.json'));

  const content = provider.provideTextDocumentContent(res.uri);
  assert.equal(content, JSON.stringify(toolPayload, null, 2));

  provider.dispose();
});

test('openOutputDocument handles invalid or missing requests without side effects', async () => {
  resetMocks();
  const provider = new OutputProvider(10);
  const events: Event[] = [];

  // Missing session
  const resNoSess = await openOutputDocument({
    provider,
    companionKey: '/workspace',
    session: '',
    outputId: 'assistant:1',
    events,
  });
  assert.equal(resNoSess.opened, false);

  // Unresolvable output
  const resMissing = await openOutputDocument({
    provider,
    companionKey: '/workspace',
    session: 's1',
    outputId: 'assistant:999',
    events,
  });
  assert.equal(resMissing.opened, false);
  assert.equal(resMissing.error, '자료를 더 이상 열 수 없음');

  assert.equal(openedDocs.length, 0, 'openTextDocument must not be called on failure');
  assert.equal(shownDocs.length, 0, 'showTextDocument must not be called on failure');

  provider.dispose();
});

test('openOutputDocument converts openTextDocument failure to opened: false and releases protection (§3.1)', async () => {
  resetMocks();
  const provider = new OutputProvider(1);
  const events: Event[] = [
    {
      seq: 50,
      type: 'part.appended',
      data: { role: 'assistant', part: { kind: 'text', text: 'content' } },
    },
  ];

  const origOpen = vscodeMock.workspace.openTextDocument;
  vscodeMock.workspace.openTextDocument = async () => {
    throw new Error('Simulated workspace open error');
  };

  try {
    const res = await openOutputDocument({
      provider,
      companionKey: '/workspace',
      session: 's1',
      outputId: 'assistant:50',
      events,
    });
    assert.equal(res.opened, false);
    assert.match(res.error ?? '', /Simulated workspace open error/);
  } finally {
    vscodeMock.workspace.openTextDocument = origOpen;
  }

  // Provider's tempPinned size must be 0 (no leaked protection)
  assert.equal((provider as any).snapshots.tempPinnedSize, 0, 'in-flight protection must be released on openTextDocument failure');

  provider.dispose();
});

test('openOutputDocument converts showTextDocument failure to opened: false and releases protection (§3.1)', async () => {
  resetMocks();
  const provider = new OutputProvider(1);
  const events: Event[] = [
    {
      seq: 50,
      type: 'part.appended',
      data: { role: 'assistant', part: { kind: 'text', text: 'content' } },
    },
  ];

  const origShow = vscodeMock.window.showTextDocument;
  vscodeMock.window.showTextDocument = async () => {
    throw new Error('Simulated editor window error');
  };

  try {
    const res = await openOutputDocument({
      provider,
      companionKey: '/workspace',
      session: 's1',
      outputId: 'assistant:50',
      events,
    });
    assert.equal(res.opened, false);
    assert.match(res.error ?? '', /Simulated editor window error/);
  } finally {
    vscodeMock.window.showTextDocument = origShow;
  }

  // Provider's tempPinned size must be 0 (no leaked protection)
  assert.equal((provider as any).snapshots.tempPinnedSize, 0, 'in-flight protection must be released on showTextDocument failure');

  provider.dispose();
});

test('OutputProvider prunes unpinned items on didCloseTextDocument', () => {
  resetMocks();
  const provider = new OutputProvider(2);

  const uri1 = { scheme: 'magi-output', toString: () => 'magi-output:/w/s/assistant/1/a.md' } as any;
  const uri2 = { scheme: 'magi-output', toString: () => 'magi-output:/w/s/assistant/2/b.md' } as any;
  const uri3 = { scheme: 'magi-output', toString: () => 'magi-output:/w/s/assistant/3/c.md' } as any;

  // Add uri1 and uri2 as pinned open documents in workspace
  textDocuments.push(
    { uri: uri1 },
    { uri: uri2 },
  );

  provider.put(uri1, 'content 1');
  provider.put(uri2, 'content 2');
  provider.put(uri3, 'content 3');

  // Both uri1 and uri2 were pinned; uri3 added via temporary overflow (size = 3)
  assert.equal(provider.get(uri1), 'content 1');
  assert.equal(provider.get(uri2), 'content 2');
  assert.equal(provider.get(uri3), 'content 3');

  // Close doc 1
  textDocuments.splice(0, 1);
  for (const listener of closeDocListeners) {
    listener({ uri: uri1 });
  }

  // uri1 was evicted after closing
  assert.equal(provider.get(uri1), undefined);
  assert.equal(provider.get(uri2), 'content 2');
  assert.equal(provider.get(uri3), 'content 3');

  provider.dispose();
});

test('openOutputDocument uses document returned by setTextDocumentLanguage and retains protection during close events (§3.2)', async () => {
  resetMocks();
  const provider = new OutputProvider(1);
  const events: Event[] = [
    {
      seq: 60,
      type: 'part.appended',
      data: { role: 'assistant', part: { kind: 'text', text: '# Heading\nMarkdown text' } },
    },
  ];

  let closeEventFiredDuringLanguageSwitch = false;
  const origSetLang = vscodeMock.languages.setTextDocumentLanguage;
  vscodeMock.languages.setTextDocumentLanguage = async (originalDoc: any, lang: string) => {
    // Simulate VS Code closing old document during language change
    closeEventFiredDuringLanguageSwitch = true;
    for (const listener of closeDocListeners) {
      listener(originalDoc);
    }
    // Return a distinct new document object as VS Code does
    return {
      uri: originalDoc.uri,
      languageId: lang,
      isReplacedInstance: true,
      lineCount: 10,
    };
  };

  try {
    const res = await openOutputDocument({
      provider,
      companionKey: '/workspace',
      session: 's1',
      outputId: 'assistant:60',
      events,
    });

    assert.equal(res.opened, true);
    assert.ok(closeEventFiredDuringLanguageSwitch);
    assert.equal(shownDocs.length, 1);
    // Verified that showTextDocument received the returned document object (§3.2)
    assert.equal(shownDocs[0].doc.isReplacedInstance, true, 'showTextDocument must receive the document returned by setTextDocumentLanguage');
    assert.equal(shownDocs[0].doc.languageId, 'markdown');

    // Document must NOT have been evicted by the close event because it was temp-protected
    assert.ok(res.uri);
    assert.equal(provider.get(res.uri), '# Heading\nMarkdown text', 'snapshot must survive close event during language switch');
  } finally {
    vscodeMock.languages.setTextDocumentLanguage = origSetLang;
    provider.dispose();
  }
});

test('openOutputDocument handles setTextDocumentLanguage failure by opening raw document and returning warning (§3.2)', async () => {
  resetMocks();
  const provider = new OutputProvider(1);
  const events: Event[] = [
    {
      seq: 65,
      type: 'part.appended',
      data: { role: 'assistant', part: { kind: 'text', text: 'plain markdown' } },
    },
  ];

  const origSetLang = vscodeMock.languages.setTextDocumentLanguage;
  vscodeMock.languages.setTextDocumentLanguage = async () => {
    throw new Error('Language server not installed');
  };

  try {
    const res = await openOutputDocument({
      provider,
      companionKey: '/workspace',
      session: 's1',
      outputId: 'assistant:65',
      events,
    });

    // Opens successfully with verbatim content inspection
    assert.equal(res.opened, true);
    assert.equal(shownDocs.length, 1);
    assert.equal(shownDocs[0].doc.uri, res.uri);
    // Returns warning so caller can notify user without masking language setting failure
    assert.match(res.warning ?? '', /언어 모드 설정 실패: Language server not installed/);
  } finally {
    vscodeMock.languages.setTextDocumentLanguage = origSetLang;
    provider.dispose();
  }
});

test('Chat host receives output message, dispatches open, and handles failures via note without mode changes (§3.1)', async () => {
  resetMocks();
  const companion = {
    calls: [] as any[],
    workdir: '/workspace/test',
    session: 'sess-active',
    you: 'user',
    state: { state: 'idle' as any },
    version: '1.0.0',
    socket: '/tmp/magi.sock',
    facts: {},
    reach: async () => null,
    onChanged: () => ({ dispose() {} }),
    ask: async () => ({ ok: true }),
  };

  const chat = new Chat(companion as any, { fsPath: '/ext', scheme: 'file' } as any);
  const posted: any[] = [];
  (chat as any).post = (m: any) => posted.push(m);
  (chat as any).draw = () => {};
  (chat as any).sid = 'sess-active';
  (chat as any).events = [
    {
      seq: 70,
      type: 'part.appended',
      data: {
        role: 'assistant',
        part: { kind: 'text', text: 'Chat message to open in editor tab' },
      },
    },
  ];

  // 1. Valid output request for active session
  await chat.fromView({ kind: 'output', session: 'sess-active', outputId: 'assistant:70' });

  assert.equal(openedDocs.length, 1);
  assert.equal(shownDocs.length, 1);
  assert.ok(shownDocs[0].doc.uri.toString().includes('assistant%3A70'));

  // 2. Outdated request for switched session -> posts note
  posted.length = 0;
  await chat.fromView({ kind: 'output', session: 'sess-old-expired', outputId: 'assistant:70' });
  assert.equal(posted.length, 1);
  assert.equal(posted[0].kind, 'note');
  assert.match(posted[0].text, /자료를 더 이상 열 수 없음 — 세션이 일치하지 않습니다\./);

  // 3. Open failure (openTextDocument throws) -> handled via note, no uncaught rejection, no mode change
  const origOpen = vscodeMock.workspace.openTextDocument;
  vscodeMock.workspace.openTextDocument = async () => {
    throw new Error('Disk read fault');
  };
  posted.length = 0;
  try {
    await chat.fromView({ kind: 'output', session: 'sess-active', outputId: 'assistant:70' });
    assert.equal(posted.length, 1, 'exactly one note must be posted on open failure');
    assert.equal(posted[0].kind, 'note');
    assert.match(posted[0].text, /Disk read fault/);
    assert.ok(!posted.some((m) => m.kind === 'ask' || m.kind === 'compose' || m.kind === 'rows'), 'no input or ask mode mutation');
  } finally {
    vscodeMock.workspace.openTextDocument = origOpen;
  }

  // 4. Open failure (showTextDocument throws) -> handled via note, no uncaught rejection, no mode change
  const origShow = vscodeMock.window.showTextDocument;
  vscodeMock.window.showTextDocument = async () => {
    throw new Error('Display window unavailable');
  };
  posted.length = 0;
  try {
    await chat.fromView({ kind: 'output', session: 'sess-active', outputId: 'assistant:70' });
    assert.equal(posted.length, 1, 'exactly one note must be posted on show failure');
    assert.equal(posted[0].kind, 'note');
    assert.match(posted[0].text, /Display window unavailable/);
    assert.ok(!posted.some((m) => m.kind === 'ask' || m.kind === 'compose' || m.kind === 'rows'), 'no input or ask mode mutation');
  } finally {
    vscodeMock.window.showTextDocument = origShow;
  }

  chat.dispose();
});

test('Chat.dispose disposes provider, workspace registration, and companion listener exactly once (§3.3)', () => {
  resetMocks();
  let providerRegDisposed = 0;
  let companionListenerDisposed = 0;

  const origRegister = vscodeMock.workspace.registerTextDocumentContentProvider;
  vscodeMock.workspace.registerTextDocumentContentProvider = (scheme: string, provider: any) => {
    const reg = origRegister(scheme, provider);
    return {
      dispose() {
        if (scheme === OutputProvider.scheme) {
          providerRegDisposed++;
        }
        reg.dispose();
      },
    };
  };

  const companion = {
    workdir: '/workspace/test',
    session: 'sess-1',
    state: { state: 'idle' as any },
    version: '1.0.0',
    onChanged: () => ({
      dispose() {
        companionListenerDisposed++;
      },
    }),
    ask: async () => ({ ok: true }),
  };

  try {
    const chat = new Chat(companion as any, { fsPath: '/ext', scheme: 'file' } as any);
    const outputProvider = (chat as any).outputProvider;
    let providerDisposeCalls = 0;
    const origProviderDispose = outputProvider.dispose.bind(outputProvider);
    outputProvider.dispose = () => {
      providerDisposeCalls++;
      origProviderDispose();
    };

    chat.dispose();

    assert.equal(providerRegDisposed, 1, 'workspace provider registration must be disposed exactly once');
    assert.equal(providerDisposeCalls, 1, 'outputProvider must be disposed exactly once (no double-free)');
    assert.equal(companionListenerDisposed, 1, 'companion listener subscription must be disposed exactly once');
  } finally {
    vscodeMock.workspace.registerTextDocumentContentProvider = origRegister;
  }
});
