import { Response } from './protocol';

/**
 * What the companion is doing, in one word, decided in ONE place.
 *
 * The screen has two readers — the status bar and, later, the plan panel — and this repository has
 * already paid for letting each decide: the status bar drew "idle" for what the daemon had not
 * said, while the other panel drew "nothing running" for the same silence. One rule, two copies,
 * two different sentences about one fact.
 *
 * `Unknown` is a real answer and not a shrug. "We could not ask" is a different fact from "it said
 * it is idle", and a screen that folds them into one blank claims to know something it does not.
 */
export enum State {
  /** Nobody is listening on this workspace's socket. */
  NotRunning = 'not-running',
  /** It answered and it is not busy. */
  Idle = 'idle',
  /** A turn is running. */
  Working = 'working',
  /** It is blocked on a person: a permission or a question. */
  Waiting = 'waiting',
  /** We could not ask. NOT the same as idle. */
  Unknown = 'unknown',
}

export interface Activity {
  state: State;
  /** What it is blocked on, when it is blocked. */
  asking?: string;
  /** The latest progress note, when a tool has said something. */
  doing?: string;
}

/** No socket, no daemon, nothing to say. */
export function notRunning(): Activity { return { state: State.NotRunning }; }

/** We reached for it and could not get an answer — say that, do not call it idle. */
export function cannotSay(): Activity { return { state: State.Unknown }; }

/**
 * Read one `status` reply.
 *
 * Waiting beats working: a turn that is blocked on somebody IS running, but what the person needs
 * to know is that it wants them. A screen that reported "working" there would leave a question
 * standing with nothing pointing at it.
 */
export function of(resp: Response | null): Activity {
  if (!resp) return cannotSay();
  if (!resp.ok) return cannotSay();
  if (resp.waiting) {
    return { state: State.Waiting, asking: resp.waiting.what || resp.waiting.text || resp.waiting.kind };
  }
  const doing = (resp.doing ?? '').trim();
  if (doing) return { state: State.Working, doing };
  return { state: State.Idle };
}

/** The one-line label every screen shows for a state. One vocabulary, so two screens cannot drift. */
export function label(a: Activity): string {
  switch (a.state) {
    case State.NotRunning: return 'not running';
    case State.Idle: return 'idle';
    case State.Working: return a.doing ? `working · ${a.doing}` : 'working';
    case State.Waiting: return a.asking ? `waiting on you · ${a.asking}` : 'waiting on you';
    case State.Unknown: return 'cannot say';
  }
}

/**
 * How the companion is set up, as one `status` reply tells it.
 *
 * Kept apart from Activity on purpose. Activity is what it is DOING and changes every second; this
 * is what it is RUNNING ON and changes when somebody changes it. Folding them into one type made
 * every screen redraw its whole footer on each poll, and made "the model is unknown" and "it is
 * idle" arrive as one fact when they are two.
 *
 * Every field is optional because every one can be genuinely unsaid: an older daemon does not send
 * them, and `model` is absent whenever the request named no session.
 */
export interface Setup {
  model?: string;
  backend?: string;
  permission?: string;
}

export function setupOf(resp: Response | null): Setup {
  const out: Setup = {};
  if (!resp?.ok) return out;
  // Keys are left OUT rather than set to undefined. A caller that asks "did it say?" with `in` or
  // with Object.keys gets the true answer, and a screen that spreads this over a previous reading
  // does not overwrite what was known with nothing.
  const put = (k: keyof Setup, v: string | undefined): void => {
    const t = (v ?? '').trim();
    if (t) out[k] = t;
  };
  put('model', resp.model);
  put('backend', resp.backend);
  put('permission', resp.permission);
  return out;
}

/** Whether two readings say the same thing, so a screen redraws only when something moved. */
export function sameSetup(a: Setup, b: Setup): boolean {
  return a.model === b.model && a.backend === b.backend && a.permission === b.permission;
}

/**
 * What the conversation panel says when there is nothing to talk to, and whether it offers a way out.
 *
 * ⚠ **The offer is not tied to the verdict.** `not-running` had a "Start one" button and `unknown`
 * had a sentence and nothing to press — so a person whose companion could not be reached for any
 * reason we cannot name had no way forward from that panel. The JetBrains client had the same hole
 * and it is what a live report was about: a socket file left by a dead daemon lands in "cannot say"
 * on Windows, and every route to reviving it was closed.
 *
 * Starting one when a companion is actually alive is safe: the daemon claims the socket path with a
 * lock and probes it first, so the second one refuses itself. Losing that race is ordinary, not an
 * error — which is exactly why the offer does not need to be sure.
 *
 * Nothing is offered for idle/working/waiting: the status bar already says those, and a button to
 * start a companion that is answering would be a button that does nothing.
 */
export function panelNote(a: Activity | null): { text: string; offerStart: boolean } {
  if (!a) return { text: '', offerStart: false };
  switch (a.state) {
    case State.NotRunning:
      return { text: 'No companion is running for this workspace.', offerStart: true };
    case State.Unknown:
      return {
        text: ['Could not reach the companion.', a.asking].filter(Boolean).join(' '),
        // Said plainly and still offered: "we could not ask" is not "it is fine", and the way out
        // must not depend on our being sure what is wrong.
        offerStart: true,
      };
    default:
      return { text: '', offerStart: false };
  }
}
