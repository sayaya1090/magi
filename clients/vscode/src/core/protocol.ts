/**
 * The daemon's wire, as the core spells it (`internal/adapter/daemon/protocol.go`).
 *
 * Field names are copied, not invented. TypeScript drops unknown keys silently and hands back
 * `undefined` for missing ones — the same trap Kotlin's `ignoreUnknownKeys` sets — so a name that
 * disagrees is not an exception but a default, and the screen says "nothing" while nothing fails.
 * `wire.test.ts` reads the Go source and checks these names against it.
 */

/** The wire-protocol version this client speaks. The daemon carries its own in `about`. */
export const PROTO_VERSION = 1;

export interface Request {
  method: string;
  session?: string;
  text?: string;
  callId?: string;
  /** The permission verdict as the core spells it. One vocabulary, so the two cannot drift. */
  decision?: 'allow' | 'deny' | 'always';
  answer?: string;
  name?: string;
  keep?: boolean;
  tier?: 'project' | 'global';
  since?: number;
  /** Tool arguments, for the `tool` door — the ONE door that reads them. */
  args?: unknown;
  /**
   * `cron-set`: when the job comes round, in five cron fields.
   *
   * ⚠ **Top level, not inside `args`.** `answerCronEdit` reads `req.Schedule`, and this client sent
   * it as `args.schedule` — so the daemon got an empty schedule and nothing said so: Go's decoder
   * drops a field it does not know, and `args` is a field it knows and this door never reads. The
   * JetBrains client had it right; only this one was wrong.
   */
  schedule?: string;
  /**
   * `rewind`: how many TURNS to drop, not a sequence number.
   *
   * The door reads `n` and hands it to `App.Rewind(sid, n)`. `since` — which this client sent — is a
   * field the wire has (the transcript stream reads it) and this door never looks at, so the value
   * went nowhere and the daemon used its own default.
   */
  n?: number;
  /**
   * `hand`: whether the work handed over is a question rather than a request.
   *
   * The name is the core's (`Looking`, `json:"looking"`). The far side treats the two differently —
   * a question is answered without changing their workspace — so getting this wrong hands somebody
   * write access to their own tree when a person meant to ask them something.
   */
  looking?: boolean;
  /**
   * `submit`/`steer`: the files this prompt attaches, as `{path, lines}` (`WireRef`).
   *
   * Structured, not spliced into `text`. `internal/app/refs.go` renders each excerpt inside the
   * workspace jail, caps it, and persists it with the prompt — none of which happens for a path
   * written into the person's own words, which is what this client used to send.
   */
  refs?: { path: string; lines?: string }[];
}

/** One event out of the log, as `transcript` streams it. */
export interface Event {
  seq: number;
  type: string;
  ts?: string;
  actor?: { kind?: string; id?: string };
  data?: unknown;
}

export interface Waiting {
  /** "permission" | "question" — the core's own two words. */
  kind: string;
  callId?: string;
  what?: string;
  text?: string;
}

