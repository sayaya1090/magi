import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as path from 'node:path';
import { createAnswerState } from '../core/answer_state';
import {
  createWebviewInputAdapter,
  createSuggestController,
  WebviewActionAdapter,
  WebviewInputElements,
} from '../web/chat_adapter';

/**
 * VS Code 공통 계약 fixture 테스트 실행기 (§6.38)
 *
 * `clients/test-fixtures/`의 단일 원본 JSON 파일 5종을 직접 읽어 실행합니다.
 * 순수 모델 계층과 실제 호스트 계층(WebviewInputAdapter/ActionAdapter) 양방향을 검증합니다.
 */

const FIXTURES_DIR = path.resolve(__dirname, '../../../test-fixtures');

interface FixtureStep {
  step: number;
  action: string;
  session?: string;
  callId?: string;
  questionText?: string;
  text?: string;
  attemptKey?: string;
  ok?: boolean;
  error?: string;
  target?: string;
  token?: string;
  assert?: {
    currentSession?: string;
    currentCallId?: string;
    inFlight?: boolean;
    inputText?: string;
    recoveriesCount?: number;
    hasRecoveryForText?: string;
    isDone?: boolean;
    version?: number;
    isProgrammaticRestore?: boolean;
    isClosed?: boolean;
    uiSideEffects?: number;
    inputChanges?: number;
    documentsOpened?: number;
    newRequests?: number;
    canSubmit?: boolean;
  };
}

interface FixtureData {
  id: string;
  title: string;
  description: string;
  steps: FixtureStep[];
}

const VALID_ACTIONS = new Set([
  'switchSession',
  'userEdit',
  'submit',
  'result',
  'deleteRecovery',
  'restore',
  'dispose',
  'selectionCallback',
]);

const VALID_ASSERT_KEYS = new Set([
  'currentSession',
  'currentCallId',
  'inFlight',
  'inputText',
  'recoveriesCount',
  'hasRecoveryForText',
  'isDone',
  'version',
  'isProgrammaticRestore',
  'isClosed',
  'uiSideEffects',
  'inputChanges',
  'documentsOpened',
  'newRequests',
  'canSubmit',
]);

function loadFixture(filename: string): FixtureData {
  const filePath = path.join(FIXTURES_DIR, filename);
  assert.ok(fs.existsSync(filePath), `Fixture file not found: ${filePath}`);
  const content = fs.readFileSync(filePath, 'utf-8');
  return JSON.parse(content) as FixtureData;
}

