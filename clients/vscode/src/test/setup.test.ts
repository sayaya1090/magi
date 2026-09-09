import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { State, panelNote, setupOf, sameSetup } from '../core/activity';
import { noteCompletion, whyNoCompletion } from '../core/complete';

/**
 * The daemon has always sent these three; nothing read them.
 *
 * That is why a person could not see what was answering them: `answerStatus` fills Permission,
 * Backend and Model, and this client dropped all three on the floor. A field name that disagrees
 * fails silently here — JSON hands back undefined and the status bar simply shows nothing.
 */
test('a status reply says what the companion is running on', () => {
  const s = setupOf({ ok: true, model: 'gpt-oss:20b', backend: 'http://localhost:11434/v1', permission: 'allow' });
  assert.deepEqual(s, { model: 'gpt-oss:20b', backend: 'http://localhost:11434/v1', permission: 'allow' });
});

/**
 * The model is absent whenever the request named no session — measured against a live daemon.
 * "Nobody said" must not become an empty string that a screen prints as a blank model.
 */
test('a reply with no session leaves the model unsaid, not blank', () => {
  const s = setupOf({ ok: true, backend: 'http://localhost:11434/v1', permission: 'allow' });
  assert.equal(s.model, undefined);
  assert.equal(s.backend, 'http://localhost:11434/v1');
});

/**
 * A refusal says nothing about the setup — even when the reply still carries fields.
 *
 * The empty-reply case cannot catch this: a refusal with no fields comes out empty either way. The
 * one that matters is a refusal that still has a backend in it, which is what a daemon answering
 * "not for this session" looks like — reading those would put a stale model on the status bar and
 * keep it there.
 */
test('a refusal says nothing about the setup, even carrying fields', () => {
  assert.deepEqual(setupOf({ ok: false, error: 'no', model: 'stale', backend: 'b', permission: 'allow' }), {});
  assert.deepEqual(setupOf({ ok: false, error: 'no' }), {});
  assert.deepEqual(setupOf(null), {});
});

/** Whitespace is not a value. A model of " " would print as a blank and read as broken. */
test('blank fields are unsaid', () => {
  assert.deepEqual(setupOf({ ok: true, model: '  ', backend: '', permission: 'ask' }), { permission: 'ask' });
});

/** The screen redraws only when something moved; the poll runs every two seconds. */
test('two readings of the same setup are the same', () => {
  const a = { model: 'm', backend: 'b', permission: 'p' };
  assert.ok(sameSetup(a, { ...a }));
  assert.ok(!sameSetup(a, { ...a, model: 'other' }));
});

/**
 * Why a completion was silent is remembered, and a working one clears it.
 *
 * The door answers ok with nothing when it has nothing to say, and puts the reason in `reason`. This
 * client dropped that field, so "completion is on and nothing appears" had no answer anywhere. It is
 * not shown per keystroke — that would be noise — so the value has to survive until somebody looks.
 */
test('the reason a completion was empty is kept, and a good one clears it', () => {
  noteCompletion('', 'autocomplete is off in this companion');
  assert.match(whyNoCompletion(), /autocomplete is off/);

  // ★ A reason left standing while things work is a sentence that has aged. The case that matters
  // is text AND a reason arriving together — a reply can carry both, and clearing only when the
  // reason is absent would leave the old sentence up while completion works fine.
  noteCompletion('const x = 1;', 'autocomplete is off in this companion');
  assert.equal(whyNoCompletion(), '', 'a working completion did not clear the reason');
  noteCompletion('', 'off again');
  noteCompletion('const y = 2;', undefined);
  assert.equal(whyNoCompletion(), '');

  // Empty with no reason given says nothing rather than inventing one.
  noteCompletion('', undefined);
  assert.equal(whyNoCompletion(), '');
});

/**
 * ★ The way out must not depend on our being sure what is wrong.
 *
 * `not-running` had a "Start one" button and `unknown` had a bare sentence — so a person whose
 * companion could not be reached for a reason we cannot name had nothing to press. The JetBrains
 * client had the same hole, and it is what a live report was about: on Windows a socket file left by
 * a dead daemon lands in "cannot say", and every route to reviving it was closed.
 *
 * Offering it when a companion is actually alive is safe: the daemon claims the socket with a lock
 * and probes it first, so a second one refuses itself. Losing that race is ordinary, not an error.
 */
test('a companion we cannot reach still offers a way out', () => {
  const note = panelNote({ state: State.Unknown, asking: 'the socket is there and nothing answered' });
  assert.equal(note.offerStart, true, 'nothing to press when the companion could not be reached');
  assert.match(note.text, /Could not reach/);
  // The reason travels with it — "cannot say" is not "it is fine", and the reason is what a person
  // acts on when the button does not help.
  assert.match(note.text, /nothing answered/);
});

test('a companion that is not running offers the same way out', () => {
  const note = panelNote({ state: State.NotRunning });
  assert.equal(note.offerStart, true);
  assert.match(note.text, /No companion is running/);
});

/**
 * Nothing is offered while it is answering. A button to start a companion that is already talking
 * would be a button that does nothing — and this panel sits above the composer, where a line in the
 * way is a line in the way.
 */
test('a working companion says nothing here', () => {
  for (const state of [State.Idle, State.Working, State.Waiting]) {
    const note = panelNote({ state });
    assert.equal(note.text, '', `${state} draws a note above the composer`);
    assert.equal(note.offerStart, false, `${state} offers to start one`);
  }
  assert.deepEqual(panelNote(null), { text: '', offerStart: false });
});
