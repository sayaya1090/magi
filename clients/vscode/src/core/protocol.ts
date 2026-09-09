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
  sessions?: unknown[];
  /** `profiles`: the backends this daemon's config names. */
  profiles?: unknown[];
  /** `config-get`: the settings it will let a client change — a whitelist held by the engine. */
  config?: unknown[];
  /** `roster`: the companions this machine can name. */
  roster?: unknown[];
  /** `hand-state`: how the work handed to another companion is going. */
  handover?: unknown;
  /**
   * The doors that answer a STRUCT rather than prose.
   *
   * ⚠ None of these fills `out`, measured against a live daemon. The panel read `out` for all of
   * them and drew nothing, on every build, without failing — an absent field is an empty string and
   * an empty string parses to an empty list.
   */
  jobs?: unknown;
  cron?: unknown[];
  context?: unknown;
  /**
   * `children`: the conversations this one spawned.
   *
   * ⚠ Its OWN field, not `sessions`. Reading `sessions` here returns nothing for ever — measured
   * 2026-09-09 by listing what each door fills, and this client did exactly that.
   */
  children?: unknown[];
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
