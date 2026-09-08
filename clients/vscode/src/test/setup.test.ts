import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import { setupOf, sameSetup } from '../core/activity';
import { jobIds, cronNames } from '../core/prose';

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
 * `jobs` and `cron` answer prose meant for a person, so what is read out of them is offered only
 * when it is recognised. Handing back words plucked out of a sentence would send the daemon an id
 * that never existed — and `job-kill` would refuse, which reads as the job refusing to stop.
 */
test('job ids are read when they are there and invented when they are not', () => {
  assert.deepEqual(jobIds('running: job_7f2a1  (make test)\nqueued: bg-2 (lint)'), ['job_7f2a1', 'bg-2']);
  assert.deepEqual(jobIds('nothing is running in the background'), []);
  assert.deepEqual(jobIds(''), []);
});

test('schedule names are read the same cautious way', () => {
  assert.deepEqual(cronNames('nightly   0 3 * * *   run the suite'), ['nightly']);
  assert.deepEqual(cronNames('standup: 7 9 * * 1-5'), ['standup']);
  assert.deepEqual(cronNames('no schedules'), []);
});
