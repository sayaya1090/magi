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
      arbNonBlankString,
      arbCompanionKey,
      (text1, text2, compKey) => {
        // Ensure text1 and text2 are distinct
        const distinctText2 = text1 === text2 ? text2 + '_distinct' : text2;
        const mgr = createRecoveryState();

        const item1 = mgr.register({
          kind: 'reply_failed',
          text: text1,
          companionKey: compKey,
          eventKey: 'key-1',
        });
        const item2 = mgr.register({
          kind: 'reply_failed',
          text: distinctText2,
          companionKey: compKey,
          eventKey: 'key-2',
        });

        assert.ok(item1 !== undefined);
        assert.ok(item2 !== undefined);
        assert.notEqual(item1!.recoveryId, item2!.recoveryId, 'Different texts must produce distinct recovery items');
        assert.equal(mgr.listItems().length, 2);
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

        // 1. Register initial item
        const item = mgr.register({
          kind,
          text: initialText,
          companionKey: compKey,
          eventKey,
        });
        assert.ok(item !== undefined);
        assert.equal(mgr.listItems().length, 1);

        // 2. Explicitly delete the item
        const deleted = mgr.deleteItem(item!.recoveryId);
        assert.equal(deleted, true);
        assert.equal(mgr.getItem(item!.recoveryId), undefined);
        assert.equal(mgr.listItems().length, 0);

        // 3. Replay the exact consumed eventKey
        const replayed = mgr.register({
          kind,
          text: initialText,
          companionKey: compKey,
          eventKey,
        });
        assert.equal(replayed, undefined, 'Replay of consumed event must return undefined');
        assert.equal(mgr.listItems().length, 0, 'Deleted item must NOT be resurrected');

        // 4. A brand new failure with a new eventKey must register successfully
        const newItem = mgr.register({
          kind,
          text: nextText,
          companionKey: compKey,
          eventKey: 'fresh-new-event-key-2',
        });
        assert.ok(newItem !== undefined);
        assert.notEqual(newItem!.recoveryId, item!.recoveryId);
        assert.equal(mgr.listItems().length, 1);
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
        type: 'delete';
        index: number;
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
      type: fc.constant('delete' as const),
      index: fc.integer({ min: 0, max: 20 }),
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

        for (const cmd of commands) {
          if (cmd.type === 'register') {
            const res = mgr.register(cmd);

            if (cmd.text.length === 0) {
              assert.equal(res, undefined);
            } else if (res) {
              assert.ok(res.text.length > 0);
              assert.ok(res.seq >= 1);
            }
          } else if (cmd.type === 'delete') {
            const current = mgr.listItems();
            if (current.length > 0) {
              const target = current[cmd.index % current.length];
              const delRes = mgr.deleteItem(target.recoveryId);
              assert.equal(delRes, true);
              assert.equal(mgr.getItem(target.recoveryId), undefined);
            }
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
