import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fc from 'fast-check';
import {
  createAnswerState,
  AskEvent,
  ReplyResultEvent,
} from '../core/answer_state';
import {
  assertProperty,
  arbKoreanOrAsciiString,
  arbNonBlankString,
  arbBlankString,
} from './support/fc_helpers';

const arbCompanionKey = fc.constantFrom(
  '/workspace/repo-a',
  '/workspace/repo-b',
  'comp:1',
  'comp:colon:2'
);

const arbSessionId = fc.constantFrom(
  'sess-1',
  'sess-2',
  'sess:alpha',
  'sess:beta'
);

const arbCallId = fc.constantFrom('q-shared', 'q-alpha', 'q-beta', 'q:choice:1');

test('§5.8.4 Property: Draft isolation across companions, sessions, and questions', () => {
  assertProperty(
    '§5.8.4 Property: Draft isolation across companions, sessions, and questions',
    fc.property(
      arbCompanionKey,
      arbCompanionKey,
      arbSessionId,
      arbSessionId,
      arbCallId,
      arbNonBlankString,
      arbNonBlankString,
      (compA, compB, sessA, sessB, callId, draftGeneral, draftQuestion) => {
        // Ensure distinct context targets
        const comp2 = compA === compB ? compB + '-alt' : compB;
        const sess2 = sessA === sessB ? sessB + '-alt' : sessB;

        const mgr = createAnswerState();

        // 1. In Context (compA, sessA): Set general draft and question draft
        mgr.switchContext(compA, sessA);
        mgr.onInputChange(draftGeneral);

        mgr.onAskChange({
          kind: 'question',
          callId,
          what: 'Question in A',
          options: ['Option 1'],
        });
        mgr.enterAnswerMode(callId, 'Question in A', draftGeneral);
        mgr.onInputChange(draftQuestion);

        // Verify stored drafts in context A
        assert.equal(mgr.getGeneralDraft(compA, sessA), draftGeneral);
        assert.equal(mgr.getQuestionDraft(callId, compA, sessA), draftQuestion);

        // 2. In Context (comp2, sessA) - different companion: Must be isolated
        assert.equal(mgr.getGeneralDraft(comp2, sessA), '');
        assert.equal(mgr.getQuestionDraft(callId, comp2, sessA), '');
        assert.equal(mgr.getPendingQuestion(comp2, sessA), null);

        // 3. In Context (compA, sess2) - different session: Must be isolated
        assert.equal(mgr.getGeneralDraft(compA, sess2), '');
        assert.equal(mgr.getQuestionDraft(callId, compA, sess2), '');
        assert.equal(mgr.getPendingQuestion(compA, sess2), null);

        // 4. Switch to Context (comp2, sess2) and verify returned input text
        const swRes = mgr.switchContext(comp2, sess2, { currentInputText: draftQuestion });
        assert.equal(swRes.nextInputText, '');
        assert.equal(mgr.getGeneralDraft(comp2, sess2), '');

        // 5. Switch back to Context (compA, sessA) and verify draft restoration
        const swBack = mgr.switchContext(compA, sessA);
        assert.equal(swBack.companionKey, compA);
        assert.equal(swBack.sessionId, sessA);
        assert.equal(mgr.getQuestionDraft(callId, compA, sessA), draftQuestion);
        assert.equal(mgr.getGeneralDraft(compA, sessA), draftGeneral);
      }
    )
  );
});

