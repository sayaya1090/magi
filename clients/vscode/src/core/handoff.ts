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
  /**
   * A row this machine did not measure — it arrived as a record another machine signed.
   *
   * The core's words: "Visible, not commandable — its socket is a path on a machine this caller has
   * no door to." So it HAS a socket string, and a filter that asks only whether a socket is there
   * lets it through.
   */
  sighting?: boolean;
  /**
   * A dial just proved somebody is listening. **Local rows only** — a sighting's liveness is the
   * one thing nobody here can check, which is what `sighting` is for.
   *
   * ⚠ Two-valued on the wire, not three. `omitempty` means a FALSE `live` is not sent at all, so
   * `live === false` never happens and a branch written on it never runs. Presence is the whole
   * signal: it is there, or nothing was said.
   */
  live?: boolean;
}

/** Read the `roster` reply. Rows without a socket are dropped: there is nothing to dial. */
export function peers(resp: Response | null): Peer[] {
  if (!resp?.ok) return [];
  const rows = (resp.roster ?? []) as Peer[];
  // ⚠ **A socket string is not a door.** A sighting is a row another machine signed, and its socket
  // is a path over there — dialling it here reaches nothing. This filter asked only whether the
  // string was present, so the hand-off picker offered companions on other machines and handing
  // work to one went nowhere, with the receipt polled until the window closed.
  //
  // The JetBrains client asks for both halves in one breath (`it.live && !it.sighting`), and the
  // fleet section of this very file already writes down why: drawing an unreachable row like a
  // reachable one "is how somebody sends work to nobody".
  return rows.filter((r) => typeof r?.socket === 'string' && r.socket && !r.sighting);
}

/**
 * What a person picked this companion out of the list by.
 *
 * ⚠ **The same rule everywhere a companion is named.** A person reads the fleet section and then
 * picks from the hand-off list; if the two spell one companion differently there is no way to match
 * them up. So the panel imports this rather than keeping its own.
 *
 * The WORKDIR's last segment before the socket's filename, because that is the name a person knows
 * it by — `word`, `excel`, `ws-agy` — while the socket is `daemon-word-37iu1p70.sock`, which has the
 * name in it plus a hash nobody reads. Measured on a live roster of eight: not one row carried a
 * `name`, so the fallback IS the label in practice.
 */
export function peerLabel(p: Peer): string {
  const named = p.name?.trim();
  if (named) return named;
  const dir = (p.workdir ?? '').replace(/[/\\]+$/, '').split(/[/\\]/).pop();
  if (dir) return dir;
  return p.socket.split('/').pop() || p.socket;
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
  const h = resp.handover ?? null;
  if (!h) return { line: 'handed over — nothing back yet', over: false };
  // ⚠ **Two endings, not one.** `over` means nothing is coming — the far side crashed, restarted,
  // or refused — and `news` is why. Reading only `done`, this window called that "working" and kept
  // polling a receipt nobody would ever answer. The fields it DID read (`state`, `error`) have
  // never been on this wire, so both of those branches were dead the whole time: an invented name
  // reads as `undefined`, `undefined` is falsy, and nothing anywhere said so. The cast that let
  // that happen is gone — the shape is typed at the wire now.
  //
  // `over` is asked first, because a handover can end WITHOUT finishing, and reporting the finish
  // it did not have is the shape the core warns about: a crash reported as an empty answer.
  if (h.over) return { line: h.news ? `stopped: ${h.news}` : 'stopped, and no reason came with it', over: true };
  if (h.done) return { line: `done: ${(h.answer ?? '').split('\n')[0].slice(0, 120) || '(no answer)'}`, over: true };
  return { line: 'working', over: false };
}
