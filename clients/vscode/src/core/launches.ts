/**
 * When an automatic start is allowed — one workspace's budget.
 *
 * ⚠ **The rule lives in `clients/contract/lifecycle-policy.json`, not here.** Both editors have to
 * obey one policy and they had already drifted: this client counted spawns in a rolling 60s window,
 * the JetBrains one allowed three ever with a 60s gap and let nothing but a connection restore them
 * (measured 2026-09-11). Both are defensible and they are not the same rule, which is the problem.
 * The contract file is the cases; this is one implementation of them, and the sibling reads the same
 * file. docs/CLIENT_LIFECYCLE §5 states the targets in prose.
 *
 * **It never reads a clock.** Every decision takes `now` from the caller, so all of it is measurable
 * on a virtual clock — one of these budgets is a minute long, and a test that actually waits cannot
 * be written, while a test that does not measure is the same as no test.
 *
 * **Two counts, deliberately apart.** Spawns in the rolling window say "this is starting too often"
 * and time restores them. Consecutive failures say "starting it does not help" and time does NOT
 * erase them — otherwise a crashloop is forgiven once a minute, forever.
 */
export type Verdict = 'allow' | 'budget' | 'grace' | 'blocked';

export interface Policy {
  windowMs: number;
  spawnsPerWindow: number;
  failuresToBlock: number;
  graceMs: number;
  stableMs: number;
}

export const DEFAULT_POLICY: Policy = {
  windowMs: 60_000, spawnsPerWindow: 3, failuresToBlock: 3, graceMs: 5_000, stableMs: 60_000,
};

export class Launches {
  private spawns: number[] = [];
  private failures = 0;
  private lostAt: number | null = null;
  private stoppedByUser = false;
  /**
   * When the connection came up. ⚠ **This is what decides whether a loss is a failure.**
   *
   * The contract says so: an unexpected exit before the 60s stable window counts as a consecutive
   * failure (docs/CLIENT_LIFECYCLE §5). Without it, a daemon that comes up and dies two seconds
   * later retries forever — it never misses the ready deadline, so `failed` never counts it. The
   * contract case "a momentary connection does not forgive a failure" caught exactly that.
   */
  private readyAt: number | null = null;

  constructor(private readonly p: Policy = DEFAULT_POLICY) {}

  /**
   * `manual` is a person asking. **It is never refused for budget** — the budget exists to stop a
   * loop nobody asked for, and a person asking is somebody asking. It clears a block too.
   */
  may(now: number, manual = false): Verdict {
    if (manual) { this.clear(); return 'allow'; }
    if (this.stoppedByUser || this.failures >= this.p.failuresToBlock) return 'blocked';
    if (this.lostAt !== null && now - this.lostAt < this.p.graceMs) return 'grace';
    this.trim(now);
    if (this.spawns.length >= this.p.spawnsPerWindow) return 'budget';
    return 'allow';
  }

  /** A process was actually started. The window counts SPAWNS, not attempts. */
  spawned(now: number): void { this.trim(now); this.spawns.push(now); this.lostAt = null; }

  /** Connected. **Forgives nothing yet** — that is `stable`'s decision. */
  ready(now: number): void { this.lostAt = null; this.readyAt = now; }

  /** Connected for the whole stable window. The only thing that clears the counts. */
  stable(_now: number): void { this.clear(); }

  /** The connection went away. The grace starts here, so a dying daemon and a new one do not fight
   * over the socket. */
  lost(now: number): void {
    if (this.readyAt !== null && now - this.readyAt < this.p.stableMs) this.failures++;
    this.readyAt = null;
    this.lostAt = now;
  }

  /** Missed the ready deadline, or died unexpectedly before the stable window. */
  failed(now: number): void { this.failures++; this.readyAt = null; this.lostAt = now; }

  /** An update replacement the person asked for. **Not a failure** — the process changed, nothing
   * broke. The clock restarts so the next loss does not read this as "came up and died". */
  replaced(now: number): void { this.lostAt = null; this.readyAt = now; }

  /** That replacement failed. This one counts: what the person asked for was the replacement. */
  replaceFailed(now: number): void { this.failed(now); }

  /** The person stopped this companion. Reviving it is arguing with them. */
  userStopped(_now: number): void { this.stoppedByUser = true; }

  private clear(): void {
    this.spawns = []; this.failures = 0; this.lostAt = null; this.readyAt = null;
    this.stoppedByUser = false;
  }

  private trim(now: number): void {
    this.spawns = this.spawns.filter((t) => now - t < this.p.windowMs);
  }
}

/**
 * How long to wait before reconnecting — 1, 2, 4, 8, 16, 30 seconds, then 30.
 *
 * ⚠ **Jitter first, cap second.** The other order puts ±20% on the 30s step and reaches 36s, which
 * makes the contract's "at most 30s" false.
 *
 * `rand` is anything returning 0..1; the tests feed it 0 and 1 directly to measure the edges — a
 * test that rolls a hundred random numbers and says "about right" cannot see a boundary.
 */
export const BACKOFF_STEPS_MS = [1_000, 2_000, 4_000, 8_000, 16_000, 30_000];
export const BACKOFF_CAP_MS = 30_000;
export const BACKOFF_JITTER = 0.2;

export function backoffMs(attempt: number, rand: number): number {
  const base = BACKOFF_STEPS_MS[Math.min(Math.max(attempt, 1), BACKOFF_STEPS_MS.length) - 1];
  const spread = base * BACKOFF_JITTER;
  const jittered = base - spread + 2 * spread * Math.min(Math.max(rand, 0), 1);
  return Math.min(Math.trunc(jittered), BACKOFF_CAP_MS);
}
