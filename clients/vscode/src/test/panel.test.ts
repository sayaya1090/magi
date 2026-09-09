import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';
import { context, fleet, jobs, schedules } from '../core/panel';
import { Row, turnsBack } from '../core/transcript';

/**
 * ★ These doors answer a STRUCT. They never fill `out`.
 *
 * Measured against a live daemon on 2026-09-09: `jobs` answered `{"ok":true,"jobs":{}}`, `roster`
 * answered a `roster` array of eight rows, `context` answers a `context` object. The panel read
 * `out` for all four sections and the two commands that act on them read it too — so three of the
 * panel's five sections were empty on every build and "stop a background job" always said nothing
 * was running. Nothing failed: an absent field is an empty string, and an empty string is exactly
 * what "nothing to report" looks like.
 *
 * The shapes below are copied from `internal/adapter/daemon/protocol.go`, not invented.
 */
test('a background job is read from the jobs struct, not from out', () => {
  const { jobs: live } = jobs({
    ok: true,
    jobs: { background: [{ id: 'job_7f2a', command: 'make test', running: true }] },
  });
  assert.equal(live.length, 1);
  assert.equal(live[0].id, 'job_7f2a');
  assert.equal(live[0].running, true);
  assert.match(live[0].what, /make test/);
});

/** A prose reader would find these too — and there is no prose. This is the regression that mattered. */
test('a jobs reply with only out reports nothing', () => {
  assert.deepEqual(jobs({ ok: true, out: 'running: job_7f2a (make test)' }).jobs, []);
});

test('a child agent is a job too, and says how it went', () => {
  const { jobs: live } = jobs({
    ok: true,
    jobs: { children: [{ id: 'c1', tool: 'explore', task: 'find the parser', err: 'timed out' }] },
  });
  assert.equal(live.length, 1);
  assert.equal(live[0].running, false);
  assert.match(live[0].what, /timed out/);
});

/**
 * Both queues, in the one order they run in. A screen that showed only handovers said a companion
 * had nothing waiting while the correction the person typed sat in the other queue.
 */
test('queued work names who is waiting', () => {
  const { queued } = jobs({
    ok: true,
    jobs: { queued: [{ kind: 'person', text: 'also fix the test' }, { kind: 'handover', from: 'web', text: 'review this' }] },
  });
  assert.equal(queued.length, 2);
  assert.match(queued[0], /^you:/);
  assert.match(queued[1], /from web/);
});

/** A schedule that can never run is the row to mark — nothing else on any screen mentions it again. */
test('a schedule carrying a problem says so', () => {
  const rows = schedules({ ok: true, cron: [{ name: 'nightly', schedule: '0 3 * * *', problem: 'no such profile' }] });
  assert.equal(rows.length, 1);
  assert.equal(rows[0].name, 'nightly');
  assert.match(rows[0].line, /no such profile/);
});

test('a switched-off schedule is drawn as off', () => {
  const rows = schedules({ ok: true, cron: [{ name: 'standup', schedule: '7 9 * * 1-5', enabled: false }] });
  assert.match(rows[0].line, /off/);
});

/** A dead companion is a fact. Drawing it as live is how somebody sends work to nobody. */
/**
 * The fleet says which rows this window can actually reach.
 *
 * ⚠ **This test used to feed a value the wire never sends.** It set `live: false`, and `Live` is
 * `omitempty` — a false one is not encoded at all. So the branch it exercised (`live === false`)
 * could not run against a real daemon, and the mark it asserted never drew. Third test in this
 * session found asserting on a shape the daemon does not produce.
 *
 * The wire has three cases and they are three different facts: a dial proved somebody is listening
 * (`live`); a row another machine signed, whose liveness nobody here can check (`sighting` — the
 * core calls it "visible, not commandable"); and a local row where nothing was said. This function's
 * own comment already had the rule — "drawing it as live is how somebody sends work to nobody" —
 * and the code was breaking it for two of the three.
 */
test('the fleet tells reachable from elsewhere from unanswered', () => {
  const lines = fleet({
    ok: true,
    roster: [
      { name: 'web', state: 'idle', model: 'sonnet', live: true, workdir: '/w' },
      { socket: '/x/daemon-word-1.sock' },
      { socket: '/over/there/daemon-ws-9.sock', sighting: true },
    ],
  });
  assert.equal(lines.length, 3);
  assert.ok(!/elsewhere|no answer/.test(lines[0]), 'a proven dial was marked as unreachable');
  assert.match(lines[1], /daemon-word-1\.sock/);
  assert.match(lines[1], /no answer/, 'nothing was said and it drew like a companion that answered');
  assert.match(lines[2], /elsewhere/,
    'a row from another machine drew like one this window can talk to — the socket is a path over there');
});

