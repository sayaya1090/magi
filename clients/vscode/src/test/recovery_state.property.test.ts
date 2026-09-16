import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fc from 'fast-check';
import {
  createRecoveryState,
  RecoveryItemKind,
} from '../core/recovery_state';
import {
  assertProperty,
  arbKoreanOrAsciiString,
  arbNonBlankString,
} from './support/fc_helpers';

const arbKind: fc.Arbitrary<RecoveryItemKind> = fc.constantFrom(
  'reply_failed',
  'session_creation_failed',
  'session_creation_conflict'
);

const arbCompanionKey = fc.constantFrom(
  '/workspace/project-a',
  '/workspace/project-b',
  'comp:1',
  'comp:colon:2'
);

const arbSessionId = fc.constantFrom(
  'sess-1',
  'sess-2',
  'sess:alpha',
  'sess:beta',
  undefined
);

const arbCallId = fc.constantFrom('q-deploy', 'q-review', 'q:choice:1', undefined);
const arbTaskId = fc.constantFrom('task-1', 'task-2', 'task:init', undefined);

test('§5.8.4 Property: Empty string is never registered and whitespace is preserved verbatim in recovery_state', () => {
  assertProperty(
    '§5.8.4 Property: Empty string is never registered and whitespace is preserved verbatim in recovery_state',
    fc.property(
      arbKoreanOrAsciiString,
      arbKind,
      arbCompanionKey,
      arbSessionId,
      (text, kind, companionKey, sessionId) => {
        const mgr = createRecoveryState();
        const res = mgr.register({
          kind,
          text,
          companionKey,
          sessionId,
        });

        if (text.length === 0) {
          assert.equal(res, undefined, 'Empty text must return undefined');
          assert.equal(mgr.listItems().length, 0, 'No items should be created for empty text');
        } else {
          assert.ok(res !== undefined, 'Non-empty text must be registered');
          assert.equal(res!.text, text, 'Text with whitespace/newlines/Korean must be preserved verbatim');
          assert.equal(mgr.listItems().length, 1);
          assert.equal(mgr.listItems()[0].text, text);
        }
      }
    )
  );
});

test('§5.8.4 Property: Duplicate eventKey replay is strictly idempotent and does not increment attempts', () => {
  assertProperty(
    '§5.8.4 Property: Duplicate eventKey replay is strictly idempotent and does not increment attempts',
    fc.property(
      fc.string({ minLength: 1, maxLength: 30 }),
      arbNonBlankString,
      arbKind,
      arbCompanionKey,
      fc.integer({ min: 1, max: 5 }),
      (eventKey, text, kind, companionKey, duplicateCount) => {
        const mgr = createRecoveryState();

        // First registration
        const first = mgr.register({
          kind,
          text,
          companionKey,
          eventKey,
        });
        assert.ok(first !== undefined);
        assert.equal(first!.attempts, 1);

        // Subsequent replays with the identical eventKey
        for (let i = 0; i < duplicateCount; i++) {
          const replay = mgr.register({
            kind,
            text,
            companionKey,
            eventKey,
          });
          assert.equal(replay, undefined, 'Replayed eventKey must return undefined');
        }

        const items = mgr.listItems();
        assert.equal(items.length, 1, 'Exactly one item must exist');
        assert.equal(items[0].attempts, 1, 'Attempts must remain 1');
        assert.equal(mgr.isEventConsumed(eventKey), true);
      }
    )
  );
});