export interface Response {
  ok: boolean;
  error?: string;
  out?: string;
  /**
   * The tool names a roster carries — `tools`, and also `mcp-attach`, which answers with what the
   * server it just attached offers (`answerMCPAttach` returns `Response{OK: true, Tools: names}`).
   *
   * Undeclared until now, so the attach answer's most useful half could not be read at all: the
   * screen could say "attached" and never what was attached. The JetBrains client has read this
   * field since it grew the same button.
   */
  /** `status`: what to call the person, when a plugin renamed them (`magi.set_user_label`). */
  user?: string;
  /**
   * `status`: whether this companion ends a working turn by declaring to a council.
   *
   * A runtime fact, settable per companion, and it sits in the same answer as `permission` and
   * `model` — the three things "what is this companion running on" is made of. This client read
   * two of the three. The core put it on the wire because something outside the daemon has to be
   * able to tell the truth about it: the gap it closes was measured when a helper's tool
   * descriptions told a model to finish with `council{complete:true}` on a companion that had the
   * council switched off, and the model called it and got `unknown tool: council`.
   */
  council?: boolean;
  tools?: string[];
  /** `about` only: the daemon's wire version and what it will answer. */
  proto?: number;
  caps?: string[];
  version?: string;
  /** `status`: absent when the engine is not blocked on anybody. */
  waiting?: Waiting;
  /** `status`: the latest progress note from a tool still running. Empty most of the time. */
  doing?: string;
  /**
   * `status`: how this companion is set up right now.
   *
   * The daemon has always sent these (`answerStatus` fills Permission, Backend and — when the
   * request names a session — Model). Nothing here read them, so every screen had to say "ask
   * elsewhere" about the one question a person opens the panel with: what is answering me.
   *
   * ⚠ `model` is only filled when the request carries a session. A poll that asks `status` with no
   * session gets a reply with no model, and reading that absence as "no model" would be wrong: it
   * means nobody said which conversation.
   */
  permission?: string;
  backend?: string;
  model?: string;
  /** `transcript`: one frame per event. */
  event?: Event;
  /** A stream's opening note — e.g. that a tail was asked for and a whole conversation is coming. */
  why?: string;
  session?: string;
  /**
   * `sessions` / `children`: one conversation, as `daemon.SessionRow` spells it.
   *
   * Typed rather than `unknown[]` for the reason `handover` was: an untyped wire lets each reader
   * cast it to a shape of its own, and a name that is not on the wire then reads as `undefined`
   * with nothing failing. That cost two measured defects in one session — a handover that died
   * read as "working" for the life of the window, and a fleet row from another machine drew like
   * one this window could talk to. Typed, an invented name stops compiling.
   */
  sessions?: SessionRow[];
  /** `profiles`: the backends this daemon's config names. */
  profiles?: { name?: string; tier?: string }[];
  /** `config-get`: the settings it will let a client change — a whitelist held by the engine. */
  config?: {
    key?: string; value?: string; source?: string; tier?: string; file?: string;
    applies?: string; doc?: string; profile?: boolean;
    /**
     * A config layer that would not PARSE, and the reason — a string, not a flag.
     *
     * ⚠ Declared `boolean` here until 2026-09-10, which is a shape the daemon never sends and which
     * could not have carried the reason even if something had drawn it. The core says what the
     * silence costs: "A file with a typo in it and a file that says nothing are the same absence to
     * a reader who is only shown values… A read that cannot say 'your global file is broken' is the
     * third silence."
     */
    unreadable?: string;
  }[];
  /** `roster`: the companions this machine can name. */
  roster?: RosterRow[];
  /** `hand-state`: how the work handed to another companion is going. */
  /**
   * `hand-state`: what became of one piece of work handed to a companion.
   *
   * Two endings, and they are not the same one — the core says so and says what collapsing them
   * costs: `done` means a turn finished and `answer` is what was said; `over` means nothing is
   * coming and `news` says why. "A caller that collapsed them would report a crash as an empty
   * answer."
   *
   * It was `unknown` here, so the reader cast it to a shape of its own — and got two of the four
   * names wrong. Typed now: an invented name stops compiling instead of reading `undefined`.
   */
  handover?: { done?: boolean; answer?: string; news?: string; over?: boolean };
  /**
   * The doors that answer a STRUCT rather than prose.
   *
   * ⚠ None of these fills `out`, measured against a live daemon. The panel read `out` for all of
   * them and drew nothing, on every build, without failing — an absent field is an empty string and
   * an empty string parses to an empty list.
   */
  jobs?: {
    background?: { id?: string; command?: string; running?: boolean; killed?: boolean;
      exit?: number; started?: string; tail?: string }[];
    children?: { id?: string; tool?: string; task?: string; started?: string; ended?: string;
      running?: boolean; steps?: number; err?: string }[];
    queued?: { kind?: string; text?: string; from?: string }[];
  };
  cron?: {
    name?: string; schedule?: string; enabled?: boolean; next?: string; problem?: string;
    command?: string; timeout?: string; prompt?: string;
  }[];
  context?: {
    model?: string; window?: number; used?: number; estimated?: boolean; messages?: number;
    cached?: number; cacheReported?: boolean; compactions?: number; shed?: number;
    lastAt?: string; lastBefore?: number; lastAfter?: number; topics?: unknown;
    parts?: Record<string, number>;
  };
  /**
   * `children`: the conversations this one spawned.
   *
   * ⚠ Its OWN field, not `sessions`. Reading `sessions` here returns nothing for ever — measured
   * 2026-09-09 by listing what each door fills, and this client did exactly that.
   */
  children?: SessionRow[];
  /**
   * `complete` and `suggest`: why the answer was empty, when it was.
   *
   * The door answers ok with nothing rather than failing — a completer with nothing to say is the
   * ordinary case. So the only way to tell "switched off" from "the model had nothing" is this
   * field, and dropping it makes "why is completion silent" unanswerable.
   */
  reason?: string;
  /** `job-kill` / `mcp-detach`: whether anything was actually removed. */
  removed?: boolean;
  models?: string[];
  done?: boolean;
}

