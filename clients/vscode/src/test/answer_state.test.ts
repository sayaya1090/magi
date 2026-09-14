import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { createAnswerState } from '../core/answer_state';

test('Scenario 1: 일반 초안 작성 -> 자유 질문 도착 -> 답변 작성 -> 취소 -> 재진입 (초안 상호 보존)', () => {
  const state = createAnswerState();

  // 1. 일반 초안 작성
  const inputRes1 = state.onInputChange('일반 작성 중이던 메모');
  assert.equal(inputRes1.target, 'general');
  assert.equal(state.getGeneralDraft(), '일반 작성 중이던 메모');
  assert.equal(state.getPendingQuestion(), null);

  // 2. 자유 질문 도착 및 직접 입력 진입
  const enterRes1 = state.enterAnswerMode('q1', '어떻게 처리할까요?', state.getGeneralDraft());
  assert.equal(enterRes1.enterAnswerMode, true);
  assert.equal(enterRes1.callId, 'q1');
  assert.equal(enterRes1.label, '어떻게 처리할까요?');
  assert.equal(enterRes1.nextInputText, '');
  assert.equal(enterRes1.clearAutoCompletion, true);
  assert.equal(state.getPendingQuestion(), 'q1');
  assert.equal(state.getGeneralDraft(), '일반 작성 중이던 메모');

  // 3. 질문 답변 작성
  const inputRes2 = state.onInputChange('이렇게 수정해 주세요');
  assert.equal(inputRes2.target, 'q1');
  assert.equal(state.getQuestionDraft('q1'), '이렇게 수정해 주세요');
  assert.equal(state.getGeneralDraft(), '일반 작성 중이던 메모');

  // 4. 취소 (Esc / 취소 버튼)
  const exitRes = state.exitAnswerMode('이렇게 수정해 주세요');
  assert.equal(exitRes.exitAnswerMode, true);
  assert.equal(exitRes.nextInputText, '일반 작성 중이던 메모');
  assert.equal(exitRes.clearAutoCompletion, true);
  assert.equal(state.getPendingQuestion(), null);
  assert.equal(state.getGeneralDraft(), '일반 작성 중이던 메모');
  assert.equal(state.getQuestionDraft('q1'), '이렇게 수정해 주세요');

  // 5. 질문에 재진입
  const enterRes2 = state.enterAnswerMode('q1', '어떻게 처리할까요?', state.getGeneralDraft());
  assert.equal(enterRes2.enterAnswerMode, true);
  assert.equal(enterRes2.nextInputText, '이렇게 수정해 주세요');
  assert.equal(enterRes2.clearAutoCompletion, true);
  assert.equal(state.getPendingQuestion(), 'q1');
  assert.equal(state.getGeneralDraft(), '일반 작성 중이던 메모');
});

test('Scenario 2: A 전송 -> 답변 B 수정 -> A 실패: B와 일반 초안 보존', () => {
  const state = createAnswerState();

  state.onInputChange('보존되어야 할 일반 초안');
  state.enterAnswerMode('q1', '질문', state.getGeneralDraft());
  state.onInputChange('답변 A');

  // A 전송
  const subA = state.submitReply('q1', '답변 A', false);
  assert.equal(subA.ok, true);
  assert.equal(subA.attemptId, 1);
  assert.equal(subA.exitAnswerMode, true);
  assert.equal(subA.nextInputText, '보존되어야 할 일반 초안');
  assert.equal(subA.clearAutoCompletion, true);
  assert.equal(state.isInFlight('q1'), true);
  assert.equal(state.getPendingQuestion(), null);
  assert.equal(state.getGeneralDraft(), '보존되어야 할 일반 초안');

  // 재진입하여 새로운 수정본 B 작성 (draftVersion 증가)
  state.enterAnswerMode('q1', '질문', state.getGeneralDraft());
  state.onInputChange('답변 B (새로 작성된 초안)');
  assert.equal(state.getQuestionDraft('q1'), '답변 B (새로 작성된 초안)');
  assert.ok(state.getDraftVersion('q1') > (subA.attemptId ?? 0));

  // 이전 A의 실패 결과 도착
  const failA = state.onReplyResult(
    { callId: 'q1', attemptId: 1, ok: false, text: '답변 A' },
    { kind: 'question', callId: 'q1', what: '질문' }
  );
  assert.equal(failA.handled, true);
  assert.equal(failA.ok, false);
  // 수정된 버전이 더 높으므로 초안 B가 덮어씌워지지 않고 reenter도 발생하지 않음
  assert.notEqual(failA.reenterAnswerMode, true);
  assert.equal(state.getQuestionDraft('q1'), '답변 B (새로 작성된 초안)');
  assert.equal(state.getGeneralDraft(), '보존되어야 할 일반 초안');
  assert.deepEqual(state.getFailedDrafts('q1'), ['답변 A']);
});

