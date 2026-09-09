import { test } from 'node:test';
import * as assert from 'node:assert/strict';
import * as fs from 'fs';
import * as path from 'path';
import { rows, turnOpen, verdictWord } from '../core/transcript';
import { retryAfter } from '../core/daemon';
import { usage } from '../core/panel';
import { Event } from '../core/protocol';

const REPO = path.join(__dirname, '..', '..', '..', '..');

let seq = 0;
const delta = (messageId: string, kind: string, text: string): Event =>
  ({ seq: seq++, type: 'part.delta', data: { messageId, kind, text } });
const appended = (messageId: string, kind: string, text: string): Event =>
  ({ seq: seq++, type: 'part.appended', data: { messageId, role: 'assistant', part: { kind, text } } });
const prompt = (text: string): Event =>
  ({ seq: seq++, type: 'prompt.submitted', data: { parts: [{ text }] } });
const finished = (): Event => ({ seq: seq++, type: 'turn.finished', data: {} });

/**
 * ★ The conversation has to move WHILE the model answers.
 *
 * `part.delta` carries each chunk; the `part.appended` fact is only written once the stream finishes.
 * This client ignored the deltas, so a long answer on a local model was a frozen panel with a status
 * bar that said "working" — nothing failed, nothing appeared. The JetBrains client has streamed all
 * along.
 */
test('a reply appears while it is still arriving', () => {
  const got = rows([delta('m1', 'text', 'Look'), delta('m1', 'text', 'ing at')]);
  assert.equal(got.length, 1);
  assert.equal(got[0].who, 'agent');
  assert.equal(got[0].text, 'Looking at');
  assert.equal(got[0].draft, true);
});

/**
 * ⚠ The fact REPLACES its draft. The appended part carries the whole text, so keeping both shows
 * the answer twice — and the second copy is the one a person would quote.
 */
test('the finished part replaces the draft rather than adding to it', () => {
  const got = rows([
    delta('m1', 'text', 'Look'), delta('m1', 'text', 'ing at it'),
    appended('m1', 'text', 'Looking at it'),
  ]);
  assert.equal(got.length, 1, `the answer is drawn twice: ${JSON.stringify(got)}`);
  assert.equal(got[0].text, 'Looking at it');
  assert.ok(!got[0].draft, 'the row is still marked a draft after the fact landed');
});

/** One message streams reasoning and text as two drafts; writing one must not drop the other. */
test('reasoning and text are separate drafts of one message', () => {
  const got = rows([
    delta('m1', 'reasoning', 'thinking…'), delta('m1', 'text', 'the answer'),
    appended('m1', 'text', 'the answer'),
  ]);
  assert.equal(got.length, 2);
  assert.equal(got[0].who, 'thinking');
  assert.equal(got[0].draft, true, 'the reasoning draft was dropped by the text fact');
  assert.equal(got[1].text, 'the answer');
});

/** Reasoning is folded shut, streamed or not — the same rule the finished part follows. */
test('a streamed reasoning draft is folded', () => {
  assert.equal(rows([delta('m1', 'reasoning', 'hm')])[0].folded, true);
});

/**
 * ★ Orphan drafts are swept when the turn ends.
 *
 * The core streams chunks and then does not write the fact on several paths — a reply the spin guard
 * discarded, a tool call that arrived as text, an interrupt or a provider error. Left standing, a
 * half-answer sits on the screen of whichever window was attached and nowhere else.
 */
test('a half-streamed answer does not outlive its turn', () => {
  const got = rows([prompt('do it'), delta('m1', 'text', 'I will start by'), finished()]);
  assert.deepEqual(got.map((r) => r.who), ['user'], `an orphan draft survived: ${JSON.stringify(got)}`);
});

/** A streamed reply answers the prompt above it — the pending bar must come down as text arrives. */
test('the pending bar comes down when the first chunk lands', () => {
  const got = rows([prompt('do it'), delta('m1', 'text', 'starting')]);
  assert.equal(got[0].who, 'user');
  assert.ok(!got[0].pending, 'the prompt still shows as unanswered while the reply is streaming');
});

/** An empty chunk is not a row, and a kind this screen does not draw is not one either. */
test('empty and unknown chunks make no row', () => {
  assert.deepEqual(rows([delta('m1', 'text', '')]), []);
  assert.deepEqual(rows([delta('m1', 'tool-call', 'x')]), []);
});

const usageEv = (tokens: number, window: number, percent?: number): Event =>
  ({ seq: seq++, type: 'context.usage', data: { tokens, window, percent } });
const folded = (before: number, after: number): Event =>
  ({ seq: seq++, type: 'compaction', data: { tokensBefore: before, tokensAfter: after, summary: 's' } });

/**
 * ★ A fold leaves a gap, and the gap needs a reason.
 *
 * Without this row the transcript simply stops earlier than a person remembers: the fold replaces
 * everything up to a point with a summary, and scrolling back finds nothing that explains it.
 */
test('a fold says so where it happened', () => {
  const got = rows([folded(40000, 12000)]);
  assert.equal(got.length, 1);
  assert.equal(got[0].who, 'system');
  assert.match(got[0].text, /40000→12000/);
  assert.match(got[0].text, /−28000, −70%/);
});

/**
 * ⚠ A summary can come out LARGER than what it replaced, and that is the one outcome worth seeing.
 * Rendering it as "−0, −0%" — which clamping would — hides it.
 */
test('a fold that made things bigger says that', () => {
  assert.match(rows([folded(1000, 1500)])[0].text, /\+500, the summary is LARGER/);
});

test('a fold of nothing does not divide by zero', () => {
  assert.match(rows([folded(0, 0)])[0].text, /−0, −0%/);
});

