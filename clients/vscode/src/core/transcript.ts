import { Event } from './protocol';

/**
 * Events into rows, in ONE place.
 *
 * Invariant 0-1: the rule that builds a conversation's rows is written once. Two copies drift on
 * whichever side is not measured, and this repository has already paid that bill.
 *
 * Invariant 0-2: draw shallowly. What to say is the daemon's decision; a client that parses
 * payloads to compose sentences composes them once per client, and there are six clients.
 */

export type Who = 'user' | 'agent' | 'tool' | 'thinking' | 'council' | 'system' | 'error';

export interface Row {
  /** The event that put this row here, so a later frame can find it again. */
  seq: number;
  who: Who;
  text: string;
  /** Tool rows only: the call this row is about, so its result can land on it. */
  callId?: string;
  /** Tool rows only, once the result arrives. Absent means still running. */
  ok?: boolean;
  /** A row whose prompt has no answer yet — the screen draws a bar beside it. */
  pending?: boolean;
  /** Council rows: which member said it. Only three names ever get a colour. */
  member?: string;
  /** Rows that are folded shut by default (reasoning, tool bodies). */
  folded?: boolean;
}

interface PartLike {
  kind?: string;
  text?: string;
  toolCall?: { callId?: string; name?: string };
  toolResult?: { callId?: string; content?: unknown; isError?: boolean };
  error?: string;
}

/**
 * Fold a log into rows.
 *
 * Not every event becomes a row, and the ones that do not are as deliberate as the ones that do:
 * `context.usage` and `todos.changed` are facts for other screens, and putting them in the
 * transcript would make the conversation a log file.
 */
export function rows(events: Event[]): Row[] {
  const out: Row[] = [];
  const answered = new Set<number>();
  for (const e of events) {
    const d = (e.data ?? {}) as Record<string, unknown>;
    switch (e.type) {
      case 'prompt.submitted': {
        const parts = (d.parts as PartLike[] | undefined) ?? [];
        const text = parts.map((p) => p.text ?? '').join('').trim();
        if (text) out.push({ seq: e.seq, who: 'user', text, pending: true });
        break;
      }
      case 'part.appended': {
        const p = (d.part ?? {}) as PartLike;
        const role = String(d.role ?? '');
        // Any assistant part answers the prompts above it. The mark travels on the ROW rather than
        // being recomputed at draw time: a screen that re-derived it would have to hold the whole
        // log to draw one row.
        if (role === 'assistant' || role === 'tool') {
          for (const r of out) if (r.pending && !answered.has(r.seq)) { r.pending = false; answered.add(r.seq); }
        }
        if (p.kind === 'text' && (p.text ?? '').trim()) {
          out.push({ seq: e.seq, who: 'agent', text: p.text!.trim() });
        } else if (p.kind === 'reasoning' && (p.text ?? '').trim()) {
          out.push({ seq: e.seq, who: 'thinking', text: p.text!.trim(), folded: true });
        } else if (p.kind === 'tool-call' && p.toolCall) {
          out.push({ seq: e.seq, who: 'tool', text: p.toolCall.name ?? 'tool', callId: p.toolCall.callId });
        } else if (p.kind === 'tool-result' && p.toolResult) {
          // The result lands ON the call's row rather than starting a new one — one call, one line.
          const call = p.toolResult.callId;
          const row = [...out].reverse().find((r) => r.who === 'tool' && r.callId === call);
          if (row) row.ok = !p.toolResult.isError;
        } else if (p.kind === 'error' && (p.error ?? '').trim()) {
          out.push({ seq: e.seq, who: 'error', text: p.error!.trim() });
        }
        break;
      }
      case 'error': {
        const msg = String(d.message ?? '').trim();
        // A recovered error is not an ending, and the payload says which. Dropping that distinction
        // would make every retry look like a failure.
        if (msg) out.push({ seq: e.seq, who: 'error', text: d.recovered ? `${msg} (recovered)` : msg });
        break;
      }
      case 'council.verdict': {
        const member = String(d.member ?? '');
        const text = String(d.feedback ?? d.rationale ?? '').trim();
        if (text) out.push({ seq: e.seq, who: 'council', text, member });
        break;
      }
      case 'turn.finished':
        // Not a row. It ends the turn, and the screen reads that from the LAST row's pending mark.
        for (const r of out) r.pending = false;
        break;
      default:
        break; // facts for other screens (context.usage, todos.changed, labels.changed, …)
    }
  }
  return out;
}

/** The three council seats that get a colour. Nobody else does — a fourth name is not a seat. */
export const SEATS = ['melchior', 'balthasar', 'casper'];

export function seat(member: string | undefined): string | null {
  const m = (member ?? '').toLowerCase();
  return SEATS.includes(m) ? m : null;
}

/**
 * How many turns back a given prompt is — what the `rewind` door wants.
 *
 * ⚠ **The door counts TURNS, not sequence numbers.** It reads `n` and passes it to
 * `App.Rewind(sid, n)`, whose own comment is about counting genuine user prompts. This client sent
 * the picked row's `seq` in a field called `since`, which the door does not read at all: the daemon
 * got n=0 and the person's chosen point had nothing to do with what happened. The JetBrains client
 * sends `n = 1` and has always been right.
 *
 * "Back to just before this prompt" drops that prompt AND everything after it, so the count includes
 * the picked one: the newest prompt is 1, the one before it is 2.
 *
 * 0 means the seq is not one of these rows — a caller must not send that as "rewind nothing", because
 * the door would take it as its own default.
 */
export function turnsBack(asked: Row[], seq: number): number {
  const i = asked.findIndex((r) => r.seq === seq);
  return i < 0 ? 0 : asked.length - i;
}

/** One item of the agent's plan. */
export interface Todo {
  content: string;
  status: string;
}

/**
 * The plan, read off the same stream the rows come from.
 *
 * ⚠ **The panel is called Plan and had no plan in it.** The UI design promises "todo and its state"
 * and the panel drew now/context/jobs/scheduled/fleet — five dials and no list. The JetBrains client
 * reads `todos.changed` for exactly this; this one ignored the event, so nothing failed and nothing
 * appeared.
 *
 * The LAST event wins rather than deltas being replayed: `todos.changed` carries the whole list every
 * time, and a reader that accumulated would be wrong from the first event. It is a FACT type, so it
 * rides the replay — a window that reattaches learns the current plan without a door of its own,
 * which is why no door was ever needed.
 */
export function todos(events: Event[]): Todo[] {
  let out: Todo[] = [];
  for (const e of events) {
    if (e.type !== 'todos.changed') continue;
    const list = ((e.data ?? {}) as { todos?: unknown[] }).todos ?? [];
    out = list
      .map((t) => (t ?? {}) as { content?: unknown; status?: unknown })
      .filter((t) => typeof t.content === 'string' && t.content)
      .map((t) => ({ content: String(t.content), status: String(t.status ?? '') }));
  }
  return out;
}

/** The plan as lines, with the mark a person reads the state by. */
export function planLines(list: Todo[]): string {
  const mark = (s: string): string =>
    s === 'completed' ? '✓' : s === 'in_progress' ? '◐' : '☐';
  return list.map((t) => `${mark(t.status)} ${t.content}`).join('\n');
}
