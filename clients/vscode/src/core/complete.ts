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

/**
 * Why the last completion came back empty, if it did.
 *
 * The door answers ok with nothing when it has nothing to say — that is the ordinary case, so it
 * cannot be an error — and it puts the reason in `reason`. Saying it on every keystroke would be
 * noise; saying it nowhere makes "why is completion silent" unanswerable, which is the state this
 * client was in. So it is REMEMBERED, and the one screen a person opens when something is not
 * working reads it.
 *
 * ⚠ **A completion that produced text clears it.** A reason left standing while things work is a
 * sentence that has aged — the failure mode this tree keeps paying for.
 */
let lastEmpty = '';

export function noteCompletion(text: string, reason: string | undefined): void {
  lastEmpty = text.trim() ? '' : (reason ?? '').trim();
}

export function whyNoCompletion(): string { return lastEmpty; }