test('Scenario 3: A 실패 -> B 재전송 -> A 결과 재도착: B의 잠금·초안·입력 모드 보존', () => {
  const state = createAnswerState();

  state.enterAnswerMode('q1', '질문');
  state.onInputChange('초기 답변 A');
  const subA = state.submitReply('q1', '초기 답변 A', false);
  assert.equal(subA.attemptId, 1);

  // A 실패 처리
  const failA = state.onReplyResult(
    { callId: 'q1', attemptId: 1, ok: false, text: '초기 답변 A' },
    { kind: 'question', callId: 'q1', what: '질문' }
  );
  assert.equal(failA.handled, true);
  assert.equal(failA.reenterAnswerMode, true);
  assert.equal(state.isInFlight('q1'), false);

  // 답변 B로 수정 후 재전송
  state.onInputChange('재전송 답변 B');
  const subB = state.submitReply('q1', '재전송 답변 B', false);
  assert.equal(subB.attemptId, 2);
  assert.equal(state.isInFlight('q1'), true);
  assert.equal(state.getInFlight('q1')?.attemptId, 2);
  assert.equal(state.getQuestionDraft('q1'), '재전송 답변 B');
  assert.equal(state.getPendingQuestion(), null);

  // 이전 attempt 1(A)의 지연된 결과가 뒤늦게 다시 도착
  const staleArrival = state.onReplyResult(
    { callId: 'q1', attemptId: 1, ok: false, text: '초기 답변 A' },
    { kind: 'question', callId: 'q1', what: '질문' }
  );
  // attemptId 불일치로 무시되어야 함
  assert.equal(staleArrival.handled, false);
  assert.equal(staleArrival.reason, 'mismatched_attempt_id');

  // B의 잠금, 초안, 모드가 보존되어야 함
  assert.equal(state.isInFlight('q1'), true);
  assert.equal(state.getInFlight('q1')?.attemptId, 2);
  assert.equal(state.getInFlight('q1')?.text, '재전송 답변 B');
  assert.equal(state.getQuestionDraft('q1'), '재전송 답변 B');
  assert.equal(state.getPendingQuestion(), null);
});

test('Scenario 4: 질문 교체 후 이전 요청 실패, ID 없는 응답, 중복 제출 차단', () => {
  const state = createAnswerState();

  // 1. 중복 제출 차단 검증
  state.enterAnswerMode('q1', '질문 1');
  const sub1 = state.submitReply('q1', '답변 1');
  assert.equal(sub1.ok, true);
  assert.equal(sub1.attemptId, 1);
  assert.equal(state.isInFlight('q1'), true);

  // 같은 질문 전송 중 추가 제출 시도 차단
  const dupSub = state.submitReply('q1', '중복 답변');
  assert.equal(dupSub.ok, false);
  assert.equal(dupSub.error, 'in_flight');
  assert.equal(dupSub.message, 'reply already in flight…');
  assert.equal(state.getInFlight('q1')?.attemptId, 1);

  // 선택지 클릭을 통한 중복 제출 차단도 동일하게 동작
  const dupChoice = state.submitReply('q1', '선택지 옵션', true);
  assert.equal(dupChoice.ok, false);
  assert.equal(dupChoice.error, 'in_flight');

  // 2. ID 없는 응답 무시 검증
  const invalidResult = state.onReplyResult({
    callId: 'q1',
    attemptId: undefined,
    ok: false,
    text: '실패'
  });
  assert.equal(invalidResult.handled, false);
  assert.equal(invalidResult.reason, 'missing_or_invalid_attempt_id');
  assert.equal(state.isInFlight('q1'), true);

  const stringIdResult = state.onReplyResult({
    callId: 'q1',
    attemptId: '1' as unknown,
    ok: false,
    text: '실패'
  });
  assert.equal(stringIdResult.handled, false);
  assert.equal(stringIdResult.reason, 'missing_or_invalid_attempt_id');
  assert.equal(state.isInFlight('q1'), true);

  // 3. 질문 교체 후 이전 요청 실패가 현재 입력창에 끼어들지 않는지 검증
  // 새 질문 q2 도착 및 입력 모드 진입
  state.enterAnswerMode('q2', '새 질문 2', '일반 초안');
  state.onInputChange('q2 전용 작성 중인 내용');
  assert.equal(state.getPendingQuestion(), 'q2');
  assert.equal(state.getQuestionDraft('q2'), 'q2 전용 작성 중인 내용');

  // 이전 질문 q1의 실패 응답 도착 (현재 활성 질문은 q2)
  const failQ1 = state.onReplyResult(
    { callId: 'q1', attemptId: 1, ok: false, text: 'q1에 보냈던 내용' },
    { kind: 'question', callId: 'q2', what: '새 질문 2' }
  );
  assert.equal(failQ1.handled, true);
  assert.equal(failQ1.ok, false);
  assert.equal(failQ1.restoredInStoreOnly, true);
  assert.notEqual(failQ1.reenterAnswerMode, true);

  // 현재 활성 질문 q2의 모드와 입력 내용이 방해받지 않고 그대로 유지됨
  assert.equal(state.getPendingQuestion(), 'q2');
  assert.equal(state.getQuestionDraft('q2'), 'q2 전용 작성 중인 내용');
  // q1의 실패 내역은 q1 저장소에만 보존됨
  assert.equal(state.getQuestionDraft('q1'), 'q1에 보냈던 내용');
  assert.deepEqual(state.getFailedDrafts('q1'), ['q1에 보냈던 내용']);
});