/**
 * ★ The window meter comes off the STREAM, because the door for it is a capability a daemon may not
 * have — measured on a live one advertising nine capabilities and not that one.
 */
test('how full the window is comes off the stream', () => {
  assert.match(usage([usageEv(35103, 200000)]), /35103 \/ 200000 \(18%\)/);
});

/** The core's own percent wins when it sends one — it knows what it counted. */
test('the percent the core sent is the percent shown', () => {
  assert.match(usage([usageEv(1, 200000, 42)]), /\(42%\)/);
});

/** The LAST reading wins: this is a meter, not a log. */
test('a later reading replaces an earlier one', () => {
  assert.match(usage([usageEv(10, 100), usageEv(80, 100)]), /80 \/ 100 \(80%\)/);
});

/**
 * ⚠ It is transient, so a reattached window has none until a turn runs — and that unknown must not
 * draw as 0%. An empty window and an unmeasured one look nothing alike to somebody deciding whether
 * to fold.
 */
test('no reading says nothing rather than zero percent', () => {
  assert.equal(usage([]), '');
  assert.equal(usage([{ seq: 1, type: 'context.usage', data: { tokens: 5 } }]), '');
});

const submitted = (msgId: string, text: string): Event =>
  ({ seq: seq++, type: 'prompt.submitted', data: { messageId: msgId, parts: [{ text }] } });
const abandoned = (msgId: string): Event =>
  ({ seq: seq++, type: 'prompt.abandoned', data: { msgId } });

/**
 * ★ A cancelled prompt kept claiming it was waiting for an answer.
 *
 * `pending` is cleared by an assistant part or by `turn.finished`, and an interrupted prompt gets
 * neither — so the bar stood for the life of the window about a request that had been stopped. A
 * standing claim that has gone false is the shape this tree calls "sentences that age".
 */
test('a cancelled prompt stops claiming it is waiting', () => {
  const got = rows([submitted('m1', 'do it'), abandoned('m1')]);
  assert.equal(got.length, 1);
  assert.ok(!got[0].pending, 'the row still says it is waiting for an answer');
  assert.equal(got[0].abandoned, true, 'the row does not say it was cancelled');
});

/**
 * ⚠ The two events spell the same id differently: `messageId` on the prompt, `msgId` on the
 * abandonment. Reading either one for the other finds nothing and marks nothing — silently.
 */
test('the abandonment finds its prompt across the two spellings', () => {
  const proto = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'internal', 'core', 'event', 'payload.go'), 'utf8');
  assert.match(proto, /MessageID string\s+`json:"messageId"/, 'the prompt no longer carries messageId');
  assert.match(proto, /MsgID string\s+`json:"msgId"/, 'the abandonment no longer carries msgId');
});

/** Only the named prompt. Marking every row would cancel work that is still running. */
test('an abandonment marks only its own prompt', () => {
  const got = rows([submitted('m1', 'first'), submitted('m2', 'second'), abandoned('m1')]);
  assert.equal(got[0].abandoned, true);
  assert.ok(!got[1].abandoned, 'the other prompt was marked too');
  assert.equal(got[1].pending, true, 'the other prompt stopped waiting');
});

/** An abandonment for a prompt this window never saw marks nothing rather than guessing. */
test('an abandonment with no matching prompt is ignored', () => {
  const got = rows([submitted('m1', 'first'), abandoned('nope')]);
  assert.ok(!got[0].abandoned);
});

const imagePart = (path: string): Event =>
  ({ seq: seq++, type: 'part.appended',
     data: { role: 'assistant', part: { kind: 'image', image: { path, mime: 'image/png' } } } });

/**
 * ★ A part kind this fold does not name is not an empty row — it is a row that never existed.
 *
 * The web console hit exactly this and its comment says so: "An image and an error both reached the
 * log and neither reached the page." `error` was handled here and `image` was not, so a tool that
 * answered with a picture produced nothing at all on this screen — no row, no error, nothing.
 *
 * The path rather than the picture: a webview cannot read a file off disk without being handed a URI
 * for it, and drawing nothing while that is worked out is the defect being fixed.
 */
test('a picture a tool returned is not dropped on the floor', () => {
  const got = rows([imagePart('/tmp/shot.png')]);
  assert.equal(got.length, 1, 'the image produced no row at all');
  assert.equal(got[0].who, 'image');
  assert.equal(got[0].text, '/tmp/shot.png');
});

/** An image part with nowhere to point is not a row — a path is the whole of what this can show. */
test('an image with no path makes no row', () => {
  assert.deepEqual(rows([{ seq: 1, type: 'part.appended',
    data: { role: 'assistant', part: { kind: 'image', image: {} } } }]), []);
});

/** And it does not steal the error row, which shares the branch it was added beside. */
test('an error part still draws as an error', () => {
  const got = rows([{ seq: 1, type: 'part.appended',
    data: { role: 'assistant', part: { kind: 'error', error: 'it broke' } } }]);
  assert.equal(got[0].who, 'error');
  assert.equal(got[0].text, 'it broke');
});

/**
 * Which door the composer knocks on, and where it learns that.
 *
 * The defect: this client always sent `submit`. `submit` is a NEW top-level request, so the core
 * runs `resetForNewTopLevel` — it empties the plan and winds back the turn notes and the completion
 * gate. Typed during a running turn that means a person who added one clarifying sentence has just
 * deleted the plan of the turn they were clarifying. `steer` is the door for that, and it exists.
 *
 * And the fact is here, not on `status`: the `status` door has no field meaning "a turn is running"
 * (`answerStatus`), `waiting` means blocked on a person, and `doing` is a progress note that
 * exactly one builtin tool file out of fifty writes. The JetBrains client asked `status` and so
 * answered "idle" for nearly every running turn — the same defect wearing a working mechanism.
 */
