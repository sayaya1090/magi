import { test } from 'node:test';
import * as assert from 'node:assert/strict';
const Module = require('module');

// Stub 'vscode' module before loading Chat
const origLoad = (Module as any)._load;
(Module as any)._load = function (request: string, parent: any, isMain: boolean) {
  if (request === 'vscode') {
    return {
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
        file: (f: string) => ({ fsPath: f, scheme: 'file', path: f }),
        joinPath: (base: any, ...paths: string[]) => ({
          fsPath: [base.fsPath, ...paths].join('/'),
          scheme: 'file',
          path: [base.path, ...paths].join('/'),
        }),
      },
      workspace: {
        registerTextDocumentContentProvider: () => ({ dispose() {} }),
        onDidCloseTextDocument: () => ({ dispose() {} }),
        getConfiguration: () => ({ get: () => true }),
      },
      commands: {
        executeCommand: async () => {},
      },
      window: {
        createTextEditorDecorationType: () => ({ dispose() {} }),
        visibleTextEditors: [],
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
    };
  }
  return origLoad.apply(this, [request, parent, isMain]);
};

// Now import Chat
import { Chat } from '../ide/chat';

function createMockCompanion() {
  const calls: { door: string; payload: any }[] = [];
  return {
    calls,
    workdir: '/workspace',
    session: '',
    you: 'tester',
    state: { state: 'idle' as any },
    version: '1.0.0',
    socket: '/tmp/magi.sock',
    facts: {},
    reach: async () => null,
    onChanged: (_fn: any) => ({ dispose() {} }),
    ask: async (door: string, payload?: any): Promise<any> => {
      calls.push({ door, payload });
      return { ok: true };
    },
  };
}

function createMockWebviewView() {
  const listeners: ((m: any) => void)[] = [];
  const messages: any[] = [];
  const view = {
    webview: {
      options: {},
      html: '',
      onDidReceiveMessage(fn: (m: any) => void) {
        listeners.push(fn);
        return { dispose() {} };
      },
      postMessage(m: any) {
        messages.push(m);
      },
    },
    onDidDispose(_fn: () => void) {
      return { dispose() {} };
    },
  };
  return { view, listeners, messages };
}

test('Chat B0: consecutive sends during session creation delay call session-new once and route second to steer', async () => {
  const companion = createMockCompanion();
  let sessionNewResolvers: ((val: any) => void)[] = [];

  companion.ask = async (door: string, payload?: any): Promise<any> => {
    companion.calls.push({ door, payload });
    if (door === 'session-new') {
      return new Promise((resolve) => {
        sessionNewResolvers.push(resolve);
      });
    }
    return { ok: true };
  };

  const chat = new Chat(companion as any, { fsPath: '/ext', scheme: 'file' } as any);
  const posted: any[] = [];
  (chat as any).post = (m: any) => posted.push(m);
  (chat as any).draw = () => {};

  // 1. Send first message while sid is empty
  const p1 = chat.fromView({ kind: 'say', text: 'Message A' });

  // 2. Send second message immediately while session-new is still in-flight
  const p2 = chat.fromView({ kind: 'say', text: 'Message B' });

  await new Promise((r) => setTimeout(r, 10));
  assert.equal(sessionNewResolvers.length, 1, 'session-new must be initiated once');

  // Resolve session-new with session id 'sess-1'
  sessionNewResolvers[0]({ session: 'sess-1' });

  await Promise.all([p1, p2]);

  // Verify call sequence
  const sessionNewCalls = companion.calls.filter((c) => c.door === 'session-new');
  assert.equal(sessionNewCalls.length, 1, 'session-new must only be called once for consecutive sends');

  const doors = companion.calls.map((c) => c.door);
  assert.deepEqual(doors, ['session-new', 'submit', 'steer'], 'first message must submit, second message must steer');

  const submitCall = companion.calls.find((c) => c.door === 'submit');
  assert.deepEqual(submitCall?.payload, { session: 'sess-1', text: 'Message A' });

  const steerCall = companion.calls.find((c) => c.door === 'steer');
  assert.deepEqual(steerCall?.payload, { session: 'sess-1', text: 'Message B' });
});