/**
 * ★ A companion is named the same way in both lists.
 *
 * A person reads the fleet section and then picks from the hand-off list. Two spellings of one
 * companion leave no way to match them up — and measured on a live roster of eight rows, not one
 * carried a `name`, so what the fallback picks IS the label people see.
 */
test('the fleet names a companion by its folder, like the hand-off list does', () => {
  const line = fleet({
    ok: true,
    roster: [{ socket: '/x/daemon-word-37iu1p70.sock', workdir: '/Users/x/Library/magi/word', state: 'idle' }],
  })[0];
  assert.match(line, /^word · idle/, `not named by its folder: ${line}`);
  assert.ok(!line.includes('daemon-word-37iu1p70'), `the socket filename is still the label: ${line}`);
  // And the full path is not repeated — it is a narrow sidebar and the name already says it.
  assert.ok(!line.includes('/Users/x/'), `the whole path is in the line: ${line}`);
});

test('the context section says how full the window is', () => {
  const s = context({ ok: true, context: { window: 200000, used: 35103, estimated: true, parts: { tools: 32148, system: 2955 } } });
  assert.match(s, /35103 \/ 200000 \(18%\)/);
  assert.match(s, /estimated/);
  // Biggest first: what is filling the window is the question, and the answer is usually one entry.
  assert.ok(s.indexOf('tools') < s.indexOf('system'), `parts are not biggest-first: ${s}`);
});

/** No window, nothing to say. A percentage of an unknown window would be invented. */
test('a context reply with no window says nothing', () => {
  assert.equal(context({ ok: true, context: { used: 10 } }), '');
  assert.equal(context({ ok: false }), '');
  assert.equal(context(null), '');
});

/** A refusal is not an empty list of facts — every reader returns nothing rather than guessing. */
test('a refusal yields nothing everywhere', () => {
  assert.deepEqual(jobs({ ok: false }).jobs, []);
  assert.deepEqual(schedules({ ok: false }), []);
  assert.deepEqual(fleet({ ok: false }), []);
});

/**
 * ★ `children` has its own field. Reading `sessions` here answers nothing, for ever.
 *
 * The third instance of one defect class found on 2026-09-09, after the panel reading `out` and the
 * capability gates: a client reads a field the door does not fill, an absent field is an empty list,
 * and the feature reports "nothing here" on every build without anything failing. This one is a
 * shape test rather than a reader test, because the reading is inline — what it pins is the NAME.
 */
test('the children door fills children, not sessions', () => {
  const doors = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'internal', 'adapter', 'daemon', 'protocol.go'), 'utf8');
  assert.match(doors, /Children\s+\[\]SessionRow\s+`json:"children/,
    'the wire no longer calls it `children` — the client reads that name');

  const cmd = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'doors.ts'), 'utf8');
  const where = cmd.slice(cmd.indexOf("reg('magi.children'"), cmd.indexOf("// ---- scheduled work"));
  assert.ok(where.includes('r.children'), 'the children command does not read r.children');
  assert.ok(!/\br\.sessions\b/.test(where), 'the children command still reads r.sessions');
});

/**
 * ★ `rewind` counts TURNS. It does not read `since`.
 *
 * The door hands `n` to `App.Rewind(sid, n)`. This client sent the picked row's `seq` in a field
 * called `since` — a field the wire HAS (the transcript stream reads it) and this door never looks
 * at — so the value went nowhere and the daemon used its own default. The person's chosen point had
 * nothing to do with what happened, and nothing said so. The variant that is quietest: not an unknown
 * field, but a known one this door ignores.
 */
test('going back to a prompt is counted in turns, newest first', () => {
  const asked: Row[] = [
    { seq: 10, who: 'user', text: 'first' },
    { seq: 20, who: 'user', text: 'second' },
    { seq: 30, who: 'user', text: 'third' },
  ];
  // The newest is one turn back; the oldest is all three.
  assert.equal(turnsBack(asked, 30), 1);
  assert.equal(turnsBack(asked, 20), 2);
  assert.equal(turnsBack(asked, 10), 3);
});

/** A seq that is not one of these rows is 0 — and a caller must not send 0 as "rewind nothing". */
test('a point no longer in the conversation counts as none', () => {
  assert.equal(turnsBack([{ seq: 10, who: 'user', text: 'x' }], 99), 0);
  assert.equal(turnsBack([], 10), 0);
});

/** And the call sends `n`, not `since`. Pinned because the two are both real wire fields. */
test('the rewind call sends n and not since', () => {
  const cmd = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'doors.ts'), 'utf8');
  const call = /call\('rewind',\s*\{([^}]*)\}/.exec(cmd);
  assert.ok(call, 'the rewind call is not where this guard looks');
  assert.match(call![1], /\bn\b/, 'rewind does not send n');
  assert.ok(!/\bsince:/.test(call![1]), 'rewind still sends since, which this door never reads');
});
