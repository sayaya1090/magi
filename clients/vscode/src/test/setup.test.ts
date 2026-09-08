import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { setupOf, sameSetup } from '../core/activity';
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
