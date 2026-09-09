import { Response } from './protocol';
import { peerLabel } from './handoff';
import { Event } from './protocol';

/**
 * The panel's facts, read from the fields the daemon actually fills.
 *
 * ⚠ **This replaced a prose parser that could never have worked.** `jobs`, `roster` and `context`
 * answer STRUCTURED fields (`jobs`, `roster`, `context`) and never fill `out` — measured against a
 * live daemon. The panel read `out` for all four sections and the two commands that act on them read
 * it too, so three of the panel's five sections were empty on every build and "stop a background
 * job" always answered "nothing is running". Nothing failed: an absent field reads as an empty
 * string, and an empty string parses to an empty list.
 *
 * The lesson is the shape of the earlier defect too: the parser it replaced carried a careful comment
 * about these doors answering prose. That comment was never measured.
 */

/** One background job or child, as a line. */
export interface Job {
  id: string;
  what: string;
  running: boolean;
}

export function jobs(resp: Response | null): { queued: string[]; jobs: Job[] } {
  if (!resp?.ok) return { queued: [], jobs: [] };
  const j = (resp.jobs ?? {}) as {
    background?: { id?: string; command?: string; running?: boolean; exit?: number; killed?: boolean }[];
    children?: { id?: string; tool?: string; task?: string; running?: boolean; err?: string }[];
    queued?: { kind?: string; text?: string; from?: string }[];
  };
  const out: Job[] = [];
  for (const b of j.background ?? []) {
    if (!b.id) continue;
    const how = b.running ? 'running' : b.killed ? 'killed' : `exit ${b.exit ?? 0}`;
    out.push({ id: b.id, what: `${(b.command ?? '').split('\n')[0].slice(0, 60)} · ${how}`, running: !!b.running });
  }
  for (const c of j.children ?? []) {
    if (!c.id) continue;
    const how = c.err ? `failed: ${c.err.slice(0, 40)}` : c.running ? 'running' : 'done';
    out.push({ id: c.id, what: `${c.tool ?? 'child'}: ${(c.task ?? '').slice(0, 50)} · ${how}`, running: !!c.running });
  }
  // Queued work is not a job to stop — it is what runs NEXT, and both queues are shown in the one
  // order they will run in. A screen that showed only handovers said a companion had nothing
  // waiting while the correction the person typed sat in the other queue.
  const queued = (j.queued ?? [])
    .map((q) => `${q.kind === 'handover' ? `from ${q.from ?? 'somebody'}` : 'you'}: ${(q.text ?? '').split('\n')[0].slice(0, 60)}`);
  return { queued, jobs: out };
}

/** A standing schedule, as a row and as a name something can act on. */
export interface Schedule {
  name: string;
  line: string;
}

export function schedules(resp: Response | null): Schedule[] {
  if (!resp?.ok) return [];
  const rows = resp.cron ?? [];
  return rows.filter((r) => r.name).map((r) => ({
    name: r.name!,
    // A row carrying a problem is the row to mark: nothing else on any screen mentions it again.
    line: [
      r.name,
      r.schedule,
      // ⚠ **`enabled` is a Go bool with `omitempty`, so FALSE NEVER GOES ON THE WIRE.** A switched
      // off job arrives as `{"name":"nightly"}` — no `enabled`, and no `next` either, because the
      // core says "Next is RFC3339, and empty when the job never runs — switched off, or Problem
      // says why" (measured by marshalling the row: on → `enabled:true`, off → the field is gone).
      //
      // So `=== false` was never true and the cell fell through to an empty string: a schedule
      // somebody switched OFF drew exactly like one whose next run is merely unknown. The request
      // side of this same switch is a `*bool` and the core's comment says why — "the switch is
      // three-valued on the wire" — so the distinction was known where it was needed and lost here.
      r.enabled !== true ? 'off' : r.next ? `next ${r.next}` : '',
      r.problem ? `⚠ ${r.problem}` : '',
      // ★ **Which KIND of job it is, not just its words.** The two are exclusive on the wire and
      // the core says why they are different things: a command job "모델을 안 부르고 도구 권한
      // 관문도 안 지난다(설정에 적은 것이 곧 승인)" — it runs a shell command on this machine
      // unattended, with no permission prompt, because being written in the config IS the approval.
      //
      // Measured against a live daemon 2026-09-10: `go build ./...` every five minutes and "저장소를
      // 훑어 본다" at 3am drew as the same shape of row, so the one row on the list that runs
      // commands behind nobody's back looked exactly like the one that asks the model a question.
      // The daemon's own sentence tells them apart ("runs a command" / "asks"); this line did not.
      //
      // `$` is the mark, because that is what the JetBrains panel already uses for this exact fact
      // ("도는 잡은 `$` 로 시작해 한눈에 갈린다"). One vocabulary, so two screens cannot drift.
      r.command ? `$ ${r.command.split('\n')[0].slice(0, 40)}`
        : (r.prompt ?? '').split('\n')[0].slice(0, 40),
    ].filter(Boolean).join(' · '),
  }));
}