test('§5.8.4 Property: Merging identical failures increments attempts, updates seq, and clears stale error', () => {
  assertProperty(
    '§5.8.4 Property: Merging identical failures increments attempts, updates seq, and clears stale error',
    fc.property(
      arbNonBlankString,
      arbKind,
      arbCompanionKey,
      arbSessionId,
      arbCallId,
      arbTaskId,
      fc.string({ minLength: 1, maxLength: 30 }),
      fc.boolean(),
      (text, kind, companionKey, sessionId, callId, creationTaskId, initialError, secondHasError) => {
        const mgr = createRecoveryState();

        // First failure event
        const first = mgr.register({
          kind,
          text,
          companionKey,
          sessionId,
          callId,
          creationTaskId,
          error: initialError,
          eventKey: 'event-1',
        });
        assert.ok(first !== undefined);
        assert.equal(first!.attempts, 1);
        assert.equal(first!.error, initialError);
        const initialSeq = first!.seq;

        // Second failure event (different eventKey, but identical context/text)
        const secondError = secondHasError ? 'New second error' : undefined;
        const second = mgr.register({
          kind,
          text,
          companionKey,
          sessionId,
          callId,
          creationTaskId,
          error: secondError,
          eventKey: 'event-2',
        });

        assert.ok(second !== undefined);
        assert.equal(second!.recoveryId, first!.recoveryId, 'Merged item must retain recoveryId');
        assert.equal(second!.attempts, 2, 'Attempts count must increment to 2');
        assert.ok(second!.seq > initialSeq, 'Seq must update to newer global sequence');

        // Check error clearance contract (§4.6.2):
        // If the new event has no error, stale error must be cleared (undefined)
        if (!secondHasError) {
          assert.equal(second!.error, undefined, 'Stale error must be cleared when new event has no error');
        } else {
          assert.equal(second!.error, 'New second error');
        }

        const items = mgr.listItems();
        assert.equal(items.length, 1, 'Only one merged item must exist in storage');
      }
    )
  );
});

test('§5.8.4 Property: Distinct sources, kinds, or texts are never merged', () => {
  assertProperty(
    '§5.8.4 Property: Distinct sources, kinds, or texts are never merged',
    fc.property(
      arbNonBlankString,
      arbCompanionKey,
      arbCompanionKey,
      arbSessionId,
      arbSessionId,
      arbCallId,
      arbCallId,
      arbTaskId,
      arbTaskId,
      (text, comp1, comp2, sess1, sess2, call1, call2, task1, task2) => {
        const c2 = comp1 === comp2 ? comp2 + '-diff' : comp2;
        const s2 = sess1 === sess2 ? sess2 + '-diff' : sess2;
        const q2 = call1 === call2 ? call2 + '-diff' : call2;
        const t2 = task1 === task2 ? task2 + '-diff' : task2;

        // 1. Different companionKeys: same text, never merged
        {
          const mgr = createRecoveryState();
          const itemA = mgr.register({ kind: 'reply_failed', text, companionKey: comp1, eventKey: 'c-1' });
          const itemB = mgr.register({ kind: 'reply_failed', text, companionKey: c2, eventKey: 'c-2' });
          assert.notEqual(itemA?.recoveryId, itemB?.recoveryId);
          assert.equal(mgr.listItems().length, 2);
        }

        // 2. Different sessionIds: same text, never merged
        {
          const mgr = createRecoveryState();
          const itemA = mgr.register({ kind: 'reply_failed', text, companionKey: comp1, sessionId: sess1, eventKey: 's-1' });
          const itemB = mgr.register({ kind: 'reply_failed', text, companionKey: comp1, sessionId: s2, eventKey: 's-2' });
          assert.notEqual(itemA?.recoveryId, itemB?.recoveryId);
          assert.equal(mgr.listItems().length, 2);
        }

        // 3. Different callIds: same text, never merged
        {
          const mgr = createRecoveryState();
          const itemA = mgr.register({ kind: 'reply_failed', text, companionKey: comp1, sessionId: sess1, callId: call1, eventKey: 'q-1' });
          const itemB = mgr.register({ kind: 'reply_failed', text, companionKey: comp1, sessionId: sess1, callId: q2, eventKey: 'q-2' });
          assert.notEqual(itemA?.recoveryId, itemB?.recoveryId);
          assert.equal(mgr.listItems().length, 2);
        }

        // 4. Different creationTaskIds: same text, never merged
        {
          const mgr = createRecoveryState();
          const itemA = mgr.register({ kind: 'session_creation_failed', text, companionKey: comp1, creationTaskId: task1, eventKey: 't-1' });
          const itemB = mgr.register({ kind: 'session_creation_failed', text, companionKey: comp1, creationTaskId: t2, eventKey: 't-2' });
          assert.notEqual(itemA?.recoveryId, itemB?.recoveryId);
          assert.equal(mgr.listItems().length, 2);
        }

        // 5. Different kinds: same text, never merged
        {
          const mgr = createRecoveryState();
          const itemA = mgr.register({ kind: 'reply_failed', text, companionKey: comp1, eventKey: 'k-1' });
          const itemB = mgr.register({ kind: 'session_creation_failed', text, companionKey: comp1, eventKey: 'k-2' });
          assert.notEqual(itemA?.recoveryId, itemB?.recoveryId);
          assert.equal(mgr.listItems().length, 2);
        }

        // 6. Different texts: same context, never merged
        {
          const mgr = createRecoveryState();
          const itemA = mgr.register({ kind: 'reply_failed', text, companionKey: comp1, eventKey: 'txt-1' });
          const itemB = mgr.register({ kind: 'reply_failed', text: text + '_other', companionKey: comp1, eventKey: 'txt-2' });
          assert.notEqual(itemA?.recoveryId, itemB?.recoveryId);
          assert.equal(mgr.listItems().length, 2);
        }
      }
    )
  );
});