test('a turn is open while a question stands unanswered under it', () => {
  const asked = [
    { seq: 1, type: 'prompt.submitted', data: { messageId: 'm1', parts: [{ kind: 'text', text: 'go' }] } },
  ];
  assert.equal(turnOpen(asked), true, 'a prompt with no answer and no turn.finished is an open turn');

  const answered = [...asked, { seq: 2, type: 'turn.finished', data: {} }];
  assert.equal(turnOpen(answered), false, 'the turn ended — the next thing typed is a new request');

  assert.equal(turnOpen([]), false, 'an empty conversation is not a running turn');

  // And an abandoned prompt is not an open turn either: nobody is working on it.
  const dropped = [...asked, { seq: 2, type: 'prompt.abandoned', data: { msgId: 'm1' } }];
  assert.equal(turnOpen(dropped), false, 'an abandoned prompt left the turn open');
});

test('the composer picks steer or submit from that fact, not from status', () => {
  const chat = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'chat.ts'), 'utf8');
  const say = chat.slice(chat.indexOf("case 'say'"), chat.indexOf("case 'start'"));
  assert.ok(say.length > 100, 'the submit branch was not found — this guard is reading nothing');
  assert.ok(/turnOpen\(/.test(say), 'the composer does not ask whether a turn is open');
  assert.ok(/'steer'/.test(say), "the composer never sends steer — a mid-turn word wipes that turn's plan");
  assert.ok(!/ask\('submit'/.test(say), 'the door is still hardcoded to submit');
  // The core must still treat the two differently, or this whole choice means nothing.
  const app = fs.readFileSync(path.join(REPO, 'internal', 'app', 'app.go'), 'utf8');
  const submit = app.slice(app.indexOf('func (a *App) Submit('), app.indexOf('func (a *App) Steer('));
  assert.ok(/resetForNewTopLevel/.test(submit), 'Submit no longer resets the turn — re-read this guard');
  assert.ok(!/resetForNewTopLevel/.test(app.slice(app.indexOf('func (a *App) Steer('),
    app.indexOf('func (a *App) Steer(') + 900)), 'Steer now resets too — the two doors stopped differing');
});

/**
 * A tool row says WHICH call it was, not just what kind.
 *
 * Measured against the sibling: the JetBrains client draws the name, a status glyph and a one-line
 * summary of the arguments, and has since it grew a tool row. This client drew the name alone — so
 * a turn running thirty commands was thirty rows reading `bash ✓`, and the transcript could not
 * answer the one question it exists for.
 *
 * The summary is bounded and it is never invented: no arguments means no summary, and the row is
 * the bare name again — which is the truth about a call that was given none.
 */
test('a tool row carries what the call was asked to do', () => {
  const call = (name: string, args: unknown): Event =>
    ({ seq: seq++, type: 'part.appended',
       data: { part: { kind: 'tool-call', toolCall: { callId: 'c1', name, args } } } });

  const [bash] = rows([call('bash', { command: 'go test ./... -count=1' })]);
  assert.equal(bash.text, 'bash');
  assert.equal(bash.args, 'go test ./... -count=1', 'the row does not say which command it ran');

  // The daemon may send args as a JSON string rather than an object — both are the same fact.
  const [read] = rows([call('read', '{"path":"internal/app/app.go"}')]);
  assert.equal(read.args, 'internal/app/app.go', 'a string-encoded argument was not read');

  // Nothing recognised: keep the raw shape rather than dropping it. "No summary" is the defect.
  const [odd] = rows([call('weird', { zork: 'frobnicate' })]);
  assert.ok(odd.args && odd.args.includes('frobnicate'), 'an unrecognised argument vanished');

  // Nothing to say stays nothing. An invented summary would be worse than a bare name.
  assert.equal(rows([call('todowrite', {})])[0].args, undefined);
  assert.equal(rows([call('council', null)])[0].args, undefined);

  // One line, bounded — a summary that wraps is not a summary.
  const long = rows([call('write', { path: 'a'.repeat(400) })])[0].args!;
  assert.ok(long.length <= 101, `the summary is ${long.length} chars — it is a paragraph, not a line`);
  assert.ok(!rows([call('bash', { command: 'one\ntwo\nthree' })])[0].args!.includes('\n'),
    'the summary carries a newline — it will break the row it sits in');

  // And the screen actually draws it.
  //
  // ⚠ "the source mentions r.args" is not that. The first cut of this asserted exactly that, and a
  // mutation that disabled the branch (`if (false && r.who === 'tool' && r.args)`) walked straight
  // through: the mention survives, the drawing does not. Same blind spot as "bound, so it must be
  // read" and "declared, so it must be read" — presence is not effect. So the CONDITION is pinned,
  // and the body is checked for the append that makes it visible.
  const chat = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'chat.ts'), 'utf8');
  const at = chat.indexOf("if (r.who === 'tool' && r.args) {");
  assert.ok(at > 0, 'the tool-row branch is gone or has been rewritten — it must test r.args and nothing else');
  const branch = chat.slice(at, at + 400);
  assert.ok(/\.append\(/.test(branch), 'the branch reads the summary and never puts it on the row');
  assert.ok(/className = 'args'/.test(branch), 'the summary is drawn with no class — it would read as the name');
  assert.ok(/\.args\s*\{/.test(chat), 'the summary has no style — it would read as the name itself');
});