test('§5.8.4 Property: In-flight locking and duplicate submission prevention per context', () => {
  assertProperty(
    '§5.8.4 Property: In-flight locking and duplicate submission prevention per context',
    fc.property(
      arbCompanionKey,
      arbCompanionKey,
      arbSessionId,
      arbSessionId,
      arbCallId,
      arbNonBlankString,
      (compA, compB, sessA, sessB, callId, replyText) => {
        const comp2 = compA === compB ? compB + '-alt' : compB;
        const sess2 = sessA === sessB ? sessB + '-alt' : sessB;

        const mgr = createAnswerState();
        mgr.switchContext(compA, sessA);

        mgr.onAskChange({
          kind: 'question',
          callId,
          what: 'Lock Test Question',
        });
        mgr.enterAnswerMode(callId, 'Lock Test', '');
        mgr.onInputChange(replyText);

        // 1. First submission succeeds and locks question
        const sub1 = mgr.submitReply(callId, replyText);
        assert.equal(sub1.ok, true);
        assert.equal(sub1.action, 'reply');
        assert.ok(typeof sub1.attemptId === 'number' && sub1.attemptId > 0);
        assert.equal(mgr.isInFlight(callId, compA, sessA), true);

        const inFlight = mgr.getInFlight(callId, compA, sessA);
        assert.ok(inFlight !== undefined);
        assert.equal(inFlight!.attemptId, sub1.attemptId);

        // 2. Duplicate submission for the same question in the same context fails
        const sub2 = mgr.submitReply(callId, 'second text attempt');
        assert.equal(sub2.ok, false);
        assert.equal(sub2.error, 'in_flight');
        // Existing attempt and lock remain unchanged
        assert.equal(mgr.isInFlight(callId, compA, sessA), true);
        assert.equal(mgr.getInFlight(callId, compA, sessA)!.attemptId, sub1.attemptId);

        // 3. Different context with the same callId is independent and not locked
        assert.equal(mgr.isInFlight(callId, comp2, sess2), false);
        mgr.switchContext(comp2, sess2);
        mgr.onAskChange({
          kind: 'question',
          callId,
          what: 'Same callId in Context 2',
        });
        const subContext2 = mgr.submitReply(callId, 'context 2 reply');
        assert.equal(subContext2.ok, true);
        assert.equal(mgr.isInFlight(callId, comp2, sess2), true);
        assert.notEqual(subContext2.attemptId, sub1.attemptId);
      }
    )
  );
});

test('§5.8.4 Property: A submitted -> B modified -> A failed protects modified draft B and preserves A in recovery', () => {
  assertProperty(
    '§5.8.4 Property: A submitted -> B modified -> A failed protects modified draft B and preserves A in recovery',
    fc.property(
      arbCompanionKey,
      arbSessionId,
      arbCallId,
      arbNonBlankString,
      arbNonBlankString,
      arbKoreanOrAsciiString,
      (compKey, sessionId, callId, textA, textB, generalDraft) => {
        const mgr = createAnswerState();
        mgr.switchContext(compKey, sessionId);

        // Set initial general draft
        mgr.onInputChange(generalDraft);

        // Setup question and type text A
        mgr.onAskChange({
          kind: 'question',
          callId,
          what: 'Question Header',
        });
        mgr.enterAnswerMode(callId, 'Question Header', generalDraft);
        mgr.onInputChange(textA);

        // 1. Submit attempt A
        const subA = mgr.submitReply(callId, textA, false, 'Question Header');
        assert.equal(subA.ok, true);
        const attemptAId = subA.attemptId!;
        assert.equal(mgr.isInFlight(callId), true);

        // 2. While in-flight, user modifies question draft to text B
        mgr.enterAnswerMode(callId, 'Question Header', generalDraft);
        mgr.onInputChange(textB);
        assert.equal(mgr.getQuestionDraft(callId), textB);

        // 3. Server returns failure for attempt A
        const outcome = mgr.onReplyResult(
          {
            callId,
            attemptId: attemptAId,
            ok: false,
            error: 'Server timeout',
            companionKey: compKey,
            session: sessionId,
            generation: subA.generation,
            webviewId: subA.webviewId,
          },
          { kind: 'question', callId, what: 'Question Header' }
        );

        assert.equal(outcome.handled, true);
        assert.equal(outcome.ok, false);

        // Invariant: Modified draft B is strictly protected (NOT overwritten)
        assert.equal(mgr.getQuestionDraft(callId), textB, 'Draft B must not be overwritten by failed attempt A');
        assert.equal(mgr.isInFlight(callId), false, 'In-flight lock must be released');

        // Invariant: General draft is untouched
        assert.equal(mgr.getGeneralDraft(), generalDraft, 'General draft must be preserved');

        // Invariant: Recovery item stores original submitted text A (not text B and not server text)
        const recoveryItems = mgr.getRecoveryState().listItems();
        assert.equal(recoveryItems.length, 1);
        const recItem = recoveryItems[0];
        // Trimmed comparison because direct reply submission trims text
        assert.equal(recItem.text, textA.trim(), 'Recovery item must preserve submitted text A');
        assert.equal(recItem.kind, 'reply_failed');
      }
    )
  );
});

