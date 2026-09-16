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
  const sub1 = state.submitReply('q1', 'q1에 보냈던 내용');
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

test('Scenario 6: 같은 callId의 서로 다른 세션 (S1 vs S2) 격리 검증 (§4.5)', () => {
  const state = createAnswerState();

  // 1. S1 문맥에서 q1 질문 답변 작성 및 전송
  state.switchContext('/workdir', 'session-1');
  state.onInputChange('S1 일반 초안');
  assert.equal(state.getGeneralDraft(), 'S1 일반 초안');

  state.enterAnswerMode('q1', '공통 질문 ID', state.getGeneralDraft());
  state.onInputChange('S1에서 작성한 q1 답변');
  const sub1 = state.submitReply('q1', 'S1에서 작성한 q1 답변');
  assert.equal(sub1.ok, true);
  assert.equal(sub1.attemptId, 1);
  assert.equal(sub1.session, 'session-1');
  assert.equal(state.isInFlight('q1'), true);

  // 2. S2 문맥으로 전환 (동일한 callId 'q1'을 가진 질문 대기 중)
  // 전환 전 S1의 composer 상태('S1 일반 초안')를 currentInputText로 전달
  const res2 = state.switchContext('/workdir', 'session-2', {
    currentInputText: 'S1 일반 초안',
    activeAsk: { kind: 'question', callId: 'q1', what: '공통 질문 ID', options: [] }
  });
  // S2는 별도 문맥이므로 S1의 in-flight에 영향을 받지 않아야 함
  assert.equal(state.getGeneralDraft(), '');
  assert.equal(state.isInFlight('q1'), false);
  assert.equal(state.getPendingQuestion(), 'q1');
  assert.equal(res2.enterAnswerMode, true);
  assert.equal(res2.callId, 'q1');

  // S2에서 q1 답변 작성 및 전송
  state.onInputChange('S2에서 작성한 q1 답변');
  const sub2 = state.submitReply('q1', 'S2에서 작성한 q1 답변');
  assert.equal(sub2.ok, true);
  assert.equal(sub2.attemptId, 2);
  assert.equal(sub2.session, 'session-2');
  assert.equal(state.isInFlight('q1'), true);

  // 3. S1의 1번 시도에 대한 지연된 실패 응답 도착 (현재 활성 세션은 S2)
  const fail1 = state.onReplyResult(
    { callId: 'q1', attemptId: 1, ok: false, text: 'S1에서 작성한 q1 답변', session: 'session-1', companionKey: '/workdir' },
    { kind: 'question', callId: 'q1', what: '공통 질문 ID' }
  );
  assert.equal(fail1.handled, true);
  assert.equal(fail1.ok, false);
  // 현재 활성 세션(S2)이 아니므로 UI를 바꾸지 않고 저장소에만 복구되어야 함
  assert.equal(fail1.restoredInStoreOnly, true);
  assert.notEqual(fail1.reenterAnswerMode, true);

  // S2의 inFlight와 상태는 온전히 유지
  assert.equal(state.isInFlight('q1', '/workdir', 'session-2'), true);
  assert.equal(state.getQuestionDraft('q1', '/workdir', 'session-2'), 'S2에서 작성한 q1 답변');

  // S1 저장소에 실패 초안이 정상 보존되었는지 검증
  assert.equal(state.isInFlight('q1', '/workdir', 'session-1'), false);
  assert.equal(state.getQuestionDraft('q1', '/workdir', 'session-1'), 'S1에서 작성한 q1 답변');
  assert.deepEqual(state.getFailedDrafts('q1', '/workdir', 'session-1'), ['S1에서 작성한 q1 답변']);
  assert.equal(state.getGeneralDraft('/workdir', 'session-1'), 'S1 일반 초안');
});