// -------------------------------------------------------------
// 1. 순수 모델 실행기 (AnswerStateManager & RecoveryStateManager)
// -------------------------------------------------------------
function runPureModelScenario(fixture: FixtureData): void {
  const scenarioId = fixture.id;
  const mgr = createAnswerState();
  const companionKey = 'test-comp';

  let currentSession = '';
  let currentCallId = '';
  let currentInputText = '';
  let isClosed = false;
  let inputChanges = 0;
  let documentsOpened = 0;
  let newRequests = 0;
  let isProgrammaticRestore = false;
  const attemptMap = new Map<string, { callId: string; attemptId: number; text: string; session: string }>();

  for (const step of fixture.steps) {
    const stepNum = step.step;
    const action = step.action;

    if (!VALID_ACTIONS.has(action)) {
      assert.fail(`[${scenarioId}] Step ${stepNum}: unsupported action '${action}'`);
    }

    switch (action) {
      case 'switchSession': {
        if (isClosed) {
          inputChanges++;
        } else {
          currentSession = step.session || '';
          const res = mgr.switchContext(companionKey, currentSession, { currentInputText });
          currentInputText = res.nextInputText;

          if (step.callId) {
            currentCallId = step.callId;
            mgr.onAskChange(
              {
                kind: 'question',
                callId: step.callId,
                what: step.questionText || step.callId,
              },
              currentInputText
            );
            const modeRes = mgr.enterAnswerMode(
              step.callId,
              step.questionText || step.callId,
              currentInputText
            );
            if (modeRes.nextInputText !== undefined) {
              currentInputText = modeRes.nextInputText;
            }
          } else {
            currentCallId = '';
          }
        }
        break;
      }
      case 'userEdit': {
        if (isClosed) {
          inputChanges++;
        } else {
          const text = step.text || '';
          currentInputText = text;
          mgr.onInputChange(text);
          isProgrammaticRestore = false;
        }
        break;
      }
      case 'submit': {
        const attemptKey = step.attemptKey;
        if (!attemptKey) {
          assert.fail(`[${scenarioId}] Step ${stepNum}: submit requires non-empty attemptKey`);
        }
        if (isClosed) {
          newRequests++;
        } else {
          const callId = step.callId || currentCallId;
          const sub = mgr.submitReply(callId, currentInputText);
          if (!sub.ok || !sub.attemptId) {
            assert.fail(`[${scenarioId}] Step ${stepNum}: submitReply failed for callId '${callId}'`);
          }
          attemptMap.set(attemptKey, { callId, attemptId: sub.attemptId, text: currentInputText, session: currentSession });
        }
        break;
      }
      case 'result': {
        const attemptKey = step.attemptKey;
        if (!attemptKey) {
          assert.fail(`[${scenarioId}] Step ${stepNum}: result requires attemptKey`);
        }
        const att = attemptMap.get(attemptKey);
        if (!att) {
          assert.fail(`[${scenarioId}] Step ${stepNum}: unknown attemptKey '${attemptKey}'`);
        }
        const ok = step.ok ?? false;
        const error = step.error;

        if (isClosed) {
          // Dispose된 상태의 콜백은 무시되어야 함
        } else {
          const out = mgr.onReplyResult({
            callId: att.callId,
            attemptId: att.attemptId,
            ok,
            error,
            companionKey,
            session: att.session,
          });
          if (out.nextInputText !== undefined) {
            currentInputText = out.nextInputText;
          }
        }
        break;
      }
      case 'deleteRecovery': {
        if (isClosed) {
          inputChanges++;
        } else {
          const target = step.target || 'last';
          const recs = mgr.getRecoveryState().listItems({ companionKey, sessionId: currentSession });
          const targetItem = target === 'first' ? recs[0] : recs[recs.length - 1];
          if (!targetItem) {
            assert.fail(`[${scenarioId}] Step ${stepNum}: no recovery item to delete`);
          }
          const deleted = mgr.getRecoveryState().deleteItem(targetItem.recoveryId);
          assert.ok(deleted, `[${scenarioId}] Step ${stepNum}: failed to delete recovery item ${targetItem.recoveryId}`);
        }
        break;
      }
      case 'restore': {
        if (isClosed) {
          inputChanges++;
        } else {
          const text = step.text || '';
          currentInputText = text;
          isProgrammaticRestore = true;
        }
        break;
      }
      case 'dispose': {
        isClosed = true;
        break;
      }
      case 'selectionCallback': {
        if (isClosed) {
          // Dispose된 상태의 선택 콜백 무시
        } else {
          const token = step.token;
          if (token) {
            currentInputText += token;
            mgr.onInputChange(currentInputText);
          }
        }
        break;
      }
    }

    // Assertions
    if (step.assert) {
      const ass = step.assert;
      const unhandledKeys = new Set(Object.keys(ass));
      for (const k of Object.keys(ass)) {
        if (!VALID_ASSERT_KEYS.has(k)) {
          assert.fail(`[${scenarioId}] Step ${stepNum}: unknown assertion key '${k}'`);
        }
      }

      const check = (field: string, expected: unknown, actual: unknown) => {
        assert.deepEqual(
          actual,
          expected,
          `[${scenarioId}] Step ${stepNum} assertion failed for '${field}': expected ${JSON.stringify(expected)}, but was ${JSON.stringify(actual)}`
        );
      };

      if (ass.currentSession !== undefined) {
        check('currentSession', ass.currentSession, currentSession);
        unhandledKeys.delete('currentSession');
      }
      if (ass.currentCallId !== undefined) {
        check('currentCallId', ass.currentCallId, currentCallId);
        unhandledKeys.delete('currentCallId');
      }
      if (ass.inFlight !== undefined) {
        const inFlight = currentCallId ? mgr.isInFlight(currentCallId, companionKey, currentSession) : false;
        check('inFlight', ass.inFlight, inFlight);
        unhandledKeys.delete('inFlight');
      }
      if (ass.inputText !== undefined) {
        check('inputText', ass.inputText, currentInputText);
        unhandledKeys.delete('inputText');
      }
      if (ass.recoveriesCount !== undefined) {
        const recs = mgr.getRecoveryState().listItems({ companionKey, sessionId: currentSession });
        check('recoveriesCount', ass.recoveriesCount, recs.length);
        unhandledKeys.delete('recoveriesCount');
      }
      if (ass.hasRecoveryForText !== undefined) {
        const recs = mgr.getRecoveryState().listItems({ companionKey, sessionId: currentSession });
        const has = recs.some((r) => r.text === ass.hasRecoveryForText);
        check('hasRecoveryForText', true, has);
        unhandledKeys.delete('hasRecoveryForText');
      }
      if (ass.isDone !== undefined) {
        const inFlight = currentCallId ? mgr.isInFlight(currentCallId, companionKey, currentSession) : false;
        const qDraft = currentCallId ? mgr.getQuestionDraft(currentCallId, companionKey, currentSession) : '';
        const isDone = !inFlight && qDraft === '';
        check('isDone', ass.isDone, isDone);
        unhandledKeys.delete('isDone');
      }
      if (ass.version !== undefined) {
        const ver = currentCallId ? mgr.getDraftVersion(currentCallId, companionKey, currentSession) : 0;
        check('version', ass.version, ver);
        unhandledKeys.delete('version');
      }
      if (ass.isProgrammaticRestore !== undefined) {
        check('isProgrammaticRestore', ass.isProgrammaticRestore, isProgrammaticRestore);
        unhandledKeys.delete('isProgrammaticRestore');
      }
      if (ass.isClosed !== undefined) {
        check('isClosed', ass.isClosed, isClosed);
        unhandledKeys.delete('isClosed');
      }
      if (ass.inputChanges !== undefined) {
        check('inputChanges', ass.inputChanges, inputChanges);
        unhandledKeys.delete('inputChanges');
      }
      if (ass.documentsOpened !== undefined) {
        check('documentsOpened', ass.documentsOpened, documentsOpened);
        unhandledKeys.delete('documentsOpened');
      }
      if (ass.newRequests !== undefined) {
        check('newRequests', ass.newRequests, newRequests);
        unhandledKeys.delete('newRequests');
      }
      if (ass.uiSideEffects !== undefined) {
        const actualEffects = inputChanges + documentsOpened + newRequests;
        check('uiSideEffects', ass.uiSideEffects, actualEffects);
        unhandledKeys.delete('uiSideEffects');
      }
      if (ass.canSubmit !== undefined) {
        const inFlight = currentCallId ? mgr.isInFlight(currentCallId, companionKey, currentSession) : false;
        const canSubmit = !isClosed && !inFlight;
        check('canSubmit', ass.canSubmit, canSubmit);
        unhandledKeys.delete('canSubmit');
      }

      if (unhandledKeys.size > 0) {
        assert.fail(
          `[${scenarioId}] Step ${stepNum}: unhandled assertion keys in pure model runner: ${Array.from(unhandledKeys).join(', ')}`
        );
      }
    }
  }
}

