/**
 * A reference to a piece of the workspace, as the composer carries it.
 *
 * `path:start-end`, one-based and inclusive, which is what the console's chips say and what a
 * person reads in an error message. A single line is `path:12`, not `path:12-12`.
 */
export interface Ref {
  path: string;
  from?: number;
  to?: number;
}

export function refText(r: Ref): string {
  if (!r.from) return r.path;
  if (!r.to || r.to === r.from) return `${r.path}:${r.from}`;
  return `${r.path}:${r.from}-${r.to}`;
}

/** The lead this puts in the composer, so the person types their question after it. */
export function askLead(refs: Ref[]): string {
  return refs.length ? `About ${refs.map(refText).join(', ')}: ` : '';
}

/**
 * One attachment as the WIRE spells it (`internal/core/command/command.go`'s `FileRef`):
 * `{path, lines}`, where lines is "12-40" or "12" and empty means the whole file.
 *
 * Structured, not spliced. This client used to put `path:12-40` at the head of the person's own
 * text, which is the convention the core retired — `internal/app/refs.go` says so in its opening
 * paragraph. What the old way cost, measured against what the core does with `refs`: the excerpt
 * is never rendered (the agent gets a path and has to go read it, or does not), it is never
 * resolved inside the workspace jail, never capped (16KB a ref, 64KB the lot), never persisted
 * with the prompt — so the transcript cannot show what the agent was shown, and a replay shows
 * nothing. And an attachment that cannot be served said nothing at all, where the core renders
 * its refusal in place.
 *
 * The JetBrains client has sent this shape all along (`Wire.kt`'s `FileRef`).
 */
export interface WireRef {
  path: string;
  lines?: string;
}

/** A composer chip as the wire takes it. `lines` is omitted for a whole-file attachment. */
export function wireRef(r: Ref): WireRef {
  if (!r.from) return { path: r.path };
  return { path: r.path, lines: !r.to || r.to === r.from ? `${r.from}` : `${r.from}-${r.to}` };
}

/**
 * Make the glob metacharacters in a typed query mean themselves.
 *
 * Somebody typing `@page[1` is naming a file, not writing a character class. Measured against a
 * running daemon on 2026-09-10: `**` + `/*page[1*` comes back `ok:false, "invalid glob pattern:
 * syntax error in pattern"`, and the mention popup — which reads `out` and never looks at `ok` —
 * turns that into an empty list. A CLOSED bracket is worse than the error: `pa[nl]el` is a valid
 * character class, so it quietly searches for something the person did not type and shows the
 * results as if they were the answer.
 *
 * The web console has done this since `globQuote` (`clients/web/server/files.go`) and the JetBrains
 * plugin since its mention popup; the same five characters, so that the same `@` finds the same
 * files on all three. The wildcards NOT escaped here are the ones the caller wraps the query in.
 */
export function globQuote(q: string): string {
  let out = '';
  for (const ch of q) {
    if ('*?[]\\'.includes(ch)) out += '\\';
    out += ch;
  }
  return out;
}
