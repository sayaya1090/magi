import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { createRecoveryState } from '../core/recovery_state';
import { createAnswerState } from '../core/answer_state';

test('§4.6.1 & §4.6.2: RecoveryState - 기본 등록, 빈 문자열 거절, 공백/개행 원문 보존', () => {
  const rec = createRecoveryState();

  // 1. 빈 문자열 등록 거부 (§4.6.1)
  assert.equal(rec.register({
    companionKey: '/ws',
    sessionId: 's1',
    kind: 'reply_failed',
    text: '',
  }), undefined, 'empty text must not be registered');

  // 2. 공백·개행만 있는 비어 있지 않은 문자열은 trim 없이 원문 보존 (§4.6.1)
  const wsText = '   \n\t  ';
  const item1 = rec.register({
    companionKey: '/ws',
    sessionId: 's1',
    callId: 'q1',
    kind: 'reply_failed',
    text: wsText,
  });
  assert.ok(item1);
  assert.equal(item1!.text, wsText, 'whitespace and newlines must be preserved verbatim');
  assert.equal(item1!.attempts, 1);
  assert.equal(item1!.title, 'q1');
  assert.equal(item1!.reason, '답변 전송을 확인하지 못함');

  // 3. HTML 특수문자가 포함된 텍스트 원문 보존
  const htmlText = '<script>alert("xss")</script>\n<div>&amp;</div>';
  const item2 = rec.register({
    companionKey: '/ws',
    sessionId: 's1',
    callId: 'q2',
    kind: 'reply_failed',
    text: htmlText,
    error: 'server_down',
  });
  assert.ok(item2);
  assert.equal(item2!.text, htmlText);
  assert.equal(item2!.error, 'server_down');
  assert.equal(item2!.reason, '답변 전송을 확인하지 못함: server_down');
});

test('§4.6.2: RecoveryState - 중복 합산, 최신 오류 갱신, 소비 이벤트 중복 무시', () => {
  const rec = createRecoveryState();

  // 1. A 첫 실패 등록
  const a1 = rec.register({
    companionKey: '/ws',
    sessionId: 's1',
    callId: 'q1',
    kind: 'reply_failed',
    text: 'A draft',
    error: 'timeout',
    eventKey: 'reply:/ws:s1:q1:1',
  });
  assert.ok(a1);
  assert.equal(a1!.attempts, 1);
  assert.equal(a1!.error, 'timeout');
  const rId = a1!.recoveryId;

  // 2. 동일 eventKey의 중복 결과(stale duplicate)는 재등록 및 횟수 증가 없이 무시 (§4.6.2)
  const dup = rec.register({
    companionKey: '/ws',
    sessionId: 's1',
    callId: 'q1',
    kind: 'reply_failed',
    text: 'A draft',
    error: 'stale_err',
    eventKey: 'reply:/ws:s1:q1:1',
  });
  assert.equal(dup, undefined, 'duplicate eventKey must be ignored');
  assert.equal(rec.getItem(rId)?.attempts, 1, 'attempts must not increase on duplicate event');

  // 3. 새 시도(attempt 2)로 동일 내용 A 실패 시 하나로 합치고 횟수·오류 갱신 (§4.6.2)
  const a2 = rec.register({
    companionKey: '/ws',
    sessionId: 's1',
    callId: 'q1',
    kind: 'reply_failed',
    text: 'A draft',
    error: 'connection_reset',
    eventKey: 'reply:/ws:s1:q1:2',
  });
  assert.ok(a2);
  assert.equal(a2!.recoveryId, rId, 'same item must be grouped');
  assert.equal(a2!.attempts, 2, 'attempts must become 2');
  assert.equal(a2!.error, 'connection_reset', 'error must be updated');

  // 4. 같은 질문에 다른 내용 B 실패 시 별도 항목 등록 (§4.6.2)
  const b = rec.register({
    companionKey: '/ws',
    sessionId: 's1',
    callId: 'q1',
    kind: 'reply_failed',
    text: 'B different draft',
    error: 'timeout',
    eventKey: 'reply:/ws:s1:q1:3',
  });
  assert.ok(b);
  assert.notEqual(b!.recoveryId, rId, 'different text must have different recoveryId');
  assert.equal(b!.attempts, 1);

  // 목록에 A(attempts 2), B(attempts 1) 둘 다 최신 순 정렬
  const items = rec.listItems();
  assert.equal(items.length, 2);
  assert.equal(items[0].recoveryId, b!.recoveryId, 'latest failure B is first');
  assert.equal(items[1].recoveryId, rId, 'A is second');
});