// -------------------------------------------------------------
// 2. 실제 호스트 실행기 (WebviewInputAdapter / ActionAdapter)
// -------------------------------------------------------------
function runHostScenario(fixture: FixtureData): void {
  const scenarioId = fixture.id;
  const state = createAnswerState();
  const companionKey = 'test-comp';

  let documentsOpened = 0;
  let newRequests = 0;

  let inputListener: (() => void) | null = null;
  const say = {
    value: '',
    placeholder: '',
    focus() {},
    setSelectionRange() {},
    ownerDocument: null,
    addEventListener(evt: string, cb: any) {
      if (evt === 'input') inputListener = cb;
    },
    removeEventListener(evt: string, cb: any) {
      if (evt === 'input' && inputListener === cb) inputListener = null;
    },
  };
  const sendBtn = { textContent: '', disabled: false, addEventListener() {}, removeEventListener() {} };
  const replyModeEl = { hidden: true };
  const replyTargetEl = { textContent: '' };
  const replyCancelEl = { addEventListener() {}, removeEventListener() {} };
  const noteEl = { textContent: '' };
  const hintEl = { textContent: '' };

  const actions: WebviewActionAdapter = {
    openFile: () => { documentsOpened++; return true; },
    openDiff: () => { documentsOpened++; return true; },
    openOutput: () => { documentsOpened++; return true; },
    answer: () => { newRequests++; return true; },
    reply: () => { newRequests++; return true; },
    say: () => { newRequests++; return true; },
    act: () => { newRequests++; return true; },
    suggest: () => { newRequests++; return true; },
    mention: () => { newRequests++; return true; },
    drop: () => {},
    start: () => {},
    ready: () => {},
  };

  const suggestCtrl = createSuggestController();
  const elements: WebviewInputElements = {
    say: say as any,
    sendBtn: sendBtn as any,
    replyModeEl: replyModeEl as any,
    replyTargetEl: replyTargetEl as any,
    replyCancelEl: replyCancelEl as any,
    noteEl: noteEl as any,
    hintEl: hintEl as any,
  };

  const adapter = createWebviewInputAdapter(elements, actions, state, suggestCtrl);

  // 대조군(Control): 정상 생존 상태에서 제안·멘션 및 선택 콜백이 실제 적용됨을 사전 검증
  adapter.onContextChange(companionKey, 'ctrl-sess');
  say.value = 'Control Draft';
  state.onInputChange('Control Draft');
  const ctrlReqId = suggestCtrl.getReqId();
  adapter.handleMentions(['file1.txt', 'file2.txt'], ctrlReqId, 'general');
  assert.equal(hintEl.textContent, 'files: file1.txt  file2.txt', 'Control mentions must update hintEl');
  assert.deepEqual(suggestCtrl.getMentions(), ['file1.txt', 'file2.txt'], 'Control mentions must update suggestCtrl');
  adapter.handleSuggestion('suggested_cmd', ctrlReqId, 'general');
  assert.equal(hintEl.textContent, 'Tab: suggested_cmd', 'Control suggestion must update hintEl');
  say.value = '';
  adapter.handleCompose('Control Draft file1.txt');
  assert.equal(say.value, 'Control Draft file1.txt', 'Control compose must update say.value');

  // 리셋
  say.value = '';
  hintEl.textContent = '';
  suggestCtrl.clearSuggestion();

  let currentSession = '';
  let currentCallId = '';
  let isClosed = false;
  let postDisposeInputChanges = 0;
  let postDisposeNewRequests = 0;
  let postDisposeDocsOpened = 0;
  const attemptMap = new Map<string, { callId: string; attemptId: number; text: string; session: string }>();

  for (const step of fixture.steps) {
    const stepNum = step.step;
    const action = step.action;

    if (!VALID_ACTIONS.has(action)) {
      assert.fail(`[${scenarioId}] Step ${stepNum}: unsupported action '${action}'`);
    }

    switch (action) {
      case 'switchSession': {
        currentSession = step.session || '';
        currentCallId = step.callId || '';
        adapter.onContextChange(
          companionKey,
          currentSession,
          currentCallId ? ({ kind: 'question', callId: currentCallId } as any) : null,
          0,
          'view-1'
        );
        if (currentCallId) {
          adapter.enterAnswerMode(currentCallId, step.questionText || currentCallId);
        }
        break;
      }
      case 'userEdit': {
        const text = step.text || '';
        say.value = text;
        const cb = inputListener as (() => void) | null;
        if (cb) {
          cb();
        } else {
          state.onInputChange(text);
        }
        if (isClosed) {
          postDisposeInputChanges++;
        }
        break;
      }
      case 'restore': {
        const text = step.text || '';
        // 제품의 실제 프로그램 복원 경로 호출 (DOM 직접 렌더링, onInputChange 미호출)
        adapter.applyAnswerModeUI(currentCallId, text);
        if (isClosed) {
          postDisposeInputChanges++;
        }
        break;
      }
      case 'submit': {
        const attemptKey = step.attemptKey;
        if (!attemptKey || typeof attemptKey !== 'string') {
          assert.fail(`[${scenarioId}] Step ${stepNum}: submit requires non-empty attemptKey`);
        }
        const callId = step.callId || currentCallId;
        const sub = state.submitReply(callId, say.value);
        if (!sub.ok || !sub.attemptId) {
          assert.fail(`[${scenarioId}] Step ${stepNum}: submitReply failed`);
        }
        attemptMap.set(attemptKey, { callId, attemptId: sub.attemptId, text: say.value, session: currentSession });
        actions.reply(callId, say.value, sub.attemptId, {
          companionKey,
          session: currentSession,
          generation: 0,
          webviewId: 'view-1',
        });
        break;
      }
      case 'dispose': {
        // 실제 어댑터 및 서제스트 컨트롤러 dispose
        adapter.dispose();
        suggestCtrl.dispose();
        isClosed = true;
        break;
      }
      case 'result': {
        const attemptKey = step.attemptKey;
        if (!attemptKey || typeof attemptKey !== 'string') {
          assert.fail(`[${scenarioId}] Step ${stepNum}: result requires non-empty attemptKey`);
        }
        const att = attemptMap.get(attemptKey);
        if (!att) {
          assert.fail(`[${scenarioId}] Step ${stepNum}: unknown attemptKey '${attemptKey}'`);
        }
        if (typeof step.ok !== 'boolean') {
          assert.fail(`[${scenarioId}] Step ${stepNum}: result requires boolean 'ok'`);
        }
        const prevSay = say.value;
        const prevRequests = newRequests;
        const prevDocs = documentsOpened;

        // Dispose된 어댑터에 늦은 완료 콜백 주입 (ok 및 attemptKey 동적 소비)
        adapter.handleReplyResult(
          {
            callId: att.callId,
            attemptId: att.attemptId,
            ok: step.ok,
            error: step.error,
            companionKey,
            session: att.session,
            generation: 0,
            webviewId: 'view-1',
          },
          null
        );

        if (isClosed) {
          if (say.value !== prevSay) postDisposeInputChanges++;
          if (newRequests !== prevRequests) postDisposeNewRequests++;
          if (documentsOpened !== prevDocs) postDisposeDocsOpened++;
        }
        break;
      }
      case 'selectionCallback': {
        const prevSay = say.value;
        const prevRequests = newRequests;
        const prevDocs = documentsOpened;
        const prevHint = hintEl.textContent;

        // Dispose된 서제스트 컨트롤러/어댑터에 늦은 선택/제안/컴포즈 콜백 주입
        const curReqId = suggestCtrl.getReqId();
        adapter.handleSuggestion(step.token || 'suggested', curReqId, currentCallId || 'general');
        adapter.handleMentions([step.token || 'file1.txt'], curReqId, currentCallId || 'general');
        adapter.handleCompose(step.token || 'file1.txt');

        if (isClosed) {
          if (say.value !== prevSay) postDisposeInputChanges++;
          if (newRequests !== prevRequests) postDisposeNewRequests++;
          if (documentsOpened !== prevDocs) postDisposeDocsOpened++;
          if (hintEl.textContent !== prevHint) postDisposeInputChanges++;
        }
        break;
      }
      default:
        assert.fail(`[${scenarioId}] Step ${stepNum}: unhandled action in host runner '${action}'`);
    }

    if (step.assert) {
      const ass = step.assert;
      const unhandledKeys = new Set(Object.keys(ass));
      for (const k of Object.keys(ass)) {
        if (!VALID_ASSERT_KEYS.has(k)) {
          assert.fail(`[${scenarioId}] Step ${stepNum}: unknown assertion key '${k}'`);
        }
      }

      const check = (field: string, expected: unknown, actual: unknown) => {
        assert.deepEqual(
          actual,
          expected,
          `[${scenarioId}] Step ${stepNum} host assertion failed for '${field}': expected ${JSON.stringify(expected)}, but was ${JSON.stringify(actual)}`
        );
      };

      if (ass.currentSession !== undefined) {
        check('currentSession', ass.currentSession, currentSession);
        unhandledKeys.delete('currentSession');
      }
      if (ass.currentCallId !== undefined) {
        check('currentCallId', ass.currentCallId, currentCallId);
        unhandledKeys.delete('currentCallId');
      }
      if (ass.inFlight !== undefined) {
        const inFlight = currentCallId ? state.isInFlight(currentCallId, companionKey, currentSession) : false;
        check('inFlight', ass.inFlight, inFlight);
        unhandledKeys.delete('inFlight');
      }
      if (ass.inputText !== undefined) {
        check('inputText', ass.inputText, say.value);
        unhandledKeys.delete('inputText');
      }
      if (ass.isProgrammaticRestore !== undefined) {
        // 호스트에서 restore 액션은 applyAnswerModeUI를 호출하여 사용자 입력 이벤트 없이 직접 반영됨
        check('isProgrammaticRestore', ass.isProgrammaticRestore, action === 'restore');
        unhandledKeys.delete('isProgrammaticRestore');
      }
      if (ass.version !== undefined) {
        const ver = state.getDraftVersion(currentCallId, companionKey, currentSession);
        check('version', ass.version, ver);
        unhandledKeys.delete('version');
      }
      if (ass.isClosed !== undefined) {
        check('isClosed', ass.isClosed, isClosed);
        unhandledKeys.delete('isClosed');
      }
      if (ass.inputChanges !== undefined) {
        check('inputChanges', ass.inputChanges, postDisposeInputChanges);
        unhandledKeys.delete('inputChanges');
      }
      if (ass.documentsOpened !== undefined) {
        check('documentsOpened', ass.documentsOpened, postDisposeDocsOpened);
        unhandledKeys.delete('documentsOpened');
      }
      if (ass.newRequests !== undefined) {
        check('newRequests', ass.newRequests, postDisposeNewRequests);
        unhandledKeys.delete('newRequests');
      }
      if (ass.uiSideEffects !== undefined) {
        const actual = postDisposeInputChanges + postDisposeDocsOpened + postDisposeNewRequests;
        check('uiSideEffects', ass.uiSideEffects, actual);
        unhandledKeys.delete('uiSideEffects');
      }
      if (ass.canSubmit !== undefined) {
        // 실제 전송 API 호출 시도: 종료된 어댑터는 신규 요청을 전송하지 않아야 함
        const reqsBefore = newRequests;
        adapter.send();
        const actualCanSubmit = newRequests > reqsBefore;
        check('canSubmit', ass.canSubmit, actualCanSubmit);
        unhandledKeys.delete('canSubmit');
      }

      if (unhandledKeys.size > 0) {
        assert.fail(
          `[${scenarioId}] Step ${stepNum}: unhandled assertion keys in host runner: ${Array.from(unhandledKeys).join(', ')}`
        );
      }
    }
  }
}

