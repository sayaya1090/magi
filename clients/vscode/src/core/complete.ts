/**
 * What the companion is asked for at the cursor, and what comes back.
 *
 * Prefix AND suffix, both. Sending only what is behind the cursor makes the model close a bracket
 * the file already closes — the JetBrains client sends both for exactly that reason and says so.
 */
export interface CompleteArgs {
  prefix: string;
  suffix: string;
}

/** Bounded on each side. The window either side of a cursor is what helps; the file is not. */
export function around(text: string, offset: number, budget = 4000): CompleteArgs {
  const at = Math.max(0, Math.min(offset, text.length));
  return {
    prefix: text.slice(Math.max(0, at - budget), at),
    suffix: text.slice(at, at + budget),
  };
}

/**
 * What of a completion is worth showing.
 *
 * A model that answers with the line it was given is answering nothing, and a ghost that repeats
 * what is already there reads as the editor being stuck. Empty means "draw nothing".
 */
export function usable(out: string, prefix: string): string {
  const t = out.replace(/\r/g, '');
  if (!t.trim()) return '';
  // It sometimes re-emits the tail of the prefix. Drop that overlap rather than drawing it twice.
  const tail = prefix.slice(-200);
  for (let n = Math.min(tail.length, t.length); n > 0; n--) {
    if (tail.endsWith(t.slice(0, n))) return t.slice(n);
  }
  return t;
}
