import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { ImmutableSnapshotStore } from '../core/snapshot';
import { ApprovalSnapshots } from '../core/diff';
import { OutputSnapshots } from '../core/output';

test('ImmutableSnapshotStore: 동일 키 재저장 시 최초 내용 불변 및 false 반환', () => {
  const store = new ImmutableSnapshotStore<string>(5);
  const first = store.put('key1', 'original-content');
  assert.equal(first, true, '최초 저장은 성공해야 함');
  assert.equal(store.get('key1'), 'original-content', '초기 내용 보존');

  // 동일 키 재저장 시도
  const second = store.put('key1', 'mutated-content');
  assert.equal(second, false, '동일 키 재저장은 거절(false)되어야 함');
  assert.equal(store.get('key1'), 'original-content', '기존 내용이 변경되지 않고 온전히 유지되어야 함');
});

test('ImmutableSnapshotStore: 빈 문자열 문서도 유효한 내용으로 정상 저장 및 조회', () => {
  const store = new ImmutableSnapshotStore<string>(5);
  assert.equal(store.put('empty-doc', ''), true);
  assert.equal(store.has('empty-doc'), true);
  assert.equal(store.get('empty-doc'), '', '빈 문자열이 undefined로 취급되지 않고 ""로 반환');

  // 빈 문자열 재저장 시도도 불변성 유지
  assert.equal(store.put('empty-doc', 'not-empty'), false);
  assert.equal(store.get('empty-doc'), '');
});

test('ImmutableSnapshotStore: get 이후 축출 순서 — 접근 기반 LRU가 아닌 엄격한 삽입 순서(FIFO) 축출', () => {
  // 용량 2인 저장소
  const store = new ImmutableSnapshotStore<string>(2);
  store.put('key-1', 'content-1');
  store.put('key-2', 'content-2');

  // key-1에 get() 접근 수행 -> LRU라면 key-1이 최신이 되고 key-2가 축출 대상이 됨
  const read = store.get('key-1');
  assert.equal(read, 'content-1');

  // key-3 추가 -> 용량 2 초과
  // 삽입 순서(FIFO)에 의해 먼저 삽입된 key-1이 축출되어야 함 (LRU였다면 key-2가 축출됨)
  store.put('key-3', 'content-3');

  assert.equal(store.has('key-1'), false, 'get() 접근에도 불구하고 삽입 순서가 빠른 key-1이 축출됨 (not LRU)');
  assert.equal(store.has('key-2'), true, '나중에 삽입된 key-2는 유지됨');
  assert.equal(store.has('key-3'), true, '새로 삽입된 key-3은 유지됨');
});

test('ImmutableSnapshotStore: 중첩 임시 보호의 개별 해제 및 참조 카운팅, 멱등 해제', () => {
  const pinned = new Set<string>();
  const store = new ImmutableSnapshotStore<string>(1, (k) => pinned.has(k));
  store.put('base', 'base-content');
  pinned.add('base');

  const shared = 'shared-target';

  // 비동기 작업 1 시작: shared 보호
  const unprotect1 = store.protectTemp([shared]);
  store.put(shared, 'shared-content');
  assert.equal(store.tempPinnedSize, 1);

  // 비동기 작업 2 동시 시작: 동일한 shared 중첩 보호
  const unprotect2 = store.protectTemp([shared]);
  assert.equal(store.tempPinnedSize, 1);

  // 작업 1 완료 또는 취소로 unprotect1 호출
  unprotect1();

  // 작업 2가 여전히 살아있으므로 shared는 축출되지 않고 보호되어야 함
  assert.equal(store.has(shared), true, '작업 2의 보호로 인해 shared 보존');
  assert.equal(store.tempPinnedSize, 1, '참조 카운트 1 잔류');

  // unprotect1 중복 호출은 멱등적이어야 함 (카운트 추가 감소 없음)
  unprotect1();
  assert.equal(store.has(shared), true, '중복 호출에도 작업 2 보호 불변');

  // 작업 2 완료로 탭에 고정 후 unprotect2 호출
  pinned.add(shared);
  unprotect2();
  assert.equal(store.tempPinnedSize, 0, '모든 보호 해제 완료');
  assert.equal(store.has(shared), true, '탭에 고정되었으므로 보존');
});

test('ImmutableSnapshotStore: 모든 항목 고정 시 일시적 용량 초과 허용', () => {
  const pinned = new Set<string>(['doc1', 'doc2']);
  const store = new ImmutableSnapshotStore<string>(2, (k) => pinned.has(k));

  store.put('doc1', 'content1');
  store.put('doc2', 'content2');
  assert.equal(store.size, 2);

  // doc1, doc2 모두 고정된 상태에서 doc3 추가 -> 용량 2 초과
  store.put('doc3', 'content3');
  assert.equal(store.size, 3, '모든 항목이 고정된 경우 일시적 용량 초과 허용');
  assert.equal(store.has('doc1'), true);
  assert.equal(store.has('doc2'), true);
  assert.equal(store.has('doc3'), true);
});

