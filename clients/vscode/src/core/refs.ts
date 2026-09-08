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