test('§4.6.2: RecoveryState - 다른 세션·컴패니언 격리 및 목록 필터링', () => {
  const rec = createRecoveryState();

  // 1. C1 / S1의 Q1
  rec.register({
    companionKey: '/c1',
    sessionId: 's1',
    callId: 'q1',
    kind: 'reply_failed',
    text: 'Text C1 S1',
    eventKey: 'ev1',
  });

  // 2. C1 / S2의 같은 Q1 (다른 세션)
  rec.register({
    companionKey: '/c1',
    sessionId: 's2',
    callId: 'q1',
    kind: 'reply_failed',
    text: 'Text C1 S2',
    eventKey: 'ev2',
  });

  // 3. C2 / S1의 같은 Q1 (다른 컴패니언)
  rec.register({
    companionKey: '/c2',
    sessionId: 's1',
    callId: 'q1',
    kind: 'reply_failed',
    text: 'Text C2 S1',
    eventKey: 'ev3',
  });

  // 4. C1 세션 미귀속 생성 작업 초안
  rec.register({
    companionKey: '/c1',
    creationTaskId: 'create-1',
    kind: 'session_creation_failed',
    text: 'Unassigned creation draft',
    eventKey: 'ev4',
  });

  // C1/S1 기본 목록: S1 항목 + 세션 미귀속 항목만 노출 (§4.6.3)
  const defaultList = rec.listItems({ companionKey: '/c1', sessionId: 's1', includeOtherSessions: false });
  assert.equal(defaultList.length, 2);
  assert.ok(defaultList.some((it) => it.text === 'Text C1 S1'));
  assert.ok(defaultList.some((it) => it.text === 'Unassigned creation draft'));
  assert.ok(!defaultList.some((it) => it.text === 'Text C1 S2'), 'S2 must not be in S1 default list');
  assert.ok(!defaultList.some((it) => it.text === 'Text C2 S1'), 'C2 must not be in C1 list');

  // C1 전체(과거 세션 포함) 목록
  const allC1List = rec.listItems({ companionKey: '/c1', includeOtherSessions: true });
  assert.equal(allC1List.length, 3);
});

test('§4.6.2: RecoveryState - 명시적 삭제 후 옛 결과 부활 차단 및 새 실패의 독립 등록', () => {
  const rec = createRecoveryState();

  const item = rec.register({
    companionKey: '/ws',
    sessionId: 's1',
    callId: 'q1',
    kind: 'reply_failed',
    text: 'Deleted target text',
    eventKey: 'reply:/ws:s1:q1:1',
  });
  assert.ok(item);
  const rId = item!.recoveryId;

  // 1. 명시적 삭제
  assert.equal(rec.deleteItem(rId), true);
  assert.equal(rec.getItem(rId), undefined);
  assert.equal(rec.listItems().length, 0);

  // 2. 삭제된 항목의 옛 실패 이벤트 재생 시 부활 차단 (§4.6.2)
  const replay = rec.register({
    companionKey: '/ws',
    sessionId: 's1',
    callId: 'q1',
    kind: 'reply_failed',
    text: 'Deleted target text',
    eventKey: 'reply:/ws:s1:q1:1',
  });
  assert.equal(replay, undefined, 'replayed event must not resurrect deleted item');
  assert.equal(rec.listItems().length, 0);

  // 3. 사용자가 이후 새 시도로 같은 내용을 보내 다시 실패한 것은 새 사건으로 등록 (§4.6.2)
  const fresh = rec.register({
    companionKey: '/ws',
    sessionId: 's1',
    callId: 'q1',
    kind: 'reply_failed',
    text: 'Deleted target text',
    eventKey: 'reply:/ws:s1:q1:2',
  });
  assert.ok(fresh);
  assert.notEqual(fresh!.recoveryId, rId, 'new failure after deletion must receive new recoveryId');
  assert.equal(fresh!.attempts, 1);
});