test('Chat B0: screen switch during session creation preserves active view session and routes message to target session', async () => {
  const companion = createMockCompanion();
  let resolveSessionNew!: (val: any) => void;

  companion.ask = async (door: string, payload?: any): Promise<any> => {
    companion.calls.push({ door, payload });
    if (door === 'session-new') {
      return new Promise((res) => { resolveSessionNew = res; });
    }
    return { ok: true };
  };

  const chat = new Chat(companion as any, { fsPath: '/ext', scheme: 'file' } as any);
  (chat as any).post = () => {};
  (chat as any).draw = () => {};
  (chat as any).openStream = async () => {};

  // First message starts session-new
  const p1 = chat.fromView({ kind: 'say', text: 'Original message' });
  await new Promise((r) => setTimeout(r, 10));

  // While session-new is waiting, user switches screen to 'sess-switched'
  chat.showSession('sess-switched');
  assert.equal(chat.session, 'sess-switched', 'active session updated to switched');

  // Complete session-new with 'sess-created'
  resolveSessionNew({ session: 'sess-created' });
  await p1;

  // Active session on chat view MUST NOT be overwritten by late session-new
  assert.equal(chat.session, 'sess-switched', 'late session-new must not overwrite active view session');

  // The message was sent to the session created for it ('sess-created')
  const submitCall = companion.calls.find((c) => c.door === 'submit');
  assert.equal(submitCall?.payload?.session, 'sess-created');
  assert.equal(submitCall?.payload?.text, 'Original message');
});

test('Chat B0: target session turn state is isolated from switched screen events', async () => {
  const companion = createMockCompanion();
  const chat = new Chat(companion as any, { fsPath: '/ext', scheme: 'file' } as any);
  (chat as any).post = () => {};
  (chat as any).draw = () => {};
  (chat as any).openStream = async () => {};

  // Initially on sess-1, turn is marked active in sess-1
  (chat as any).sid = 'sess-1';
  (chat as any).sessionActiveTurns.add('sess-1');

  // User queues a message for sess-1
  let resolveSend!: () => void;
  const sendPromise = new Promise<void>((r) => { resolveSend = r; });
  companion.ask = async (door: string, payload?: any): Promise<any> => {
    companion.calls.push({ door, payload });
    await sendPromise;
    return { ok: true };
  };

  const p1 = chat.fromView({ kind: 'say', text: 'Message in sess-1' });

  // User switches to an idle session 'sess-idle' with empty events
  chat.showSession('sess-idle');
  assert.equal((chat as any).events.length, 0);

  resolveSend();
  await p1;

  // Since target session was sess-1 (active turn), it must pick 'steer', NOT 'submit'
  const call = companion.calls.find((c) => c.payload?.text === 'Message in sess-1');
  assert.equal(call?.door, 'steer', 'must choose steer based on target session turn state, not switched idle view');
  assert.equal(call?.payload?.session, 'sess-1');
});

test('Chat B0: first send failure restores draft and note without unhandled rejection', async () => {
  const companion = createMockCompanion();
  companion.ask = async (door: string, payload?: any): Promise<any> => {
    companion.calls.push({ door, payload });
    if (door === 'session-new') {
      return { error: 'companion daemon unreachable' };
    }
    return { ok: false };
  };

  const chat = new Chat(companion as any, { fsPath: '/ext', scheme: 'file' } as any);
  const posted: any[] = [];
  (chat as any).post = (m: any) => posted.push(m);
  (chat as any).draw = () => {};

  // Attach a ref
  chat.attach([{ label: 'foo.ts', fileNav: { path: 'src/foo.ts' } } as any]);

  // fromView should complete without throwing
  await chat.fromView({ kind: 'say', text: 'Draft text to restore' });

  // Session creating promise must be cleared for retry
  assert.equal((chat as any).sessionCreating, null);

  // Posted messages must include note and compose with restored draft
  const noteMsg = posted.find((m) => m.kind === 'note' && m.text?.includes('not sent — companion daemon unreachable'));
  assert.ok(noteMsg, 'failure note must be posted');

  const composeMsg = posted.find((m) => m.kind === 'compose' && m.text === 'Draft text to restore');
  assert.ok(composeMsg, 'draft must be restored to composer');

  // Ref was restored to this.refs
  assert.equal((chat as any).refs.length, 1);
  assert.equal((chat as any).refs[0].label, 'foo.ts');
});