test('§5.8.4 Property: A failed -> B resubmitted -> A duplicate result arrives protects in-flight attempt B', () => {
  assertProperty(
    '§5.8.4 Property: A failed -> B resubmitted -> A duplicate result arrives protects in-flight attempt B',
    fc.property(
      arbCompanionKey,
      arbSessionId,
      arbCallId,
      arbNonBlankString,
      arbNonBlankString,
      fc.boolean(),
      (compKey, sessionId, callId, textA, textB, duplicateOk) => {
        const mgr = createAnswerState();
        mgr.switchContext(compKey, sessionId);

        mgr.onAskChange({ kind: 'question', callId, what: 'Q' });

        // 1. Submit attempt A
        const subA = mgr.submitReply(callId, textA, true, 'Q'); // choice submission preserves verbatim
        assert.equal(subA.ok, true);
        const attemptAId = subA.attemptId!;

        // 2. Attempt A fails
        mgr.onReplyResult(
          {
            callId,
            attemptId: attemptAId,
            ok: false,
            error: 'Network failure',
            companionKey: compKey,
            session: sessionId,
            generation: subA.generation,
            webviewId: subA.webviewId,
          },
          { kind: 'question', callId, what: 'Q' }
        );
        assert.equal(mgr.isInFlight(callId), false);
        const recCountAfterA = mgr.getRecoveryState().listItems().length;

        // 3. User resubmits attempt B
        const subB = mgr.submitReply(callId, textB, true, 'Q');
        assert.equal(subB.ok, true);
        const attemptBId = subB.attemptId!;
        assert.notEqual(attemptBId, attemptAId);
        assert.equal(mgr.isInFlight(callId), true);
        assert.equal(mgr.getInFlight(callId)!.attemptId, attemptBId);

        // 4. Stale / duplicate result for attempt A arrives
        mgr.onReplyResult(
          {
            callId,
            attemptId: attemptAId,
            ok: duplicateOk,
            error: duplicateOk ? undefined : 'Late fail',
            companionKey: compKey,
            session: sessionId,
            generation: subA.generation,
            webviewId: subA.webviewId,
          },
          { kind: 'question', callId, what: 'Q' }
        );

        // Invariant: Stale attempt A result must not affect active attempt B
        assert.equal(mgr.isInFlight(callId), true, 'Attempt B must remain in flight');
        assert.equal(mgr.getInFlight(callId)!.attemptId, attemptBId, 'Attempt B attemptId must remain active');
        // No duplicate recovery item should be added for consumed attempt A
        assert.equal(mgr.getRecoveryState().listItems().length, recCountAfterA);

        // 5. Final attempt B result arrives
        const outcomeB = mgr.onReplyResult(
          {
            callId,
            attemptId: attemptBId,
            ok: true,
            companionKey: compKey,
            session: sessionId,
            generation: subB.generation,
            webviewId: subB.webviewId,
          },
          { kind: 'question', callId, what: 'Q' }
        );

        assert.equal(outcomeB.handled, true);
        assert.equal(outcomeB.ok, true);
        assert.equal(mgr.isInFlight(callId), false, 'Attempt B resolved cleanly');
      }
    )
  );
});

test('§5.8.4 Property: Late result across context switch updates target context storage without mutating active context', () => {
  assertProperty(
    '§5.8.4 Property: Late result across context switch updates target context storage without mutating active context',
    fc.property(
      arbCompanionKey,
      arbCompanionKey,
      arbSessionId,
      arbSessionId,
      arbCallId,
      arbNonBlankString,
      arbNonBlankString,
      (compA, compB, sessA, sessB, callId, replyA, activeDraftB) => {
        const comp2 = compA === compB ? compB + '-alt' : compB;
        const sess2 = sessA === sessB ? sessB + '-alt' : sessB;

        const mgr = createAnswerState();

        // 1. Submit attempt in Context A
        mgr.switchContext(compA, sessA);
        mgr.onAskChange({ kind: 'question', callId, what: 'Question in A' });
        const subA = mgr.submitReply(callId, replyA, true);
        assert.equal(subA.ok, true);
        const attemptAId = subA.attemptId!;
        assert.equal(mgr.isInFlight(callId, compA, sessA), true);

        // 2. Switch to Context B and type active input
        mgr.switchContext(comp2, sess2);
        mgr.onInputChange(activeDraftB);
        assert.equal(mgr.getGeneralDraft(comp2, sess2), activeDraftB);
        assert.equal(mgr.getPendingQuestion(comp2, sess2), null);

        // 3. Late result for Context A arrives
        const outcome = mgr.onReplyResult(
          {
            callId,
            attemptId: attemptAId,
            ok: false,
            error: 'Late error',
            companionKey: compA,
            session: sessA,
            generation: subA.generation,
            webviewId: subA.webviewId,
          },
          null // active ask in Context B is null
        );

        assert.equal(outcome.handled, true);
        assert.equal(outcome.restoredInStoreOnly, true, 'Result should be recorded in store only');

        // Invariant: Context B active draft and mode are completely unaffected
        assert.equal(mgr.getGeneralDraft(comp2, sess2), activeDraftB, 'Context B input must remain untouched');
        assert.equal(mgr.getPendingQuestion(comp2, sess2), null, 'Context B mode must remain general');

        // Invariant: Context A stored state was updated
        assert.equal(mgr.isInFlight(callId, compA, sessA), false, 'Context A in-flight status must be cleared');
      }
    )
  );
});