/** A door this build advertises in `about`. Read the advertisement; never call an absent door. */
export type Cap =
  | 'handshake' | 'roster' | 'transcript' | 'sessions' | 'session-new' | 'children'
  | 'cron' | 'cron-set' | 'cron-remove' | 'job-kill' | 'tool-servers' | 'settings' | 'context';

/** One conversation on the roster or the session list (`daemon.SessionRow`). */
export interface SessionRow {
  id?: string;
  title?: string;
  agent?: string;
  origin?: string;
  model?: string;
  labels?: string[];
  for?: string;
  created?: string;
  lastActivity?: string;
}

/**
 * One companion on this machine, or one another machine told us about (`daemon.RosterRow`).
 *
 * ⚠ `live` and `sighting` are the pair that says whether this window can DIAL the row, and both
 * are `omitempty`: a false `live` is never sent, so `live === false` cannot happen and a branch
 * written on it never runs. Presence is the whole signal.
 */
export interface RosterRow {
  host?: string;
  socket?: string;
  name?: string;
  role?: string;
  team?: string;
  hub?: boolean;
  workdir?: string;
  account?: string;
  state?: string;
  version?: string;
  pid?: number;
  addr?: string;
  started?: string;
  by?: string;
  /**
   * ⚠ **These five drifted out of step with the struct.** Measured against `RosterRow`
   * (`internal/adapter/daemon/roster.go`) 2026-09-10: `hub` was `string` for a bool, `can` was
   * `string[]` for an int, `does` was `string` for a `[]string`, `waiting` was `string` for an int,
   * `handling` was `number` for a bool.
   *
   * A wrong type here is not cosmetic and TypeScript cannot catch it — JSON crosses the boundary as
   * `unknown` and this declaration is a promise nobody checks. What it DOES do is block the correct
   * read: `r.waiting > 0` was rejected with "Operator '>' cannot be applied to types 'string' and
   * 'number'", so the one screen that wants the queue depth could not compile it.
   *
   * `can` is a COUNT of what a companion can take, not a list; `does` is the list.
   */
  can?: number;
  does?: string[];
  waiting?: number;
  /**
   * In the middle of a piece of handed-over work when last seen.
   *
   * Not a spare fact: the core signs it beside `waiting` because together they decide routing —
   * "fleet.Resolve routes a team address to the lightest companion, and **load is Waiting + (1 if
   * Handling)**". A row that shows only the queue calls a companion free when it is carrying one.
   */
  handling?: boolean;
  session?: string;
  permission?: string;
  backend?: string;
  model?: string;
  user?: string;
  live?: boolean;
  sighting?: boolean;
  ageSeconds?: number;
}