/**
 * A tool result answers TWO questions, and the row draws both.
 *
 * The core measured this on a live run and wrote it down (`session.ToolResult.Advisory`): a
 * post-edit hook and a language server set `isError` on purpose — that is what makes the model stop
 * and act — and `isError` is also what every screen draws its glyph from, so a file that was
 * written and then linted drew as a write that FAILED. The file was on disk, the model treated it
 * as done, and both windows said ✗. The fix was to split "did the work happen" from "is there
 * something to read"; the JetBrains client took it and this one had not even declared the field.
 *
 * And the second half: a row that draws ✗ with nothing else names the shape of the trouble and none
 * of its content, which is what a person opened the transcript for.
 */
test('a tool result tells done-with-notes from failed, and says why it failed', () => {
  const pair = (isError: boolean, extra: Record<string, unknown>): Event[] => [
    { seq: seq++, type: 'part.appended',
      data: { part: { kind: 'tool-call', toolCall: { callId: 'c9', name: 'write', args: { path: 'a.ts' } } } } },
    { seq: seq++, type: 'part.appended',
      data: { part: { kind: 'tool-result', toolResult: { callId: 'c9', isError, ...extra } } } },
  ];

  const plain = rows(pair(false, { content: 'wrote 12 lines' }))[0];
  assert.equal(plain.ok, true);
  assert.equal(plain.note, undefined, 'an ordinary success is not flagged as having something to read');
  assert.equal(plain.out, undefined, 'a success carries a failure reason');

  // The measured one: isError AND advisory. The work happened.
  const linted = rows(pair(true, { advisory: true, content: 'a.ts:3 unused import' }))[0];
  assert.equal(linted.ok, true, 'a written-then-linted file still draws as a write that failed');
  assert.equal(linted.note, true, 'nothing marks it as done-with-something-to-read');
  assert.equal(linted.out, undefined,
    "an advisory's text is the AGENT's to act on — on the row it paints a success in failure colours");

  // A real failure keeps its words.
  const broke = rows(pair(true, { content: 'permission denied: /etc/hosts' }))[0];
  assert.equal(broke.ok, false);
  assert.equal(broke.out, 'permission denied: /etc/hosts', 'the failure drew ✗ and said nothing else');

  // Read the VALUE, not its rendering — a JSON string stringified again leaves escapes on screen.
  assert.equal(rows(pair(true, { content: '{"why":"no such file"}' }))[0].out, '{"why":"no such file"}');

  // And the screen draws all three, with the reason under the row.
  const chat = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'chat.ts'), 'utf8');
  assert.ok(/r\.note\s*\?/.test(chat), 'the glyph has two outcomes, not three');
  const at = chat.indexOf("if (r.who === 'tool' && r.out) {");
  assert.ok(at > 0, 'nothing draws the failure reason');
  assert.ok(/\.append\(/.test(chat.slice(at, at + 300)), 'the reason is read and never put on the row');
  assert.ok(/\.out\s*\{/.test(chat), 'the reason has no style — it would read as the tool name');
});

/**
 * Every verdict makes a row, and the row says how they voted.
 *
 * Three defects, measured by putting the two ports' row models side by side (JetBrains carries 24
 * fields, this one carried 14):
 *
 *  - a verdict with no prose pushed no row, so a member who voted with nothing to add vanished and
 *    a council of three drew as a council of two — with nothing saying a seat was missing;
 *  - `decision` was dropped, which is the one thing a vote IS;
 *  - `round` was dropped, so three rounds of three members read as one block.
 *
 * The rule about the empty case is the sibling's, and it is written from a live report: a verdict
 * marked `silent` arrived carrying a full rationale, and the shaper drew the fallback words instead
 * of the ones that came. Prose that arrived is never replaced.
 */