test('§5.8.4 Property: Duplicate onReplyResult is strictly idempotent', () => {
  assertProperty(
    '§5.8.4 Property: Duplicate onReplyResult is strictly idempotent',
    fc.property(
      arbCompanionKey,
      arbSessionId,
      arbCallId,
      arbNonBlankString,
      fc.boolean(),
      (compKey, sessionId, callId, text, isOk) => {
        const mgr = createAnswerState();
        mgr.switchContext(compKey, sessionId);
        mgr.onAskChange({ kind: 'question', callId, what: 'Question' });

        const sub = mgr.submitReply(callId, text, true);
        assert.equal(sub.ok, true);

        const event: ReplyResultEvent = {
          callId,
          attemptId: sub.attemptId!,
          ok: isOk,
          error: isOk ? undefined : 'Some failure',
          companionKey: compKey,
          session: sessionId,
          generation: sub.generation,
          webviewId: sub.webviewId,
        };

        // First application
        const out1 = mgr.onReplyResult(event, { kind: 'question', callId, what: 'Question' });
        assert.equal(out1.handled, true);
        const recoveryCount1 = mgr.getRecoveryState().listItems().length;
        const failedDrafts1 = mgr.getFailedDrafts(callId).length;

        // Second application of the exact same event
        mgr.onReplyResult(event, { kind: 'question', callId, what: 'Question' });

        // Invariant: Second application does not duplicate recovery items or failed drafts
        assert.equal(mgr.getRecoveryState().listItems().length, recoveryCount1);
        assert.equal(mgr.getFailedDrafts(callId).length, failedDrafts1);
        assert.equal(mgr.isInFlight(callId), false);
      }
    )
  );
});

test('§5.8.4 Property: Blank and whitespace-only text is rejected by submitReply with { ok: false, error: "empty" }', () => {
  assertProperty(
    '§5.8.4 Property: Blank and whitespace-only text is rejected by submitReply with { ok: false, error: "empty" }',
    fc.property(
      arbCompanionKey,
      arbSessionId,
      arbCallId,
      arbBlankString,
      fc.boolean(),
      (compKey, sessionId, callId, blankText, isChoice) => {
        const mgr = createAnswerState();
        mgr.switchContext(compKey, sessionId);
        mgr.onAskChange({ kind: 'question', callId, what: 'Blank Test' });
        mgr.enterAnswerMode(callId, 'Blank Test', blankText);

        const res = mgr.submitReply(callId, blankText, isChoice);
        assert.equal(res.ok, false);
        assert.equal(res.error, 'empty');
        assert.equal(mgr.isInFlight(callId), false);
      }
    )
  );
});

