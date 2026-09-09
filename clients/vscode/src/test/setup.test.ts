import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';
import * as activity from '../core/activity';
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
  for (const state of [State.Attached, State.Working, State.Waiting]) {
    const note = panelNote({ state });
    assert.equal(note.text, '', `${state} draws a note above the composer`);
    assert.equal(note.offerStart, false, `${state} offers to start one`);
  }
  assert.deepEqual(panelNote(null), { text: '', offerStart: false });
});

/**
 * The screen says only what the daemon said.
 *
 * `status` has three shapes and they are not three states: `waiting` filled, `doing` filled, and
 * NEITHER. The third is the ordinary one — `doing` is a long-running tool's progress note and one
 * builtin tool file out of fifty writes it, and the door has no field meaning "a turn is running"
 * (`answerStatus`). So the third shape is "it answered and said no more", and calling it `idle` is
 * a claim about a companion that may be working flat out.
 *
 * Pinned as a rule about the WORD, not just the branch: this file's own module comment records the
 * first version of this defect (Unknown drawn as idle), and the second version lived one line
 * below it for as long. A guard that only checked the branch would have passed both times.
 */
test('a daemon that said nothing is not called idle', () => {
  const quiet = activity.of({ ok: true });
  assert.notEqual(quiet.state, 'idle', 'the empty answer is drawn as "idle" — the daemon never said that');
  assert.equal(activity.label(quiet), 'attached', 'say what is known: it answered, and no more');

  // The two that ARE said keep their words.
  assert.equal(activity.of({ ok: true, doing: 'go build ./...' }).state, State.Working);
  assert.equal(activity.of({ ok: true, waiting: { kind: 'permission' } }).state, State.Waiting);

  // And "could not ask" stays its own answer — folding it in here is the defect this file's
  // opening paragraph is about.
  assert.equal(activity.of(null).state, State.Unknown);
  assert.equal(activity.of({ ok: false, error: 'nope' }).state, State.Unknown);

  // No screen keeps the retired word. A label is what a person reads, so a stale one is the defect
  // still shipping with the code fixed underneath it.
  for (const f of ['activity.ts', 'workspace.ts']) {
    const src = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'core', f), 'utf8')
      .replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '');
    assert.ok(!/['"`]idle['"`]/.test(src), `${f} still hands a screen the word "idle"`);
  }
});

/**
 * The person's own name, when the daemon has one.
 *
 * There is a real producer: an SSO-style plugin injects the authenticated username with
 * `magi.set_user_label`, the engine latches it (so a plugin that logs in during startup does not
 * write it under an empty session id — that was the "username missing on the first turn" bug), and
 * `status` answers with it. It is a runtime fact and comes down this wire and nowhere else.
 *
 * Both IDE clients dropped it, so every screen called whoever had logged in "user". The
 * ide-bridge's copy of `setupOf` carried it all along — two of the three copies were wrong.
 *
 * Empty is never sent by the core, so this is present-or-absent and never blank; a screen that
 * received one anyway must still fall back rather than draw a nameless row.
 */
test('the person is called what the daemon calls them', () => {
  assert.equal(setupOf({ ok: true, user: 'jiyoung@corp' }).user, 'jiyoung@corp');
  assert.ok(!('user' in setupOf({ ok: true })), 'an unsaid name is stored as a key with nothing in it');
  assert.ok(!('user' in setupOf({ ok: true, user: '   ' })), 'a blank name became a nameless label');

  // Declared on the wire — without that it cannot be read at all, which is how it was lost.
  const proto = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'core', 'protocol.ts'), 'utf8');
  assert.ok(/^\s{2}user\?:/m.test(proto), 'Response does not declare `user` — status fills it');

  // And it reaches the label. A name read into a field nobody paints is the same defect.
  const chat = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'chat.ts'), 'utf8');
  assert.ok(/r\.who === 'user' && you \? you/.test(chat), "the user row's label ignores the name");
  assert.ok(/paint\(r, this\.companion\.you\)/.test(chat), 'the name is never handed to the painter');
});

/**
 * "What is this companion running on" is three facts, and the screen carried two.
 *
 * `permission`, `model` and `council` arrive in the same `status` answer. This client declared the
 * first two and not the third, so whether the companion ends its turns by declaring to a council
 * was unknowable from the editor. Undeclared means unreadable — the same way `tools` and `user`
 * were lost, measured twice more in this session.
 *
 * Three-valued on purpose: on, off, and NOT SAID. An older daemon sends nothing, and drawing that
 * as "off" claims to know something. The rule is this repository's own (§0.5-7) and the reason the
 * core put the field on the wire is written down: a helper's tool descriptions told a model to
 * finish with `council{complete:true}` on a companion that had it switched off, and the model
 * called it and got `unknown tool: council`.
 */
test('whether the council is on is a fact the screen can show', () => {
  assert.equal(setupOf({ ok: true, council: true }).council, 'on');
  assert.equal(setupOf({ ok: true, council: false }).council, 'off');
  assert.ok(!('council' in setupOf({ ok: true })),
    'a daemon that did not say drew as "off" — that is a claim, not a reading');

  const proto = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'core', 'protocol.ts'), 'utf8');
  assert.ok(/^\s{2}council\?:/m.test(proto), 'Response does not declare `council` — status fills it');

  // And it reaches a surface. The tooltip is where the other two already stand.
  const status = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'status.ts'), 'utf8');
  assert.ok(/setup\.council/.test(status), 'the fact is read into Setup and no screen shows it');
});