test('every council verdict makes a row, and it carries the vote', () => {
  const verdict = (d: Record<string, unknown>): Event =>
    ({ seq: seq++, type: 'council.verdict', data: d });

  const spoke = rows([verdict({ member: 'Melchior', round: 2, decision: 'continue', rationale: 'the test is blind' })])[0];
  assert.equal(spoke.who, 'council');
  assert.equal(spoke.text, 'the test is blind');
  assert.equal(spoke.decision, 'continue', 'the row does not say how they voted');
  assert.equal(spoke.round, 2, 'the row does not say which round');

  // The one that used to vanish.
  const quiet = rows([verdict({ member: 'Casper', round: 1, decision: 'abstain', silent: true })]);
  assert.equal(quiet.length, 1, 'a verdict with nothing to say produced no row — the seat vanished');
  assert.equal(quiet[0].decision, 'abstain');
  assert.ok(quiet[0].text.trim(), 'the row is there and blank — nothing says why it is empty');

  // Prose that arrived is never replaced by the fallback, even when `silent` is set.
  const both = rows([verdict({ member: 'Balthasar', silent: true, rationale: 'the backend timed out twice' })])[0];
  assert.equal(both.text, 'the backend timed out twice',
    'a rationale that arrived was thrown away for the fallback — the live report behind this rule');

  // A verdict that says nothing at all is still a row, not an absence.
  assert.equal(rows([verdict({ member: 'Melchior' })]).length, 1);

  // And the screen shows the vote. Carrying it into a field nobody paints is the same defect.
  const chat = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'chat.ts'), 'utf8');
  const at = chat.indexOf("const v = r.who === 'council' ? verdictWord(");
  assert.ok(at > 0, 'the label never builds a vote — the row carries it and nothing draws it');
  // ⚠ **To the next statement, not a byte count.** A fixed window made this guard depend on how
  // long the comments in between are — adding one pushed `r.round` out of view and the guard
  // reported a defect that was not there.
  const end = chat.indexOf('const mark', at);
  assert.ok(end > at, 'the label no longer builds a mark after the vote — this slice is unbounded');
  const line = chat.slice(at, end);
  // ⚠ The vote must go through the wording, not straight from the field: `r.decision` in the label
  // IS the defect one test over — the raw `continue` reading as approval.
  assert.ok(/verdictWord\(r\.decision, r\.silent\)/.test(line),
    'the label does not word the verdict, or does not pass `silent` — no-answer then reads as abstain');
  assert.ok(/v\.word/.test(line) && /r\.round/.test(line), 'the label leaves out the vote or the round');
  assert.ok(!/`\s*\$\{r\.decision\}/.test(line), 'the label puts the raw decision on screen again');
  assert.ok(/label: who \+ vote/.test(chat), 'the vote is built and never joined to the label');
});

/**
 * A parked prompt is marked as parked, and the mark reaches the screen.
 *
 * The core parks a message typed while a turn is running and runs it as its own turn afterwards
 * (`interjection.deferred`, the F5 ledger). Unread, the parked row drew exactly like one being
 * worked on — same bar, same everything — so a person could not tell whether the sentence they
 * typed had been picked up or shelved.
 *
 * The screen half is asserted because the field half is not the feature: a flag nobody paints is
 * the defect with a note attached, which this session has now measured five separate times.
 */
test('a parked prompt says so, and the mark is drawn', () => {
  const asked: Event[] = [{ seq: seq++, type: 'prompt.submitted',
    data: { messageId: 'm7', parts: [{ kind: 'text', text: 'also check the windows path' }] } }];

  assert.equal(rows(asked)[0].queued, undefined, 'an ordinary prompt is marked as parked');

  const parked = rows([...asked, { seq: seq++, type: 'interjection.deferred', data: { messageId: 'm7' } }])[0];
  assert.equal(parked.queued, true, 'a parked prompt draws exactly like one being worked on');

  // Answered: the mark comes down, and so does the bar — "in its place" is not "still waiting".
  const done = rows([...asked,
    { seq: seq++, type: 'interjection.deferred', data: { messageId: 'm7' } },
    { seq: seq++, type: 'interjection.answered', data: { messageId: 'm7' } }])[0];
  assert.equal(done.queued, false);
  assert.equal(done.pending, false);

  // Abandoned clears it too — a parked message on a cancelled turn is not still queued.
  const gone = rows([...asked,
    { seq: seq++, type: 'interjection.deferred', data: { messageId: 'm7' } },
    { seq: seq++, type: 'prompt.abandoned', data: { msgId: 'm7' } }])[0];
  assert.equal(gone.queued, false);
  assert.equal(gone.abandoned, true);

  const chat = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'chat.ts'), 'utf8');
  assert.ok(/r\.queued \? ' queued' : ''/.test(chat), 'the row carries the mark and the screen never draws it');
  // ⚠ Look in the STYLE block, not the whole file. The first cut matched anywhere, and `r.queued`
  // in the TypeScript contains the substring `.queued` — so deleting the CSS rule left the guard
  // green. Presence somewhere is not a style; this reads the sheet the webview actually carries.
  const style = chat.slice(chat.indexOf('<style>'), chat.indexOf('</style>'));
  assert.ok(style.length > 200, 'the style block was not found — this guard is reading nothing');
  assert.ok(/\.queued\b[^{]*\{/.test(style), 'the parked class has no style — it would look like any other row');
});

/**
 * A stream that ends is not a conversation that ended.
 *
 * The daemon restarts often — a self-update, a crash, somebody stopping it. Until now `whenClosed`
 * only nulled a field: this window stopped receiving, said nothing, and looked exactly like a
 * companion with nothing to say. Measured against the sibling, which tells the three endings apart
 * (we closed it / the daemon went / it broke, with the reason) and reattaches on two of them.
 *
 * Read off the source: the seam is a socket callback and there is no daemon in this process. What
 * is checkable here is that the ending is REPORTED, that something tries again, and that the retry
 * backs off and stops — a loop with no wait turns one restart into a busy panel, and one that
 * never stops drags a person back to a conversation they left.
 */
test('a stream that ends says so and is picked up again', () => {
  const chat = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'chat.ts'), 'utf8');

  const at = chat.indexOf('s.whenClosed(');
  assert.ok(at > 0, 'nothing watches for the stream ending — this guard is reading nothing');
  // ⚠ Cut at the block's own end, not by a character count. A 900-char window reached into
  // `reattach()` below, so deleting the note HERE still matched its note THERE and the mutation
  // survived — the guard was reading the wrong function's words.
  const closed = chat.slice(at, chat.indexOf('\n    });', at));
  assert.ok(closed.length > 50 && closed.length < 900, `the whenClosed block did not cut cleanly (${closed.length})`);
  assert.ok(/kind: 'note'/.test(closed), 'the stream ends and the person is told nothing');
  assert.ok(/reattach\(/.test(closed), 'the stream ends and nothing tries to get it back');
  // Somebody else may already have moved on. Reattaching then would drag them back.
  assert.ok(/this\.stream !== s/.test(closed), 'an ending is reported for a stream we already replaced');

  const rat = chat.indexOf('private async reattach(');
  assert.ok(rat > 0, 'reattach is called and not defined');
  const body = chat.slice(rat, rat + 900);
  assert.ok(/setTimeout/.test(body), 'the retry has no wait — one restart becomes a busy panel');
  assert.ok(/retryAfter\(/.test(body), 'the retry no longer uses the schedule the tests can run');
  assert.ok(/this\.view/.test(body), 'the retry does not stop when the panel closes');
  assert.ok(/reconnecting/.test(body),
    'it retries in silence — with a backoff this long the last failure stands as the current state');
});

/**
 * The retry schedule itself, run rather than read.
 *
 * The window's loop can only be read from source — a mutation that returned early from it walked
 * straight past the guard above, which is the honest limit of reading source. So the SCHEDULE is a
 * rule in core and this runs it: it starts at a second (a daemon that just went is not back this
 * millisecond, and a tight loop turns one restart into a busy panel), it grows, and it stops at
 * thirty seconds (somebody who starts it again should not wait minutes for the window to notice).
 */
test('the retry waits, grows, and stops growing', () => {
  assert.equal(retryAfter(0), 1_000, 'the first retry is immediate — one restart becomes a busy panel');
  assert.equal(retryAfter(1), 2_000);
  assert.equal(retryAfter(2), 4_000);
  assert.ok(retryAfter(3) > retryAfter(2), 'the wait does not grow');
  assert.equal(retryAfter(20), 30_000, 'the wait is unbounded — a restart would go unnoticed for minutes');
  // A caller that starts at -1 must not get a negative or a zero wait.
  assert.equal(retryAfter(-1), 1_000);
});

/**
 * Only the newest attempt to open a stream gets to be the stream.
 *
 * `openStream` waits twice — for the session list, then for the connection — and two things can
 * happen in between: a person picks another conversation, and the reattach timer calls in on its
 * own. If the slower attempt still installs itself, the panel streams a conversation nobody asked
 * for while `this.sid` names another, and BOTH push into one `events` array, so two conversations
 * interleave in one transcript.
 *
 * The reattach loop made this ordinary — before it, opening twice took a person doing two things
 * quickly. Same shape as the status poll's guard, one level up.
 */
test('a stream that a newer attempt overtook does not install itself', () => {
  const chat = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'chat.ts'), 'utf8');
  const at = chat.indexOf('private async openStream(');
  assert.ok(at > 0, 'openStream was not found — this guard is reading nothing');
  const body = chat.slice(at, chat.indexOf('\n  private draw(', at));
  assert.ok(body.length > 200, 'openStream did not cut cleanly');

  assert.ok(/const mine = \+\+this\.opening/.test(body), 'the attempt takes no number, so it cannot know it was overtaken');

  // Every await must be followed by the check, before anything is written or installed.
  const awaits = [...body.matchAll(/await [^\n]*\n/g)];
  assert.ok(awaits.length >= 2, `only ${awaits.length} awaits seen — openStream waits twice`);
  for (const a of awaits) {
    const after = body.slice(a.index! + a[0].length, a.index! + a[0].length + 260);
    assert.ok(/mine !== this\.opening/.test(after),
      `an await is not followed by the overtaken check: …${a[0].trim().slice(0, 60)}`);
  }

  // And a connection that lost the race is handed back, or the daemon keeps a subscription open.
  assert.ok(/mine !== this\.opening\) \{ s\.close\(\); return; \}/.test(body),
    'the losing attempt drops its socket without closing it — the daemon keeps streaming to nobody');
});

/**
 * ★ A `continue` vote is a REJECTION, and this client printed the word as it arrived.
 *
 * `council.Decision` is three words and one reads as its own opposite: `continue` means "not done,
 * more work is needed". It is the gate on ending the turn — the work cannot pass it — so a row that
 * says "melchior continue" tells the reader the vote let the work proceed.
 *
 * The core has already paid for this twice elsewhere. The terminal has said "reject" since it drew
 * its first verdict (`councilVerdictLabel`), and the web server carries a test called
 * `TestAContinueVoteReadsAsTheRejectionItIs` whose comment is exact: "The page printed the raw word
 * in a neutral colour, which reads as progress — the opposite of what the vote means."
 *
 * The words are pinned against the TERMINAL's table, not written twice: three surfaces saying one
 * verdict three ways is the same defect one level up.
 */
test('a council verdict is worded the way the other surfaces word it', () => {
  const tui = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'internal', 'adapter', 'tui', 'render.go'), 'utf8');
  const at = tui.indexOf('func councilVerdictLabel(');
  assert.ok(at > 0, 'the terminal no longer has the table this guard copies — the words are unanchored');
  const table = tui.slice(at, tui.indexOf('\n}', at));
  const pairs = [...table.matchAll(/case "([a-z]+)":\s*\n\s*return "([^"]*)", "([^"]+)"/g)];
  assert.ok(pairs.length >= 3, `only ${pairs.length} verdict words read from the terminal — the parser is stale`);

  for (const [, decision, icon, word] of pairs) {
    const got = verdictWord(decision);
    assert.equal(got.word, word, `"${decision}" reads as "${got.word}" here and "${word}" in the terminal`);
    assert.equal(got.icon, icon, `"${decision}" is marked "${got.icon}" here and "${icon}" in the terminal`);
  }
  // The one this guard exists for, stated outright so a reader sees the claim without the table.
  assert.equal(verdictWord('continue').word, 'reject',
    'a rejection is still worded as the word that reads like approval');

  // A verdict nobody gave is not a considered abstention. `silent` rides beside `abstain` so the
  // tally does not count it; drawing it as abstain reports a backend failure as deliberation.
  assert.equal(verdictWord('abstain', true).word, 'no answer');
  assert.notEqual(verdictWord('abstain', true).word, verdictWord('abstain').word,
    'a member that never spoke reads the same as one that weighed the work and declined');

  assert.equal(verdictWord(undefined).word, '', 'a row with no decision must say nothing, not guess');
  assert.equal(verdictWord('deferred').word, 'deferred', 'an unknown decision is swallowed or renamed');
});

/** And the row must actually use it — a correct function nobody calls is the older defect here. */
test('the council row carries the verdict wording and the silence', () => {
  const drawn = rows([
    { seq: 1, type: 'council.verdict', data: { member: 'Melchior', round: 2, decision: 'continue', rationale: 'the tests do not run' } },
    { seq: 2, type: 'council.verdict', data: { member: 'Casper', round: 2, decision: 'abstain', silent: true } },
  ] as unknown as Parameters<typeof rows>[0]);
  assert.equal(drawn.length, 2, 'the shaper did not draw the verdicts this guard hands it');
  assert.equal(drawn[0].decision, 'continue', 'the row no longer carries the core word it was given');
  assert.equal(drawn[1].silent, true, 'the row drops `silent`, so the label cannot tell no-answer from abstain');
});

/**
 * ★ A verdict says what it stands on, and what it says to keep.
 *
 * Measured across four surfaces (2026-09-09): the terminal, the web console and the JetBrains
 * client all read `lens`, `cite` and `keep` off `CouncilVerdictData`; this one read none of them,
 * with nothing anywhere saying that was deliberate.
 *
 * The core states the case that matters on `cite` itself: it is recorded "because it is checkable
 * — magi looks it up in the material the member was shown", and "an empty one on a `done` is
 * itself worth seeing". A screen that drops it draws a vote standing on nothing exactly like a
 * vote standing on the record. `keep` arrives on approvals too, and that is when it is worth
 * reading: it is what a rewrite forced by another member's objection would otherwise drop.
 */
test('a verdict carries its lens, what it stands on, and what it would keep', () => {
  const payload = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'internal', 'core', 'event', 'payload.go'), 'utf8');
  const at = payload.indexOf('type CouncilVerdictData struct');
  assert.ok(at > 0, 'the core no longer has the verdict payload this guard reads');
  const struct = payload.slice(at, payload.indexOf('\n}', at));
  for (const f of ['lens', 'cite', 'keep']) {
    assert.ok(new RegExp(`json:"${f}`).test(struct), `the wire no longer carries \`${f}\``);
  }

  const drawn = rows([{
    seq: 1, type: 'council.verdict',
    data: { member: 'Melchior', round: 1, decision: 'done', lens: 'correctness',
      cite: 'NO-EVIDENCE', keep: 'the retry budget', rationale: 'reads right' },
  }] as unknown as Parameters<typeof rows>[0]);
  assert.equal(drawn.length, 1, 'the shaper did not draw the verdict this guard hands it');
  assert.equal(drawn[0].lens, 'correctness', 'the row drops the lens — three seats read alike');
  assert.equal(drawn[0].cite, 'NO-EVIDENCE',
    'the row drops what the vote stands on — an approval resting on nothing draws like any other');
  assert.equal(drawn[0].keep, 'the retry budget', 'the row drops what a revision must preserve');

  // Carried is not drawn: the panel must put all three on screen.
  const chat = fs.readFileSync(path.join(__dirname, '..', '..', 'src', 'ide', 'chat.ts'), 'utf8');
  assert.ok(/r\.lens \? ` \[\$\{r\.lens\}\]`/.test(chat), 'the label does not show the lens');
  for (const f of ['cite', 'keep']) {
    assert.ok(new RegExp(`'${f}'[^\\n]*r\\.${f}`).test(chat),
      `the row body never draws \`${f}\` — the shaper carries it and nothing paints it`);
  }
});