test('Scenario 7: 동일 sessionId에 서로 다른 companionKey (C1 vs C2) 격리 검증 (§4.5)', () => {
  const state = createAnswerState();

  // C1 / session-alpha 문맥
  state.switchContext('companion-1', 'session-alpha');
  state.onInputChange('C1의 작업 초안');
  state.enterAnswerMode('auth-q', '인증 질문', state.getGeneralDraft());
  state.onInputChange('C1 인증 토큰 입력');

  // C2 / session-alpha 문맥으로 전환 (sessionId는 같으나 companionKey가 다름)
  // 전환 시점에 C1의 작성 중이던 'C1 인증 토큰 입력' 전달
  state.switchContext('companion-2', 'session-alpha', { currentInputText: 'C1 인증 토큰 입력' });
  assert.equal(state.getGeneralDraft(), '');
  assert.equal(state.getPendingQuestion(), null);
  assert.equal(state.getQuestionDraft('auth-q'), '');

  // C2에서 별도 일반 초안 및 질문 초안 작성
  state.onInputChange('C2의 작업 초안');
  state.enterAnswerMode('auth-q', '인증 질문', state.getGeneralDraft());
  state.onInputChange('C2 인증 토큰 입력');

  // C1으로 다시 전환
  const resBack = state.switchContext('companion-1', 'session-alpha', {
    currentInputText: 'C2 인증 토큰 입력',
    activeAsk: { kind: 'question', callId: 'auth-q', what: '인증 질문' }
  });
  // C1 문맥의 초안들이 보존되어 복원되어야 함
  assert.equal(resBack.enterAnswerMode, true);
  assert.equal(resBack.nextInputText, 'C1 인증 토큰 입력');
  assert.equal(state.getGeneralDraft(), 'C1의 작업 초안');
  assert.equal(state.getQuestionDraft('auth-q'), 'C1 인증 토큰 입력');

  // C2 문맥의 초안도 독립적으로 보존되어 있어야 함
  assert.equal(state.getGeneralDraft('companion-2', 'session-alpha'), 'C2의 작업 초안');
  assert.equal(state.getQuestionDraft('auth-q', 'companion-2', 'session-alpha'), 'C2 인증 토큰 입력');
});

test('Scenario 8: 스냅샷 불변성 (Snapshot Immutability) 검증 (§4.5 Item 4)', () => {
  const state = createAnswerState();
  state.switchContext('/workspace', 'sess-immut');
  state.enterAnswerMode('q-immut', '불변 질문', '원래 일반 초안');
  state.onInputChange('원래 질문 초안');

  const sub = state.submitReply('q-immut', '원래 질문 초안');
  assert.equal(sub.ok, true);

  // 실패 기록 추가
  state.onReplyResult({
    callId: 'q-immut',
    attemptId: sub.attemptId,
    ok: false,
    text: '원래 질문 초안'
  });

  // 1. getState() 스냅샷 변조 시도
  const snapshot = state.getState();
  assert.equal(snapshot.generalDraft, '원래 일반 초안');
  assert.deepEqual(snapshot.failedDrafts['q-immut'], ['원래 질문 초안']);

  // 스냅샷의 중첩 배열 및 객체 임의 수정
  snapshot.failedDrafts['q-immut'].push('해킹된 실패 내역');
  snapshot.questionDrafts['q-immut'] = '해킹된 질문 초안';
  if (snapshot.inFlightReplies['q-immut']) {
    snapshot.inFlightReplies['q-immut'].text = '해킹된 인플라이트';
  }

  // 내부 상태는 변조되지 않아야 함
  assert.deepEqual(state.getFailedDrafts('q-immut'), ['원래 질문 초안']);
  assert.equal(state.getQuestionDraft('q-immut'), '원래 질문 초안');
  const freshSnapshot = state.getState();
  assert.deepEqual(freshSnapshot.failedDrafts['q-immut'], ['원래 질문 초안']);
  assert.equal(freshSnapshot.questionDrafts['q-immut'], '원래 질문 초안');

  // 2. getInFlight() 복사본 변조 시도
  const inFlight = state.getInFlight('q-immut');
  if (inFlight) {
    inFlight.text = '외부에서 변조';
    assert.notEqual(state.getInFlight('q-immut')?.text, '외부에서 변조');
  }
});

