import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as path from 'node:path';
import { createAnswerState } from '../core/answer_state';

/**
 * VS Code 공통 계약 fixture 테스트 실행기 (§6.38)
 *
 * `clients/test-fixtures/`의 단일 원본 JSON 파일 5종을 직접 읽어 실행합니다.
 * 복사본 없이 JetBrains와 동일한 fixture 파일을 공유합니다.
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
    canSubmit?: boolean;
  };
}

interface FixtureData {
  id: string;
  title: string;
  description: string;
  steps: FixtureStep[];
}

function loadFixture(filename: string): FixtureData {
  const filePath = path.join(FIXTURES_DIR, filename);
  assert.ok(fs.existsSync(filePath), `Fixture file not found: ${filePath}`);
  const content = fs.readFileSync(filePath, 'utf-8');
  return JSON.parse(content) as FixtureData;
}

function runScenario(fixture: FixtureData): void {
  const scenarioId = fixture.id;
  const mgr = createAnswerState();
  const companionKey = 'test-comp';

  let currentSession = '';
  let currentCallId = '';
  let currentInputText = '';
  let isClosed = false;
  let uiSideEffects = 0;
  let isProgrammaticRestore = false;
  const attemptMap = new Map<string, { callId: string; attemptId: number; text: string; session: string }>();

  for (const step of fixture.steps) {
    const stepNum = step.step;
    const action = step.action;

    switch (action) {
      case 'switchSession': {
        if (isClosed) {
          uiSideEffects++;
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
          uiSideEffects++;
        } else {
          const text = step.text || '';
          currentInputText = text;
          mgr.onInputChange(text);
          isProgrammaticRestore = false;
        }
        break;
      }
      case 'submit': {
        if (isClosed) {
          uiSideEffects++;
        } else {
          const attemptKey = step.attemptKey || '';
          const callId = step.callId || currentCallId;
          const sub = mgr.submitReply(callId, currentInputText);
          if (sub.ok && sub.attemptId) {
            attemptMap.set(attemptKey, { callId, attemptId: sub.attemptId, text: currentInputText, session: currentSession });
          }
        }
        break;
      }
      case 'result': {
        const attemptKey = step.attemptKey || '';
        const ok = step.ok ?? false;
        const error = step.error;
        const att = attemptMap.get(attemptKey);

        if (isClosed) {
          // Dispose된 소유자에게 늦은 결과 콜백이 도달하더라도 UI 부작용 차단
          if (att) {
            // chat_adapter 등 호스트에서 disposed 체크로 무시됨
            // 부작용 없음
          }
        } else {
          if (att) {
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
        }
        break;
      }
      case 'deleteRecovery': {
        if (isClosed) {
          uiSideEffects++;
        } else {
          const target = step.target || 'last';
          const recs = mgr.getRecoveryState().listItems({ companionKey, sessionId: currentSession });
          const targetItem = target === 'first' ? recs[0] : recs[recs.length - 1];
          if (targetItem) {
            const deleted = mgr.getRecoveryState().deleteItem(targetItem.recoveryId);
            assert.ok(deleted, `[${scenarioId}] Step ${stepNum}: failed to delete recovery item ${targetItem.recoveryId}`);
          }
        }
        break;
      }
      case 'restore': {
        if (isClosed) {
          uiSideEffects++;
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
      default: {
        assert.fail(`[${scenarioId}] Step ${stepNum}: unsupported action '${action}'`);
      }
    }

    // Assertions
    if (step.assert) {
      const ass = step.assert;
      const check = (field: string, expected: unknown, actual: unknown) => {
        assert.deepEqual(
          actual,
          expected,
          `[${scenarioId}] Step ${stepNum} assertion failed for '${field}': expected ${JSON.stringify(expected)}, but was ${JSON.stringify(actual)}`
        );
      };

      if (ass.currentSession !== undefined) {
        check('currentSession', ass.currentSession, currentSession);
      }
      if (ass.currentCallId !== undefined) {
        check('currentCallId', ass.currentCallId, currentCallId);
      }
      if (ass.inFlight !== undefined) {
        const inFlight = currentCallId ? mgr.isInFlight(currentCallId, companionKey, currentSession) : false;
        check('inFlight', ass.inFlight, inFlight);
      }
      if (ass.inputText !== undefined) {
        check('inputText', ass.inputText, currentInputText);
      }
      if (ass.recoveriesCount !== undefined) {
        const recs = mgr.getRecoveryState().listItems({ companionKey, sessionId: currentSession });
        check('recoveriesCount', ass.recoveriesCount, recs.length);
      }
      if (ass.hasRecoveryForText !== undefined) {
        const recs = mgr.getRecoveryState().listItems({ companionKey, sessionId: currentSession });
        const has = recs.some((r) => r.text === ass.hasRecoveryForText);
        check('hasRecoveryForText', true, has);
      }
      if (ass.isDone !== undefined) {
        const inFlight = currentCallId ? mgr.isInFlight(currentCallId, companionKey, currentSession) : false;
        const qDraft = currentCallId ? mgr.getQuestionDraft(currentCallId, companionKey, currentSession) : '';
        const isDone = !inFlight && qDraft === '';
        check('isDone', ass.isDone, isDone);
      }
      if (ass.version !== undefined) {
        const ver = currentCallId ? mgr.getDraftVersion(currentCallId, companionKey, currentSession) : 0;
        check('version', ass.version, ver);
      }
      if (ass.isProgrammaticRestore !== undefined) {
        check('isProgrammaticRestore', ass.isProgrammaticRestore, isProgrammaticRestore);
      }
      if (ass.isClosed !== undefined) {
        check('isClosed', ass.isClosed, isClosed);
      }
      if (ass.uiSideEffects !== undefined) {
        check('uiSideEffects', ass.uiSideEffects, uiSideEffects);
      }
      if (ass.canSubmit !== undefined) {
        const inFlight = currentCallId ? mgr.isInFlight(currentCallId, companionKey, currentSession) : false;
        const canSubmit = !isClosed && !inFlight;
        check('canSubmit', ass.canSubmit, canSubmit);
      }
    }
  }
}

test('§6.38 Contract Fixture: late_failure.json', () => {
  runScenario(loadFixture('late_failure.json'));
});

test('§6.38 Contract Fixture: cross_session_result.json', () => {
  runScenario(loadFixture('cross_session_result.json'));
});

test('§6.38 Contract Fixture: delete_recovery.json', () => {
  runScenario(loadFixture('delete_recovery.json'));
});

test('§6.38 Contract Fixture: same_string_edit.json', () => {
  runScenario(loadFixture('same_string_edit.json'));
});

test('§6.38 Contract Fixture: disposed_callback.json', () => {
  runScenario(loadFixture('disposed_callback.json'));
});

test('§6.38 Contract Fixture: All 5 contract fixtures are present in test-fixtures directory', () => {
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