test('Chat B0: consecutive sends in empty session followed by screen switch before creation resolves routes both to created session (submit -> steer)', async () => {
  const companion = createMockCompanion();
  let resolveSessionNew!: (val: any) => void;

  companion.ask = async (door: string, payload?: any): Promise<any> => {
    companion.calls.push({ door, payload });
    if (door === 'session-new') {
      return new Promise((res) => { resolveSessionNew = res; });
    }
    return { ok: true };
  };

  const chat = new Chat(companion as any, { fsPath: '/ext', scheme: 'file' } as any);
  (chat as any).post = () => {};
  (chat as any).draw = () => {};
  (chat as any).openStream = async () => {};

  // 1. Initially on empty session
  assert.equal(chat.session, '');

  // 2. Send A and B consecutively while session is empty
  const p1 = chat.fromView({ kind: 'say', text: 'Message A' });
  const p2 = chat.fromView({ kind: 'say', text: 'Message B' });

  // 3. Before session-new completes, user switches screen to 'sess-switched'
  chat.showSession('sess-switched');
  assert.equal(chat.session, 'sess-switched');

  // 4. Now session-new completes with 'sess-created'
  resolveSessionNew({ session: 'sess-created' });

  await Promise.all([p1, p2]);

  // 5. Active view session was not hijacked by late session-new
  assert.equal(chat.session, 'sess-switched');

  // 6. session-new was called only once
  const sessionNewCalls = companion.calls.filter((c) => c.door === 'session-new');
  assert.equal(sessionNewCalls.length, 1, 'session-new must only be called once');

  // 7. Both messages targeted 'sess-created', first as submit, second as steer
  const submitCall = companion.calls.find((c) => c.door === 'submit');
  assert.equal(submitCall?.payload?.session, 'sess-created', 'first message must target created session');
  assert.equal(submitCall?.payload?.text, 'Message A');

  const steerCall = companion.calls.find((c) => c.door === 'steer');
  assert.equal(steerCall?.payload?.session, 'sess-created', 'second message must target created session via steer');
  assert.equal(steerCall?.payload?.text, 'Message B');

  // 8. Absolutely no calls targeted 'sess-switched'
  const switchedCalls = companion.calls.filter((c) => c.payload?.session === 'sess-switched');
  assert.equal(switchedCalls.length, 0, 'no message must leak to switched session');
});