// -------------------------------------------------------------
// 테스트 정의: 순수 모델 5종 + 실제 호스트 2종 + 디렉터리 완결성
// -------------------------------------------------------------
test('§6.38 Pure Model: late_failure.json', () => {
  runPureModelScenario(loadFixture('late_failure.json'));
});

test('§6.38 Pure Model: cross_session_result.json', () => {
  runPureModelScenario(loadFixture('cross_session_result.json'));
});

test('§6.38 Pure Model: delete_recovery.json', () => {
  runPureModelScenario(loadFixture('delete_recovery.json'));
});

test('§6.38 Pure Model: same_string_edit.json', () => {
  runPureModelScenario(loadFixture('same_string_edit.json'));
});

test('§6.38 Pure Model: disposed_callback.json', () => {
  runPureModelScenario(loadFixture('disposed_callback.json'));
});

test('§6.38 Host Runner: same_string_edit.json against WebviewInputAdapter', () => {
  runHostScenario(loadFixture('same_string_edit.json'));
});

test('§6.38 Host Runner: disposed_callback.json against WebviewInputAdapter & ActionAdapter', () => {
  runHostScenario(loadFixture('disposed_callback.json'));
});

test('§6.38 Host Runner: rejects missing attemptKey on submit', () => {
  const fixture = JSON.parse(JSON.stringify(loadFixture('disposed_callback.json'))) as FixtureData;
  delete fixture.steps[2].attemptKey; // step 3 submit
  assert.throws(
    () => runHostScenario(fixture),
    (err: Error) => err.message.includes('disposed_callback') && err.message.includes('Step 3') && err.message.includes('attemptKey')
  );
});