test('§4.6.1 & §4.6.4: AnswerStateManager 통합 - A 전송 후 B 수정, A 실패 시 inFlight.text 복구 및 B 보존', () => {
  const state = createAnswerState();

  state.switchContext('/ws', 's1');
  state.enterAnswerMode('q1', '질문 1');

  // 1. A 전송 (attempt 1)
  const submitRes = state.submitReply('q1', 'TEXT_A');
  assert.ok(submitRes.ok);
  const attemptId = submitRes.attemptId!;

  // 2. 전송 대기 중 사용자가 B 작성 (draft version 증가)
  state.enterAnswerMode('q1', '질문 1');
  state.onInputChange('TEXT_B_MODIFIED');
  assert.equal(state.getQuestionDraft('q1'), 'TEXT_B_MODIFIED');

  // 3. A 실패 응답 도착 (m.text는 비어 있거나 다른 값일 수 있음)
  const outcome = state.onReplyResult({
    callId: 'q1',
    attemptId,
    ok: false,
    text: 'WRONG_M_TEXT',
    error: 'network_timeout',
    companionKey: '/ws',
    session: 's1',
    generation: 0,
    webviewId: '',
  });
  assert.equal(outcome.handled, true);
  assert.equal(outcome.ok, false);

  // B 수정 입력은 보존되어야 함 (§4.6.5 Scenario 1)
  assert.equal(state.getQuestionDraft('q1'), 'TEXT_B_MODIFIED');

  // 복구 저장소에 저장된 원문은 응답 m.text가 아닌 inFlight.text인 TEXT_A여야 함 (§4.6.1)
  const recItems = state.listRecoveryItems({ companionKey: '/ws', sessionId: 's1' });
  assert.equal(recItems.length, 1);
  assert.equal(recItems[0].text, 'TEXT_A', 'recovery text must be inFlight.text');
  assert.equal(recItems[0].error, 'network_timeout');
  assert.equal(recItems[0].attempts, 1);

  // 4. 이후 C 전송 성공 시 A 복구 자료는 자동 삭제되지 않음 (§4.6.2)
  const submitC = state.submitReply('q1', 'TEXT_C');
  assert.ok(submitC.ok);
  state.onReplyResult({
    callId: 'q1',
    attemptId: submitC.attemptId!,
    ok: true,
    companionKey: '/ws',
    session: 's1',
    generation: 0,
    webviewId: '',
  });

  const remaining = state.listRecoveryItems({ companionKey: '/ws', sessionId: 's1' });
  assert.equal(remaining.length, 1, 'successful C must not delete failed A');
  assert.equal(remaining[0].text, 'TEXT_A');
});

test('§4.6.1 & §4.6.4: AnswerStateManager 통합 - 생성 실패 및 충돌 초안 복구 등록, 정상 완료 중복 등록 없음', () => {
  const state = createAnswerState();

  // 1. 생성 작업 등록 및 초안 작성
  state.switchContext('/ws', '');
  state.registerCreationTask('/ws', 'task-fail', 'view-1', 'DRAFT_IN_FAILED_CREATION');

  // 2. sessionCreationFailed -> 복구 목록에 등록 (§4.6.1)
  state.failCreationTask('/ws', 'task-fail', 'daemon_spawn_error');
  const failList = state.listRecoveryItems({ companionKey: '/ws' });
  assert.equal(failList.length, 1);
  assert.equal(failList[0].kind, 'session_creation_failed');
  assert.equal(failList[0].text, 'DRAFT_IN_FAILED_CREATION');
  assert.equal(failList[0].error, 'daemon_spawn_error');

  // 3. 충돌 생성 완료 (conflict: true) -> 복구 목록에 등록 (§4.6.1)
  // 대상 세션 s-target에 기존 초안 준비
  state.switchContext('/ws', 's-target');
  state.onInputChange('EXISTING_GENERAL_DRAFT');

  state.switchContext('/ws', '');
  state.registerCreationTask('/ws', 'task-conflict', 'view-1', 'DRAFT_IN_CONFLICT_CREATION');
  const bindRes = state.bindUnconfirmedSession('/ws', 's-target', 'task-conflict', 'view-1');
  assert.equal(bindRes.ok, true);
  if (bindRes.ok) assert.equal(bindRes.conflict, true);

  const conflictList = state.listRecoveryItems({ companionKey: '/ws', sessionId: 's-target' });
  assert.ok(conflictList.some((it) => it.kind === 'session_creation_conflict' && it.text === 'DRAFT_IN_CONFLICT_CREATION'));

  // 4. 정상 생성 완료 (conflict: false) -> 복구 목록에 등록하지 않음 (§4.6.1)
  state.switchContext('/ws', '');
  state.registerCreationTask('/ws', 'task-normal', 'view-1', 'DRAFT_NORMAL');
  const normalBind = state.bindUnconfirmedSession('/ws', 's-fresh', 'task-normal', 'view-1');
  assert.equal(normalBind.ok, true);
  if (normalBind.ok) assert.equal(normalBind.conflict, false);

  const freshList = state.listRecoveryItems({ companionKey: '/ws', sessionId: 's-fresh' });
  assert.ok(!freshList.some((it) => it.creationTaskId === 'task-normal'), 'normal completion must not register to recovery');
  assert.ok(!state.listRecoveryItems().some((it) => it.creationTaskId === 'task-normal'));
});