test('Chat B0 / §4.5: reply 전송 대기 중 화면 이동 및 구 웹뷰 응답 세션 격리 검증', async () => {
  const companion = createMockCompanion();
  let resolveAnswer!: (val: any) => void;

  companion.ask = async (door: string, payload?: any): Promise<any> => {
    companion.calls.push({ door, payload });
    if (door === 'answer') {
      return new Promise((res) => { resolveAnswer = res; });
    }
    return { ok: true };
  };

  const chat = new Chat(companion as any, { fsPath: '/ext', scheme: 'file' } as any);
  const mockView = createMockWebviewView();
  (chat as any).openStream = async () => {};
  chat.resolveWebviewView(mockView.view as any);

  // 1. Initial session S1
  chat.showSession('sess-1');
  assert.equal(chat.session, 'sess-1');

  // 2. Webview sends reply for sess-1 with generation 1 and webviewId view-1
  const replyPromise = chat.fromView({
    kind: 'reply',
    callId: 'q-s1',
    text: 'sess-1에 대한 답변',
    attemptId: 42,
    session: 'sess-1',
    companionKey: '/workspace',
    generation: 1,
    webviewId: 'view-1',
  }, 'view-1');

  // 3. While answer call is still in-flight, user switches screen to sess-2
  chat.showSession('sess-2');
  assert.equal(chat.session, 'sess-2');

  // 4. Now answer resolves
  resolveAnswer({ ok: true });
  await replyPromise;

  // 5. Verify ask('answer') targeted sess-1
  const answerCalls = companion.calls.filter((c) => c.door === 'answer');
  assert.equal(answerCalls.length, 1);
  assert.equal(answerCalls[0].payload.session, 'sess-1');
  assert.equal(answerCalls[0].payload.callId, 'q-s1');
  assert.equal(answerCalls[0].payload.answer, 'sess-1에 대한 답변');

  // 6. Verify posted replyResult carries sess-1, generation 1, attemptId 42, webviewId view-1
  const replyResultMsg = mockView.messages.find((m: any) => m.kind === 'replyResult');
  assert.ok(replyResultMsg, 'replyResult must be posted');
  assert.equal(replyResultMsg.callId, 'q-s1');
  assert.equal(replyResultMsg.attemptId, 42);
  assert.equal(replyResultMsg.session, 'sess-1');
  assert.equal(replyResultMsg.companionKey, '/workspace');
  assert.equal(replyResultMsg.generation, 1);
  assert.equal(replyResultMsg.webviewId, 'view-1');
  assert.equal(replyResultMsg.ok, true);
});

test('Chat §4.5 Item 2: 누락·잘못된 문맥·다른 컴패니언·옛 웹뷰 요청은 상태 불변 및 RPC 0회', async () => {
  const companion = createMockCompanion();
  companion.ask = async (door: string, payload?: any): Promise<any> => {
    companion.calls.push({ door, payload });
    return { ok: true };
  };

  const chat = new Chat(companion as any, { fsPath: '/ext', scheme: 'file' } as any);
  const mockView = createMockWebviewView();
  (chat as any).openStream = async () => {};
  chat.resolveWebviewView(mockView.view as any); // currentWebviewId = 'view-1'
  chat.showSession('sess-1');

  // Case 1: 누락된 session
  await chat.fromView({ kind: 'reply', callId: 'q1', text: 'ans', attemptId: 1, companionKey: '/workspace', generation: 1, webviewId: 'view-1' }, 'view-1');
  // Case 2: 누락된 webviewId
  await chat.fromView({ kind: 'reply', callId: 'q1', text: 'ans', attemptId: 1, companionKey: '/workspace', session: 'sess-1', generation: 1 }, 'view-1');
  // Case 3: 다른 companionKey
  await chat.fromView({ kind: 'reply', callId: 'q1', text: 'ans', attemptId: 1, companionKey: '/wrong/path', session: 'sess-1', generation: 1, webviewId: 'view-1' }, 'view-1');
  // Case 4: 오래된 webviewId (view-0 vs current view-1)
  await chat.fromView({ kind: 'reply', callId: 'q1', text: 'ans', attemptId: 1, companionKey: '/workspace', session: 'sess-1', generation: 1, webviewId: 'view-0' }, 'view-0');
  // Case 5: attemptId <= 0
  await chat.fromView({ kind: 'reply', callId: 'q1', text: 'ans', attemptId: 0, companionKey: '/workspace', session: 'sess-1', generation: 1, webviewId: 'view-1' }, 'view-1');

  const answerCalls = companion.calls.filter((c) => c.door === 'answer');
  assert.equal(answerCalls.length, 0, 'all mismatched/malformed replies must yield 0 RPC calls');
});