test('§5.8.4 Property: Deleted recovery item cannot be resurrected by replaying consumed event', () => {
  assertProperty(
    '§5.8.4 Property: Deleted recovery item cannot be resurrected by replaying consumed event',
    fc.property(
      arbNonBlankString,
      arbNonBlankString,
      arbKind,
      arbCompanionKey,
      (initialText, nextText, kind, compKey) => {
        const mgr = createRecoveryState();
        const eventKey = 'consumed-event-key-1';

        // 1. Initial failure registered with eventKey
        const item1 = mgr.register({
          kind,
          text: initialText,
          companionKey: compKey,
          eventKey,
        });
        assert.ok(item1 !== undefined);
        const recId1 = item1!.recoveryId;
        assert.equal(mgr.isEventConsumed(eventKey), true);

        // 2. User deletes the recovery item explicitly
        const delRes = mgr.deleteItem(recId1);
        assert.equal(delRes, true);
        assert.equal(mgr.getItem(recId1), undefined);
        assert.equal(mgr.listItems().length, 0);

        // 3. Stale event with the consumed eventKey is replayed -> MUST NOT resurrect item
        const replayRes = mgr.register({
          kind,
          text: initialText,
          companionKey: compKey,
          eventKey,
        });
        assert.equal(replayRes, undefined, 'Replaying consumed eventKey after deletion must return undefined');
        assert.equal(mgr.getItem(recId1), undefined, 'Deleted item must not be resurrected');
        assert.equal(mgr.listItems().length, 0, 'Storage must remain empty');

        // 4. A NEW event with a DIFFERENT eventKey CAN be registered cleanly
        const newItem = mgr.register({
          kind,
          text: nextText,
          companionKey: compKey,
          eventKey: 'new-unconsumed-key-2',
        });
        assert.ok(newItem !== undefined, 'New eventKey must be registered cleanly');
        assert.notEqual(newItem!.recoveryId, recId1, 'New item must have a fresh recoveryId');
        assert.equal(mgr.listItems().length, 1);
        assert.equal(mgr.listItems()[0].text, nextText);
      }
    )
  );
});