test('§4.6.3 & §4.6.5: AnswerStateManager - 일반 초안 복사 및 이어 붙이기/취소, 모드 전환 검증', () => {
  const state = createAnswerState();
  state.switchContext('/ws', 's1');

  // 복구 항목 수동 등록
  const rec = state.getRecoveryState();
  const item = rec.register({
    companionKey: '/ws',
    sessionId: 's1',
    callId: 'q1',
    kind: 'reply_failed',
    text: 'RECOVERED_TEXT',
  })!;

  // 1. 일반 초안이 비어 있을 때: 원문 그대로 복사 및 일반 모드 전환 (§4.6.3)
  assert.equal(state.getGeneralDraft(), '');
  const res1 = state.applyRecoveryDraft({
    recoveryId: item.recoveryId,
    companionKey: '/ws',
    sessionId: 's1',
  });
  assert.equal(res1.ok, true);
  assert.equal(res1.nextInputText, 'RECOVERED_TEXT');
  assert.equal(state.getGeneralDraft(), 'RECOVERED_TEXT');

  // 2. 일반 초안 G가 있을 때: append 없이 요청 시 확인 요구 (§4.6.3)
  const resConfirm = state.applyRecoveryDraft({
    recoveryId: item.recoveryId,
    companionKey: '/ws',
    sessionId: 's1',
    append: false,
  });
  assert.equal(resConfirm.ok, false);
  assert.equal(resConfirm.reason, 'requires_confirm');
  assert.equal(resConfirm.existingDraft, 'RECOVERED_TEXT');
  // 취소 시 기존 G 보존
  assert.equal(state.getGeneralDraft(), 'RECOVERED_TEXT');

  // 3. 확인 중 사용자가 DOM에서 G를 수정한 뒤 이어 붙이기: 최신 G 반영하여 G + "\n\n" + text (§4.6.3)
  const resAppend = state.applyRecoveryDraft({
    recoveryId: item.recoveryId,
    companionKey: '/ws',
    sessionId: 's1',
    append: true,
    currentInputText: 'G_EDITED',
  });
  assert.equal(resAppend.ok, true);
  assert.equal(resAppend.nextInputText, 'G_EDITED\n\nRECOVERED_TEXT');
  assert.equal(state.getGeneralDraft(), 'G_EDITED\n\nRECOVERED_TEXT');

  // 4. 답변 모드에서 복사 확정 시 최신 답변 초안 저장 후 일반 모드 전환 (§4.6.3)
  state.enterAnswerMode('q2', '질문 2');
  assert.equal(state.getPendingQuestion(), 'q2');

  const resFromAnswer = state.applyRecoveryDraft({
    recoveryId: item.recoveryId,
    companionKey: '/ws',
    sessionId: 's1',
    append: true,
    currentInputText: 'MY_LATEST_ANSWER_DRAFT',
  });
  assert.equal(resFromAnswer.ok, true);
  assert.equal(resFromAnswer.exitAnswerMode, true);
  assert.equal(state.getPendingQuestion(), null, 'must exit answer mode');
  assert.equal(state.getQuestionDraft('q2'), 'MY_LATEST_ANSWER_DRAFT', 'latest answer draft must be saved in storage');

  // 5. 세션 불일치 또는 항목 삭제 시 취소 (§4.6.3)
  const resMismatch = state.applyRecoveryDraft({
    recoveryId: item.recoveryId,
    companionKey: '/ws',
    sessionId: 's2', // mismatch
  });
  assert.equal(resMismatch.ok, false);
  assert.equal(resMismatch.reason, 'context_mismatch');

  state.deleteRecoveryItem(item.recoveryId);
  const resDeleted = state.applyRecoveryDraft({
    recoveryId: item.recoveryId,
    companionKey: '/ws',
    sessionId: 's1',
  });
  assert.equal(resDeleted.ok, false);
  assert.equal(resDeleted.reason, 'not_found');
});

