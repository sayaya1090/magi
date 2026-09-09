import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';
import { carrying, context, fleet, jobs, offers, originWord, sayState, schedules } from '../core/panel';
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

/**
 * ★ Stopping a job has TWO endings, and `ok` is the same in both.
 *
 * `answerJobKill` returns `{OK: true, Removed: k.KillBackgroundJob(name)}`, and the comment right
 * above it says what that is for: "pressed twice must read 'already gone', not 'failure'". So the
 * daemon deliberately does NOT refuse a kill for a job that has finished — it answers ok and says
 * so in `removed`.
 *
 * ⚠ `Removed` is a Go bool with `omitempty`, so **false never goes on the wire** — the "already
 * gone" answer is literally `{"ok":true}` (measured against a running daemon 2026-09-09). A client
 * that reads `ok` alone therefore cannot tell the two apart, and this one said "asked it to stop"
 * for both.
 *
 * That is worst precisely where the button is pressed most: the row is drawn from a `jobs` poll, so
 * it outlives the job by up to one poll. Click a stale row, get told it was asked to stop, watch
 * the row vanish on the next poll — everything agrees, and none of it happened.
 */
test('stopping a job tells "stopped it" apart from "it had already finished"', () => {
  const wire = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'internal', 'adapter', 'daemon', 'protocol.go'), 'utf8');
  assert.match(wire, /Removed\s+bool\s+`json:"removed,omitempty"`/,
    'the wire no longer carries `removed` the way this guard reads it');

  const cmd = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'doors.ts'), 'utf8');
  const at = cmd.indexOf("reg('magi.stopJob'");
  assert.ok(at > 0, 'the stop-job command is not where this guard looks');
  // The command only — a wider slice reaches the next command and would pass on its sentences.
  const where = cmd.slice(at, cmd.indexOf("reg('magi.", at + 10));
  assert.ok(/\.removed\b/.test(where),
    'stopping a job never reads `removed` — "already gone" and "stopped it" print the same sentence');
  // Two endings means two sentences. One template with the field interpolated would satisfy a
  // "reads removed" check while still saying one thing.
  const said = [...where.matchAll(/showInformationMessage\(|`magi: /g)].length;
  assert.ok(said >= 3, `only ${said} pieces of a two-ending answer found — the scan is reading nothing`);
  assert.ok(/\?[\s\S]{0,160}:/.test(where),
    '`removed` is read but only one sentence exists — the person is told the same thing either way');
});

/**
 * ★ `state` is an ENUM, and the fleet row was printing the token.
 *
 * `fleet.State` (`internal/adapter/fleet/fleet.go`) is six words. This row printed whichever one
 * arrived, so a person read `abandoned` and `stopped` side by side with nothing saying which is the
 * bad one — and the core wrote down that this is exactly the harm the state exists to prevent:
 * "nobody is listening and a turn was left open — a crash, a kill, a closed laptop. Every other
 * view renders this identically to a finished session, which is why it is here."
 *
 * The words are read out of the CORE, so a seventh landing there fails here rather than reaching
 * the panel as a bare token.
 */
test('every companion state the core names is said as a phrase', () => {
  const core = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'internal', 'adapter', 'fleet', 'fleet.go'), 'utf8');
  const states = [...core.matchAll(/^\t(\w+)\s+State\s*=\s*"([a-z]+)"/gm)].map((m) => m[2]);
  assert.ok(states.length >= 6,
    `only ${states.length} fleet states read from the core — a stale parser here answers "all said" for ever`);
  for (const must of ['abandoned', 'stopped']) {
    assert.ok(states.includes(must), `the scan cannot see "${must}" — the pair this guard exists for`);
  }

  for (const st of states) {
    const said = sayState(st).trim();
    assert.ok(said, `"${st}" says nothing at all`);
    // A phrase, not the token. Length alone is not phrase-ness — `'idle '` would pass that.
    if (st === 'working' || st === 'idle') continue; // one plain word is already the plain meaning
    assert.notEqual(said, st, `"${st}" reaches the person as the protocol word itself`);
    assert.ok(said.split(/\s+/).length >= 2, `"${st}" → "${said}" is a token, not a phrase`);
  }
  // The two the core separated must not read the same.
  assert.notEqual(sayState('abandoned'), sayState('stopped'),
    'a companion that died holding work reads the same as one that finished — the exact harm the core names');
  assert.equal(sayState(undefined), '', 'a row with no state must say nothing, not guess');
  assert.equal(sayState('hibernating'), 'hibernating',
    'an unknown state is swallowed or renamed — the daemon said something and nobody hears it');

  // ⚠ **And the row must actually call it.** The first cut of this guard tested `sayState` alone,
  // and the mutation that mattered most — putting `r.state` back in the row — sailed through with
  // a perfectly correct function nobody used. Go through the seam the panel really goes through.
  const drawn = fleet({ ok: true, roster: [
    { socket: '/tmp/a.sock', name: 'one', state: 'abandoned', live: true },
    { socket: '/tmp/b.sock', name: 'two', state: 'stopped', live: true },
  ] } as unknown as Parameters<typeof fleet>[0]);
  assert.equal(drawn.length, 2, 'the fleet formatter did not draw the rows this guard hands it');
  for (const [i, st] of ['abandoned', 'stopped'].entries()) {
    assert.ok(!new RegExp(`\\b${st}\\b`).test(drawn[i]),
      `the fleet row prints the token "${st}" — ${drawn[i]}`);
    assert.ok(drawn[i].includes(sayState(st)), `the fleet row does not carry the phrase for "${st}"`);
  }
});

/**
 * ★ A schedule somebody switched OFF drew exactly like one whose next run is unknown.
 *
 * `CronRow.Enabled` is a plain Go bool with `omitempty`, so FALSE NEVER GOES ON THE WIRE. Measured
 * by marshalling the row: on → `{"name":…,"enabled":true,"next":…}`, off → `{"name":…}` — no
 * `enabled`, and no `next` either, because the core says "Next is RFC3339, and empty when the job
 * never runs — switched off, or Problem says why".
 *
 * So `r.enabled === false` was never true, the cell fell through to an empty string, and the one
 * thing a person opens this panel to check — is it on? — was the thing it would not say.
 *
 * The REQUEST side of the same switch is a `*bool`, and the core's comment says why: "the switch is
 * three-valued on the wire: absent must mean 'leave it alone'". The distinction was known exactly
 * where it was needed and lost exactly where it was read.
 */
test('a switched-off schedule says so', () => {
  const wire = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'internal', 'adapter', 'daemon', 'protocol.go'), 'utf8');
  const at = wire.indexOf('type CronRow struct');
  assert.ok(at > 0, 'the core no longer has the cron row this guard reads');
  assert.match(wire.slice(at, wire.indexOf('\n}', at)), /Enabled\s+bool\s+`json:"enabled,omitempty"`/,
    'the wire no longer carries `enabled` as an omitempty bool — the reasoning here may not hold');

  // Off is the ABSENT shape, which is the whole point. Never write `enabled: false` in a fixture
  // here: the daemon cannot send it, so a test that passes on it proves nothing.
  const off = schedules({ ok: true, cron: [{ name: 'nightly', schedule: '0 9 * * *' }] } as unknown as Parameters<typeof schedules>[0]);
  assert.equal(off.length, 1, 'the formatter did not draw the row this guard hands it');
  assert.match(off[0].line, /\boff\b/, 'a switched-off schedule does not say it is off');

  const on = schedules({ ok: true, cron: [
    { name: 'nightly', schedule: '0 9 * * *', enabled: true, next: '2026-09-11T09:00:00Z' },
  ] } as unknown as Parameters<typeof schedules>[0]);
  assert.ok(!/\boff\b/.test(on[0].line), 'a running schedule is reported as off');
  assert.match(on[0].line, /next 2026-09-11/, 'a running schedule does not say when it next runs');

  // On, but the daemon did not say when — that is not the same as off, and must not read as it.
  const soon = schedules({ ok: true, cron: [
    { name: 'nightly', schedule: '0 9 * * *', enabled: true },
  ] } as unknown as Parameters<typeof schedules>[0]);
  assert.ok(!/\boff\b/.test(soon[0].line), 'a job that is on with no next time is drawn as switched off');
});

/**
 * ★ A companion in the middle of handed-over work read as free.
 *
 * The core signs `waiting` and `handling` together and states the arithmetic: "they decide where
 * team-addressed work goes: fleet.Resolve routes a team address to the lightest companion, and
 * **load is Waiting + (1 if Handling)**". Both clients drew the queue alone (and this one drew
 * neither, because `waiting` was declared `string` and `r.waiting > 0` would not compile).
 *
 * So a companion with an empty queue that is carrying one piece showed as free — on the row a
 * person clicks to hand it another.
 */
test('the fleet row says what work a companion is carrying', () => {
  const go = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'internal', 'core', 'cluster', 'cluster.go'), 'utf8');
  assert.match(go, /load is Waiting \+ \(1 if Handling\)/,
    'the core no longer states the arithmetic this guard is built on — re-read before trusting it');

  assert.equal(carrying({}), '', 'an idle companion is given something to say');
  assert.equal(carrying({ waiting: 0 }), '', 'an empty queue is drawn as a load');
  // ⚠ `handling` is an omitempty bool: false never arrives, so absent is the ordinary case and
  // the busy case is the one that carries the field.
  assert.match(carrying({ handling: true }), /busy/,
    'a companion in the middle of handed work reads as free — the exact row somebody hands work to');
  assert.match(carrying({ waiting: 2 }), /2 queued/, 'the queue depth is not said');
  const both = carrying({ waiting: 2, handling: true });
  assert.ok(/busy/.test(both) && /2 queued/.test(both),
    'one of the two facts is dropped — the sum alone cannot say that one is already in flight');

  // And the row must carry it, not just the helper: carried-and-not-drawn is the older defect here.
  const drawn = fleet({ ok: true, roster: [
    { socket: '/tmp/a.sock', name: 'one', state: 'working', live: true, handling: true },
  ] } as unknown as Parameters<typeof fleet>[0]);
  assert.match(drawn[0], /busy/, 'the fleet row does not draw what the companion is carrying');
});

/**
 * ★ A fold that names nothing has made a promise it does not keep.
 *
 * Compaction replaces the conversation with a summary. On its own that is a loss — but the core
 * keeps the detail in the log and can pull a subject back with `recall_context`, and it says the
 * naming IS the difference: the topics are "what 'the detail is not lost' means concretely, and
 * naming them is the difference between that claim and a promise".
 *
 * So the count and the subjects travel together. `folded 3×` alone tells a person their
 * conversation was replaced and nothing about what survived.
 */
test('a fold says what is still there', () => {
  const go = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'internal', 'app', 'context_state.go'), 'utf8');
  assert.match(go, /Topics \[\]string\s+`json:"topics,omitempty"`/,
    'the core no longer carries the fold subjects the way this guard reads them');

  const said = context({ ok: true, context: {
    window: 32000, used: 12000, compactions: 3, topics: ['review.ts', 'the deploy script'],
  } } as unknown as Parameters<typeof context>[0]);
  assert.match(said, /folded 3×/, 'the fold count is gone');
  assert.match(said, /review\.ts/, 'the fold count went out with no subjects — the promise without the naming');
  assert.match(said, /the deploy script/, 'only some subjects are named');

  // A fold with no subjects says the count and stops — it must not invent an empty list.
  const bare = context({ ok: true, context: { window: 32000, used: 12000, compactions: 1 } } as unknown as Parameters<typeof context>[0]);
  assert.match(bare, /folded 1×/);
  assert.ok(!/still there/.test(bare), 'a fold with nothing named still promises something');
});

/**
 * ★ Three companions read the same until the row says what each is FOR.
 *
 * Measured against a live machine (2026-09-10) by running its real `roster` reply through this very
 * formatter: `word · idle · sonnet`, `excel · idle · sonnet`, `powerpoint · idle · sonnet` — while
 * every row carried `does` naming exactly what told them apart.
 *
 * The core says these travel for this: "Does NAMES those things… a name is enough to **pick a
 * companion out of a roster**". This row is that roster, and it is what a person reads before
 * handing work over.
 *
 * ⚠ `can` is NOT `does.length`. The core carries the count separately because the list is a sample
 * past `MaxDoes` — so a row showing three of seven must say so, or it reads as the whole offer.
 */
test('the fleet row names what a companion is for, and says when the list is a sample', () => {
  const cluster = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'internal', 'core', 'cluster', 'cluster.go'), 'utf8');
  assert.match(cluster, /a name is enough to pick a\s*\n?\s*\/\/ companion out of a roster/,
    'the core no longer states why `does` travels — re-read before trusting this rule');
  assert.match(cluster, /A SAMPLE when there are more than MaxDoes/,
    'the core no longer says the list may be a sample — the `+N` below may be wrong now');

  assert.equal(offers({}), '', 'a companion that advertises nothing is given words');
  assert.equal(offers({ does: [] }), '', 'an empty list is drawn as an offer');
  assert.equal(offers({ can: 3, does: ['a', 'b', 'c'] }), 'a, b, c',
    'a complete list is marked as if something were held back');
  // The sample case: the count says seven, the list names three.
  assert.equal(offers({ can: 7, does: ['a', 'b', 'c'] }), 'a, b, c +4',
    'a sampled list reads as the whole offer — the count is carried separately for exactly this');
  // More names than we draw: still short by the difference, even when `can` agrees with the list.
  assert.equal(offers({ can: 5, does: ['a', 'b', 'c', 'd', 'e'] }, 3), 'a, b, c +2',
    'names beyond the cut vanish with nothing saying so');

  // And the row must carry it — a correct helper nobody calls is the older defect here.
  const drawn = fleet({ ok: true, roster: [
    { socket: '/tmp/w.sock', name: 'word', state: 'idle', live: true, can: 3,
      does: ['document-structure', 'editing', 'tables-and-review'] },
  ] } as unknown as Parameters<typeof fleet>[0]);
  assert.match(drawn[0], /document-structure/, 'the fleet row does not say what the companion is for');
});

/**
 * ★ The window's make-up is drawn as shares, not token totals.
 *
 * The core states the rule and its reason: the breakdown is a chars/4 estimate even when `used` is
 * the provider's measured count, so the pieces "will not sum to a measured Used. They are honest as
 * proportions and dishonest as totals, **which is why the screen draws them as a share of their own
 * sum and says the reading is an estimate**".
 *
 * Measured against a live daemon (2026-09-10): this line printed `tools 23507 · system 2824` —
 * numbers a person adds up and compares against `used`, which is the arithmetic the core says they
 * cannot support. Drawn as shares the same reply reads `tools 89% · system 11%`, which says the
 * thing the totals hid: the window is mostly the tool catalog, not the conversation.
 */
test('the window make-up is shares of its own sum, marked as an estimate', () => {
  const go = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'internal', 'app', 'context_state.go'), 'utf8');
  assert.match(go, /honest as proportions and dishonest as totals/,
    'the core no longer states the rule this guard enforces — re-read before trusting it');

  const said = context({ ok: true, context: {
    window: 200000, used: 26331, estimated: true, parts: { system: 2824, tools: 23507 },
  } } as unknown as Parameters<typeof context>[0]);
  assert.match(said, /tools 89%/, 'the make-up is not drawn as a share of its own sum');
  assert.match(said, /system 11%/, 'a piece is missing from the make-up');
  assert.ok(!/23507/.test(said), 'the raw token total is still on screen — a number that cannot be added up');
  assert.match(said, /est\./, 'the make-up does not say it is an estimate; the flag above is about `used`');

  // No pieces: say nothing rather than draw an empty make-up or a 0%.
  const bare = context({ ok: true, context: { window: 200000, used: 100 } } as unknown as Parameters<typeof context>[0]);
  assert.ok(!/made of/.test(bare), 'a companion with no measured make-up is given one');

  // ⚠ Zero is "not known", not "empty" — the core says callers must not draw it as a measurement.
  const zero = context({ ok: true, context: {
    window: 200000, used: 100, parts: { system: 0, tools: 50 },
  } } as unknown as Parameters<typeof context>[0]);
  assert.ok(!/system/.test(zero), 'a 0 piece is drawn as a measurement — the core says it means "not known"');
});

/**
 * ★ A sighting's state is drawn with its AGE.
 *
 * The core states the rule on the field itself: "A screen that shows a state without its age is
 * claiming to know something it cannot". A sighting is a row another machine signed and this one
 * remembered; the gossip decays over an hour, so the oldest row on screen is the one whose `idle`
 * is least likely to still be true — and it was drawn exactly like the freshest.
 *
 * Words, not seconds. The normal range of this number is 0..3600, so raw seconds spend most of
 * their life in shapes like "3540s ago" — which is what the JetBrains panel printed until the same
 * wave fixed it.
 */
test('a sighting says how old the fact is, in words', () => {
  const [fresh, old, ancient] = fleet({
    ok: true,
    roster: [
      { socket: '/a/daemon-ws-1.sock', state: 'idle', sighting: true, ageSeconds: 12 },
      { socket: '/b/daemon-ws-2.sock', state: 'idle', sighting: true, ageSeconds: 3540 },
      { socket: '/c/daemon-ws-3.sock', state: 'idle', sighting: true, ageSeconds: 7260 },
    ],
  });
  assert.match(fresh, /seen <1m ago/);
  assert.match(old, /seen 59m ago/, `an hour-old sighting drew as: ${old}`);
  assert.match(ancient, /seen 2h 1m ago/, `a two-hour-old sighting drew as: ${ancient}`);
  assert.ok(!/\d+s ago/.test(old), 'raw seconds reached the screen');
});

/**
 * A local row is a live dial, not a memory — it carries age 0, and "seen just now" beside the one
 * row whose state was actually MEASURED would be noise pretending to be a caveat. And a row with no
 * age said (an older daemon) gets nothing: inventing "just now" is the one claim that would make a
 * stale row look measured.
 */
test('only a sighting carries an age', () => {
  const [local, unsaid] = fleet({
    ok: true,
    roster: [
      { socket: '/a/daemon-ws-1.sock', state: 'idle', live: true, ageSeconds: 0 },
      { socket: '/b/daemon-ws-2.sock', state: 'idle', sighting: true },
    ],
  });
  assert.ok(!/seen/.test(local), `a measured row was captioned as remembered: ${local}`);
  assert.ok(!/seen/.test(unsaid), `an unsaid age was drawn as fresh: ${unsaid}`);
});

/**
 * ★ A schedule that RUNS A COMMAND is marked as one.
 *
 * `prompt` and `command` are exclusive on the wire, and the core says why the difference matters:
 * a command job "모델을 안 부르고 도구 권한 관문도 안 지난다(설정에 적은 것이 곧 승인)" — it runs
 * a shell command on this machine unattended, with no permission prompt, because being written in
 * the config IS the approval. The daemon's own sentence tells them apart ("runs a command" /
 * "asks"); this line printed both the same way.
 *
 * Measured against a live daemon (2026-09-10): two real rows written through `cron-set` and read
 * back through `cron`, then run through this function. `$` is the mark because the JetBrains panel
 * already uses it for this fact — one vocabulary, so two screens cannot drift.
 */
test('a schedule that runs a command is told apart from one that asks', () => {
  const [runs, asks] = schedules({
    ok: true,
    cron: [
      { name: 'build', schedule: '*/5 * * * *', enabled: true, next: '2026-09-09T20:25:00Z',
        command: 'go build ./...', timeout: '10m' },
      { name: 'nightly', schedule: '0 3 * * *', enabled: true, next: '2026-09-10T18:00:00Z',
        prompt: '저장소를 훑어 본다' },
    ],
  });
  assert.match(runs.line, /\$ go build \.\/\.\.\./, `a command job drew as: ${runs.line}`);
  assert.ok(!asks.line.includes('$'), `a prompt job was marked as a command: ${asks.line}`);
  assert.match(asks.line, /저장소를 훑어 본다/);
});

/**
 * ★ A child conversation says WHO opened it, and `agent` is not who.
 *
 * The core states the trap in the field's own comment: every child records the same `agent`
 * ("spawn", a constant), and a live run proved it by bringing a meeting room back as `agent=spawn`.
 * `origin` is the discriminator — "meeting" for a room this companion holds as a participant, the
 * spawning tool's actor otherwise.
 *
 * Not cosmetic: a subagent is work THIS conversation delegated, a meeting room is a conversation
 * somebody ELSE convened and this companion is sitting in. This client's list drew them the same,
 * so the only way to tell was to open one. The JetBrains panel has split them since it grew the row.
 */
test('a child says who opened it, and an unknown opener is passed through', () => {
  assert.equal(originWord('meeting'), 'meeting');
  assert.equal(originWord('minutes'), 'minutes');
  // A word from a newer daemon beats a blank, and an invented name sends somebody to the wrong
  // place — so it is passed through, not folded into "child" or dropped.
  assert.equal(originWord('scout'), 'scout');
  // Nothing said stays nothing. An origin invented here would claim knowledge the reply never gave.
  assert.equal(originWord(undefined), '');
  assert.equal(originWord('   '), '');
});

/**
 * And the two seats a meeting opens are told apart.
 *
 * A meeting opens TWO children — the seat that talks and the seat that writes it down. If both drew
 * as one word, every meeting would put two look-alike rows on the list and neither would say which
 * was which.
 */
test('the two seats a meeting opens do not draw the same', () => {
  assert.notEqual(originWord('meeting'), originWord('minutes'));
});
