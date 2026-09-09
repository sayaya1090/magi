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
      (r.prompt ?? r.command ?? '').split('\n')[0].slice(0, 40),
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
    r.model,
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
  ].filter(Boolean).join(' · '));
}

/** How full the window is. */
export function context(resp: Response | null): string {
  if (!resp?.ok) return '';
  const c = (resp.context ?? null) as
    { window?: number; used?: number; estimated?: boolean; messages?: number;
      compactions?: number; parts?: Record<string, number> } | null;
  if (!c || !c.window) return '';
  const pct = Math.round(((c.used ?? 0) / c.window) * 100);
  const parts = Object.entries(c.parts ?? {})
    .sort((a, b) => b[1] - a[1])
    .map(([k, v]) => `${k} ${v}`)
    .join(' · ');
  return [
    `${c.used ?? 0} / ${c.window} (${pct}%)${c.estimated ? ' estimated' : ''}`,
    c.messages !== undefined ? `${c.messages} messages` : '',
    c.compactions ? `folded ${c.compactions}×` : '',
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