test('ImmutableSnapshotStore: diff 두 면(left/right) 생성 중 동시 임시 보호', () => {
  const pinned = new Set<string>(['existing']);
  // 용량 1이며 이미 1건이 고정된 상태
  const store = new ImmutableSnapshotStore<string>(1, (k) => pinned.has(k));
  store.put('existing', 'old-data');

  // diff 두 면 동시 보호
  const left = 'magi-diff:/left';
  const right = 'magi-diff:/right';
  const unprotect = store.protectTemp([left, right]);

  try {
    store.put(left, 'left-side');
    store.put(right, 'right-side');

    assert.equal(store.get(left), 'left-side', '두 면 중 먼저 저장된 left가 right 저장 시 축출되지 않음');
    assert.equal(store.get(right), 'right-side', 'right 면도 안전하게 저장됨');
    assert.equal(store.get('existing'), 'old-data', '기존 고정 문서도 유지됨');
    assert.equal(store.size, 3, '임시 보호로 인해 3건 공존');

    // 에디터 열기 성공 모사: 두 면 탭 고정
    pinned.add(left);
    pinned.add(right);
  } finally {
    unprotect();
  }

  assert.equal(store.get(left), 'left-side');
  assert.equal(store.get(right), 'right-side');
});

test('ImmutableSnapshotStore: 문서 열기 실패 시 finally 블록에서 임시 보호 안전 해제', () => {
  const pinned = new Set<string>(['pinned-doc']);
  const store = new ImmutableSnapshotStore<string>(1, (k) => pinned.has(k));
  store.put('pinned-doc', 'doc-data');

  const failKey = 'magi-diff:/will-fail';
  const unprotect = store.protectTemp([failKey]);
  assert.equal(store.tempPinnedSize, 1);

  try {
    store.put(failKey, 'fail-data');
    assert.equal(store.size, 2);
    throw new Error('vscode.diff 열기 실패 모사');
  } catch {
    // 에러 발생 처리
  } finally {
    unprotect();
  }

  // 임시 보호 누수 없이 완전히 해제되어야 하고, 미고정 항목은 정리되어야 함
  assert.equal(store.tempPinnedSize, 0, '임시 보호 누수 없음');
  assert.equal(store.has('pinned-doc'), true, '고정된 문서는 보존');
  assert.equal(store.has(failKey), false, '열기 실패한 미고정 문서는 즉시 정리됨');
  assert.equal(store.size, 1, '최대 용량 1로 복귀');
});

test('ImmutableSnapshotStore: 탭 종료 뒤 추가 put 없는 정리 (evictExcess)', () => {
  const pinned = new Set<string>(['tab1', 'tab2', 'tab3']);
  const store = new ImmutableSnapshotStore<string>(1, (k) => pinned.has(k));

  store.put('tab1', 'data1');
  store.put('tab2', 'data2');
  store.put('tab3', 'data3');
  assert.equal(store.size, 3);

  // tab1 닫힘
  pinned.delete('tab1');
  const evicted = store.evictExcess();
  assert.equal(evicted, 1, 'tab1이 새 put 호출 없이 evictExcess에 의해 즉시 축출됨');
  assert.equal(store.has('tab1'), false);
  assert.equal(store.has('tab2'), true);
  assert.equal(store.has('tab3'), true);

  // tab2, tab3도 닫힘
  pinned.delete('tab2');
  pinned.delete('tab3');
  const evictedRemaining = store.evictExcess();
  assert.equal(evictedRemaining, 1, '용량 1에 도달할 때까지 tab2가 축출되고 가장 최신 tab3만 남음');
  assert.equal(store.size, 1);
  assert.equal(store.has('tab3'), true);
});

test('ApprovalSnapshots 및 OutputSnapshots가 ImmutableSnapshotStore를 올바르게 상속', () => {
  const diffSnapshots = new ApprovalSnapshots(2);
  const outSnapshots = new OutputSnapshots(2);

  assert.ok(diffSnapshots instanceof ImmutableSnapshotStore);
  assert.ok(outSnapshots instanceof ImmutableSnapshotStore);

  assert.equal(diffSnapshots.put('diff1', 'd1'), true);
  assert.equal(diffSnapshots.put('diff1', 'd1-mut'), false);
  assert.equal(diffSnapshots.get('diff1'), 'd1');

  assert.equal(outSnapshots.put('out1', 'o1'), true);
  assert.equal(outSnapshots.put('out1', 'o1-mut'), false);
  assert.equal(outSnapshots.get('out1'), 'o1');
});