test('§6.38 Host Runner: rejects unknown attemptKey on result', () => {
  const fixture = JSON.parse(JSON.stringify(loadFixture('disposed_callback.json'))) as FixtureData;
  fixture.steps[4].attemptKey = 'unknown_key_xyz'; // step 5 result
  assert.throws(
    () => runHostScenario(fixture),
    (err: Error) => err.message.includes('disposed_callback') && err.message.includes('Step 5') && err.message.includes('unknown_key_xyz')
  );
});

test('§6.38 Host Runner: rejects missing boolean ok on result', () => {
  const fixture = JSON.parse(JSON.stringify(loadFixture('disposed_callback.json'))) as FixtureData;
  delete fixture.steps[4].ok; // step 5 result
  assert.throws(
    () => runHostScenario(fixture),
    (err: Error) => err.message.includes('disposed_callback') && err.message.includes('Step 5') && err.message.includes("boolean 'ok'")
  );
});

test('§6.38 Directory: All 5 contract fixtures are present in test-fixtures directory', () => {
  const expectedFixtures = [
    'late_failure.json',
    'cross_session_result.json',
    'delete_recovery.json',
    'same_string_edit.json',
    'disposed_callback.json',
  ].sort();

  const files = fs
    .readdirSync(FIXTURES_DIR)
    .filter((f) => f.endsWith('.json'))
    .sort();

  assert.deepEqual(files, expectedFixtures, 'Contract fixture directory must contain exactly the 5 contract files');
});