/**
 * ★ A turn that could not be VERIFIED is not a turn that finished.
 *
 * `TurnFinishedData.Unverified` marks a finish the execution-evidence gate could not confirm: a
 * top-level turn changed a deliverable and no independent run passed for the current version, so
 * the declared outcome — success OR "impossible" — is not backed by execution. The core says
 * plainly why the flag exists: the turn is "labeled UNVERIFIED rather than laundered into a
 * confident success".
 *
 * This client laundered it (measured 2026-09-09). `turn.finished` cleared the pending marks and
 * made no row, so a finish nobody could confirm drew exactly like one that was. The terminal has
 * surfaced it since the flag landed; the web console does not, and neither did either IDE client.
 */
test('a finish nobody could verify says so', () => {
  const payload = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'internal', 'core', 'event', 'payload.go'), 'utf8');
  const at = payload.indexOf('type TurnFinishedData struct');
  assert.ok(at > 0, 'the core no longer has the turn payload this guard reads');
  const struct = payload.slice(at, payload.indexOf('\n}', at));
  assert.match(struct, /Unverified bool\s+`json:"unverified,omitempty"`/,
    'the wire no longer carries `unverified` the way this guard reads it');
  // ⚠ omitempty on a Go bool: FALSE never goes on the wire, so the ordinary finish is the ABSENT
  // one. A guard that expected `unverified: false` would be testing a shape that never arrives.
  const ok = rows([{ seq: 1, type: 'turn.finished', data: { usage: {} } }] as unknown as Parameters<typeof rows>[0]);
  assert.equal(ok.filter((r) => /Unverified/i.test(r.text)).length, 0,
    'an ordinary finish is reported as unverified — the common case now carries a warning');

  const bad = rows([
    { seq: 1, type: 'turn.finished', data: { usage: {}, unverified: true, reason: 'the build was never run' } },
  ] as unknown as Parameters<typeof rows>[0]);
  const said = bad.find((r) => /Unverified/i.test(r.text));
  assert.ok(said, 'a finish the evidence gate could not confirm draws exactly like one that was');
  assert.ok(said!.text.includes('the build was never run'),
    'the reason is dropped — the row says something is wrong and gives no handle on what');

  // And with no reason it still says the finish was unconfirmed, rather than saying nothing.
  const bare = rows([{ seq: 1, type: 'turn.finished', data: { usage: {}, unverified: true } }] as unknown as Parameters<typeof rows>[0]);
  assert.ok(bare.some((r) => /Unverified/i.test(r.text)), 'a reasonless unverified finish is silent');
});