test('Chat §4.5 Item 1: 실제 resolveWebviewView V1 -> V2 호출 시 V1 늦은 응답 격리 및 V2 불변 검증', async () => {
  const companion = createMockCompanion();
  let resolveV1Answer!: (val: any) => void;

  companion.ask = async (door: string, payload?: any): Promise<any> => {
    companion.calls.push({ door, payload });
    if (door === 'answer') {
      return new Promise((res) => { resolveV1Answer = res; });
    }
    return { ok: true };
  };

  const chat = new Chat(companion as any, { fsPath: '/ext', scheme: 'file' } as any);
  (chat as any).openStream = async () => {};

  // 1. Resolve V1 (webviewId = 'view-1')
  const mockV1 = createMockWebviewView();
  chat.resolveWebviewView(mockV1.view as any);
  chat.showSession('sess-1');

  // 2. V1 sends reply (attempt 1) -> starts in-flight RPC
  const v1ReplyPromise = chat.fromView({
    kind: 'reply',
    callId: 'q-common',
    text: 'V1 답변',
    attemptId: 1,
    companionKey: '/workspace',
    session: 'sess-1',
    generation: 1,
    webviewId: 'view-1',
  }, 'view-1');

  // 3. Webview recreated: Resolve V2 (webviewId = 'view-2')
  const mockV2 = createMockWebviewView();
  chat.resolveWebviewView(mockV2.view as any);

  // V1 listener trying to send message is rejected before RPC
  const initialCallCount = companion.calls.length;
  mockV1.listeners[0]({
    kind: 'reply',
    callId: 'q-common',
    text: 'V1 늦은 새 메시지',
    attemptId: 2,
    companionKey: '/workspace',
    session: 'sess-1',
    generation: 1,
    webviewId: 'view-1',
  });
  assert.equal(companion.calls.length, initialCallCount, 'V1 listener must be rejected before RPC');

  // 4. Now V1 in-flight answer RPC resolves with failure
  resolveV1Answer({ error: 'rpc failed' });
  await v1ReplyPromise;

  // 5. V2 must remain completely unaffected (no replyResult or note posted to V2)
  const v2ReplyResults = mockV2.messages.filter((m: any) => m.kind === 'replyResult');
  assert.equal(v2ReplyResults.length, 0, 'V1 RPC completion must not post replyResult to V2');
  const v2Notes = mockV2.messages.filter((m: any) => m.kind === 'note');
  assert.equal(v2Notes.length, 0, 'V1 failure must not post note to V2');
});

test('Chat §4.5 Item 3: session-new 성공 시 sessionCreated 이벤트 발행 검증', async () => {
  const companion = createMockCompanion();
  let resolveSessionNew!: (val: any) => void;

  companion.ask = async (door: string, payload?: any): Promise<any> => {
    companion.calls.push({ door, payload });
    if (door === 'session-new') {
      return new Promise((res) => { resolveSessionNew = res; });
    }
    return { ok: true };
  };

  const chat = new Chat(companion as any, { fsPath: '/ext', scheme: 'file' } as any);
  const mockView = createMockWebviewView();
  (chat as any).openStream = async () => {};
  chat.resolveWebviewView(mockView.view as any);

  const sayPromise = chat.fromView({ kind: 'say', text: '새 작업 시작' }, 'view-1');
  resolveSessionNew({ session: 'sess-new-42' });
  await sayPromise;

  const sessionCreatedMsg = mockView.messages.find((m: any) => m.kind === 'sessionCreated');
  assert.ok(sessionCreatedMsg, 'sessionCreated must be posted on session-new resolution');
  assert.equal(sessionCreatedMsg.session, 'sess-new-42');
  assert.equal(sessionCreatedMsg.companionKey, '/workspace');
  assert.ok(sessionCreatedMsg.creationTaskId, 'creationTaskId must be provided');
  assert.equal(sessionCreatedMsg.webviewId, 'view-1');
});