test('Scenario 5: 성공 응답과 질문 종료 이벤트의 도착 순서가 바뀌는 경우: 일반 초안 오염 없음', () => {
  // Case 5A: 질문 종료(dismiss) 먼저 -> 성공 응답(replyResult ok) 나중
  const stateA = createAnswerState();
  stateA.onInputChange('보존되어야 할 나의 일반 메모 A');
  stateA.enterAnswerMode('q1', '질문 1', stateA.getGeneralDraft());
  stateA.onInputChange('제출할 답변 A');

  const subA = stateA.submitReply('q1', '제출할 답변 A');
  assert.equal(subA.ok, true);
  assert.equal(stateA.getPendingQuestion(), null);
  assert.equal(stateA.getGeneralDraft(), '보존되어야 할 나의 일반 메모 A');

  // 1. 질문 종료 이벤트 먼저 도착 (ask = null)
  const askDismiss = stateA.onAskChange(null, stateA.getGeneralDraft());
  assert.equal(askDismiss.exitAnswerMode, undefined);
  assert.equal(stateA.getGeneralDraft(), '보존되어야 할 나의 일반 메모 A');

  // 2. 뒤이어 성공 응답 도착
  const okA = stateA.onReplyResult({ callId: 'q1', attemptId: subA.attemptId, ok: true });
  assert.equal(okA.handled, true);
  assert.equal(okA.ok, true);
  assert.equal(stateA.getGeneralDraft(), '보존되어야 할 나의 일반 메모 A');
  assert.equal(stateA.getQuestionDraft('q1'), '');

  // Case 5B: 성공 응답 먼저 -> 질문 종료 나중
  const stateB = createAnswerState();
  stateB.onInputChange('보존되어야 할 나의 일반 메모 B');
  stateB.enterAnswerMode('q2', '질문 2', stateB.getGeneralDraft());
  stateB.onInputChange('제출할 답변 B');

  const subB = stateB.submitReply('q2', '제출할 답변 B');
  assert.equal(subB.ok, true);
  assert.equal(stateB.getGeneralDraft(), '보존되어야 할 나의 일반 메모 B');

  // 1. 성공 응답 먼저 도착
  const okB = stateB.onReplyResult({ callId: 'q2', attemptId: subB.attemptId, ok: true });
  assert.equal(okB.handled, true);
  assert.equal(okB.ok, true);
  assert.equal(stateB.getGeneralDraft(), '보존되어야 할 나의 일반 메모 B');
  assert.equal(stateB.getQuestionDraft('q2'), '');

  // 2. 뒤이어 질문 종료 이벤트 도착 (ask = null)
  const askDismissB = stateB.onAskChange(null, stateB.getGeneralDraft());
  assert.equal(askDismissB.exitAnswerMode, undefined);
  assert.equal(stateB.getGeneralDraft(), '보존되어야 할 나의 일반 메모 B');
});
