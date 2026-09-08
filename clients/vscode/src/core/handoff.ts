import { Response } from './protocol';

/**
 * Handing work to another companion on this machine, and watching what happens to it.
 *
 * The fleet panel lists the other companions; this is what a person can DO with that list. It is
 * the one thing in these clients that speaks to a socket other than this workspace's.
 */

/**
 * The mark that opens the label on handed-over work.
 *
 * Copied from `internal/adapter/fleet/fleet.go`, not invented. That constant is three things at
 * once — the sentence a person reads, the fact the no-chaining rule is read off, and the string the
 * fleet package greps to reconstruct who handed what to whom — and its own comment says a marker
 * with three readers must have exactly one spelling. Send a label without it and the receiving side
 * loses all three, silently.
 */
export const DISPATCH_MARK = '— asked by ';

/** One companion this machine can name. */
export interface Peer {
  socket: string;
  name?: string;
  workdir?: string;
  state?: string;
}

/** Read the `roster` reply. Rows without a socket are dropped: there is nothing to dial. */
export function peers(resp: Response | null): Peer[] {
  if (!resp?.ok) return [];
  const rows = (resp.roster ?? []) as Peer[];
  return rows.filter((r) => typeof r?.socket === 'string' && r.socket);
}

/** What a person picked this companion out of the list by. */
export function peerLabel(p: Peer): string {
  return p.name?.trim() || p.socket.split('/').pop() || p.socket;
}

/**
 * The label that rides in front of a handed-over request.
 *
 * ⚠ It also says there is no way back. This client cannot receive a reply — it hands work over and
 * then READS the state — so the label has to tell the far side that, or it answers into a channel
 * nobody is listening on. The JetBrains client says the same thing for the same reason.
 */
export function dispatchLabel(from: string): string {
  return `${DISPATCH_MARK}${from}, a person working in an editor on this machine. ` +
    'Answer it here; they read what you say from your transcript. There is no reply channel.';
}

/** A piece of work handed to somebody else. */
export interface Handed {
  socket: string;
  who: string;
  receipt: string;
  asked: string;
  /** The last sentence we learned. Kept so a finished one still draws without being polled. */
  line?: string;
  /**
   * Stop asking. Either it ended, or the far side does not know this receipt — a restart or an
   * expiry, which the Taker contract spells as a refusal.
   */
  over?: boolean;
}

/**
 * Read one `hand-state` reply into the sentence a screen shows.
 *
 * ⚠ **A refusal and a dead connection are different.** A refusal means stop waiting: the far side
 * restarted, or the receipt expired, and it will never know this one. A connection failure means we
 * could not ask, and asking again is right. Folding them together polls a dead receipt for the life
 * of the window — the JetBrains client fixed exactly that.
 */
export function handState(resp: Response | null): { line: string; over: boolean } {
  if (!resp) return { line: 'could not reach that companion', over: false };
  if (!resp.ok) return { line: resp.error ?? 'that companion has no record of this request', over: true };
  const h = (resp.handover ?? null) as { done?: boolean; state?: string; answer?: string; error?: string } | null;
  if (!h) return { line: 'handed over — nothing back yet', over: false };
  if (h.error) return { line: `failed: ${h.error}`, over: true };
  if (h.done) return { line: `done: ${(h.answer ?? '').split('\n')[0].slice(0, 120) || '(no answer)'}`, over: true };
  return { line: h.state ? `working: ${h.state}` : 'working', over: false };
}