test('Chat §4.5 Item 2: 끝 공백을 가진 companionKey(/workspace/dir )로 rows -> reply -> 호스트 RPC -> replyResult 원본 왕복 검증', async () => {
  const workdirWithSpace = '/workspace/dir ';
  const sessionWithSpace = 'sess-trailing ';
  const callIdWithSpace = 'q-trailing ';

  const companion = createMockCompanion();
  companion.workdir = workdirWithSpace;

  const chat = new Chat(companion as any, { fsPath: '/ext', scheme: 'file' } as any);
  const mockView = createMockWebviewView();
  (chat as any).openStream = async () => {};
  chat.resolveWebviewView(mockView.view as any);
  chat.showSession(sessionWithSpace);

  // reply 전송 (식별자에 끝 공백 포함)
  await chat.fromView({
    kind: 'reply',
    callId: callIdWithSpace,
    text: '답변 내용',
    attemptId: 10,
    companionKey: workdirWithSpace,
    session: sessionWithSpace,
    generation: 1,
    webviewId: 'view-1',
  }, 'view-1');

  // 1. 호스트 RPC 1회 호출 검증
  const answerCalls = companion.calls.filter((c) => c.door === 'answer');
  assert.equal(answerCalls.length, 1, 'trailing whitespace in companionKey must not block RPC');
  assert.equal(answerCalls[0].payload.session, sessionWithSpace);
  assert.equal(answerCalls[0].payload.callId, callIdWithSpace);
  assert.equal(answerCalls[0].payload.answer, '답변 내용');

  // 2. replyResult에 원래 companionKey, session, callId가 원본 그대로 반환되는지 검증
  const replyResultMsg = mockView.messages.find((m: any) => m.kind === 'replyResult');
  assert.ok(replyResultMsg, 'replyResult must be posted');
  assert.equal(replyResultMsg.ok, true);
  assert.equal(replyResultMsg.companionKey, workdirWithSpace, 'original companionKey with trailing space must be preserved');
  assert.equal(replyResultMsg.session, sessionWithSpace, 'original session must be preserved');
  assert.equal(replyResultMsg.callId, callIdWithSpace, 'original callId must be preserved');
  assert.equal(replyResultMsg.webviewId, 'view-1');
});

test('Chat §4.5 Item 1: V1 session-new 대기 중 V2 전환 시 V1 sessionCreated 차단 검증', async () => {
  const companion = createMockCompanion();
  let resolveSessionNew!: (val: any) => void;

  companion.ask = async (door: string, payload?: any): Promise<any> => {
    companion.calls.push({ door, payload });
    if (door === 'session-new') {
      return new Promise((res) => { resolveSessionNew = res; });
    }
    return { ok: true };
  };

  const chat = new Chat(companion as any, { fsPath: '/ext', scheme: 'file' } as any);
  (chat as any).openStream = async () => {};

  // 1. V1 연결 및 생성 시작
  const mockV1 = createMockWebviewView();
  chat.resolveWebviewView(mockV1.view as any); // currentWebviewId = 'view-1'
  const sayPromise = chat.fromView({ kind: 'say', text: 'V1 작업 시작', creationTaskId: 'create-v1' }, 'view-1');

  // 2. 생성이 대기 중인 상태에서 V2 연결 (웹뷰 재생성)
  const mockV2 = createMockWebviewView();
  chat.resolveWebviewView(mockV2.view as any); // currentWebviewId = 'view-2'

  // 3. 이제 V1의 session-new가 성공 응답을 수신
  resolveSessionNew({ session: 'sess-new-for-v1' });
  await sayPromise;

  // 4. V2에는 V1의 sessionCreated가 유출되지 않아야 함 (§4.5 Item 1)
  const v2SessionCreated = mockV2.messages.filter((m: any) => m.kind === 'sessionCreated');
  assert.equal(v2SessionCreated.length, 0, 'sessionCreated from old view must not be posted to new view');

  // 5. V1에도 view 교체로 인해 전달되지 않음
  const v1SessionCreated = mockV1.messages.filter((m: any) => m.kind === 'sessionCreated');
  assert.equal(v1SessionCreated.length, 0, 'sessionCreated must not be posted to replaced view');
});