test('Scenario 9: onReplyResult generation 불일치 시 in-flight 및 초안 불변 검증 (§4.5 Item 1)', () => {
  const state = createAnswerState();
  state.switchContext('/workspace', 'sess-gen', { generation: 2, webviewId: 'view-1' });
  state.enterAnswerMode('q1', '질문 1');
  state.onInputChange('답변 초안 gen2');

  const sub = state.submitReply('q1', '답변 초안 gen2');
  assert.equal(sub.ok, true);
  assert.equal(sub.attemptId, 1);
  assert.equal(sub.generation, 2);
  assert.equal(sub.webviewId, 'view-1');
  assert.equal(state.isInFlight('q1'), true);

  // generation 1의 동일 문맥 성공 결과가 지연 도착
  const lateRes = state.onReplyResult({
    callId: 'q1',
    attemptId: 1,
    ok: true,
    text: '답변 초안 gen2',
    companionKey: '/workspace',
    session: 'sess-gen',
    generation: 1,
    webviewId: 'view-1',
  });

  // 불일치로 처리 거부되어야 함
  assert.equal(lateRes.handled, false);
  assert.equal(lateRes.reason, 'generation_mismatch');

  // in-flight 및 초안이 유지되어야 함
  assert.equal(state.isInFlight('q1'), true);
  assert.equal(state.getQuestionDraft('q1'), '답변 초안 gen2');

  // 올바른 generation 2 결과 도착 시 정상 처리
  const validRes = state.onReplyResult({
    callId: 'q1',
    attemptId: 1,
    ok: true,
    text: '답변 초안 gen2',
    companionKey: '/workspace',
    session: 'sess-gen',
    generation: 2,
    webviewId: 'view-1',
  });
  assert.equal(validRes.handled, true);
  assert.equal(validRes.ok, true);
  assert.equal(state.isInFlight('q1'), false);
  assert.equal(state.getQuestionDraft('q1'), '');
});

test('Scenario 10: onReplyResult webviewId 불일치 시 in-flight 및 초안 불변 검증 (§4.5 Item 1)', () => {
  const state = createAnswerState();
  state.switchContext('/workspace', 'sess-wid', { generation: 1, webviewId: 'view-2' });
  state.enterAnswerMode('q1', '질문 1');
  state.onInputChange('답변 초안 view2');

  const sub = state.submitReply('q1', '답변 초안 view2');
  assert.equal(sub.ok, true);
  assert.equal(sub.webviewId, 'view-2');
  assert.equal(state.isInFlight('q1'), true);

  // 이전 웹뷰(view-1) 식별자를 가진 결과가 지연 도착
  const staleRes = state.onReplyResult({
    callId: 'q1',
    attemptId: sub.attemptId,
    ok: true,
    text: '답변 초안 view2',
    companionKey: '/workspace',
    session: 'sess-wid',
    generation: 1,
    webviewId: 'view-1',
  });

  assert.equal(staleRes.handled, false);
  assert.equal(staleRes.reason, 'webview_mismatch');

  // in-flight 및 초안이 삭제되지 않고 보존
  assert.equal(state.isInFlight('q1'), true);
  assert.equal(state.getQuestionDraft('q1'), '답변 초안 view2');
});