/** The other companions on this machine, as lines. */
/**
 * A companion's state as a phrase, because `state` is an ENUM and not prose.
 *
 * `fleet.State` (`internal/adapter/fleet/fleet.go`) is six words, and this row was printing whichever
 * one arrived. Two of them are the reason the enum exists at all, and the core says so on
 * `Abandoned`: "nobody is listening and a turn was left open — a crash, a kill, a closed laptop.
 * **Every other view renders this identically to a finished session, which is why it is here.**"
 *
 * So the core went to the trouble of separating "it finished and went away" from "it died holding
 * work", and this screen printed `stopped` next to `abandoned` and left the person to know which
 * was which. They are one letter apart in tone and nothing apart on screen.
 *
 * ⚠ **An unknown state passes through raw.** A daemon newer than this build may name a seventh, and
 * its word beats an invented sentence or a blank — the rule this client already applies to a
 * permission mode and to a completion reason it does not know.
 */
export function sayState(state: string | undefined): string {
  switch ((state ?? '').trim()) {
    case '': return '';
    case 'working': return 'working';
    case 'idle': return 'idle';
    case 'waiting': return 'waiting for a person';
    case 'abandoned': return 'left holding work — nobody is listening';
    case 'stopped': return 'finished and not running';
    case 'remote': return 'on another machine';
    default: return (state ?? '').trim();
  }
}

/**
 * What work this companion is carrying.
 *
 * The core signs `waiting` and `handling` together and says why: "they decide where team-addressed
 * work goes: fleet.Resolve routes a team address to the lightest companion, and **load is Waiting +
 * (1 if Handling)**". A row that shows only the queue calls a companion free when it is in the
 * middle of a handed-over piece — and this is the row a person clicks to hand it another.
 *
 * Both facts, not the sum. The number decides ROUTING; a person deciding by hand wants to know that
 * one is already in flight, which "1" alone does not say. `handling` is an `omitempty` bool, so
 * false never arrives and absent is the ordinary "not handling".
 */
export function carrying(r: { waiting?: number; handling?: boolean }): string {
  const q = r.waiting ?? 0;
  return [r.handling ? 'busy' : '', q > 0 ? `${q} queued` : ''].filter(Boolean).join(', ');
}

/**
 * What this companion is FOR, in the words it advertises.
 *
 * The core says why these travel at all: "Does NAMES those things… **a name is enough to pick a
 * companion out of a roster**, and what each one actually means is fetched from the machine that
 * has it when somebody wants to know". This row IS that roster — it is what a person reads before
 * handing work over.
 *
 * Measured against a live machine (2026-09-10): three companions drew as `word · idle · sonnet`,
 * `excel · idle · sonnet`, `powerpoint · idle · sonnet` — indistinguishable — while each row
 * carried `does: ["document-structure", "editing", "tables-and-review"]` unread.
 *
 * ⚠ **`can` is not `does.length`.** The core carries the count separately because the list is a
 * SAMPLE past `MaxDoes`: "A SAMPLE when there are more than MaxDoes, which is why Can is carried
 * separately rather than being len(Does)." So a row that shows three names out of seven has to say
 * so, or it reads as the whole of what that companion offers.
 */
