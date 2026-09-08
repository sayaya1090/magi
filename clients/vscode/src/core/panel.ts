import { Response } from './protocol';
import { peerLabel } from './handoff';

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
  const rows = (resp.cron ?? []) as
    { name?: string; schedule?: string; enabled?: boolean; next?: string; problem?: string; prompt?: string; command?: string }[];
  return rows.filter((r) => r.name).map((r) => ({
    name: r.name!,
    // A row carrying a problem is the row to mark: nothing else on any screen mentions it again.
    line: [
      r.name,
      r.schedule,
      r.enabled === false ? 'off' : r.next ? `next ${r.next}` : '',
      r.problem ? `⚠ ${r.problem}` : '',
      (r.prompt ?? r.command ?? '').split('\n')[0].slice(0, 40),
    ].filter(Boolean).join(' · '),
  }));
}

/** The other companions on this machine, as lines. */
export function fleet(resp: Response | null): string[] {
  if (!resp?.ok) return [];
  const rows = (resp.roster ?? []) as
    { name?: string; socket?: string; state?: string; workdir?: string; model?: string; live?: boolean }[];
  // Rows with no socket are kept: the fleet section says what the roster says, and a companion this
  // window cannot dial is still a fact about the machine. Only the HAND-OFF list drops them, because
  // there the socket is the thing being used.
  return rows.map((r) => [
    // The same naming rule the hand-off list uses, so a person can match the two.
    peerLabel({ socket: r.socket ?? '', name: r.name, workdir: r.workdir }) || '?',
    r.state,
    r.model,
    // Whether it is actually there. A row for a dead one is a fact too, and drawing it as live is
    // how somebody sends work to nobody.
    r.live === false ? 'gone' : '',
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