test('Scenario 11: bindUnconfirmedSession 생명주기 및 대상 초안 충돌 방지 검증 (§4.5 Item 1, Item 2)', () => {
  const state = createAnswerState();

  // 1. 빈 세션 (C1, '')에서 초안 작성
  state.switchContext('/ws-1', '', { webviewId: 'view-1' });
  state.onInputChange('C1 빈 세션에서 작성한 새 작업 초안');
  assert.equal(state.getGeneralDraft('/ws-1', ''), 'C1 빈 세션에서 작성한 새 작업 초안');

  // 미등록 creationTaskId 완료 시도는 폴백 없이 거절 (ok: false, §4.5 Item 2)
  assert.deepEqual(
    state.bindUnconfirmedSession('/ws-1', 'session-created-1', 'unregistered-task-id', 'view-1'),
    { ok: false, reason: 'not_found' }
  );

  // 작업 등록: 생성 시작 시 작업 ID 등록
  const regOk = state.registerCreationTask('/ws-1', 'task-create-1', 'view-1', 'C1 빈 세션에서 작성한 새 작업 초안');
  assert.equal(regOk, true);

  // 2. 일반 switchContext로 기존 세션 S2로 이동 (creationTaskId 없음)
  state.switchContext('/ws-1', 'session-2', { webviewId: 'view-1' });
  // S2로 자동 이전되지 않고 S2의 일반 초안은 비어 있어야 함
  assert.equal(state.getGeneralDraft('/ws-1', 'session-2'), '');
  // 빈 세션의 초안은 온전히 유지
  assert.equal(state.getGeneralDraft('/ws-1', ''), 'C1 빈 세션에서 작성한 새 작업 초안');

  // 3. 다른 컴패니언 C2로 이동해도 영향 없음
  state.switchContext('/ws-2', 'session-other', { webviewId: 'view-1' });
  assert.equal(state.getGeneralDraft('/ws-2', 'session-other'), '');
  assert.equal(state.getGeneralDraft('/ws-1', ''), 'C1 빈 세션에서 작성한 새 작업 초안');

  // 4. 대상 세션에 이미 초안이 있는 상태에서 bindUnconfirmedSession 시도시:
  // 유효한 생성이므로 ok: true, conflict: true 반환, 세션 생성 사실 기록(completed),
  // 기존 대상 초안 보존 및 등록된 원래 작업 자료 보존 (§4.5 Item 2)
  state.switchContext('/ws-1', 'session-has-draft', { webviewId: 'view-1' });
  state.onInputChange('이미 존재하는 S3 초안');
  const bindConflict = state.bindUnconfirmedSession('/ws-1', 'session-has-draft', 'task-create-1', 'view-1');
  assert.equal(bindConflict.ok, true);
  if (bindConflict.ok) {
    assert.equal(bindConflict.conflict, true);
    assert.equal(bindConflict.taskDraft, 'C1 빈 세션에서 작성한 새 작업 초안');
    assert.equal(bindConflict.existingDraft, '이미 존재하는 S3 초안');
  }
  assert.equal(state.getGeneralDraft('/ws-1', 'session-has-draft'), '이미 존재하는 S3 초안');
  // 등록된 작업 자료 보존 및 상태 completed 기록
  const preservedTask = state.getCreationTask('/ws-1', 'task-create-1');
  assert.equal(preservedTask?.status, 'completed');
  assert.equal(preservedTask?.draft, 'C1 빈 세션에서 작성한 새 작업 초안');

  // 등록된 새 작업 task-create-2로 웹뷰 불일치 시험
  state.registerCreationTask('/ws-1', 'task-create-2', 'view-1', '초안 2');
  const bindWrongView = state.bindUnconfirmedSession('/ws-1', 'session-created-2', 'task-create-2', 'view-other');
  assert.deepEqual(bindWrongView, { ok: false, reason: 'webview_mismatch' });

  // 5. 원래 웹뷰에서 생성 결과 세션 S1에 명시적 바인딩 성공
  const bindSuccess = state.bindUnconfirmedSession('/ws-1', 'session-created-2', 'task-create-2', 'view-1');
  assert.deepEqual(bindSuccess, { ok: true, conflict: false, draft: '초안 2' });
  assert.equal(state.getGeneralDraft('/ws-1', 'session-created-2'), '초안 2');
  assert.equal(state.getCreationTask('/ws-1', 'task-create-2')?.status, 'completed');

  // 6. 동일 작업 재완료 시도는 거절 (중복 완료 방지, §4.5 Item 2)
  const duplicateBind = state.bindUnconfirmedSession('/ws-1', 'session-created-2', 'task-create-2', 'view-1');
  assert.deepEqual(duplicateBind, { ok: false, reason: 'not_pending' });
});

test('Scenario 12: failCreationTask 생명주기 및 실패 자료 보존·새 작업 격리 검증 (§4.5 Item 1)', () => {
  const state = createAnswerState();
  state.registerCreationTask('/ws-1', 'create-1', 'view-1', '실패 전 작성한 초안');

  // 생성 실패 처리
  const failed = state.failCreationTask('/ws-1', 'create-1', 'daemon connection timeout');
  assert.equal(failed, true);

  // 실패 후에도 원래 작업 자료와 오류 보존, 삭제되지 않음 (§4.5 Item 1)
  const task1 = state.getCreationTask('/ws-1', 'create-1');
  assert.ok(task1);
  assert.equal(task1.status, 'failed');
  assert.equal(task1.error, 'daemon connection timeout');
  assert.equal(task1.draft, '실패 전 작성한 초안');

  // 이미 실패한 작업에 대한 바인딩 시도는 거절
  const bindFailed = state.bindUnconfirmedSession('/ws-1', 'session-1', 'create-1', 'view-1');
  assert.deepEqual(bindFailed, { ok: false, reason: 'not_pending' });

  // 재시도: 새 ID create-2 발급하여 등록
  const retryReg = state.registerCreationTask('/ws-1', 'create-2', 'view-1', '재시도 작성 초안');
  assert.equal(retryReg, true);

  // create-1 자료는 여전히 보존되어 있음
  assert.equal(state.getCreationTask('/ws-1', 'create-1')?.status, 'failed');
  assert.equal(state.getCreationTask('/ws-1', 'create-2')?.status, 'pending');
});