export function offers(r: { can?: number; does?: string[] }, show = 3): string {
  const named = (r.does ?? []).filter(Boolean);
  if (!named.length) return '';
  const head = named.slice(0, show);
  // More than we drew, whether the daemon sampled or we clipped.
  const rest = Math.max(r.can ?? named.length, named.length) - head.length;
  return head.join(', ') + (rest > 0 ? ` +${rest}` : '');
}

/**
 * How long ago a sighting was heard, in words a person reads at a glance.
 *
 * Seconds are what the wire carries and they are unreadable past a minute — the gossip decays over
 * an hour, so the normal range of this number is 0 to 3600 and "seen 3540s ago" is the shape most
 * of it takes. The JetBrains panel printed exactly that.
 *
 * Never invents freshness: a row with no age said (an older daemon) draws nothing rather than
 * "just now", because "just now" is the one claim that would make a stale row look measured.
 */
export function seenAgo(sec: number | undefined): string {
  if (sec === undefined || !Number.isFinite(sec) || sec < 0) return '';
  // "<1m", not "just now": the JetBrains panel builds this out of a localised "seen {0} ago", and
  // one fact spelled two ways across two screens is what this repository keeps one vocabulary for.
  if (sec < 60) return 'seen <1m ago';
  const m = Math.round(sec / 60);
  if (m < 60) return `seen ${m}m ago`;
  const h = Math.floor(m / 60);
  return `seen ${h}h${m % 60 ? ` ${m % 60}m` : ''} ago`;
}

export function fleet(resp: Response | null): string[] {
  if (!resp?.ok) return [];
  const rows = resp.roster ?? [];
  // Rows with no socket are kept: the fleet section says what the roster says, and a companion this
  // window cannot dial is still a fact about the machine. Only the HAND-OFF list drops them, because
  // there the socket is the thing being used.
  return rows.map((r) => [
    // The same naming rule the hand-off list uses, so a person can match the two.
    peerLabel({ socket: r.socket ?? '', name: r.name, workdir: r.workdir }) || '?',
    sayState(r.state),
    carrying(r),
    r.model,
    offers(r),
    // Whether it is actually there — and this is where the sentence above was not being kept.
    //
    // Two things were wrong. `live` is `omitempty`, so a FALSE one is never sent and `live === false`
    // never happened: the "gone" mark could not draw. And a sighting — a row another machine signed,
    // whose liveness nobody here can check — carries no `live` either, so it drew exactly like a
    // companion this window can talk to. Both readings said "reachable" about something that is not.
    //
    // Said positively now, from what the wire actually asserts: elsewhere for a sighting, there for
    // a proven dial, and otherwise nothing was said.
    r.sighting ? 'elsewhere' : r.live ? '' : 'no answer',
    // ★ A sighting's state has an AGE, and the core says why it must be drawn with one: "A screen
    // that shows a state without its age is claiming to know something it cannot". This row drew
    // `idle` beside `elsewhere` for a machine last heard from fifty minutes ago, and the gossip
    // decays over an hour — so the oldest row still on screen is the one whose state is least
    // likely to be true, and it was the one that looked exactly like the freshest.
    //
    // Only on sightings. A local row is a live dial with age 0, and "seen just now" beside it
    // would be noise about the one row whose state was measured rather than remembered.
    r.sighting ? seenAgo(r.ageSeconds) : '',
  ].filter(Boolean).join(' · '));
}

/** How full the window is. */
/**
 * Who opened a child conversation, in a word a person reads.
 *
 * ⚠ **`agent` does not tell children apart, and the core says so in the field's own comment:** every
 * child records the same word ("spawn" — a constant in `internal/app/spawn.go`), and a live run
 * proved it by bringing a meeting room back as `agent="spawn"`. `origin` is the discriminator —
 * "meeting" for a room this companion holds as a participant, the spawning tool's actor otherwise.
 *
 * That difference is not cosmetic. A subagent is work THIS conversation delegated; a meeting room is
 * a conversation somebody ELSE convened and this companion is sitting in. The list drew them
 * identically, so the only way to tell was to open one.
 *
 * A meeting opens TWO children — the seat that talks and the seat that writes it down — so leaving
 * the raw words in place would put two look-alike rows on the list per meeting. An unknown origin is
 * passed through unchanged: a word from a newer daemon beats a blank, and an invented name sends
 * somebody to the wrong place. Same vocabulary as the JetBrains panel's `Look.originWord`.
 */