test('§5.8.4 Property: applyRecoveryDraft requires confirmation when general draft exists and preserves drafts and in-flight locks', () => {
  assertProperty(
    '§5.8.4 Property: applyRecoveryDraft requires confirmation when general draft exists and preserves drafts and in-flight locks',
    fc.property(
      arbCompanionKey,
      arbSessionId,
      arbCallId,
      arbCallId,
      arbNonBlankString,
      arbNonBlankString,
      arbNonBlankString,
      arbKoreanOrAsciiString,
      fc.boolean(),
      (compKey, sessionId, callId, lockCallId, recoveryText, questionDraft, lockReplyText, generalDraft, hasGeneralDraft) => {
        const targetLockQ = lockCallId === callId ? lockCallId + '-locked' : lockCallId;
        const mgr = createAnswerState();
        mgr.switchContext(compKey, sessionId);

        const gDraft = hasGeneralDraft ? (generalDraft.length > 0 ? generalDraft : '기존 일반 초안') : '';
        mgr.onInputChange(gDraft);

        // 1. Create an active in-flight reply on targetLockQ to verify in-flight preservation
        mgr.onAskChange({ kind: 'question', callId: targetLockQ, what: 'Lock Question' });
        const subInFlight = mgr.submitReply(targetLockQ, lockReplyText, true, 'Lock Question');
        assert.equal(subInFlight.ok, true);
        const inFlightAttemptId = subInFlight.attemptId!;
        assert.equal(mgr.isInFlight(targetLockQ), true);

        // 2. Setup question draft for callId in answer mode
        mgr.onAskChange({ kind: 'question', callId, what: 'Question with recovery' });
        mgr.enterAnswerMode(callId, 'Question with recovery', gDraft);
        mgr.onInputChange(questionDraft);

        // 3. Register recovery item
        const item = mgr.getRecoveryState().register({
          companionKey: compKey,
          sessionId,
          callId,
          kind: 'reply_failed',
          text: recoveryText,
        });
        assert.ok(item !== null);
        const recoveryId = item!.recoveryId;

        if (gDraft.length > 0) {
          // Without append confirmation: rejected with requires_confirm
          const rej = mgr.applyRecoveryDraft({
            recoveryId,
            append: false,
            currentInputText: questionDraft,
          });
          assert.equal(rej.ok, false);
          assert.equal(rej.reason, 'requires_confirm');
          assert.equal(rej.existingDraft, gDraft);
          assert.equal(mgr.getQuestionDraft(callId), questionDraft);
          assert.equal(mgr.getGeneralDraft(), gDraft);

          // In-flight lock on targetLockQ MUST remain untouched
          assert.equal(mgr.isInFlight(targetLockQ), true);
          assert.equal(mgr.getInFlight(targetLockQ)!.attemptId, inFlightAttemptId);

          // With append confirmation: confirmed and appended
          const app = mgr.applyRecoveryDraft({
            recoveryId,
            append: true,
            currentInputText: questionDraft,
          });
          assert.equal(app.ok, true);
          assert.equal(app.exitAnswerMode, true);
          const expectedCombined = gDraft + '\n\n' + recoveryText;
          assert.equal(app.nextInputText, expectedCombined);
          assert.equal(mgr.getGeneralDraft(), expectedCombined);
          assert.equal(mgr.getQuestionDraft(callId), questionDraft);

          // In-flight lock on targetLockQ MUST still remain untouched
          assert.equal(mgr.isInFlight(targetLockQ), true);
          assert.equal(mgr.getInFlight(targetLockQ)!.attemptId, inFlightAttemptId);
        } else {
          // Empty general draft: direct copy
          const app = mgr.applyRecoveryDraft({
            recoveryId,
            append: false,
            currentInputText: questionDraft,
          });
          assert.equal(app.ok, true);
          assert.equal(app.exitAnswerMode, true);
          assert.equal(app.nextInputText, recoveryText);
          assert.equal(mgr.getGeneralDraft(), recoveryText);
          assert.equal(mgr.getQuestionDraft(callId), questionDraft);

          // In-flight lock on targetLockQ MUST remain untouched
          assert.equal(mgr.isInFlight(targetLockQ), true);
          assert.equal(mgr.getInFlight(targetLockQ)!.attemptId, inFlightAttemptId);
        }

        // Invariant: Recovery item in recoveryState is NOT deleted or altered
        const listed = mgr.getRecoveryState().getItem(recoveryId);
        assert.ok(listed !== undefined);
        assert.equal(listed!.text, recoveryText);
      }
    )
  );
});