test('§4.6.2: 반복 실패 시 최신 오류 및 사유 갱신, 오류 부재 시 이전 오류 초기화', () => {
  const rec = createRecoveryState();

  // 1. 첫 번째 실패 등록 (오류 1)
  const item1 = rec.register({
    companionKey: '/ws',
    sessionId: 's1',
    callId: 'q1',
    kind: 'reply_failed',
    text: 'TEXT_FAIL',
    error: 'timeout_err_1',
    eventKey: JSON.stringify(['reply', '/ws', 's1', 'q1', 1]),
  })!;
  assert.equal(item1.attempts, 1);
  assert.equal(item1.error, 'timeout_err_1');
  assert.ok(item1.reason.includes('timeout_err_1'));

  // 2. 같은 항목에 새 오류 (오류 2)로 반복 등록 -> error와 reason이 모두 오류 2로 갱신 (§4.6.2)
  const item2 = rec.register({
    companionKey: '/ws',
    sessionId: 's1',
    callId: 'q1',
    kind: 'reply_failed',
    text: 'TEXT_FAIL',
    error: 'connection_refused_err_2',
    eventKey: JSON.stringify(['reply', '/ws', 's1', 'q1', 2]),
  })!;
  assert.equal(item2.recoveryId, item1.recoveryId);
  assert.equal(item2.attempts, 2);
  assert.equal(item2.error, 'connection_refused_err_2');
  assert.ok(item2.reason.includes('connection_refused_err_2'));
  assert.ok(!item2.reason.includes('timeout_err_1'), 'stale error 1 must be replaced');

  // 3. 새 등록에 오류가 없는 경우 -> 이전 error 비우고 고정 문구로 갱신 (§4.6.2)
  const item3 = rec.register({
    companionKey: '/ws',
    sessionId: 's1',
    callId: 'q1',
    kind: 'reply_failed',
    text: 'TEXT_FAIL',
    error: undefined,
    eventKey: JSON.stringify(['reply', '/ws', 's1', 'q1', 3]),
  })!;
  assert.equal(item3.recoveryId, item1.recoveryId);
  assert.equal(item3.attempts, 3);
  assert.equal(item3.error, undefined, 'error must be cleared when new registration has no error');
  assert.equal(item3.reason, '답변 전송을 확인하지 못함', 'reason must revert to fixed string without stale error');
});

test('§4.6.1: 구분자 충돌 방지 (JSON 구조화 eventKey) - C="a:b", task="c" vs C="a", task="b:c"', () => {
  const rec = createRecoveryState();

  const key1 = JSON.stringify(['create_fail', 'a:b', 'c']);
  const key2 = JSON.stringify(['create_fail', 'a', 'b:c']);
  assert.notEqual(key1, key2, 'JSON keys must not collide across delimiter boundaries');

  const item1 = rec.register({
    companionKey: 'a:b',
    creationTaskId: 'c',
    kind: 'session_creation_failed',
    text: 'DRAFT_1',
    eventKey: key1,
  })!;

  const item2 = rec.register({
    companionKey: 'a',
    creationTaskId: 'b:c',
    kind: 'session_creation_failed',
    text: 'DRAFT_2',
    eventKey: key2,
  })!;

  assert.notEqual(item1.recoveryId, item2.recoveryId, 'must be treated as distinct items');
  assert.equal(rec.listItems().length, 2);
});