export function originWord(origin: string | undefined): string {
  switch ((origin ?? '').trim()) {
    case '': return '';
    case 'meeting': return 'meeting';
    case 'minutes': return 'minutes';
    default: return origin!.trim();
  }
}

export function context(resp: Response | null): string {
  if (!resp?.ok) return '';
  const c = (resp.context ?? null) as
    { window?: number; used?: number; estimated?: boolean; messages?: number;
      compactions?: number; parts?: Record<string, number>; topics?: string[] } | null;
  if (!c || !c.window) return '';
  const pct = Math.round(((c.used ?? 0) / c.window) * 100);
  /**
   * ⚠ **Shares of their own sum, not token totals.**
   *
   * The core states the rule and the reason: the breakdown is a chars/4 estimate even when `used`
   * is the provider's measured count, so the pieces "will not sum to a measured Used. They are
   * honest as proportions and dishonest as totals, **which is why the screen draws them as a share
   * of their own sum and says the reading is an estimate**".
   *
   * This line printed `tools 23507 · system 2824` — measured against a live daemon 2026-09-10 — four
   * numbers a person naturally adds up and compares against `used`, which is exactly the arithmetic
   * the core says they cannot support. The JetBrains panel has drawn shares since the field landed.
   *
   * The `estimated` flag on the line above is about `used`; these are estimates regardless, so the
   * marker belongs here too.
   */
  const named = Object.entries(c.parts ?? {}).filter(([, v]) => v > 0);
  const sum = named.reduce((n, [, v]) => n + v, 0);
  const parts = sum > 0
    ? 'made of (est.) ' + named.sort((a, b) => b[1] - a[1])
      .map(([k, v]) => `${k} ${Math.round((v * 100) / sum)}%`).join(' · ')
    : '';
  return [
    `${c.used ?? 0} / ${c.window} (${pct}%)${c.estimated ? ' estimated' : ''}`,
    c.messages !== undefined ? `${c.messages} messages` : '',
    // A fold says the conversation was replaced by a summary. On its own that is a loss; the core
    // keeps the detail in the log and can pull a subject back with `recall_context`, and says the
    // naming IS the difference: these are "what 'the detail is not lost' means concretely, and
    // naming them is the difference between that claim and a promise". So the count and the
    // subjects travel together — a count alone makes the promise without keeping it.
    c.compactions
      ? `folded ${c.compactions}×` + (c.topics?.length ? ` — still there: ${c.topics.join(', ')}` : '')
      : '',
    parts,
  ].filter(Boolean).join('\n');
}

/**
 * How full the window is, read off the STREAM instead of the door.
 *
 * The `context` door is a capability a daemon may not have — measured on a live one that advertises
 * nine capabilities and not that one, so the panel's context section read "this companion does not
 * answer that" while every turn was broadcasting the number anyway. `context.usage` is on the bus,
 * and a client already streaming the transcript has it for nothing.
 *
 * ⚠ **It is TRANSIENT, so it is not replayed.** A window that reattaches does not know until the next
 * turn runs — and that unknown must not be drawn as 0%: an empty window and an unmeasured one look
 * nothing alike to a person deciding whether to fold. Empty means "nothing to say", not "0%".
 */
export function usage(events: Event[]): string {
  type Usage = { tokens?: number; window?: number; percent?: number; outTokens?: number };
  let last: Usage | null = null;
  for (const e of events) {
    if (e.type !== 'context.usage') continue;
    last = (e.data ?? {}) as Usage;
  }
  if (!last?.window) return '';
  const pct = last.percent !== undefined
    ? Math.round(last.percent)
    : Math.round(((last.tokens ?? 0) / last.window) * 100);
  return [`${last.tokens ?? 0} / ${last.window} (${pct}%)`,
    last.outTokens ? `out ${last.outTokens}` : ''].filter(Boolean).join(' · ');
}
