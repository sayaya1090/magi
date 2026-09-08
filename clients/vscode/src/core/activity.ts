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