/**
 * ★ A question answered later is pulled down to its answer — the core wrote the link for this reader.
 *
 * Two shapes, both about something typed WHILE a turn was running:
 *
 * - `resurfacedFrom` — the drain re-runs a queued interjection as its own turn by emitting a fresh
 *   prompt with a NEW id, and this field names the original. The core: it "lets the display layer
 *   pair the query with its answer — dropping the stranded original on replay and pulling the live
 *   bubble down to just above the answer".
 * - `inReplyTo` — an inline answer emits no fresh prompt at all, so this is, in the core's words,
 *   "the only link the display layer has to pair the answer with its question".
 *
 * This client read neither (measured 2026-09-09): the question stayed where it was typed, minutes of
 * transcript above its answer, still wearing a queued mark, and a resurfaced one appeared TWICE.
 * The JetBrains shaper has done both since the fields landed.
 */
test('a question answered later sits with its answer, once', () => {
  const payload = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', '..', 'internal', 'core', 'event', 'payload.go'), 'utf8');
  for (const f of ['resurfacedFrom', 'inReplyTo']) {
    assert.ok(new RegExp(`json:"${f},omitempty"`).test(payload), `the wire no longer carries \`${f}\``);
  }

  // Resurfaced: one row, at the end, not queued, and the text that came with the re-emission.
  // ⚠ Rows in between on purpose. A first cut of this fixture had the question two from the end,
  // so a mover that guessed a POSITION instead of following the link landed on it by luck and the
  // mutation survived. The link has to be the only thing that finds it.
  const re = rows([
    { seq: 1, type: 'prompt.submitted', data: { messageId: 'q1', parts: [{ kind: 'text', text: 'and the tests?' }] } },
    { seq: 2, type: 'interjection.deferred', data: { messageId: 'q1' } },
    { seq: 3, type: 'part.appended', data: { messageId: 'a1', role: 'assistant', part: { kind: 'text', text: 'working on the build' } } },
    { seq: 4, type: 'part.appended', data: { messageId: 'a1', role: 'assistant', part: { kind: 'text', text: 'and linking it' } } },
    { seq: 5, type: 'part.appended', data: { messageId: 'a1', role: 'assistant', part: { kind: 'text', text: 'done' } } },
    { seq: 6, type: 'prompt.submitted', data: { messageId: 'q2', resurfacedFrom: 'q1', parts: [{ kind: 'text', text: 'and the tests?' }] } },
  ] as unknown as Parameters<typeof rows>[0]);
  assert.deepEqual(re.filter((r) => r.who === 'agent').map((r) => r.text),
    ['working on the build', 'and linking it', 'done'],
    'the answers were reordered — something moved a row that was not the question');
  const asked = re.filter((r) => r.who === 'user' && r.text === 'and the tests?');
  assert.equal(asked.length, 1, 'the resurfaced question appears twice — the stranded original was not dropped');
  assert.equal(re[re.length - 1].text, 'and the tests?', 'the question was not pulled down to where it is answered');
  assert.ok(!asked[0].queued, 'the moved question still wears its queued mark');

  // Inline: same pairing, but no fresh prompt exists at all.
  // ⚠ Parked FIRST, so that clearing the mark is measurable. Without the deferral the row was
  // never queued, and a move that forgot to clear the mark passed — the mutation proved it.
  const inline = rows([
    { seq: 1, type: 'prompt.submitted', data: { messageId: 'q1', parts: [{ kind: 'text', text: 'why is it slow?' }] } },
    { seq: 2, type: 'interjection.deferred', data: { messageId: 'q1' } },
    { seq: 3, type: 'part.appended', data: { messageId: 'a1', role: 'assistant', part: { kind: 'text', text: 'a moment' } } },
    { seq: 4, type: 'part.appended', data: { messageId: 'a2', role: 'assistant', inReplyTo: 'q1', part: { kind: 'text', text: 'the cache was cold' } } },
  ] as unknown as Parameters<typeof rows>[0]);
  const q = inline.find((r) => r.text === 'why is it slow?');
  assert.ok(q && !q.queued,
    'the question was answered and still wears its parked mark — it reads as still waiting');
  const at = inline.findIndex((r) => r.text === 'why is it slow?');
  const ans = inline.findIndex((r) => r.text === 'the cache was cold');
  assert.ok(at >= 0 && ans >= 0, 'the shaper did not draw the pair this guard hands it');
  assert.equal(ans - at, 1, 'the question is not immediately above its inline answer');

  // ⚠ And an ORDINARY prompt is untouched — a mover that fires on every prompt would reorder a
  // plain conversation, and every assertion above would still pass.
  const plain = rows([
    { seq: 1, type: 'prompt.submitted', data: { messageId: 'm1', parts: [{ kind: 'text', text: 'first' }] } },
    { seq: 2, type: 'part.appended', data: { messageId: 'a1', role: 'assistant', part: { kind: 'text', text: 'ok' } } },
    { seq: 3, type: 'prompt.submitted', data: { messageId: 'm2', parts: [{ kind: 'text', text: 'second' }] } },
  ] as unknown as Parameters<typeof rows>[0]);
  assert.deepEqual(plain.map((r) => r.text), ['first', 'ok', 'second'],
    'an ordinary conversation was reordered — the pairing fired without a link');
});