test('§5.8.4 Property: Multi-step random command sequence maintains sorting, isolation, and deletion invariants', () => {
  type RecoveryCommand =
    | {
        type: 'register';
        text: string;
        kind: RecoveryItemKind;
        companionKey: string;
        sessionId?: string;
        callId?: string;
        creationTaskId?: string;
        eventKey?: string;
        error?: string;
      }
    | {
        type: 'replay';
        eventKeyIndex: number;
      }
    | {
        type: 'delete';
        index: number;
      }
    | {
        type: 'clear';
      }
    | {
        type: 'filter';
        companionKey?: string;
        sessionId?: string;
        includeOtherSessions?: boolean;
      };

  const arbCommand: fc.Arbitrary<RecoveryCommand> = fc.oneof(
    fc.record({
      type: fc.constant('register' as const),
      text: arbKoreanOrAsciiString,
      kind: arbKind,
      companionKey: arbCompanionKey,
      sessionId: arbSessionId,
      callId: arbCallId,
      creationTaskId: arbTaskId,
      eventKey: fc.oneof(fc.constantFrom('ev-shared-1', 'ev-shared-2', 'ev:3'), fc.uuid()),
      error: fc.oneof(fc.constant(undefined), fc.constant('error: timeout'), fc.constant('error: connection reset')),
    }),
    fc.record({
      type: fc.constant('replay' as const),
      eventKeyIndex: fc.integer({ min: 0, max: 50 }),
    }),
    fc.record({
      type: fc.constant('delete' as const),
      index: fc.integer({ min: 0, max: 20 }),
    }),
    fc.record({
      type: fc.constant('clear' as const),
    }),
    fc.record({
      type: fc.constant('filter' as const),
      companionKey: fc.oneof(fc.constant(undefined), arbCompanionKey),
      sessionId: fc.oneof(fc.constant(undefined), arbSessionId),
      includeOtherSessions: fc.boolean(),
    })
  );

  assertProperty(
    '§5.8.4 Property: Multi-step random command sequence maintains sorting, isolation, and deletion invariants',
    fc.property(
      fc.array(arbCommand, { minLength: 1, maxLength: 40 }),
      (commands) => {
        const mgr = createRecoveryState();
        const consumedKeys: string[] = [];

        for (const cmd of commands) {
          if (cmd.type === 'register') {
            const res = mgr.register(cmd);

            if (cmd.text.length === 0) {
              assert.equal(res, undefined);
            } else if (res) {
              assert.ok(res.text.length > 0);
              assert.ok(res.seq >= 1);
              if (cmd.eventKey && !consumedKeys.includes(cmd.eventKey)) {
                consumedKeys.push(cmd.eventKey);
              }
            }
          } else if (cmd.type === 'replay') {
            if (consumedKeys.length > 0) {
              const key = consumedKeys[cmd.eventKeyIndex % consumedKeys.length];
              const replayRes = mgr.register({
                kind: 'reply_failed',
                text: 'replay attempt text',
                companionKey: '/workspace/repo-a',
                eventKey: key,
              });
              assert.equal(replayRes, undefined, 'Replaying already consumed eventKey must return undefined');
              assert.equal(mgr.isEventConsumed(key), true);
            }
          } else if (cmd.type === 'delete') {
            const current = mgr.listItems();
            if (current.length > 0) {
              const target = current[cmd.index % current.length];
              const delRes = mgr.deleteItem(target.recoveryId);
              assert.equal(delRes, true);
              assert.equal(mgr.getItem(target.recoveryId), undefined);
            }
          } else if (cmd.type === 'clear') {
            mgr.clear();
            assert.equal(mgr.listItems().length, 0);
            consumedKeys.length = 0;
          } else if (cmd.type === 'filter') {
            const filtered = mgr.listItems({
              companionKey: cmd.companionKey,
              sessionId: cmd.sessionId,
              includeOtherSessions: cmd.includeOtherSessions,
            });

            // Invariant: list is always sorted descending by seq
            for (let i = 0; i < filtered.length - 1; i++) {
              assert.ok(
                filtered[i].seq >= filtered[i + 1].seq,
                `Items must be sorted descending by seq: ${filtered[i].seq} >= ${filtered[i + 1].seq}`
              );
            }

            // Invariant: filtered companionKey strictly matches
            if (cmd.companionKey !== undefined) {
              for (const it of filtered) {
                assert.equal(it.companionKey, cmd.companionKey);
              }
            }

            // Invariant: sessionId matches unless includeOtherSessions is true
            if (cmd.sessionId !== undefined && !cmd.includeOtherSessions) {
              for (const it of filtered) {
                assert.ok(
                  it.sessionId === cmd.sessionId || !it.sessionId,
                  `Session ID must match or be unassigned: ${it.sessionId} === ${cmd.sessionId}`
                );
              }
            }
          }
        }
      }
    )
  );
});