test('§5.8.4 Property: Multi-step random command sequence for answer_state maintains invariants', () => {
  interface DispatchedAttempt {
    attemptId: number;
    callId: string;
    companionKey: string;
    sessionId: string;
    generation?: number;
    webviewId?: string;
    submittedText: string;
    isChoice: boolean;
    resolved: boolean;
  }

  type StateCommand =
    | { type: 'switch'; comp: string; sess: string; input: string }
    | { type: 'type'; text: string }
    | { type: 'ask'; callId: string; what: string }
    | { type: 'enter'; callId: string }
    | { type: 'exit' }
    | { type: 'submitReply'; callId: string; text: string; isChoice: boolean }
    | {
        type: 'replyResult';
        attemptIndex: number;
        variant: 'valid' | 'mismatched_id' | 'mismatched_comp' | 'mismatched_sess' | 'stale_replay';
        ok: boolean;
      }
    | { type: 'applyRecovery'; append: boolean };

  const arbStateCommand: fc.Arbitrary<StateCommand> = fc.oneof(
    fc.record({
      type: fc.constant('switch' as const),
      comp: arbCompanionKey,
      sess: arbSessionId,
      input: arbKoreanOrAsciiString,
    }),
    fc.record({
      type: fc.constant('type' as const),
      text: arbKoreanOrAsciiString,
    }),
    fc.record({
      type: fc.constant('ask' as const),
      callId: arbCallId,
      what: arbNonBlankString,
    }),
    fc.record({
      type: fc.constant('enter' as const),
      callId: arbCallId,
    }),
    fc.record({
      type: fc.constant('exit' as const),
    }),
    fc.record({
      type: fc.constant('submitReply' as const),
      callId: arbCallId,
      text: arbKoreanOrAsciiString,
      isChoice: fc.boolean(),
    }),
    fc.record({
      type: fc.constant('replyResult' as const),
      attemptIndex: fc.integer({ min: 0, max: 100 }),
      variant: fc.constantFrom(
        'valid' as const,
        'valid' as const,
        'mismatched_id' as const,
        'mismatched_comp' as const,
        'mismatched_sess' as const,
        'stale_replay' as const
      ),
      ok: fc.boolean(),
    }),
    fc.record({
      type: fc.constant('applyRecovery' as const),
      append: fc.boolean(),
    })
  );

  assertProperty(
    '§5.8.4 Property: Multi-step random command sequence for answer_state maintains invariants',
    fc.property(
      fc.array(arbStateCommand, { minLength: 1, maxLength: 40 }),
      (commands) => {
        const mgr = createAnswerState();
        let currentInput = '';
        let currentAsk: AskEvent | null = {
          kind: 'question',
          callId: 'q-init',
          what: 'Initial Question',
        };

        // Initialize with context and an active ask
        mgr.switchContext('/workspace/repo-a', 'sess-1');
        mgr.onAskChange(currentAsk);

        // Initial submission to ensure baseline attempt exists in ledger
        const initSub = mgr.submitReply('q-init', 'initial valid reply', true, 'Initial Question');
        assert.equal(initSub.ok, true);
        assert.ok(typeof initSub.attemptId === 'number' && initSub.attemptId > 0);

        const ledger: DispatchedAttempt[] = [
          {
            attemptId: initSub.attemptId!,
            callId: 'q-init',
            companionKey: '/workspace/repo-a',
            sessionId: 'sess-1',
            generation: initSub.generation,
            webviewId: initSub.webviewId,
            submittedText: 'initial valid reply',
            isChoice: true,
            resolved: false,
          },
        ];

        let submitsExecuted = 1;
        let resultsExecuted = 0;

        for (const cmd of commands) {
          if (cmd.type === 'switch') {
            const res = mgr.switchContext(cmd.comp, cmd.sess, {
              currentInputText: currentInput,
              activeAsk: currentAsk,
            });
            currentInput = res.nextInputText;
          } else if (cmd.type === 'type') {
            currentInput = cmd.text;
            const target = mgr.onInputChange(cmd.text);
            const ctx = mgr.getCurrentContext();
            if (target.target === 'general') {
              assert.equal(mgr.getGeneralDraft(ctx.companionKey, ctx.sessionId), cmd.text);
            } else {
              assert.equal(mgr.getQuestionDraft(target.target, ctx.companionKey, ctx.sessionId), cmd.text);
            }
          } else if (cmd.type === 'ask') {
            currentAsk = {
              kind: 'question',
              callId: cmd.callId,
              what: cmd.what,
            };
            const modeRes = mgr.onAskChange(currentAsk, currentInput);
            if (modeRes.nextInputText !== undefined) {
              currentInput = modeRes.nextInputText;
            }
          } else if (cmd.type === 'enter') {
            const modeRes = mgr.enterAnswerMode(cmd.callId, 'Title', currentInput);
            if (modeRes.nextInputText !== undefined) {
              currentInput = modeRes.nextInputText;
            }
          } else if (cmd.type === 'exit') {
            const modeRes = mgr.exitAnswerMode(currentInput);
            if (modeRes.nextInputText !== undefined) {
              currentInput = modeRes.nextInputText;
            }
          } else if (cmd.type === 'submitReply') {
            const ctx = mgr.getCurrentContext();
            const inFlightBefore = mgr.isInFlight(cmd.callId, ctx.companionKey, ctx.sessionId);
            const isBlank = !cmd.text.trim();

            const res = mgr.submitReply(cmd.callId, cmd.text, cmd.isChoice);
            if (isBlank) {
              assert.equal(res.ok, false);
              assert.equal(res.error, 'empty');
              assert.equal(mgr.isInFlight(cmd.callId, ctx.companionKey, ctx.sessionId), inFlightBefore);
            } else if (inFlightBefore) {
              assert.equal(res.ok, false);
              assert.equal(res.error, 'in_flight');
              assert.equal(mgr.isInFlight(cmd.callId, ctx.companionKey, ctx.sessionId), true);
            } else {
              assert.equal(res.ok, true);
              assert.ok(typeof res.attemptId === 'number' && res.attemptId > 0);
              assert.equal(mgr.isInFlight(cmd.callId, ctx.companionKey, ctx.sessionId), true);
              ledger.push({
                attemptId: res.attemptId!,
                callId: cmd.callId,
                companionKey: ctx.companionKey,
                sessionId: ctx.sessionId,
                generation: res.generation,
                webviewId: res.webviewId,
                submittedText: cmd.isChoice ? cmd.text : cmd.text.trim(),
                isChoice: cmd.isChoice,
                resolved: false,
              });
              submitsExecuted++;
              if (res.nextInputText !== undefined) {
                currentInput = res.nextInputText;
              }
            }
          } else if (cmd.type === 'replyResult') {
            if (ledger.length > 0) {
              const target = ledger[cmd.attemptIndex % ledger.length];
              const ctx = mgr.getCurrentContext();

              if (cmd.variant === 'valid' && !target.resolved) {
                // Must be in flight before applying valid result
                assert.equal(mgr.isInFlight(target.callId, target.companionKey, target.sessionId), true);
                const activeGeneralBefore = mgr.getGeneralDraft(ctx.companionKey, ctx.sessionId);
                const activePendingBefore = mgr.getPendingQuestion(ctx.companionKey, ctx.sessionId);

                const event: ReplyResultEvent = {
                  callId: target.callId,
                  attemptId: target.attemptId,
                  ok: cmd.ok,
                  error: cmd.ok ? undefined : 'Command error',
                  companionKey: target.companionKey,
                  session: target.sessionId,
                  generation: target.generation,
                  webviewId: target.webviewId,
                };

                const outcome = mgr.onReplyResult(event, currentAsk);
                assert.equal(outcome.handled, true, 'Valid replyResult must be handled by state manager');
                // Lock must be released
                assert.equal(mgr.isInFlight(target.callId, target.companionKey, target.sessionId), false);

                // If result belongs to non-active context, active context must NOT be mutated
                const isCurrent =
                  target.companionKey === ctx.companionKey && target.sessionId === ctx.sessionId;
                if (!isCurrent) {
                  if (!cmd.ok) {
                    assert.equal(outcome.restoredInStoreOnly, true);
                  }
                  assert.equal(mgr.getGeneralDraft(ctx.companionKey, ctx.sessionId), activeGeneralBefore);
                  assert.equal(mgr.getPendingQuestion(ctx.companionKey, ctx.sessionId), activePendingBefore);
                }

                if (outcome.nextInputText !== undefined) {
                  currentInput = outcome.nextInputText;
                }

                target.resolved = true;
                resultsExecuted++;
              } else if (cmd.variant === 'mismatched_id') {
                const badEvent: ReplyResultEvent = {
                  callId: target.callId,
                  attemptId: target.attemptId + 999999,
                  ok: cmd.ok,
                  error: cmd.ok ? undefined : 'Bad attempt error',
                  companionKey: target.companionKey,
                  session: target.sessionId,
                  generation: target.generation,
                  webviewId: target.webviewId,
                };
                const inFlightBefore = mgr.isInFlight(target.callId, target.companionKey, target.sessionId);
                const outcome = mgr.onReplyResult(badEvent, currentAsk);
                assert.equal(outcome.handled, false, 'Mismatched attemptId must be rejected');
                assert.equal(mgr.isInFlight(target.callId, target.companionKey, target.sessionId), inFlightBefore);
                resultsExecuted++;
              } else if (cmd.variant === 'mismatched_comp') {
                const badEvent: ReplyResultEvent = {
                  callId: target.callId,
                  attemptId: target.attemptId,
                  ok: cmd.ok,
                  error: cmd.ok ? undefined : 'Bad companion error',
                  companionKey: target.companionKey + '-wrong',
                  session: target.sessionId,
                  generation: target.generation,
                  webviewId: target.webviewId,
                };
                const inFlightBefore = mgr.isInFlight(target.callId, target.companionKey, target.sessionId);
                const outcome = mgr.onReplyResult(badEvent, currentAsk);
                assert.equal(outcome.handled, false, 'Mismatched companionKey must be rejected');
                assert.equal(mgr.isInFlight(target.callId, target.companionKey, target.sessionId), inFlightBefore);
                resultsExecuted++;
              } else if (cmd.variant === 'mismatched_sess') {
                const badEvent: ReplyResultEvent = {
                  callId: target.callId,
                  attemptId: target.attemptId,
                  ok: cmd.ok,
                  error: cmd.ok ? undefined : 'Bad session error',
                  companionKey: target.companionKey,
                  session: target.sessionId + '-wrong',
                  generation: target.generation,
                  webviewId: target.webviewId,
                };
                const inFlightBefore = mgr.isInFlight(target.callId, target.companionKey, target.sessionId);
                const outcome = mgr.onReplyResult(badEvent, currentAsk);
                assert.equal(outcome.handled, false, 'Mismatched session must be rejected');
                assert.equal(mgr.isInFlight(target.callId, target.companionKey, target.sessionId), inFlightBefore);
                resultsExecuted++;
              } else {
                // If variant is stale_replay or target was already resolved:
                if (!target.resolved) {
                  // Resolve it first so it becomes genuinely resolved
                  const resolveEvent: ReplyResultEvent = {
                    callId: target.callId,
                    attemptId: target.attemptId,
                    ok: true,
                    companionKey: target.companionKey,
                    session: target.sessionId,
                    generation: target.generation,
                    webviewId: target.webviewId,
                  };
                  const res = mgr.onReplyResult(resolveEvent, currentAsk);
                  assert.equal(res.handled, true, 'Initial resolution must succeed before stale replay');
                  target.resolved = true;
                }
                // Replay after resolution must be rejected
                const staleEvent: ReplyResultEvent = {
                  callId: target.callId,
                  attemptId: target.attemptId,
                  ok: cmd.ok,
                  error: cmd.ok ? undefined : 'Stale replay error',
                  companionKey: target.companionKey,
                  session: target.sessionId,
                  generation: target.generation,
                  webviewId: target.webviewId,
                };
                const outcome = mgr.onReplyResult(staleEvent, currentAsk);
                assert.equal(outcome.handled, false, 'Stale replay of resolved attempt must not be handled');
                resultsExecuted++;
              }
            }
          } else if (cmd.type === 'applyRecovery') {
            const items = mgr.getRecoveryState().listItems();
            if (items.length > 0) {
              const item = items[0];
              const ctx = mgr.getCurrentContext();
              const appRes = mgr.applyRecoveryDraft({
                recoveryId: item.recoveryId,
                companionKey: ctx.companionKey,
                sessionId: ctx.sessionId,
                append: cmd.append,
                currentInputText: currentInput,
              });
              if (appRes.ok && appRes.nextInputText !== undefined) {
                currentInput = appRes.nextInputText;
              }
            }
          }

          // Core Invariants check after each command:
          // 1. Check in-flight attempts have valid attemptId > 0
          for (const att of ledger) {
            if (!att.resolved) {
              const inFlight = mgr.getInFlight(att.callId, att.companionKey, att.sessionId);
              if (inFlight && inFlight.attemptId === att.attemptId) {
                assert.ok(inFlight.attemptId > 0);
                assert.equal(mgr.isInFlight(att.callId, att.companionKey, att.sessionId), true);
              }
            }
          }
        }

        assert.ok(submitsExecuted >= 1, 'At least 1 submit must have executed');
      }
    )
  );
});
